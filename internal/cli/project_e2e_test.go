package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/KalebHawkins/zero/spec"
)

// TestProjectEndToEnd drives a two-stage project against the fake API with
// the real go toolchain: the order lock, the shared folder, a regression,
// zero use, submit, and --reference on an empty folder.
func TestProjectEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("the go command is not on PATH")
	}
	h := newHarness(t)
	if err := h.fake.addContent(life(t)); err != nil {
		t.Fatal(err)
	}
	h.signIn()
	dir := filepath.Join(h.workspace, "life")

	// Stage 2 is locked until stage 1 is passed. The server's sentence is
	// printed, and nothing is written.
	code, _, stderr := h.run("start", "life-2")
	if code != 1 || strings.TrimSpace(stderr) != "Stage 2 of Life opens when stage 1 is passed. Finish it first: zero start life-1" {
		t.Errorf("locked stage: code %d, stderr %q", code, stderr)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("the locked stage wrote a folder")
	}

	// Stage 1 goes into the project folder, not a folder named life-1.
	out := h.ok("start", "life-1")
	wantContains(t, "start life-1", out, "Life, stage 1 of 2: The rule is ready in "+dir, "cd "+dir)
	if _, err := os.Stat(filepath.Join(h.workspace, "life-1")); err == nil {
		t.Error("start wrote a folder named after the stage")
	}
	h.dir = dir

	// The stub returns a zero value: a plain failing test, no regressions.
	code, out, _ = h.run("test")
	if code != 1 {
		t.Fatalf("test on the stub: exit code %d\n%s", code, out)
	}
	wantContains(t, "stage 1 stub", out, "✗ Task 1: Apply the rule", "Next(false, 3) = false, want true",
		"Tasks passed: 0 of 1. Fix the first failing task")
	if strings.Contains(out, "broke") {
		t.Errorf("stage 1 reported a regression:\n%s", out)
	}

	myRules := lifeFile(t, "life-1/solution/rules.go") + "\n// My notes.\n"
	writeFile(t, filepath.Join(dir, "rules.go"), myRules)
	out = h.ok("submit")
	wantContains(t, "submit stage 1", out, "Submitted. The rule is finished.")

	// Stage 2 keeps the learner's rules.go and adds its own files.
	out = h.ok("start", "life-2")
	wantContains(t, "start life-2", out, "Life, stage 2 of 2: Count the neighbors is ready in "+dir,
		"This stage builds on your code from Wrap. Before zero test, copy it in with: zero use wrap\n  wrap.go -> wrap.go\nThis stage's code calls it, so without it the build fails.",
		"  zero use wrap\n  zero test")
	if strings.Contains(out, "Kept your") {
		t.Errorf("stage 2 start says it kept a file, but every file it sends is new:\n%s", out)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "rules.go")); string(got) != myRules {
		t.Errorf("stage 2 replaced the learner's rules.go:\n%s", got)
	}
	for _, name := range []string{"grid.go", "stage2_test.go", "stage1_test.go", "go.mod"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("after stage 2 start: %v", err)
		}
	}

	// The learner breaks stage 1's code. The stage 2 task fails, and the
	// stage 1 test is reported as a regression.
	writeFile(t, filepath.Join(dir, "rules.go"), lifeFile(t, "life-1/starter/rules.go"))
	runs := len(h.fake.runs)
	code, out, _ = h.run("test")
	if code != 1 {
		t.Fatalf("test with a regression: exit code %d\n%s", code, out)
	}
	t.Logf("zero test with a regression:\n%s", out)
	wantContains(t, "regression output", out,
		"✗ Task 1: Count with wrapping edges\n",
		"Stage 1 broke:\n    ✗ Stage 1: TestNext\n        Next(false, 3) = false, want true\n",
		"Tasks passed: 0 of 1. Stage 1 broke. Fix it first, then run: zero test")
	if len(h.fake.runs) != runs+1 {
		t.Fatalf("the run was not posted")
	}
	rep := h.fake.runs[len(h.fake.runs)-1]
	if rep.OK || rep.Exercise != "life-2" || len(rep.Regressions) != 1 || rep.Regressions[0].Name != "TestNext" ||
		rep.Regressions[0].Status != spec.StatusFail || !strings.Contains(rep.Regressions[0].Message, "want true") {
		t.Errorf("report = %+v", rep)
	}
	// With only the regression left, the task passes but ok stays false.
	writeFile(t, filepath.Join(dir, "grid.go"), strings.Replace(lifeFile(t, "life-2/solution/grid.go"), "Wrap(", "wrapTmp(", -1)+
		"\nfunc wrapTmp(i, n int) int { return ((i % n) + n) % n }\n")
	code, out, _ = h.run("test")
	if code != 1 {
		t.Errorf("test with only a regression: exit code %d", code)
	}
	wantContains(t, "regression only", out, "✓ Task 1: Count with wrapping edges", "Tasks passed: 1 of 1. Stage 1 broke.")
	if rep := h.fake.runs[len(h.fake.runs)-1]; rep.OK || rep.Tasks[0].Status != spec.StatusPass {
		t.Errorf("report with only a regression: ok %v, task %q", rep.OK, rep.Tasks[0].Status)
	}
	// Without the copied-in wrap.go, submit names the missing file.
	code, _, stderr = h.run("submit")
	if code != 1 || !strings.Contains(stderr, "Not submitted: wrap.go is missing") {
		t.Errorf("submit without wrap.go: code %d, %q", code, stderr)
	}
	writeFile(t, filepath.Join(dir, "wrap.go"), lifeFile(t, "wrap/solution/wrap.go"))
	code, out, _ = h.run("submit")
	if code != 1 || !strings.Contains(out, "Not submitted") || len(h.fake.submits) != 1 {
		t.Errorf("submit with a regression: code %d, %d submits\n%s", code, len(h.fake.submits), out)
	}
	os.Remove(filepath.Join(dir, "wrap.go"))
	writeFile(t, filepath.Join(dir, "rules.go"), myRules)
	writeFile(t, filepath.Join(dir, "grid.go"), lifeFile(t, "life-2/starter/grid.go"))

	// zero use: list, then each refusal, then the copy.
	out = h.ok("use")
	wantContains(t, "use list", out, "This stage can use:", "wrap", "wrap.go -> wrap.go", "zero use wrap")
	code, _, stderr = h.run("use", "hello-world")
	if code != 1 || !strings.HasPrefix(stderr, "This stage does not use hello-world.") {
		t.Errorf("use of an exercise the stage does not use: code %d, %q", code, stderr)
	}
	code, _, stderr = h.run("use", "wrap")
	wrapDir := filepath.Join(h.workspace, "wrap")
	if code != 1 || !strings.Contains(stderr, wrapDir+" does not exist") || !strings.Contains(stderr, "zero start wrap.") ||
		!strings.Contains(stderr, "zero start life-2 --reference") {
		t.Errorf("use without the exercise folder: code %d, %q", code, stderr)
	}
	h.dir = h.home
	code, _, stderr = h.run("use", "wrap")
	if code != 1 || !strings.HasPrefix(stderr, "zero use works only inside a project stage") {
		t.Errorf("use outside a project: code %d, %q", code, stderr)
	}
	h.ok("start", "wrap")
	h.dir = wrapDir
	code, _, stderr = h.run("use", "wrap")
	if code != 1 || !strings.HasPrefix(stderr, "zero use works only inside a project stage. Wrap is a plain exercise.") {
		t.Errorf("use inside a plain exercise: code %d, %q", code, stderr)
	}
	writeFile(t, filepath.Join(wrapDir, "wrap.go"), lifeFile(t, "wrap/solution/wrap.go"))
	h.dir = dir
	writeFile(t, filepath.Join(dir, "wrap.go"), "package life\n")
	code, out, _ = h.run("use", "wrap")
	if code != 1 || !strings.Contains(out, "Kept your wrap.go.") || !strings.Contains(out, "zero use wrap --force") {
		t.Errorf("use over an existing file: code %d\n%s", code, out)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "wrap.go")); string(got) != "package life\n" {
		t.Error("use overwrote wrap.go without --force")
	}
	os.Remove(filepath.Join(dir, "wrap.go"))
	out = h.ok("use", "wrap")
	wantContains(t, "use output", out, "✓ Copied wrap/wrap.go to wrap.go", "Next: zero test")
	if got, _ := os.ReadFile(filepath.Join(dir, "wrap.go")); string(got) != lifeFile(t, "wrap/solution/wrap.go") {
		t.Errorf("wrap.go = %q", got)
	}

	// Pass stage 2. submit uploads every edit_all file.
	writeFile(t, filepath.Join(dir, "grid.go"), lifeFile(t, "life-2/solution/grid.go"))
	out = h.ok("test")
	wantContains(t, "stage 2 pass", out, "✓ Task 1: Count with wrapping edges", "Tasks passed: 1 of 1. Every test passes. Run: zero submit")
	os.Rename(filepath.Join(dir, "rules.go"), filepath.Join(dir, "rules.go.bak"))
	code, _, stderr = h.run("submit")
	if code != 1 || !strings.Contains(stderr, "rules.go is missing") {
		t.Errorf("submit without rules.go: code %d, %q", code, stderr)
	}
	os.Rename(filepath.Join(dir, "rules.go.bak"), filepath.Join(dir, "rules.go"))
	out = h.ok("submit")
	wantContains(t, "submit stage 2", out, "Submitted. Count the neighbors is finished.")
	sub := h.fake.submits[len(h.fake.submits)-1]
	// The file zero use copied in is the learner's work: it is submitted too.
	want := []spec.File{
		{Path: "rules.go", Content: myRules, Mode: spec.ModeEdit},
		{Path: "grid.go", Content: lifeFile(t, "life-2/solution/grid.go"), Mode: spec.ModeEdit},
		{Path: "wrap.go", Content: lifeFile(t, "wrap/solution/wrap.go"), Mode: spec.ModeEdit},
	}
	if !sub.Report.OK || sub.Report.Exercise != "life-2" || !reflect.DeepEqual(sub.Files, want) {
		t.Errorf("stage 2 submission: report ok %v, files %+v", sub.Report.OK, sub.Files)
	}

	// Starting an earlier stage over a later one is refused: it would put
	// stage 1's given files back over stage 2's.
	code, _, stderr = h.run("start", "life-1")
	if code != 1 || strings.TrimSpace(stderr) != "Stage 2 is ahead of this one in "+dir+"; starting stage 1 would replace newer files. Use --force to do it anyway." {
		t.Errorf("start stage 1 over stage 2: code %d, %q", code, stderr)
	}
	if saved := readSaved(t, dir); saved.Exercise.ID != "life-2" {
		t.Errorf("the refused start changed .zero/exercise.json to %s", saved.Exercise.ID)
	}
	// --reference on stage 1 is a usage error: there is no earlier stage.
	code, _, stderr = h.run("start", "life-1", "--reference")
	if code != 2 || !strings.Contains(stderr, "life-1 is stage 1, so there is no earlier stage") {
		t.Errorf("--reference on stage 1: code %d, %q", code, stderr)
	}
	// With --force it goes ahead, as before.
	h.ok("start", "life-1", "--force")
	if saved := readSaved(t, dir); saved.Exercise.ID != "life-1" {
		t.Errorf("start --force left .zero/exercise.json at %s", saved.Exercise.ID)
	}
	// Restarting the same stage, or a later one, is never refused.
	h.ok("start", "life-1")
	h.ok("start", "life-2")

	// Pass wrap, so the reference can restore the file copied from it.
	h.dir = wrapDir
	h.ok("submit")

	// --reference on an empty folder: a new computer.
	other := filepath.Join(h.home, "elsewhere")
	h.env["ZERO_WORKSPACE"] = other
	h.dir = h.home
	out = h.ok("start", "life-2", "--reference")
	newDir := filepath.Join(other, "life")
	wantContains(t, "reference start", out, "Laid down the reference solution of stage 1: 3 files.",
		"Restored wrap.go, copied in from wrap.", "is ready in "+newDir)
	// The restored wrap.go is done: no zero use step, so no loop back to --reference.
	wantContains(t, "reference start", out, "This stage builds on your code from Wrap. It is already in the project:\n  wrap.go")
	if strings.Contains(out, "zero use") {
		t.Errorf("after --reference the start message still asks for zero use:\n%s", out)
	}
	if got, _ := os.ReadFile(filepath.Join(newDir, "wrap.go")); string(got) != lifeFile(t, "wrap/solution/wrap.go") {
		t.Errorf("--reference did not restore wrap.go: %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(newDir, "rules.go")); string(got) != lifeFile(t, "life-1/solution/rules.go") {
		t.Errorf("rules.go is not the reference: %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(newDir, "grid.go")); string(got) != lifeFile(t, "life-2/starter/grid.go") {
		t.Errorf("grid.go is not the stage 2 stub: %q", got)
	}
	if !reflect.DeepEqual(h.fake.references, []string{"life-1", "life-2"}) {
		t.Errorf("reference requests = %v", h.fake.references)
	}
	// The stage starts from the reference: stage 1 passes, stage 2's task fails.
	h.dir = newDir
	code, out, _ = h.run("test")
	if code != 1 || strings.Contains(out, "broke") || strings.Contains(out, "does not compile") || !strings.Contains(out, "✗ Task 1: Count with wrapping edges") {
		t.Errorf("test after --reference: code %d\n%s", code, out)
	}
	// zero use on the restored entry, with no wrap folder here: the files are done.
	out = h.ok("use", "wrap")
	wantContains(t, "use after --reference", out, "The files from Wrap are already in the project: wrap.go", "Next: zero test")
	out = h.ok("use")
	wantContains(t, "use list after --reference", out, "your code from Wrap, already in the project", "Nothing to copy. Next: zero test")
	code, _, stderr = h.run("use", "wrap", "--force")
	if code != 1 || !strings.Contains(stderr, "does not exist") {
		t.Errorf("use --force without the exercise folder: code %d, %q", code, stderr)
	}
	// A second --reference refuses to replace the learner's work.
	writeFile(t, filepath.Join(newDir, "rules.go"), myRules)
	code, _, stderr = h.run("start", "life-2", "--reference")
	if code != 1 || !strings.Contains(stderr, "hold your work: rules.go") || !strings.Contains(stderr, "zero start life-2 --reference --force") {
		t.Errorf("reference over work: code %d, %q", code, stderr)
	}
	if got, _ := os.ReadFile(filepath.Join(newDir, "rules.go")); string(got) != myRules {
		t.Error("--reference replaced rules.go without --force")
	}
	h.ok("start", "life-2", "--reference", "--force")
	if got, _ := os.ReadFile(filepath.Join(newDir, "rules.go")); string(got) != lifeFile(t, "life-1/solution/rules.go") {
		t.Error("--reference --force did not lay down rules.go")
	}
	code, _, stderr = h.run("start", "hello-world", "--reference")
	if code != 2 || !strings.Contains(stderr, "is not a project stage") {
		t.Errorf("--reference on a plain exercise: code %d, %q", code, stderr)
	}
}

func readSaved(t *testing.T, dir string) spec.Saved {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, ".zero", "exercise.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved spec.Saved
	if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	return saved
}

// TestUsesWithoutCopy checks a uses entry with no copy list: the stage builds
// on what that exercise taught, and there is nothing to copy (SPEC section 6).
func TestUsesWithoutCopy(t *testing.T) {
	h := newHarness(t)
	if err := h.fake.addContent(life(t)); err != nil {
		t.Fatal(err)
	}
	two := h.fake.entries["life-2"]
	two.uses = []spec.Use{{Exercise: "wrap"}}
	two.exercise.Edit = []string{"grid.go"}
	h.fake.passedIDs["life-1"] = true
	h.signIn()
	dir := filepath.Join(h.workspace, "life")

	out := h.ok("start", "life-2")
	wantContains(t, "start", out, "This stage builds on Wrap. There is nothing to copy.", "  cd "+dir+"\n  zero test")
	if strings.Contains(out, "zero use") || strings.Contains(out, "copy it in") {
		t.Errorf("start offers zero use for an entry with nothing to copy:\n%s", out)
	}
	h.dir = dir
	out = h.ok("use")
	wantContains(t, "use list", out, "wrap  builds on Wrap, nothing to copy", "Nothing to copy. Next: zero test")
	if strings.Contains(out, "To copy the files") {
		t.Errorf("use list offers a copy:\n%s", out)
	}
	out = h.ok("use", "wrap")
	if strings.TrimSpace(out) != "This stage builds on what Wrap taught, so there is nothing to copy." {
		t.Errorf("use wrap = %q", out)
	}
}
