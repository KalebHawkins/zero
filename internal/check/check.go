// Package check runs an exercise's tests and builds the Report.
//
// The only checker is "go-test": it runs `go test -json ./...` in the
// exercise folder and reads the stream of JSON events.
//
// Starter files return zero values, so the usual first run is a set of
// plain failing tests. A test binary that stops early, after a panic, is
// still handled: the tests it never ran are reported as not run.
package check

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/KalebHawkins/zero/spec"
)

// ErrNoGo means the go command is not on PATH.
var ErrNoGo = errors.New("the go command was not found")

// maxMessageLines limits the message of one test.
const maxMessageLines = 30

// event is one line of `go test -json` output.
type event struct {
	Action      string `json:"Action"`
	Package     string `json:"Package"`
	ImportPath  string `json:"ImportPath"`
	Test        string `json:"Test"`
	Output      string `json:"Output"`
	FailedBuild string `json:"FailedBuild"`
}

// Test is what one test (or subtest) did.
type Test struct {
	Name   string
	Status string   // "pass", "fail", "skip", or "" when the test never finished
	Output []string // its output lines, in order
}

// Result is a parsed `go test -json` stream.
type Result struct {
	Tests       map[string]*Test
	Order       []string // test names in the order they first appeared
	BuildFailed bool
	BuildOutput string // compiler output from build-output events
	Other       string // lines that are not JSON, and package output of failed packages
}

// Parse reads a `go test -json` stream. Lines that are not JSON events are
// kept in Other: older Go versions print compile errors that way.
func Parse(r io.Reader) (*Result, error) {
	res := &Result{Tests: map[string]*Test{}}
	var build, other strings.Builder
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			res.add(line, &build, &other)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	res.BuildOutput = build.String()
	res.Other = other.String()
	return res, nil
}

func (res *Result) add(line string, build, other *strings.Builder) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return
	}
	var ev event
	if !strings.HasPrefix(trimmed, "{") || json.Unmarshal([]byte(trimmed), &ev) != nil || ev.Action == "" {
		other.WriteString(strings.TrimRight(line, "\r\n") + "\n")
		return
	}
	switch ev.Action {
	case "build-output":
		build.WriteString(ev.Output)
		return
	case "build-fail":
		res.BuildFailed = true
		return
	}
	if ev.Test == "" {
		// A package event.
		switch ev.Action {
		case "fail":
			if ev.FailedBuild != "" {
				res.BuildFailed = true
			}
		case "output":
			if strings.Contains(ev.Output, "[build failed]") || strings.Contains(ev.Output, "[setup failed]") {
				res.BuildFailed = true
			}
		}
		return
	}
	// The same test name in two packages is one test here: a failure wins.
	t := res.Tests[ev.Test]
	if t == nil {
		t = &Test{Name: ev.Test}
		res.Tests[ev.Test] = t
		res.Order = append(res.Order, ev.Test)
	}
	switch ev.Action {
	case "output":
		t.Output = append(t.Output, ev.Output)
	case "pass", "skip":
		if t.Status != spec.StatusFail {
			t.Status = ev.Action
		}
	case "fail":
		t.Status = spec.StatusFail
	}
}

// Meta is the part of a Report that does not come from the tests.
type Meta struct {
	At       time.Time
	Duration time.Duration
	CLI      string
}

