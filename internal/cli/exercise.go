package cli

import (
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strings"

	"github.com/KalebHawkins/zero/internal/api"
	"github.com/KalebHawkins/zero/internal/check"
	"github.com/KalebHawkins/zero/internal/workspace"
	"github.com/KalebHawkins/zero/spec"
)

// start gets an exercise and writes it into the workspace.
func (a *app) start() error {
	force := a.takeFlag("--force")
	if len(a.args) != 1 || strings.HasPrefix(a.args[0], "-") {
		return usage("zero start needs one exercise id. Example: zero start hello-world")
	}
	id := a.args[0]
	cfg, client, err := a.signedIn()
	if err != nil {
		return err
	}
	resp, err := client.Exercise(id)
	var apiErr *api.Error
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
		return fail("%s", joinSentence(apiErr.Message, "Copy the id from the exercise page on "+cfg.APIURL+"."))
	}
	if err != nil {
		return err
	}
	if resp.Exercise.ID == "" {
		return fail("%s sent no exercise for %q. Check that api_url is the site address: zero config list", cfg.APIURL, id)
	}
	res, err := workspace.Write(cfg.Workspace, resp, force)
	if err != nil {
		return fail("Cannot write the exercise into %s: %v. Choose a folder you can write with: zero config set workspace <folder>", cfg.Workspace, err)
	}

	a.out.line("%s is ready in %s", a.out.bold(resp.Exercise.Title), res.Dir)
	for _, p := range res.Kept {
		a.out.line("Kept your %s. To replace it with the starter file, run: zero start %s --force", p, id)
	}
	a.out.blank()
	a.out.line("Next:")
	a.out.line("  cd %s", res.Dir)
	a.out.line("  zero test")
	return nil
}

// exercise finds the exercise that holds the current folder.
func (a *app) exercise() (string, *spec.Saved, error) {
	dir, saved, err := workspace.Find(a.Dir)
	if errors.Is(err, workspace.ErrNotInExercise) {
		return "", nil, err
	}
	if err != nil {
		return "", nil, fail("%s. Get the exercise again with: zero start <id>", capitalize(err.Error()))
	}
	return dir, saved, nil
}

// runTests runs the checker and prints the result. submitting says that
// `zero submit` asked, so a pass does not end with "Run: zero submit".
func (a *app) runTests(dir string, ex spec.Exercise, submitting bool) (spec.Report, error) {
	if ex.Checker != spec.CheckerGoTest {
		return spec.Report{}, fail("This exercise uses the checker %q, which zero %s does not know. Install the newest zero.", ex.Checker, a.Version)
	}
	rep, err := check.GoTest(dir, ex, a.Version, a.Now)
	if errors.Is(err, check.ErrNoGo) {
		return rep, fail("The go command was not found. Install Go, then run: zero doctor")
	}
	if err != nil {
		return rep, err
	}
	a.printReport(ex, rep, submitting)
	return rep, nil
}

// printReport prints the exercise name, every task with its mark, the
// messages of the failing tests, and a one-line summary.
func (a *app) printReport(ex spec.Exercise, rep spec.Report, submitting bool) {
	p := a.out
	p.line("%s", p.bold(ex.Title))
	p.blank()
	if rep.BuildError != "" {
		p.line("The code does not compile:")
		p.blank()
		p.line("%s", indent(rep.BuildError, "    "))
		p.blank()
	}
	passed := 0
	for _, t := range rep.Tasks {
		if t.Status == spec.StatusPass {
			passed++
			p.line("%s Task %d: %s", p.green(markPass), t.N, t.Title)
			continue
		}
		p.line("%s Task %d: %s", p.red(markFail), t.N, t.Title)
		if rep.BuildError != "" {
			continue
		}
		for _, test := range t.Tests {
			if test.Status == spec.StatusPass {
				continue
			}
			label := test.Name
			if test.Status == spec.StatusSkip {
				label += " (skipped)"
			}
			p.line("    %s %s", p.red(markFail), label)
			if test.Message != "" {
				p.line("%s", indent(test.Message, "        "))
			}
		}
	}
	p.blank()
	count := fmt.Sprintf("Tasks passed: %d of %d.", passed, len(rep.Tasks))
	switch {
	case rep.OK && submitting:
		p.line("%s Every test passes.", count)
	case rep.OK:
		p.line("%s Every test passes. Run: zero submit", count)
	case rep.BuildError != "":
		p.line("%s Fix the compile error, then run: zero test", count)
	default:
		p.line("%s Fix the first failing task, then run: zero test", count)
	}
}

