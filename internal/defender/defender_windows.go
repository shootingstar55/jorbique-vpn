//go:build windows

package defender

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"

	"jorbiquevpn/internal/wincom"
)

// Defender's exclusion list is WMI state. Add-MpPreference is a CIM cmdlet over
// root\Microsoft\Windows\Defender, and MSFT_MpPreference's Add method is what it ends up
// calling; going to the class directly costs the COM dance below but takes a PowerShell
// launch out of startup. It also takes the admin probe with it: the old script asked whether
// it was elevated and whether the cmdlet existed before doing anything, and both answers are
// just an error from Add now — which the caller has always ignored, because a missing
// exclusion is a slower client, not a broken one.

const (
	defenderNamespace = `root\Microsoft\Windows\Defender`
	preferenceClass   = "MSFT_MpPreference"

	// Defender's WMI provider talks to the antimalware service, which can be busy or wedged;
	// AddExclusions runs in its own goroutine at startup and should not sit there forever.
	wmiTimeout = 10 * time.Second
)

// AddExclusions adds the install directory and, when it is there, sing-box.exe to Windows
// Defender's path exclusions. Failure is deliberately silent.
func AddExclusions(projectRoot string) {
	paths := make([]string, 0, 2)
	if abs, err := filepath.Abs(projectRoot); err == nil {
		paths = append(paths, abs)
	}
	if abs, err := filepath.Abs(filepath.Join(projectRoot, "sing-box.exe")); err == nil {
		if _, err := os.Stat(abs); err == nil {
			paths = append(paths, abs)
		}
	}
	if len(paths) == 0 {
		return
	}
	// Add takes an array, so both paths go in one call. Re-adding a path Defender already
	// excludes is a no-op there, which is what the old script's read-back check bought.
	_ = withDefenderService(func(service *ole.IDispatch) error {
		return addExclusionPaths(service, paths)
	})
}

func addExclusionPaths(service *ole.IDispatch, paths []string) error {
	class, err := object(oleutil.CallMethod(service, "Get", preferenceClass))
	if err != nil {
		return fmt.Errorf("get %s: %w", preferenceClass, err)
	}
	defer class.Release()

	methods, err := object(oleutil.GetProperty(class, "Methods_"))
	if err != nil {
		return fmt.Errorf("methods: %w", err)
	}
	defer methods.Release()

	add, err := object(oleutil.CallMethod(methods, "Item", "Add"))
	if err != nil {
		return fmt.Errorf("Add method: %w", err)
	}
	defer add.Release()

	signature, err := object(oleutil.GetProperty(add, "InParameters"))
	if err != nil {
		return fmt.Errorf("Add parameters: %w", err)
	}
	defer signature.Release()

	args, err := object(oleutil.CallMethod(signature, "SpawnInstance_"))
	if err != nil {
		return fmt.Errorf("spawn parameters: %w", err)
	}
	defer args.Release()

	if _, err := oleutil.PutProperty(args, "ExclusionPath", paths); err != nil {
		return fmt.Errorf("ExclusionPath: %w", err)
	}
	if _, err := oleutil.CallMethod(service, "ExecMethod_", preferenceClass, "Add", args); err != nil {
		return fmt.Errorf("Add: %w", err)
	}
	return nil
}

// object unwraps the IDispatch a WMI call returns, so the call sites above read as one line
// each instead of three.
func object(v *ole.VARIANT, err error) (*ole.IDispatch, error) {
	if err != nil {
		return nil, err
	}
	d := v.ToIDispatch()
	if d == nil {
		return nil, errors.New("no object returned")
	}
	return d, nil
}

// withDefenderService connects to the Defender WMI namespace and hands the service to fn, on a
// COM thread of its own and under a timeout: see wincom.Run for what the timeout does and does
// not stop.
func withDefenderService(fn func(service *ole.IDispatch) error) error {
	return defenderCOM.Run(func() error { return callDefender(fn) })
}

var defenderCOM = &wincom.Runner{Timeout: wmiTimeout}

func callDefender(fn func(service *ole.IDispatch) error) error {
	unknown, err := oleutil.CreateObject("WbemScripting.SWbemLocator")
	if err != nil {
		return err
	}
	defer unknown.Release()
	locator, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return err
	}
	defer locator.Release()
	service, err := object(oleutil.CallMethod(locator, "ConnectServer", ".", defenderNamespace))
	if err != nil {
		return fmt.Errorf("connect %s: %w", defenderNamespace, err)
	}
	defer service.Release()
	return fn(service)
}