// Report maps a parsed run onto the tasks of an exercise. stderr is what
// `go test` wrote to standard error; failed says whether it exited with an
// error.
func Report(ex spec.Exercise, res *Result, stderr string, failed bool, m Meta) spec.Report {
	rep := spec.Report{
		Exercise:   ex.ID,
		Checker:    ex.Checker,
		At:         m.At.UTC().Format(time.RFC3339),
		DurationMS: m.Duration.Milliseconds(),
		CLI:        m.CLI,
		Tasks:      []spec.TaskResult{},
	}

	// `go test` failed without one test result: nothing was built or run.
	buildFailed := res.BuildFailed || (failed && len(res.Tests) == 0)
	if buildFailed {
		rep.BuildError = buildError(res, stderr)
	}

	rep.OK = !buildFailed && len(ex.Tasks) > 0
	for _, task := range ex.Tasks {
		tr := spec.TaskResult{N: task.N, Title: task.Title, Status: spec.StatusPass, Tests: []spec.TestResult{}}
		if len(task.Tests) == 0 {
			tr.Status = spec.StatusFail
		}
		for _, name := range task.Tests {
			var t spec.TestResult
			if buildFailed {
				t = spec.TestResult{Name: name, Status: spec.StatusFail, Message: spec.MessageDidNotCompile}
			} else {
				t = testResult(res, name)
			}
			tr.Tests = append(tr.Tests, t)
			switch {
			case t.Status == spec.StatusFail:
				tr.Status = spec.StatusFail
			case t.Status == spec.StatusSkip && tr.Status != spec.StatusFail:
				tr.Status = spec.StatusSkip
			}
		}
		if tr.Status != spec.StatusPass {
			rep.OK = false
		}
		rep.Tasks = append(rep.Tasks, tr)
	}
	return rep
}

// ProjectReport is Report for a stage of a project. The tests named in the
// stage's tasks decide the tasks, as in Report. Every other test that
// failed, or started and never finished, is a regression: an earlier stage
// broke. A regression makes the report not OK. When the code does not
// compile, there are no regressions: every task fails already.
func ProjectReport(ex spec.Exercise, res *Result, stderr string, failed bool, m Meta) spec.Report {
	rep := Report(ex, res, stderr, failed, m)
	if rep.BuildError != "" {
		return rep
	}
	rep.Regressions = Regressions(ex, res)
	if len(rep.Regressions) > 0 {
		rep.OK = false
	}
	return rep
}

// Regressions lists the failing top-level tests that no task of ex names,
// in the order they ran. A subtest counts through its parent.
func Regressions(ex spec.Exercise, res *Result) []spec.TestResult {
	named := map[string]bool{}
	for _, task := range ex.Tasks {
		for _, name := range task.Tests {
			named[name] = true
		}
	}
	var out []spec.TestResult
	for _, name := range res.Order {
		if named[name] || strings.Contains(name, "/") {
			continue
		}
		switch res.Tests[name].Status {
		case spec.StatusPass, spec.StatusSkip:
			continue
		}
		out = append(out, testResult(res, name))
	}
	return out
}

func testResult(res *Result, name string) spec.TestResult {
	t := res.Tests[name]
	if t == nil {
		return spec.TestResult{Name: name, Status: spec.StatusFail, Message: spec.MessageDidNotRun}
	}
	out := spec.TestResult{Name: name, Status: t.Status}
	switch t.Status {
	case spec.StatusPass:
		return out
	case spec.StatusSkip:
		out.Message = cleanMessage(t.Output)
		return out
	}
	// Failed, or never finished.
	out.Status = spec.StatusFail
	lines := append([]string(nil), t.Output...)
	for _, sub := range res.Order {
		st := res.Tests[sub]
		if !strings.HasPrefix(sub, name+"/") || st.Status == spec.StatusPass || st.Status == spec.StatusSkip {
			continue
		}
		// Only the deepest failing subtests carry messages of their own.
		msg := cleanMessage(st.Output)
		if msg == "" {
			continue
		}
		label := strings.TrimPrefix(sub, name+"/")
		lines = append(lines, label+": "+strings.ReplaceAll(msg, "\n", "\n  ")+"\n")
	}
	out.Message = cleanMessage(lines)
	if out.Message == "" {
		if t.Status == "" {
			out.Message = "this test did not finish"
		} else {
			out.Message = "the test failed without a message"
		}
	}
	return out
}

var (
	// "    hello_test.go:8: " at the start of a line written by t.Errorf.
	locationPrefix = regexp.MustCompile(`^\s*[^\s:]+\.go:\d+: `)
	// "\t/home/me/zero/x/calc.go:7 +0x1d" in a panic trace.
	traceLocation = regexp.MustCompile(`^\t(\S+\.go):(\d+)`)
	goroutineLine = regexp.MustCompile(`^goroutine \d+ \[`)
)

