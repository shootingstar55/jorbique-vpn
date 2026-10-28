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
	if got := sd.String(); got != "O:BAD:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)" && got != "O:BAD:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)" {
		// The owner/control assertions above are stable; ACE identities must remain
		// exactly SYSTEM and Administrators across Windows' canonical formatting.
		if !strings.Contains(got, ";;;SY)") || !strings.Contains(got, ";;;BA)") || strings.Count(got, "(A;") != 2 {
			t.Fatalf("unexpected staging ACL: %s", got)
		}
	}
}
