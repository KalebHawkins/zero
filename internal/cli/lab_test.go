package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KalebHawkins/zero/spec"
)

// labFolder writes a project folder whose current stage is a lab stage, as
// zero start leaves it, and makes it the folder of the next command.
func labFolder(t *testing.T, h *harness) string {
	t.Helper()
	dir := filepath.Join(h.workspace, "worldd")
	saved := spec.Saved{
		Exercise: spec.Exercise{
			ID: "worldd-10", Title: "Running the server in a container", Checker: spec.CheckerLab,
			Edit:  []string{"Containerfile"},
			Tasks: []spec.Task{{N: 1, Title: "Build the image", Tests: []string{"TestImageBuilds"}}},
			Lab: &spec.Lab{Images: []string{"docker.io/library/golang:1.27"}, Timeout: 600,
				Names: spec.LabNames{Image: "zero-lab-worldd", Container: "zero-lab-worldd", Volume: "zero-lab-worldd-data"}},
		},
		Files:   []spec.FileRef{{Path: "Containerfile", Mode: "edit"}},
		Project: &spec.Project{ID: "worldd", Title: "The world server", Stage: 10, Stages: 10, EditAll: []string{"Containerfile"}},
	}
	if h.fake.entries == nil {
		h.fake.entries = map[string]*fakeEntry{}
	}
	h.fake.entries["worldd-10"] = &fakeEntry{exercise: saved.Exercise}
	b, _ := json.Marshal(saved)
	if err := os.MkdirAll(filepath.Join(dir, ".zero"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, ".zero", "exercise.json"), b, 0o644)
	os.WriteFile(filepath.Join(dir, "Containerfile"), []byte("FROM docker.io/library/golang:1.27\n"), 0o644)
	h.dir = dir
	return dir
}

// labRunner answers podman info with podmanErr, the earlier tests with a
// pass, and the lab tests with labResult. It records every command.
func labRunner(calls *[]string, podmanErr error, labResult string) func(context.Context, string, []string, string, ...string) ([]byte, []byte, error) {
	return func(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, []byte, error) {
		call := strings.Join(append([]string{name}, args...), " ")
		*calls = append(*calls, call)
		switch {
		case call == "podman info":
			return nil, []byte("Cannot connect to Podman\n"), podmanErr
		case call == "go test -json ./...":
			return []byte(`{"Action":"pass","Package":"theworld/internal/sim","Test":"TestRun"}` + "\n"), nil, nil
		case strings.HasSuffix(call, "./lab/"):
			out := fmt.Sprintf(`{"Action":%q,"Package":"theworld/lab","Test":"TestImageBuilds"}`+"\n", labResult)
			if labResult == "fail" {
				return []byte(out), nil, &exec.ExitError{}
			}
			return []byte(out), nil, nil
		}
		return nil, nil, nil
	}
}

func TestLabStageTest(t *testing.T) {
	t.Run("a pass is reported with the checker lab", func(t *testing.T) {
		h := newHarness(t)
		h.signIn()
		labFolder(t, h)
		var calls []string
		h.runner = labRunner(&calls, nil, "pass")
		out := h.ok("test")
		wantContains(t, "zero test", out, "builds and runs a container with podman", "✓ Task 1: Build the image", "Every test passes. Run: zero submit")
		if len(h.fake.runs) != 1 || h.fake.runs[0].Checker != "lab" || !h.fake.runs[0].OK {
			t.Errorf("runs posted = %+v; want one passing lab report", h.fake.runs)
		}
		if last := calls[len(calls)-1]; last != "podman image rm zero-lab-worldd" {
			t.Errorf("the last command is %q; want the image removed by name", last)
		}
	})

	t.Run("without podman it says how to install it and posts nothing", func(t *testing.T) {
		h := newHarness(t)
		h.signIn()
		labFolder(t, h)
		var calls []string
		h.runner = labRunner(&calls, fmt.Errorf("exec: \"podman\": %w", exec.ErrNotFound), "pass")
		code, _, stderr := h.run("test")
		if code != 1 {
			t.Errorf("exit code %d, want 1", code)
		}
		wantContains(t, "stderr", stderr, "the podman command was not found", "https://podman.io/docs/installation", "podman machine init", "podman machine start")
		if len(h.fake.runs) != 0 || len(calls) != 1 {
			t.Errorf("runs posted %d, commands %q; want none after podman info", len(h.fake.runs), calls)
		}
	})

	t.Run("podman that does not answer is named", func(t *testing.T) {
		h := newHarness(t)
		h.signIn()
		labFolder(t, h)
		var calls []string
		h.runner = labRunner(&calls, &exec.ExitError{}, "pass")
		code, _, stderr := h.run("test")
		if code != 1 {
			t.Errorf("exit code %d, want 1", code)
		}
		wantContains(t, "stderr", stderr, "podman info failed", "Cannot connect to Podman", "podman machine start")
	})

	t.Run("submit refuses a failing lab", func(t *testing.T) {
		h := newHarness(t)
		h.signIn()
		labFolder(t, h)
		var calls []string
		h.runner = labRunner(&calls, nil, "fail")
		code, stdout, _ := h.run("submit")
		if code != 1 {
			t.Errorf("exit code %d, want 1", code)
		}
		wantContains(t, "zero submit", stdout, "✗ Task 1: Build the image", "Not submitted")
	})
}
