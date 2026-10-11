//go:build windows

package ami

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"golang.org/x/sys/windows"
)

func configureProcess(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }
func lockFile(f *os.File) error {
	err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return errSessionBusy
	}
	return err
}
func signalGroup(pid int, force bool) error {
	cmd := exec.Command("taskkill", "/pid", strconv.Itoa(pid), "/T", "/F")
	configureProcess(cmd)
	return cmd.Run()
}
func shellCommand(command string) (string, []string) {
	return "powershell.exe", []string{"-NoProfile", "-NonInteractive", "-Command", command}
}
func fileIdentity(info os.FileInfo) string {
	s := info.Sys().(*syscall.Win32FileAttributeData)
	return fmt.Sprintf("%d:%d", s.CreationTime.HighDateTime, s.CreationTime.LowDateTime)
}

func processSignal(state *os.ProcessState) string { return "" }