// noise reports whether a line is `go test` framing and not the test's own
// message.
func noise(line string) bool {
	s := strings.TrimSpace(line)
	for _, p := range []string{"=== RUN", "=== PAUSE", "=== CONT", "=== NAME", "--- FAIL", "--- PASS", "--- SKIP", "FAIL\t", "ok  \t", "exit status "} {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return s == "FAIL" || s == "PASS"
}

// cleanMessage turns the raw output lines of a test into its message: no
// `--- FAIL` framing, no file:line prefix, no goroutine trace.
func cleanMessage(raw []string) string {
	var lines []string
	for i := 0; i < len(raw); i++ {
		for _, line := range strings.Split(strings.TrimRight(raw[i], "\r\n"), "\n") {
			lines = append(lines, line)
		}
	}
	var out []string
	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], " \t\r")
		if noise(line) {
			continue
		}
		if goroutineLine.MatchString(line) {
			// A panic trace follows. Keep only where the learner's code was.
			for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
				out = out[:len(out)-1]
			}
			if at := panicLocation(lines[i+1:]); at != "" {
				out = append(out, "at "+at)
			}
			break
		}
		if loc := locationPrefix.FindString(line); loc != "" {
			line = line[len(loc):]
		} else {
			// t.Errorf indents the later lines of a message by eight spaces.
			line = strings.TrimPrefix(line, "        ")
		}
		out = append(out, line)
	}
	for len(out) > 0 && strings.TrimSpace(out[0]) == "" {
		out = out[1:]
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	if len(out) > maxMessageLines {
		more := len(out) - maxMessageLines
		out = append(out[:maxMessageLines], fmt.Sprintf("(%d more lines)", more))
	}
	return strings.Join(out, "\n")
}

// panicLocation finds the first frame of a goroutine trace that is not in
// the Go standard library and returns it as "file.go:line".
func panicLocation(trace []string) string {
	for _, line := range trace {
		m := traceLocation.FindStringSubmatch(strings.TrimRight(line, "\r\n"))
		if m == nil {
			continue
		}
		file := strings.ReplaceAll(m[1], `\`, "/")
		if strings.Contains(file, "/src/testing/") || strings.Contains(file, "/src/runtime/") {
			continue
		}
		return path.Base(file) + ":" + m[2]
	}
	return ""
}

// buildError picks the compiler output: the build-output events of newer Go
// versions, or standard error and stray lines of older ones.
func buildError(res *Result, stderr string) string {
	text := res.BuildOutput
	if strings.TrimSpace(text) == "" {
		text = stderr
	}
	if strings.TrimSpace(text) == "" {
		text = res.Other
	}
	// The same error comes once for each package that builds the broken file
	// (the package, its test binary, the packages that import it, and vet).
	// The learner sees each line once, in the order it first came.
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, " \t\r")
		// "# hello [hello.test]" names the package; "FAIL ..." repeats the result.
		if strings.HasPrefix(line, "# ") || strings.HasPrefix(line, "FAIL") {
			continue
		}
		if strings.TrimSpace(line) == "" {
			if len(out) > 0 && out[len(out)-1] != "" {
				out = append(out, "")
			}
			continue
		}
		if seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, line)
	}
	text = strings.TrimSpace(strings.Join(out, "\n"))
	if text == "" {
		text = "go test could not build the code and printed no reason"
	}
	return text
}

// GoTest runs `go test -json ./...` in dir and returns the Report. project
// says that dir is a project folder: failing tests outside the current
// stage's tasks are then reported as regressions.
func GoTest(dir string, ex spec.Exercise, project bool, cliVersion string, now func() time.Time) (spec.Report, error) {
	if _, err := exec.LookPath("go"); err != nil {
		return spec.Report{}, ErrNoGo
	}
	start := now()
	cmd := exec.Command("go", "test", "-json", "./...")
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	var exitErr *exec.ExitError
	if runErr != nil && !errors.As(runErr, &exitErr) {
		return spec.Report{}, fmt.Errorf("go test did not start: %w", runErr)
	}
	res, err := Parse(&stdout)
	if err != nil {
		return spec.Report{}, err
	}
	meta := Meta{At: start, Duration: now().Sub(start), CLI: cliVersion}
	if project {
		return ProjectReport(ex, res, stderr.String(), runErr != nil, meta), nil
	}
	return Report(ex, res, stderr.String(), runErr != nil, meta), nil
}
