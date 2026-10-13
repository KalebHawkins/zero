package check

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/KalebHawkins/zero/spec"
)

// ErrNoPodman means the podman command is not on PATH.
var ErrNoPodman = errors.New("the podman command was not found")

// PodmanError means podman is installed but `podman info` failed, for
// example because its machine is not started on macOS or Windows. Output is
// what podman printed.
type PodmanError struct {
	Output string
}

func (e *PodmanError) Error() string { return "podman info failed: " + e.Output }

// Runner starts a program in dir with env added to the environment, waits
// for it, and returns what it wrote. err is an *exec.ExitError when the
// program ran and failed, and wraps exec.ErrNotFound when it is not on PATH.
// The tests replace it, so no test needs podman.
type Runner func(ctx context.Context, dir string, env []string, name string, args ...string) (stdout, stderr []byte, err error)

// Exec is the Runner that starts real programs. On Unix each program starts
// in a process group of its own, so the interrupt a terminal sends on Ctrl+C
// reaches zero alone: a podman build that go test started is not cut off
// halfway. When ctx ends, the program gets one interrupt and is waited for.
// go test then starts nothing new and lets the running test end. A program
// still running stopWait later is killed with its whole group.
func Exec(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.SysProcAttr = ownGroup()
	var kill atomic.Pointer[time.Timer]
	cmd.Cancel = func() error {
		if err := cmd.Process.Signal(os.Interrupt); err != nil {
			// Windows has no interrupt to send.
			return killGroup(cmd.Process)
		}
		kill.Store(time.AfterFunc(stopWait, func() { killGroup(cmd.Process) }))
		return nil
	}
	cmd.WaitDelay = stopWait + 10*time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if t := kill.Load(); t != nil {
		t.Stop()
	}
	return stdout.Bytes(), stderr.Bytes(), err
}

// stopWait is how long Exec waits for a program after its interrupt: long
// enough for a running image build to finish.
var stopWait = 3 * time.Minute

// podmanInfoTimeout limits `podman info`, which hangs while a podman
// machine starts.
const podmanInfoTimeout = 30 * time.Second

// labGrace is how long the command waits past the stage's timeout before it
// stops `go test` itself: go test reports its own timeout first.
const labGrace = time.Minute

// LabEnv returns the environment a lab stage's tests run with: the names of
// its image, container and volume, and its base images separated by spaces.
func LabEnv(lab spec.Lab) []string {
	return []string{
		spec.EnvLabImage + "=" + lab.Names.Image,
		spec.EnvLabContainer + "=" + lab.Names.Container,
		spec.EnvLabVolume + "=" + lab.Names.Volume,
		spec.EnvLabImages + "=" + strings.Join(lab.Images, " "),
	}
}

// LabCleanup removes the container, the volume and the image of a lab stage,
// by name and in that order. A missing object is no error, so its errors are
// ignored. The container goes with its anonymous volumes; nothing else is
// removed, and nothing is pruned.
func LabCleanup(ctx context.Context, run Runner, dir string, names spec.LabNames) {
	run(ctx, dir, nil, "podman", "rm", "--force", "--volumes", "--time", "0", names.Container)
	run(ctx, dir, nil, "podman", "volume", "rm", names.Volume)
	run(ctx, dir, nil, "podman", "image", "rm", names.Image)
}

// PodmanReady runs `podman info` and says whether podman answers. It returns
// ErrNoPodman or a *PodmanError when it does not.
func PodmanReady(ctx context.Context, run Runner, dir string) error {
	ctx, cancel := context.WithTimeout(ctx, podmanInfoTimeout)
	defer cancel()
	stdout, stderr, err := run(ctx, dir, nil, "podman", "info")
	switch {
	case err == nil:
		return nil
	case errors.Is(err, exec.ErrNotFound):
		return ErrNoPodman
	}
	out := strings.TrimSpace(string(stderr))
	if out == "" {
		out = strings.TrimSpace(string(stdout))
	}
	if out == "" {
		out = err.Error()
	}
	return &PodmanError{Output: firstLines(out, 5)}
}

