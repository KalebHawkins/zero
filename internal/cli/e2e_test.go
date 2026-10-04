package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/KalebHawkins/zero/internal/config"
	"github.com/KalebHawkins/zero/spec"
)

// TestEndToEnd drives a learner's first session against the fake API:
// login, doctor, start, a failing test, a fix, a passing test, submit. It
// runs the real go toolchain on the hello-world files.
func TestEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("the go command is not on PATH")
	}
	h := newHarness(t)
	h.fake.pendingPolls = 1 // the first poll answers 202, the second 200
	h.env["ZERO_NO_BROWSER"] = "1"

	// Before login.
	code, _, stderr := h.run("whoami")
	if code != 1 || strings.TrimSpace(stderr) != "Not signed in. Run: zero login" {
		t.Fatalf("whoami before login: code %d, stderr %q", code, stderr)
	}

	// login
	out := h.ok("login")
	wantContains(t, "login output", out,
		"Your code is: ABCD-EFGH",
		"Approve it on this page: "+h.fake.siteRoot+"/cli/?code=ABCD-EFGH",
		"Signed in as kryo.",
		"Next: zero doctor")
	if len(h.opened) != 0 {
		t.Errorf("a browser was opened although ZERO_NO_BROWSER=1: %v", h.opened)
	}
	if len(h.slept) != 2 || h.slept[0] != 2*time.Second {
		t.Errorf("slept %v, want two waits of the server's interval, 2s", h.slept)
	}
	if len(h.fake.devices) != 1 || h.fake.devices[0].Version != testVersion || h.fake.devices[0].OS != runtime.GOOS {
		t.Errorf("device request = %+v", h.fake.devices)
	}
	raw, err := os.ReadFile(h.configPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved config.File
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatalf("the config file is not JSON: %v", err)
	}
	// The token is saved under the site it came from.
	want := config.SiteToken{Token: h.fake.token, Login: "kryo"}
	if len(saved.Tokens) != 1 || saved.Tokens[h.fake.siteRoot] != want || saved.Token != "" {
		t.Errorf("config file = %s", raw)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(h.configPath)
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Errorf("config file mode = %o, want 600", mode)
		}
	}

	out = h.ok("whoami")
	wantContains(t, "whoami output", out, "Signed in as kryo (Kaleb) on "+h.fake.siteRoot)

	// doctor --report posts the Doctor. Its exit code depends on this
	// computer, so only the content is checked.
	_, out, _ = h.run("doctor", "--report")
	wantContains(t, "doctor report", out,
		"zero doctor report",
		"zero:    "+testVersion,
		"os:      "+runtime.GOOS+"/"+runtime.GOARCH,
		"[ok]   go: go",
		"[ok]   workspace: "+h.workspace+" is writable",
		"[ok]   api: "+h.fake.siteRoot+" answers")
	if len(h.fake.doctors) != 1 {
		t.Fatalf("the fake got %d doctor posts, want 1", len(h.fake.doctors))
	}
	doc := h.fake.doctors[0]
	names := []string{}
	for _, c := range doc.Checks {
		names = append(names, c.Name)
		if !c.OK && c.Fix == "" {
			t.Errorf("check %s failed without a fix", c.Name)
		}
	}
	if got := strings.Join(names, " "); got != "go git podman cc editor workspace api" {
		t.Errorf("checks = %q", got)
	}
	if doc.CLI != testVersion || doc.At != "2026-10-04T15:04:09Z" {
		t.Errorf("doctor cli = %q, at = %q", doc.CLI, doc.At)
	}

	// start
	out = h.ok("start", "hello-world")
	dir := filepath.Join(h.workspace, "hello-world")
	wantContains(t, "start output", out, "Hello, World is ready in "+dir, "cd "+dir, "zero test")
	for _, name := range []string{"hello.go", "hello_test.go", "go.mod", "cmd/hello/main.go", "README.txt", ".zero/exercise.json"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name))); err != nil {
			t.Errorf("start did not write %s: %v", name, err)
		}
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "hello.go")); string(got) != fixture(t, "starter/hello.go") {
		t.Errorf("hello.go is not the starter file:\n%s", got)
	}
	if len(h.fake.started) != 1 {
		t.Errorf("the fake saw %d starts, want 1", len(h.fake.started))
	}

	// test fails on the starter. The command finds the exercise from a
	// subfolder too.
	h.dir = filepath.Join(dir, "cmd", "hello")
	code, out, stderr = h.run("test")
	if code != 1 {
		t.Fatalf("test on the starter: exit code %d, want 1\n%s\n%s", code, out, stderr)
	}
	wantContains(t, "failing test output", out,
		"Hello, World\n",
		"✗ Task 1: Say hello\n",
		"    ✗ TestHello\n",
		`        Hello() = "Goodbye, World", want "Hello, World"`+"\n",
		"Tasks passed: 0 of 1. Fix the first failing task, then run: zero test")
	if strings.Contains(out, "--- FAIL") || strings.Contains(out, "hello_test.go:") {
		t.Errorf("the output still has go test noise:\n%s", out)
	}
	if stderr != "" {
		t.Errorf("stderr = %q", stderr)
	}
	if len(h.fake.runs) != 1 {
		t.Fatalf("the fake got %d runs, want 1", len(h.fake.runs))
	}
	wantFail := spec.Report{
		Exercise: "hello-world", Checker: "go-test", OK: false,
		At: "2026-10-04T15:04:09Z", CLI: testVersion,
		Tasks: []spec.TaskResult{{N: 1, Title: "Say hello", Status: "fail",
			Tests: []spec.TestResult{{Name: "TestHello", Status: "fail",
				Message: `Hello() = "Goodbye, World", want "Hello, World"`}}}},
	}
	if got, want := asJSON(t, h.fake.runs[0]), asJSON(t, wantFail); got != want {
		t.Errorf("failing report:\n%s\nwant:\n%s", got, want)
	}
	h.dir = dir

	// submit refuses while the tests fail and posts nothing.
	code, out, _ = h.run("submit")
	if code != 1 || len(h.fake.submits) != 0 || len(h.fake.runs) != 1 {
		t.Errorf("submit on the starter: code %d, %d submits, %d runs", code, len(h.fake.submits), len(h.fake.runs))
	}
	wantContains(t, "refused submit output", out, "✗ Task 1: Say hello", "Not submitted: the tests do not pass yet.")

	// hint: one per call, then it wraps around.
	out = h.ok("hint")
	wantContains(t, "first hint", out, "Hint 1 of 1: Where to look", "Open hello.go. The text between the quotes is what Hello returns.")
	out = h.ok("hint")
	wantContains(t, "second hint", out, "You have seen every hint. Starting again from the first.", "Hint 1 of 1: Where to look")

	// A compile error fills build_error and fails every task.
	broken := strings.Replace(fixture(t, "starter/hello.go"), `"Goodbye, World"`, "greeting", 1)
	writeFile(t, filepath.Join(dir, "hello.go"), broken)
	code, out, _ = h.run("test")
	if code != 1 {
		t.Errorf("test on code that does not compile: exit code %d, want 1", code)
	}
	wantContains(t, "compile error output", out,
		"The code does not compile:",
		"    ./hello.go:6:9: undefined: greeting",
		"✗ Task 1: Say hello",
		"Tasks passed: 0 of 1. Fix the compile error, then run: zero test")
	if last := h.fake.runs[len(h.fake.runs)-1]; last.OK || !strings.Contains(last.BuildError, "undefined: greeting") || last.Tasks[0].Status != "fail" {
		t.Errorf("compile error report = %+v", last)
	}

	// The learner fixes the file. test passes.
	writeFile(t, filepath.Join(dir, "hello.go"), fixture(t, "solution/hello.go"))
	out = h.ok("test")
	wantContains(t, "passing test output", out,
		"Hello, World\n",
		"✓ Task 1: Say hello\n",
		"Tasks passed: 1 of 1. Every test passes. Run: zero submit")
	if last := h.fake.runs[len(h.fake.runs)-1]; !last.OK || last.BuildError != "" || last.Tasks[0].Status != "pass" || last.Tasks[0].Tests[0].Status != "pass" {
		t.Errorf("passing report = %+v", last)
	}
	runsBeforeSubmit := len(h.fake.runs)

	// run starts the exercise's program.
	out = h.ok("run")
	if out != "Hello, World\n" {
		t.Errorf("run printed %q, want the program's output", out)
	}

	// next still names this exercise.
	out = h.ok("next")
	wantContains(t, "next before submit", out, "Next: Hello, World. Run: zero start hello-world")

	// submit
	out = h.ok("submit")
	wantContains(t, "submit output", out,
		"✓ Task 1: Say hello",
		"Submitted. Hello, World is finished.",
		"Next: Variables. Run: zero start variables")
	if len(h.fake.submits) != 1 {
		t.Fatalf("the fake got %d submits, want 1", len(h.fake.submits))
	}
	sub := h.fake.submits[0]
	if !sub.Report.OK || sub.Report.Exercise != "hello-world" {
		t.Errorf("submitted report = %+v", sub.Report)
	}
	wantFile := spec.File{Path: "hello.go", Content: fixture(t, "solution/hello.go"), Mode: "edit"}
	if len(sub.Files) != 1 || sub.Files[0] != wantFile {
		t.Errorf("submitted files = %+v, want only the edited hello.go", sub.Files)
	}
	if len(h.fake.runs) != runsBeforeSubmit {
		t.Errorf("submit also posted a run")
	}

	out = h.ok("next")
	wantContains(t, "next after submit", out, "Next: Variables. Run: zero start variables")

	// Starting again keeps the learner's file; --force replaces it.
	out = h.ok("start", "hello-world")
	wantContains(t, "second start", out, "Kept your hello.go.", "zero start hello-world --force")
	if got, _ := os.ReadFile(filepath.Join(dir, "hello.go")); string(got) != fixture(t, "solution/hello.go") {
		t.Errorf("a second start overwrote hello.go")
	}
	h.ok("start", "--force", "hello-world")
	if got, _ := os.ReadFile(filepath.Join(dir, "hello.go")); string(got) != fixture(t, "starter/hello.go") {
		t.Errorf("start --force did not restore the starter hello.go")
	}

	// Every request named the command and its version.
	if len(h.fake.userAgents) != 1 || !h.fake.userAgents["zero/"+testVersion] {
		t.Errorf("User-Agent values = %v, want only zero/%s", h.fake.userAgents, testVersion)
	}

	// logout ends the token on the site and forgets it here.
	out = h.ok("logout")
	wantContains(t, "logout output", out, "Signed out of "+h.fake.siteRoot)
	raw, _ = os.ReadFile(h.configPath)
	if strings.Contains(string(raw), h.fake.token) {
		t.Errorf("the token is still in the config file: %s", raw)
	}
	if len(h.fake.logouts) != 1 || h.fake.logouts[0] != "Bearer "+h.fake.token || !h.fake.revoked {
		t.Errorf("logout requests = %q, revoked = %v; want one with the token", h.fake.logouts, h.fake.revoked)
	}
	code, _, stderr = h.run("next")
	if code != 1 || strings.TrimSpace(stderr) != "Not signed in. Run: zero login" {
		t.Errorf("next after logout: code %d, stderr %q", code, stderr)
	}

	// With the site down, test still works and prints one warning line.
	h.env["ZERO_TOKEN"] = h.fake.token
	h.srv.Close()
	writeFile(t, filepath.Join(dir, "hello.go"), fixture(t, "solution/hello.go"))
	code, out, stderr = h.run("test")
	if code != 0 {
		t.Errorf("test with the site down: exit code %d, want 0", code)
	}
	wantContains(t, "offline test output", out, "✓ Task 1: Say hello")
	if lines := strings.Split(strings.TrimSpace(stderr), "\n"); len(lines) != 1 || !strings.HasPrefix(lines[0], "Warning: the result was not sent to the site.") {
		t.Errorf("stderr with the site down = %q, want one warning line", stderr)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// asJSON renders a report without its duration, which changes every run.
func asJSON(t *testing.T, r spec.Report) string {
	t.Helper()
	r.DurationMS = 0
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
