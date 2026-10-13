//go:build unix

package check

import (
	"os"
	"syscall"
)

// ownGroup starts a program as the leader of a new process group.
func ownGroup() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// killGroup kills p and every program in its process group.
func killGroup(p *os.Process) error {
	return syscall.Kill(-p.Pid, syscall.SIGKILL)
}
