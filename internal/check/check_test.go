package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KalebHawkins/zero/spec"
)

func parseFixture(t *testing.T, name string) *Result {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	res, err := Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

var hello = spec.Exercise{
	ID: "hello-world", Title: "Hello, World", Checker: spec.CheckerGoTest,
	Tasks: []spec.Task{{N: 1, Title: "Say hello", Tests: []string{"TestHello"}}},
}

var meta = Meta{
	At:       time.Date(2026, 10, 4, 10, 4, 5, 0, time.FixedZone("CDT", -5*3600)),
	Duration: 812 * time.Millisecond,
	CLI:      "0.1.0",
}

func TestParsePassingRun(t *testing.T) {
	res := parseFixture(t, "pass.json")
	if res.BuildFailed {
		t.Error("BuildFailed is true for a passing run")
	}
	got := res.Tests["TestHello"]
	if got == nil || got.Status != spec.StatusPass {
		t.Fatalf("TestHello = %+v, want pass", got)
	}
	if len(res.Tests) != 1 {
		t.Errorf("%d tests, want 1", len(res.Tests))
	}

	rep := Report(hello, res, "", false, meta)
	want := spec.Report{
		Exercise: "hello-world", Checker: "go-test", OK: true,
		At: "2026-10-04T15:04:05Z", DurationMS: 812, CLI: "0.1.0",
	}
	if rep.Exercise != want.Exercise || rep.Checker != want.Checker || rep.OK != want.OK ||
		rep.At != want.At || rep.DurationMS != want.DurationMS || rep.CLI != want.CLI || rep.BuildError != "" {
		t.Errorf("report = %+v", rep)
	}
	if len(rep.Tasks) != 1 || rep.Tasks[0].Status != spec.StatusPass || rep.Tasks[0].N != 1 || rep.Tasks[0].Title != "Say hello" {
		t.Fatalf("tasks = %+v", rep.Tasks)
	}
	if got := rep.Tasks[0].Tests; len(got) != 1 || got[0] != (spec.TestResult{Name: "TestHello", Status: spec.StatusPass}) {
		t.Errorf("tests = %+v", got)
	}
}

func TestParseFailingRun(t *testing.T) {
	res := parseFixture(t, "fail.json")
	if res.BuildFailed {
		t.Error("BuildFailed is true for a run that compiled")
	}
	rep := Report(hello, res, "", true, meta)
	if rep.OK || rep.BuildError != "" {
		t.Errorf("ok = %v, build_error = %q", rep.OK, rep.BuildError)
	}
	if rep.Tasks[0].Status != spec.StatusFail {
		t.Errorf("task status = %q, want fail", rep.Tasks[0].Status)
	}
	// The message is the test's own line: no "--- FAIL", no file and line.
	want := spec.TestResult{Name: "TestHello", Status: spec.StatusFail,
		Message: `Hello() = "Goodbye, World", want "Hello, World"`}
	if got := rep.Tasks[0].Tests[0]; got != want {
		t.Errorf("test = %+v\nwant   %+v", got, want)
	}
}

func TestParseCompileError(t *testing.T) {
	res := parseFixture(t, "compile-error.json")
	if !res.BuildFailed {
		t.Fatal("BuildFailed is false for a compile error")
	}
	two := spec.Exercise{ID: "x", Checker: spec.CheckerGoTest, Tasks: []spec.Task{
		{N: 1, Title: "One", Tests: []string{"TestHello"}},
		{N: 2, Title: "Two", Tests: []string{"TestA", "TestB"}},
	}}
	rep := Report(two, res, "", true, meta)
	if rep.OK {
		t.Error("ok is true for a compile error")
	}
	if rep.BuildError != "./hello.go:6:9: undefined: greeting" {
		t.Errorf("build_error = %q", rep.BuildError)
	}
	for _, task := range rep.Tasks {
		if task.Status != spec.StatusFail {
			t.Errorf("task %d status = %q, want fail", task.N, task.Status)
		}
		for _, test := range task.Tests {
			if test.Status != spec.StatusFail || test.Message != spec.MessageDidNotCompile {
				t.Errorf("test %s = %+v", test.Name, test)
			}
		}
	}
}

// Go 1.22 and 1.23 print the compiler text on standard error.
func TestCompileErrorFromStderr(t *testing.T) {
	res := parseFixture(t, "compile-error-go122.json")
	if !res.BuildFailed {
		t.Fatal("BuildFailed is false")
	}
	stderr, err := os.ReadFile(filepath.Join("testdata", "compile-error-go122.stderr"))
	if err != nil {
		t.Fatal(err)
	}
	rep := Report(hello, res, string(stderr), true, meta)
	if rep.OK || rep.BuildError != "./hello.go:6:9: undefined: greeting" {
		t.Errorf("ok = %v, build_error = %q", rep.OK, rep.BuildError)
	}
}

// `go test` can fail before it writes one event, for example when go.mod is
// missing. That is a build error too.
func TestFailureWithoutEvents(t *testing.T) {
	res, err := Parse(strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	rep := Report(hello, res, "go: cannot find main module\n", true, meta)
	if rep.OK || rep.BuildError != "go: cannot find main module" || rep.Tasks[0].Status != spec.StatusFail {
		t.Errorf("report = %+v", rep)
	}
}

func TestTaskMapping(t *testing.T) {
	res := parseFixture(t, "subtests.json")
	ex := spec.Exercise{ID: "calc", Checker: spec.CheckerGoTest, Tasks: []spec.Task{
		{N: 1, Title: "Add", Tests: []string{"TestAdd"}},
		{N: 2, Title: "Skip", Tests: []string{"TestSkip"}},
		{N: 3, Title: "Messages", Tests: []string{"TestMulti", "TestDiv"}},
		{N: 4, Title: "After the panic", Tests: []string{"TestAfter"}},
		{N: 5, Title: "Not in the file", Tests: []string{"TestMissing"}},
	}}
	rep := Report(ex, res, "", true, meta)
	if rep.OK {
		t.Error("ok is true")
	}
	if rep.BuildError != "" {
		t.Errorf("build_error = %q", rep.BuildError)
	}
	wantStatus := []string{spec.StatusFail, spec.StatusSkip, spec.StatusFail, spec.StatusFail, spec.StatusFail}
	for i, task := range rep.Tasks {
		if task.Status != wantStatus[i] {
			t.Errorf("task %d status = %q, want %q", task.N, task.Status, wantStatus[i])
		}
	}

	msg := func(task, test int) string { return rep.Tasks[task].Tests[test].Message }

	// A parent test reports its failing subtests by name. The passing subtest
	// "zero" is left out.
	wantAdd := "small: checking 1 + 2\n  Add(1, 2) = -1, want 3\nbig: checking 10 + 20\n  Add(10, 20) = -10, want 30"
	if got := msg(0, 0); got != wantAdd {
		t.Errorf("TestAdd message:\n%s\nwant:\n%s", got, wantAdd)
	}
	if got := msg(1, 0); got != "not yet" {
		t.Errorf("TestSkip message = %q", got)
	}
	if got := msg(2, 0); got != "first line\n  second line\nthird line" {
		t.Errorf("TestMulti message = %q", got)
	}
	// A panic keeps its first line and the place in the learner's code. The
	// goroutine trace is dropped.
	wantDiv := "panic: runtime error: integer divide by zero [recovered, repanicked]\nat calc.go:7"
	if got := msg(2, 1); got != wantDiv {
		t.Errorf("TestDiv message = %q, want %q", got, wantDiv)
	}
	for _, task := range []int{3, 4} {
		got := rep.Tasks[task].Tests[0]
		if got.Status != spec.StatusFail || got.Message != spec.MessageDidNotRun {
			t.Errorf("task %d test = %+v, want fail with %q", task+1, got, spec.MessageDidNotRun)
		}
	}
}

// A task passes only when all of its tests pass.
func TestTaskNeedsAllTests(t *testing.T) {
	res := parseFixture(t, "pass.json")
	ex := spec.Exercise{ID: "x", Checker: spec.CheckerGoTest, Tasks: []spec.Task{
		{N: 1, Title: "Both", Tests: []string{"TestHello", "TestOther"}},
		{N: 2, Title: "One", Tests: []string{"TestHello"}},
	}}
	rep := Report(ex, res, "", false, meta)
	if rep.OK {
		t.Error("ok is true although a test did not run")
	}
	if rep.Tasks[0].Status != spec.StatusFail || rep.Tasks[1].Status != spec.StatusPass {
		t.Errorf("statuses = %q, %q; want fail, pass", rep.Tasks[0].Status, rep.Tasks[1].Status)
	}
	if got := rep.Tasks[0].Tests[0].Status; got != spec.StatusPass {
		t.Errorf("TestHello in task 1 = %q, want pass", got)
	}
}

func TestParseKeepsLinesThatAreNotJSON(t *testing.T) {
	in := "# hello\n./hello.go:1:1: syntax error\n" + `{"Action":"start","Package":"hello"}` + "\n"
	res, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Other, "syntax error") {
		t.Errorf("Other = %q", res.Other)
	}
	rep := Report(hello, res, "", true, meta)
	if rep.BuildError != "./hello.go:1:1: syntax error" {
		t.Errorf("build_error = %q", rep.BuildError)
	}
}
