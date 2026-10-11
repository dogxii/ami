//go:build windows

package ami

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

func configureProcess(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }
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
