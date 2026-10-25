//go:build windows

package tray

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"jorbiquevpn/internal/constants"
	"jorbiquevpn/internal/wincom"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"golang.org/x/sys/windows/registry"
)

const (
	autostartKey      = `SOFTWARE\Microsoft\Windows\CurrentVersion\Run`
	logonAutostartArg = "--logon-autostart"
)

// The Task Scheduler is a COM service (Schedule.Service); schtasks.exe is a client of the same
// API. Talking to it directly takes the task XML as a string, so the temp file the CLI needed,
// its UTF-16 encoding, and the second "build the task from flags instead" code path are gone.
const (
	taskCreateOrUpdate        = 6 // TASK_CREATE_OR_UPDATE
	taskLogonInteractiveToken = 3 // TASK_LOGON_INTERACTIVE_TOKEN
	// The service can be slow or wedged, and RefreshAutostartIfEnabled runs synchronously from
	// OnStartup: past this budget the caller gives up rather than leaving the window hidden.
	taskSchedulerTimeout = 5 * time.Second
)

// AutostartEnabled reports whether logon autostart is configured.
func AutostartEnabled() bool {
	return isAutostartEnabled()
}

// SetAutostart enables or disables logon autostart.
func SetAutostart(on bool) error {
	if on {
		return enableAutostart()
	}
	disableAutostart()
	return nil
}

// RefreshAutostartIfEnabled migrates legacy tasks/registry and refreshes the launcher command.
func RefreshAutostartIfEnabled() error {
	if !isAutostartEnabled() {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return nil
	}
	removeRegistryRunKey()
	if logonTaskHealthy(exe) {
		return nil
	}
	return createLogonAutostartTask(exe)
}

func isAutostartEnabled() bool {
	if logonTaskExists() {
		return true
	}
	return registryRunEnabled()
}

func enableAutostart() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return err
	}
	removeRegistryRunKey()
	if err := createLogonAutostartTask(exe); err != nil {
		return err
	}
	if !logonTaskExists() {
		return errors.New("autostart logon task was not created")
	}
	return nil
}

func disableAutostart() {
	deleteLogonAutostartTask()
	removeRegistryRunKey()
}

func createLogonAutostartTask(exe string) error {
	runAs, err := interactiveTaskUser()
	if err != nil {
		return err
	}
	xml := logonTaskXML(exe)
	return withTaskFolder(func(folder *ole.IDispatch) error {
		registered, err := oleutil.CallMethod(folder, "RegisterTask",
			constants.AutostartRegistrationName, xml, taskCreateOrUpdate, runAs, nil, taskLogonInteractiveToken, nil)
		if err != nil {
			return err
		}
		if task := registered.ToIDispatch(); task != nil {
			task.Release()
		}
		return nil
	})
}

