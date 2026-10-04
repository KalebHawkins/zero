// Command zero is the learner's command for Zero Series practice.
package main

import (
	"os"
	"time"

	"github.com/KalebHawkins/zero/internal/cli"
)

// version is the release. A build can replace it with
// -ldflags "-X main.version=1.2.3".
var version = "0.1.0"

func main() {
	dir, err := os.Getwd()
	if err != nil {
		dir = "."
	}
	os.Exit(cli.Run(os.Args[1:], cli.Env{
		Version:     version,
		Stdin:       os.Stdin,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		Getenv:      os.Getenv,
		Dir:         dir,
		IsTerminal:  isTerminal(os.Stdout),
		OpenBrowser: cli.OpenBrowser,
		Sleep:       time.Sleep,
		Now:         time.Now,
	}))
}

// isTerminal reports whether f is a character device, which a terminal is
// and a pipe or a file is not.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
