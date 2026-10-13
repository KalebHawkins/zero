//go:build unix

package check

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A program that Exec starts leads a process group of its own, so a
// terminal's Ctrl+C does not reach it or the programs it starts.
func TestExecStartsAProcessGroup(t *testing.T) {
	if _, err := exec.LookPath("ps"); err != nil {
		t.Skip("no ps command")
	}
	out, _, err := Exec(context.Background(), t.TempDir(), nil, "sh", "-c", "echo $$; ps -o pgid= -p $$")
	if err != nil {
		t.Fatal(err)
	}
	f := strings.Fields(string(out))
	if len(f) != 2 {
		t.Fatalf("output %q, want the pid and the process group", out)
	}
	if f[0] != f[1] {
		t.Errorf("the program %s is in process group %s, want a group of its own", f[0], f[1])
	}
	if f[1] == strconv.Itoa(syscall.Getpgrp()) {
		t.Errorf("the program is in the test's process group %s", f[1])
	}
}

// When ctx ends, the program gets an interrupt and is waited for: a shell
// that is running a command finishes it first.
func TestExecWaitsAfterTheInterrupt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)
	out, _, _ := Exec(ctx, t.TempDir(), nil, "sh", "-c", `trap 'echo interrupted' INT; sleep 1; echo ended`)
	if got := strings.Fields(string(out)); strings.Join(got, " ") != "interrupted ended" {
		t.Errorf("output %q, want the interrupt handled after the running command ended", out)
	}
}

// A program that ignores the interrupt is killed with its group after
// stopWait.
func TestExecKillsAfterStopWait(t *testing.T) {
	defer func(d time.Duration) { stopWait = d }(stopWait)
	stopWait = 300 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	start := time.Now()
	Exec(ctx, t.TempDir(), nil, "sh", "-c", `trap '' INT; sleep 10; echo ended`)
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("Exec returned after %v, want the group killed after stopWait", d)
	}
}
