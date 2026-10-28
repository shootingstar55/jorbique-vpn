//go:build windows

package clientupdate

import (
	"golang.org/x/sys/windows"
	"os"
	"strings"
	"testing"
)

func TestWindowsStagingUsesProtectedAdminACL(t *testing.T) {
	dir, err := newStageDir()
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		t.Fatal(err)
	}
	if owner.String() != "S-1-5-32-544" {
		t.Fatalf("staging owner is not Administrators: %s", owner)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatal("untrusted parent permissions were inherited")
	}
	if got := sd.String(); !strings.Contains(got, ";;;SY)") || !strings.Contains(got, ";;;BA)") || !strings.Contains(got, ";RC;;;OW)") || strings.Count(got, "(A;") != 3 {
		t.Fatalf("unexpected staging ACL: %s", got)
	}
}

func TestRejectForgedUpdateParent(t *testing.T) {
	if err := ApplyWindows(uint32(os.Getpid())); err == nil {
		t.Fatal("accepted a caller-controlled parent PID")
	}
}
