//go:build windows

package clientupdate

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/windows"
	"jorbiquevpn/internal/constants"
	"jorbiquevpn/internal/semver"
	"jorbiquevpn/internal/sysexec"
	"jorbiquevpn/internal/utils"
)

func installPackage(path string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	helper := filepath.Join(filepath.Dir(path), "updater.exe")
	if err := copyFile(self, helper, 0700); err != nil {
		return err
	}
	cmd := sysexec.Command(helper, "--apply-client-update", strconv.Itoa(os.Getpid()))
	utils.HideCommandWindow(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// ApplyWindows runs only in a copy of the old client. The destination comes
// from the live parent process, never from a path supplied on the command line.
func ApplyWindows(parentPID uint32) error {
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
	p, err := latest(context.Background())
	if err != nil {
		return err
	}
	if !versionPattern.MatchString(constants.AppVersion) || semver.Compare(p.version, constants.AppVersion) <= 0 {
		return errors.New("обновление на старую версию запрещено")
	}
	archive := filepath.Join(filepath.Dir(self), p.asset.Name)
	hash, err := fileHash(archive)
	if err != nil {
		return err
	}
	info, err := os.Stat(archive)
	if err != nil {
		return err
	}
	if info.Size() != p.asset.Size || !strings.EqualFold("sha256:"+hex.EncodeToString(hash), p.asset.Digest) {
		return errors.New("повторная проверка обновления не пройдена")
	}
	source, err := extractWindows(archive)
	if err != nil {
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