func logonTaskXML(exe string) string {
	exeDir := filepath.Dir(exe)
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.4" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
      <Delay>PT15S</Delay>
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>HighestAvailable</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>true</AllowHardTerminate>
    <StartWhenAvailable>true</StartWhenAvailable>
    <Enabled>true</Enabled>
    <RunOnlyIfIdle>false</RunOnlyIfIdle>
    <WakeToRun>false</WakeToRun>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Priority>7</Priority>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>%s</Command>
      <Arguments>%s</Arguments>
      <WorkingDirectory>%s</WorkingDirectory>
    </Exec>
  </Actions>
</Task>`, xmlEscapeAttr(exe), xmlEscapeAttr(logonAutostartArg), xmlEscapeAttr(exeDir))
}

func logonTaskHealthy(exe string) bool {
	xml, err := queryLogonTaskXML()
	if err != nil {
		return false
	}
	lower := strings.ToLower(xml)
	if !strings.Contains(lower, "<logontrigger") {
		return false
	}
	if !strings.Contains(lower, "interactivetoken") {
		return false
	}
	// The app manifest is requireAdministrator: a logon launch only works if the task
	// runs elevated (HighestAvailable). Legacy LeastPrivilege/LIMITED tasks are treated
	// as unhealthy so RefreshAutostartIfEnabled recreates them on next launch.
	if !strings.Contains(lower, "highestavailable") {
		return false
	}
	// The task must launch the exe directly (a cmd.exe wrapper leaves a console window behind
	// the app at logon). Treat legacy cmd-wrapped tasks as unhealthy so they get recreated.
	// Legacy here means tasks written by older builds through schtasks /tr.
	cmd := taskXMLField(xml, "Command")
	if !pathsEqual(cmd, exe) {
		return false
	}
	args := taskXMLField(xml, "Arguments")
	if !strings.Contains(strings.ToLower(args), strings.ToLower(logonAutostartArg)) {
		return false
	}
	wd := taskXMLField(xml, "WorkingDirectory")
	return pathsEqual(wd, filepath.Dir(exe))
}

func logonTaskExists() bool {
	_, err := queryLogonTaskXML()
	return err == nil
}

func deleteLogonAutostartTask() {
	_ = withTaskFolder(func(folder *ole.IDispatch) error {
		_, err := oleutil.CallMethod(folder, "DeleteTask", constants.AutostartRegistrationName, 0)
		return err
	})
}

// queryLogonTaskXML returns the registered task definition, or an error when there is no task.
func queryLogonTaskXML() (string, error) {
	xml := ""
	err := withTaskFolder(func(folder *ole.IDispatch) error {
		found, err := oleutil.CallMethod(folder, "GetTask", constants.AutostartRegistrationName)
		if err != nil {
			return err
		}
		task := found.ToIDispatch()
		if task == nil {
			return errors.New("task scheduler returned no task")
		}
		defer task.Release()
		definition, err := oleutil.GetProperty(task, "Xml")
		if err != nil {
			return err
		}
		defer func() { _ = definition.Clear() }()
		xml = definition.ToString()
		return nil
	})
	return xml, err
}

// withTaskFolder connects to the Task Scheduler and hands the root folder to fn, on a COM thread
// of its own and under a timeout: see wincom.Run for what the timeout does and does not stop.
// A refresh makes several of these calls, which is why a wedged service must stop being asked.
func withTaskFolder(fn func(folder *ole.IDispatch) error) error {
	return taskSchedulerCOM.Run(func() error { return callTaskScheduler(fn) })
}

var taskSchedulerCOM = &wincom.Runner{Timeout: taskSchedulerTimeout}

func callTaskScheduler(fn func(folder *ole.IDispatch) error) error {
	unknown, err := oleutil.CreateObject("Schedule.Service")
	if err != nil {
		return err
	}
	defer unknown.Release()
	service, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return err
	}
	defer service.Release()
	if _, err := oleutil.CallMethod(service, "Connect"); err != nil {
		return err
	}
	root, err := oleutil.CallMethod(service, "GetFolder", `\`)
	if err != nil {
		return err
	}
	folder := root.ToIDispatch()
	if folder == nil {
		return errors.New("task scheduler returned no root folder")
	}
	defer folder.Release()
	return fn(folder)
}

func taskXMLField(xml, field string) string {
	open := "<" + field + ">"
	close := "</" + field + ">"
	start := strings.Index(xml, open)
	if start < 0 {
		return ""
	}
	start += len(open)
	end := strings.Index(xml[start:], close)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(xml[start : start+end])
}

func pathsEqual(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

func registryRunEnabled() bool {
	_, ok := registryAutostartPath()
	return ok
}

func registryAutostartPath() (string, bool) {
	k, err := registry.OpenKey(registry.CURRENT_USER, autostartKey, registry.QUERY_VALUE)
	if err != nil {
		return "", false
	}
	defer k.Close()
	val, _, err := k.GetStringValue(constants.AutostartRegistrationName)
	if err != nil {
		return "", false
	}
	val = strings.TrimSpace(strings.Trim(val, `"`))
	if val == "" {
		return "", false
	}
	return val, true
}

func removeRegistryRunKey() {
	k, err := registry.OpenKey(registry.CURRENT_USER, autostartKey, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	_ = k.DeleteValue(constants.AutostartRegistrationName)
}

func interactiveTaskUser() (string, error) {
	computer := strings.TrimSpace(os.Getenv("COMPUTERNAME"))
	domain := strings.TrimSpace(os.Getenv("USERDOMAIN"))
	name := strings.TrimSpace(os.Getenv("USERNAME"))
	if name != "" {
		if domain != "" && !strings.EqualFold(domain, computer) {
			return domain + `\` + name, nil
		}
		if computer != "" {
			return computer + `\` + name, nil
		}
		return name, nil
	}
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	return u.Username, nil
}

func xmlEscapeAttr(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}
