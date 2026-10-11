//go:build linux

package ami

import (
	"fmt"
	"os"
	"syscall"
)

func fileIdentity(info os.FileInfo) string {
	s := info.Sys().(*syscall.Stat_t)
	return fmt.Sprintf("%d:%d:%d:%d", s.Dev, s.Ino, s.Ctim.Sec, s.Ctim.Nsec)
}
