package check

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/KalebHawkins/zero/spec"
)

// fakeRun records every command and answers from a script, so no test needs
// podman or go.
type fakeRun struct {
	calls []string            // "name arg arg", in order
	envs  map[string][]string // the env of each go test call, by its last argument
	reply func(call string) (stdout, stderr string, err error)
	// cancel, when set, is called before the lab run returns, as Ctrl+C would.
	cancel func()
}

func (f *fakeRun) run(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, []byte, error) {
	call := strings.Join(append([]string{name}, args...), " ")
	f.calls = append(f.calls, call)
	if f.envs == nil {
		f.envs = map[string][]string{}
	}
	if len(env) > 0 {
		f.envs[args[len(args)-1]] = env
	}
	if f.cancel != nil && strings.HasSuffix(call, "./lab/") {
		f.cancel()
	}
	if f.reply == nil {
		return nil, nil, nil
	}
	out, errOut, err := f.reply(call)
	return []byte(out), []byte(errOut), err
}

var worldd = spec.Exercise{
	ID: "worldd-10", Title: "Running the server in a container", Checker: spec.CheckerLab,
	Tasks: []spec.Task{
		{N: 1, Title: "Build the image", Tests: []string{"TestImageBuilds"}},
		{N: 2, Title: "Run it detached", Tests: []string{"TestRunsDetached"}},
	},
	Lab: &spec.Lab{
		Images:  []string{"docker.io/library/golang:1.27", "gcr.io/distroless/static-debian12:nonroot"},
		Names:   spec.LabNames{Image: "zero-lab-worldd", Container: "zero-lab-worldd", Volume: "zero-lab-worldd-data"},
		Timeout: 600,
	},
}

// events builds a `go test -json` stream with one result line for each test.
func events(results ...string) string {
	var b strings.Builder
	for i := 0; i+1 < len(results); i += 2 {
		name, action := results[i], results[i+1]
		if action == "fail" {
			fmt.Fprintf(&b, `{"Action":"output","Package":"theworld/lab","Test":%q,"Output":"    lab_test.go:9: podman build failed\n"}`+"\n", name)
		}
		fmt.Fprintf(&b, `{"Action":%q,"Package":"theworld/lab","Test":%q}`+"\n", action, name)
	}
	return b.String()
}

var exitErr = &exec.ExitError{}

func clock() func() time.Time {
	t := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	return func() time.Time { t = t.Add(time.Second); return t }
}

const (
	labRun      = "go test -tags lab -json -count=1 -timeout 600s ./lab/"
	cleanupRm   = "podman rm --force --volumes --time 0 zero-lab-worldd"
	cleanupVol  = "podman volume rm zero-lab-worldd-data"
	cleanupRmi  = "podman image rm zero-lab-worldd"
	regressions = "go test -json ./..."
)

func TestLabRunsInOrder(t *testing.T) {
	f := &fakeRun{reply: func(call string) (string, string, error) {
		switch call {
		case regressions:
			return events("TestRun", "pass"), "", nil
		case labRun:
			return events("TestImageBuilds", "pass", "TestRunsDetached", "fail"), "", exitErr
		case "podman info":
			return "host: ...", "", nil
		}
		return "", "Error: no such object", exitErr // every cleanup finds nothing
	}}
	ready := func() { f.calls = append(f.calls, "ready") }
	rep, err := Lab(context.Background(), "/p", worldd, f.run, "0.4.0", clock(), ready)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"podman info", "ready", regressions, cleanupRm, cleanupVol, cleanupRmi, labRun, cleanupRm, cleanupVol, cleanupRmi}
	if !slices.Equal(f.calls, want) {
		t.Errorf("calls:\n  %s\nwant:\n  %s", strings.Join(f.calls, "\n  "), strings.Join(want, "\n  "))
	}
	env := f.envs["./lab/"]
	for _, kv := range []string{"ZERO_LAB_IMAGE=zero-lab-worldd", "ZERO_LAB_CONTAINER=zero-lab-worldd",
		"ZERO_LAB_VOLUME=zero-lab-worldd-data", "ZERO_LAB_IMAGES=docker.io/library/golang:1.27 gcr.io/distroless/static-debian12:nonroot"} {
		if !slices.Contains(env, kv) {
			t.Errorf("the lab tests run without %s; env = %v", kv, env)
		}
	}
	if rep.Checker != "lab" || rep.OK || len(rep.Tasks) != 2 || rep.Tasks[0].Status != "pass" || rep.Tasks[1].Status != "fail" ||
		!strings.Contains(rep.Tasks[1].Tests[0].Message, "podman build failed") || len(rep.Regressions) != 0 || rep.DurationMS == 0 {
		t.Errorf("report = %+v", rep)
	}
}

