// Command zero is the learner's command for Zero Series practice.
package main

import (
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/KalebHawkins/zero/internal/cli"
)

// version is the release. A build can replace it with
// -ldflags "-X main.version=1.2.3". `go install ...@v1.2.3` records the
// tag in the build info, and release prefers that.
var version = "0.3.3"

// release returns the module's tag without its "v" when go install
// recorded one, and version otherwise.
func release() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		v := bi.Main.Version
		if strings.HasPrefix(v, "v") && !strings.Contains(v, "-0.") && !strings.Contains(v, "+dirty") {
			return strings.TrimPrefix(v, "v")
		}
	}
	return version
}

func main() {
	dir, err := os.Getwd()
	if err != nil {
		dir = "."
	}
	os.Exit(cli.Run(os.Args[1:], cli.Env{
		Version:     release(),
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