// Lab runs the checks of a lab stage in the project folder dir (docs/SPEC.md
// section 5) and returns one Report: the tasks from the lab tests and the
// regressions from every other test.
//
//  1. `podman info`, so a missing podman is named before anything runs;
//     then ready, when it is not nil, is called;
//  2. `go test -json ./...`: the earlier stages' tests, as regressions;
//  3. the stage's container, volume and image are removed by name;
//  4. `go test -tags lab -json -count=1 -timeout <t>s ./lab/` with the names
//     in the environment;
//  5. the three objects are removed by name again, however the tests ended.
//
// When the code does not compile in step 2, the lab tests do not run. When
// ctx ends (Ctrl+C), the running test ends first (see Exec), step 5 still
// runs, and Lab returns the error of ctx.
func Lab(ctx context.Context, dir string, ex spec.Exercise, run Runner, cliVersion string, now func() time.Time, ready func()) (spec.Report, error) {
	if ex.Lab == nil {
		return spec.Report{}, fmt.Errorf("the exercise %s uses the checker %q but has no lab settings", ex.ID, spec.CheckerLab)
	}
	lab := *ex.Lab
	start := now()
	if err := PodmanReady(ctx, run, dir); err != nil {
		return spec.Report{}, err
	}
	if ready != nil {
		ready()
	}

	// The earlier stages' tests. The lab tests have the build tag lab, so
	// this run does not build them.
	stdout, stderr, err := run(ctx, dir, nil, "go", "test", "-json", "./...")
	if err := started(err); err != nil {
		return spec.Report{}, err
	}
	if ctx.Err() != nil {
		return spec.Report{}, ctx.Err()
	}
	res, perr := Parse(bytes.NewReader(stdout))
	if perr != nil {
		return spec.Report{}, perr
	}
	meta := func() Meta { return Meta{At: start, Duration: now().Sub(start), CLI: cliVersion} }
	if res.BuildFailed || (err != nil && len(res.Tests) == 0) {
		// Every task fails with the compile error; the image would not build either.
		return Report(ex, res, string(stderr), true, meta()), nil
	}
	regressions := Regressions(ex, res)

	LabCleanup(ctx, run, dir, lab.Names)
	// The objects go even when ctx has ended, so the second cleanup has a context of its own.
	defer LabCleanup(context.Background(), run, dir, lab.Names)

	timeout := time.Duration(lab.Timeout) * time.Second
	lctx, cancel := context.WithTimeout(ctx, timeout+labGrace)
	defer cancel()
	stdout, stderr, err = run(lctx, dir, LabEnv(lab), "go", "test", "-tags", "lab", "-json", "-count=1",
		"-timeout", strconv.Itoa(lab.Timeout)+"s", "./lab/")
	if err := started(err); err != nil {
		return spec.Report{}, err
	}
	if ctx.Err() != nil {
		return spec.Report{}, ctx.Err()
	}
	res, perr = Parse(bytes.NewReader(stdout))
	if perr != nil {
		return spec.Report{}, perr
	}
	rep := Report(ex, res, string(stderr), err != nil, meta())
	if rep.BuildError == "" && len(regressions) > 0 {
		rep.Regressions = regressions
		rep.OK = false
	}
	return rep, nil
}

// started turns the error of a go command that did not start into ErrNoGo
// or a plain error. A command that ran and failed is no error here.
func started(err error) error {
	var exitErr *exec.ExitError
	switch {
	case err == nil, errors.As(err, &exitErr):
		return nil
	case errors.Is(err, exec.ErrNotFound):
		return ErrNoGo
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return nil
	}
	return fmt.Errorf("go test did not start: %w", err)
}

// firstLines returns at most n lines of s.
func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = append(lines[:n], fmt.Sprintf("(%d more lines)", len(lines)-n))
	}
	return strings.Join(lines, "\n")
}
