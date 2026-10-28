package clientupdate

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// Install stages the complete, verified package before stopping a running VPN.
func Install(ctx context.Context, beforeInstall func() error) error {
	path, err := DownloadLatest(ctx)
	if err != nil {
		return err
	}
	if beforeInstall != nil {
		if err := beforeInstall(); err != nil {
			_ = os.RemoveAll(filepath.Dir(path))
			return err
		}
	}
	if err := installPackage(path); err != nil {
		_ = os.RemoveAll(filepath.Dir(path))
		return err
	}
	return nil
}

func copyFile(source, target string, mode os.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, io.LimitReader(in, maxPackageBytes+1))
	if copyErr == nil {
		copyErr = out.Sync()
	}
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func fileHash(path string) ([]byte, error) {
	in, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer in.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(in, maxPackageBytes+1))
	if n > maxPackageBytes {
		return nil, errors.New("слишком большой файл обновления")
	}
	return h.Sum(nil), err
}

func extractWindows(path string) (string, error) {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer archive.Close()
	if len(archive.File) != 1 || archive.File[0].Name != "Jorbique-VPN-Extended.exe" || !archive.File[0].Mode().IsRegular() || archive.File[0].UncompressedSize64 > maxPackageBytes {
		return "", errors.New("неожиданное содержимое архива обновления")
	}
	entry, err := archive.File[0].Open()
	if err != nil {
		return "", err
	}
	defer entry.Close()
	dest := filepath.Join(filepath.Dir(path), "new-client.exe")
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		return "", err
	}
	n, copyErr := io.Copy(out, io.LimitReader(entry, maxPackageBytes+1))
	closeErr := out.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if n > maxPackageBytes || n != int64(archive.File[0].UncompressedSize64) {
		return "", errors.New("неверный размер приложения в архиве")
	}
	return dest, nil
}

// replaceClient only replaces an inactive, unchanged copy of this application.
// A launch failure restores the original binary; settings are never touched.
func replaceClient(target, source string, originalHash []byte, launch func(string) error) error {
	info, err := os.Lstat(target)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("обновляемый клиент не является обычным файлом")
	}
	current, err := fileHash(target)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, originalHash) {
		return errors.New("файл клиента изменён во время обновления")
	}
	backup, err := os.CreateTemp(filepath.Dir(target), ".jorbique-previous-*.exe")
	if err != nil {
		return err
	}
	backupPath := backup.Name()
	if err := backup.Close(); err != nil {
		return err
	}
	_ = os.Remove(backupPath)
	if err := copyFile(target, backupPath, info.Mode().Perm()); err != nil {
		return err
	}
	removeBackup := true
	defer func() {
		if removeBackup {
			_ = os.Remove(backupPath)
		}
	}()
	next, err := os.CreateTemp(filepath.Dir(target), ".jorbique-next-*.exe")
	if err != nil {
		return err
	}
	nextPath := next.Name()
	if err := next.Close(); err != nil {
		return err
	}
	_ = os.Remove(nextPath)
	defer os.Remove(nextPath)
	if err := copyFile(source, nextPath, info.Mode().Perm()); err != nil {
		return err
	}
	if err := os.Rename(nextPath, target); err != nil {
		return err
	}
	if err := launch(target); err != nil {
		if restoreErr := os.Rename(backupPath, target); restoreErr != nil {
			removeBackup = false
			return errors.Join(err, restoreErr)
		}
		return err
	}
	return nil
}
