//go:build windows

package clientupdate

import (
	"crypto/rand"
	"encoding/hex"
	"golang.org/x/sys/windows"
	"path/filepath"
	"unsafe"
)

func newStageDir() (string, error) {
	root, err := windows.GetWindowsDirectory()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	path := filepath.Join(root, "Temp", "jorbique-client-update-"+hex.EncodeToString(nonce))
	// A normal user token must not replace a verified package/helper before an
	// elevated launch. Apply the protected, inherited ACL atomically at creation.
	sd, err := windows.SecurityDescriptorFromString("O:BAG:BAD:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;RC;;;OW)")
	if err != nil {
		return "", err
	}
	sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	if err := windows.CreateDirectory(name, &sa); err != nil {
		return "", err
	}
	return path, nil
}