func (a *app) test() error {
	dir, saved, err := a.exercise()
	if err != nil {
		return err
	}
	cfg, err := a.settings()
	if err != nil {
		return err
	}
	rep, err := a.runTests(dir, saved.Exercise, false)
	if err != nil {
		return err
	}
	if cfg.Token != "" {
		if err := a.client(cfg).PostRun(saved.Exercise.ID, rep); err != nil {
			a.warn("the result was not sent to the site. %s", explain(err))
		}
	}
	if !rep.OK {
		return silent
	}
	return nil
}

// submit runs the tests again and finishes the exercise when they pass.
func (a *app) submit() error {
	dir, saved, err := a.exercise()
	if err != nil {
		return err
	}
	_, client, err := a.signedIn()
	if err != nil {
		return err
	}
	rep, err := a.runTests(dir, saved.Exercise, true)
	if err != nil {
		return err
	}
	if !rep.OK {
		a.out.line("Not submitted: the tests do not pass yet.")
		return silent
	}
	files, err := workspace.EditFiles(dir, saved)
	if err != nil {
		return fail("Cannot read your files: %v. Get the missing file back with: zero start %s", err, saved.Exercise.ID)
	}
	resp, err := client.Submit(saved.Exercise.ID, spec.SubmitRequest{Report: rep, Files: files})
	if err != nil {
		return err
	}
	if !resp.Passed {
		return fail("The site did not accept the submission. Run zero test, then zero submit again.")
	}
	a.out.line("%s Submitted. %s is finished.", a.out.green(markPass), a.out.bold(saved.Exercise.Title))
	a.printNext(resp.Next)
	return nil
}

func (a *app) next() error {
	_, client, err := a.signedIn()
	if err != nil {
		return err
	}
	next, err := client.Next()
	if err != nil {
		return err
	}
	a.printNext(next)
	return nil
}

func (a *app) printNext(next *spec.Next) {
	switch {
	case next == nil:
		a.out.line("Your path has nothing more yet.")
	case next.Kind == "exercise" || next.Kind == "":
		a.out.line("Next: %s. Run: zero start %s", a.out.bold(next.Title), next.ID)
	default:
		a.out.line("Next: %s (%s %s).", a.out.bold(next.Title), next.Kind, next.ID)
	}
}

// run starts the exercise's program with this terminal attached.
func (a *app) run() error {
	dir, saved, err := a.exercise()
	if err != nil {
		return err
	}
	argv := saved.Exercise.Run
	if len(argv) == 0 {
		return fail("This exercise has no program to run. Run: zero test")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = a.Stdin, a.Stdout, a.Stderr
	err = cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &exitErr):
		// The program printed its own error. Pass its exit code on.
		return &failure{code: exitErr.ExitCode()}
	case errors.Is(err, exec.ErrNotFound):
		return fail("The command %q was not found. Run: zero doctor", argv[0])
	}
	return fail("Cannot run %q: %v. Run: zero doctor", strings.Join(argv, " "), err)
}

// hint prints one hint per call and remembers how many it has shown.
func (a *app) hint() error {
	dir, saved, err := a.exercise()
	if err != nil {
		return err
	}
	hints := saved.Exercise.Hints
	if len(hints) == 0 {
		a.out.line("This exercise has no hints. Read README.txt, then run: zero test")
		return nil
	}
	shown := workspace.HintsShown(dir)
	if shown >= len(hints) {
		a.out.line("You have seen every hint. Starting again from the first.")
		a.out.blank()
		shown = 0
	}
	h := hints[shown]
	a.out.line("%s", a.out.bold(fmt.Sprintf("Hint %d of %d: %s", shown+1, len(hints), h.Title)))
	a.out.line("%s", h.Text)
	if err := workspace.SetHintsShown(dir, shown+1); err != nil {
		a.warn("cannot remember which hint was shown: %v. Check that you can write in %s.", err, dir)
	}
	if shown+1 < len(hints) {
		a.out.blank()
		a.out.line("For the next hint, run: zero hint")
	}
	return nil
}
