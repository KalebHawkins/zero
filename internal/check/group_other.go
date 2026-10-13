//go:build !unix

package check

import (
	"os"
	"syscall"
)

// ownGroup leaves the program in zero's own group: on Windows the console
// sends Ctrl+C to every program attached to it.
func ownGroup() *syscall.SysProcAttr {
	return nil
}

// killGroup kills p.
func killGroup(p *os.Process) error {
	return p.Kill()
}