func TestLabPasses(t *testing.T) {
	f := &fakeRun{reply: func(call string) (string, string, error) {
		switch call {
		case regressions:
			return events("TestRun", "pass"), "", nil
		case labRun:
			return events("TestImageBuilds", "pass", "TestRunsDetached", "pass"), "", nil
		}
		return "", "", nil
	}}
	rep, err := Lab(context.Background(), "/p", worldd, f.run, "0.4.0", clock(), nil)
	if err != nil || !rep.OK || rep.BuildError != "" {
		t.Fatalf("Lab = %+v, %v; want a passing report", rep, err)
	}
}

func TestLabReportsRegressions(t *testing.T) {
	f := &fakeRun{reply: func(call string) (string, string, error) {
		switch call {
		case regressions:
			return events("TestRun", "fail", "TestCheck", "pass"), "", exitErr
		case labRun:
			return events("TestImageBuilds", "pass", "TestRunsDetached", "pass"), "", nil
		}
		return "", "", nil
	}}
	rep, err := Lab(context.Background(), "/p", worldd, f.run, "0.4.0", clock(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.OK || len(rep.Regressions) != 1 || rep.Regressions[0].Name != "TestRun" || rep.Tasks[0].Status != "pass" {
		t.Errorf("report = %+v; want both tasks passing and TestRun as a regression", rep)
	}
}

// When the project does not compile, the image cannot build either: the
// report holds the build error and no container is touched.
func TestLabStopsAtABuildError(t *testing.T) {
	f := &fakeRun{reply: func(call string) (string, string, error) {
		if call == regressions {
			return `{"Action":"build-output","ImportPath":"theworld/internal/sim","Output":"internal/sim/run.go:9:2: undefined: Fast\n"}` + "\n" +
				`{"Action":"build-fail","ImportPath":"theworld/internal/sim"}` + "\n", "", exitErr
		}
		return "", "", nil
	}}
	rep, err := Lab(context.Background(), "/p", worldd, f.run, "0.4.0", clock(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"podman info", regressions}; !slices.Equal(f.calls, want) {
		t.Errorf("calls = %q, want %q", f.calls, want)
	}
	if rep.OK || !strings.Contains(rep.BuildError, "undefined: Fast") || rep.Tasks[0].Tests[0].Message != spec.MessageDidNotCompile || rep.Checker != "lab" {
		t.Errorf("report = %+v", rep)
	}
}

func TestLabNeedsPodman(t *testing.T) {
	t.Run("podman is missing", func(t *testing.T) {
		f := &fakeRun{reply: func(call string) (string, string, error) {
			return "", "", fmt.Errorf("exec: %q: %w", "podman", exec.ErrNotFound)
		}}
		ready := func() { f.calls = append(f.calls, "ready") }
		if _, err := Lab(context.Background(), "/p", worldd, f.run, "0.4.0", clock(), ready); !errors.Is(err, ErrNoPodman) {
			t.Errorf("Lab = %v, want ErrNoPodman", err)
		}
		if len(f.calls) != 1 {
			t.Errorf("calls = %q; nothing runs after podman info fails", f.calls)
		}
	})
	t.Run("podman does not answer", func(t *testing.T) {
		f := &fakeRun{reply: func(call string) (string, string, error) {
			return "", "Cannot connect to Podman. Please verify your connection to the Linux system\n", exitErr
		}}
		_, err := Lab(context.Background(), "/p", worldd, f.run, "0.4.0", clock(), nil)
		var pe *PodmanError
		if !errors.As(err, &pe) || !strings.Contains(pe.Output, "Cannot connect to Podman") {
			t.Errorf("Lab = %v, want a PodmanError with what podman said", err)
		}
	})
}

// Ctrl+C during the lab tests: the running test ends first, and the objects
// are then removed by name.
func TestLabCleansUpWhenStopped(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := &fakeRun{cancel: cancel, reply: func(call string) (string, string, error) {
		if call == labRun {
			return "", "", context.Canceled
		}
		return "", "", nil
	}}
	if _, err := Lab(ctx, "/p", worldd, f.run, "0.4.0", clock(), nil); !errors.Is(err, context.Canceled) {
		t.Errorf("Lab = %v, want context.Canceled", err)
	}
	if n := len(f.calls); n < 4 || !slices.Equal(f.calls[n-4:], []string{labRun, cleanupRm, cleanupVol, cleanupRmi}) {
		t.Errorf("calls end with %q; want the lab tests, then the cleanup by name", f.calls)
	}
}

func TestLabNeedsItsSettings(t *testing.T) {
	ex := worldd
	ex.Lab = nil
	f := &fakeRun{}
	if _, err := Lab(context.Background(), "/p", ex, f.run, "0.4.0", clock(), nil); err == nil || len(f.calls) != 0 {
		t.Errorf("Lab with no lab block = %v after %q; want an error and no command", err, f.calls)
	}
}
