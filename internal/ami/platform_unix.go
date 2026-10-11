//go:build darwin || linux

package ami

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

func configureProcess(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
func lockFile(f *os.File) error {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return errSessionBusy
	}
	return err
}
func signalGroup(pid int, force bool) error {
	signal := syscall.SIGTERM
	if force {
		signal = syscall.SIGKILL
	}
	return syscall.Kill(-pid, signal)
}
func shellCommand(command string) (string, []string) { return "/bin/sh", []string{"-c", command} }

func processSignal(state *os.ProcessState) string {
	status, ok := state.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return ""
	}
	switch status.Signal() {
	case syscall.SIGTERM:
		return "SIGTERM"
	case syscall.SIGKILL:
		return "SIGKILL"
	case syscall.SIGINT:
		return "SIGINT"
	case syscall.SIGPIPE:
		return "SIGPIPE"
	default:
		return fmt.Sprintf("SIG%d", status.Signal())
	}
}
