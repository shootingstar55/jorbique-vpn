//go:build windows

package clientupdate

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"jorbiquevpn/internal/constants"
	"jorbiquevpn/internal/semver"
	"jorbiquevpn/internal/sysexec"
	"jorbiquevpn/internal/utils"
)

func installPackage(path string, pkg packageInfo) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	helper := filepath.Join(filepath.Dir(path), "updater.exe")
	if err := copyFile(self, helper, 0700); err != nil {
		return err
	}
	cmd := sysexec.Command(helper, "--apply-client-update", strconv.Itoa(os.Getpid()))
	metadata, err := json.Marshal(pkg)
	if err != nil {
		return err
	}
	cmd.Stdin = bytes.NewReader(metadata)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	defer stdout.Close()
	utils.HideCommandWindow(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	ready := make(chan error, 1)
	go func() {
		line, err := bufio.NewReader(io.LimitReader(stdout, 64)).ReadString('\n')
		if err == nil && line != "ready\n" {
			err = errors.New("помощник обновления не подтвердил готовность")
		}
		ready <- err
	}()
	select {
	case err := <-ready:
		if err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return err
		}
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return errors.New("помощник обновления не ответил")
	}
	return cmd.Process.Release()
}

// ApplyWindows runs only in a copy of the old client. The destination comes
// from the live parent process, never from a path supplied on the command line.
func ApplyWindows(parentPID uint32) error {
	if os.Getppid() != int(parentPID) {
		return errors.New("неверный родитель обновления")
	}
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, parentPID)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(process)
	buf := make([]uint16, 32768)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(process, 0, &buf[0], &size); err != nil {
		return err
	}
	target := windows.UTF16ToString(buf[:size])
	self, err := os.Executable()
	if err != nil {
		return err
	}
	ownHash, err := fileHash(self)
	if err != nil {
		return err
	}
	parentHash, err := fileHash(target)
	if err != nil {
		return err
	}
	if !bytes.Equal(ownHash, parentHash) || target == self {
		return errors.New("обновлять можно только исходный клиент")
	}
	var p packageInfo
	// The actual, byte-identical parent sends its already authenticated metadata
	// over a private inherited pipe. No network is needed after disconnecting VPN.
	if err := json.NewDecoder(io.LimitReader(os.Stdin, 64<<10)).Decode(&p); err != nil {
		return err
	}
	if p.Asset.Name != assetName(p.Version, "windows") || p.Asset.Size <= 0 || p.Asset.Size > maxPackageBytes || !versionPattern.MatchString(p.Version) {
		return errors.New("некорректные данные обновления")
	}
	if !versionPattern.MatchString(constants.AppVersion) || semver.Compare(p.Version, constants.AppVersion) <= 0 {
		return errors.New("обновление на старую версию запрещено")
	}
	archive := filepath.Join(filepath.Dir(self), p.Asset.Name)
	hash, err := fileHash(archive)
	if err != nil {
		return err
	}
	info, err := os.Stat(archive)
	if err != nil {
		return err
	}
	if info.Size() != p.Asset.Size || !strings.EqualFold("sha256:"+hex.EncodeToString(hash), p.Asset.Digest) {
		return errors.New("повторная проверка обновления не пройдена")
	}
	source, err := extractWindows(archive)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(os.Stdout, "ready"); err != nil {
		return err
	}
	result, err := windows.WaitForSingleObject(process, 120000)
	if err != nil {
		return err
	}
	if result != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("клиент не завершился для обновления")
	}
	err = replaceClient(target, source, ownHash, func(path string) error {
		cmd := sysexec.Command(path)
		if err := cmd.Start(); err != nil {
			return err
		}
		return cmd.Process.Release()
	})
	// Windows holds the helper until exit. Delete it and its directory on reboot.
	for _, path := range []string{archive, source} {
		_ = os.Remove(path)
	}
	if p, e := windows.UTF16PtrFromString(self); e == nil {
		_ = windows.MoveFileEx(p, nil, windows.MOVEFILE_DELAY_UNTIL_REBOOT)
	}
	if p, e := windows.UTF16PtrFromString(filepath.Dir(self)); e == nil {
		_ = windows.MoveFileEx(p, nil, windows.MOVEFILE_DELAY_UNTIL_REBOOT)
	}
	return err
}
