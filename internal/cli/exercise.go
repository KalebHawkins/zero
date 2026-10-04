package cli

import (
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/KalebHawkins/zero/internal/api"
	"github.com/KalebHawkins/zero/internal/check"
	"github.com/KalebHawkins/zero/internal/workspace"
	"github.com/KalebHawkins/zero/spec"
)

// start gets an exercise and writes it into the workspace. A stage of a
// project goes into the project's shared folder.
func (a *app) start() error {
	force := a.takeFlag("--force")
	reference := a.takeFlag("--reference")
	if len(a.args) != 1 || strings.HasPrefix(a.args[0], "-") {
		return usage("zero start needs one exercise id. Example: zero start hello-world")
	}
	id := a.args[0]
	cfg, client, err := a.signedIn()
	if err != nil {
		return err
	}
	var resp *spec.ExerciseResponse
	if reference {
		resp, err = client.ExerciseWithReference(id)
	} else {
		resp, err = client.Exercise(id)
	}
	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.Status == http.StatusNotFound:
			return fail("%s", joinSentence(apiErr.Message, "Copy the id from the exercise page on "+cfg.APIURL+"."))
		case apiErr.Status == http.StatusConflict && apiErr.Code == "stage_locked":
			// The server's sentence names the stage to pass first.
			return fail("%s", apiErr.Message)
		}
	}
	if err != nil {
		return err
	}
	if resp.Exercise.ID == "" {
		return fail("%s sent no exercise for %q. Check that api_url is the site address: zero config list", cfg.APIURL, id)
	}
	if reference && resp.Project == nil {
		return usage("%s is not a project stage, so it has no reference to lay down. Run: zero start %s", id, id)
	}
	if reference && resp.Project.Stage <= 1 {
		return usage("%s is stage 1, so there is no earlier stage to lay down. Run: zero start %s", id, id)
	}
	if !force {
		var ahead *workspace.AheadError
		if err := workspace.CheckNotAhead(cfg.Workspace, resp); errors.As(err, &ahead) {
			return fail("%s", ahead.Error())
		}
	}
	var res *workspace.WriteResult
	if reference {
		res, err = workspace.WriteWithReference(cfg.Workspace, resp, force)
	} else {
		res, err = workspace.Write(cfg.Workspace, resp, force)
	}
	var exists *workspace.ExistsError
	if errors.As(err, &exists) {
		return fail("Not started: these files in %s hold your work: %s. The reference would replace them. To replace them anyway, run: zero start %s --reference --force",
			exists.Dir, strings.Join(exists.Paths, ", "), id)
	}
	if err != nil {
		return fail("Cannot write the exercise into %s: %v. Choose a folder you can write with: zero config set workspace <folder>", cfg.Workspace, err)
	}

	if p := resp.Project; p != nil {
		a.out.line("%s, stage %d of %d: %s is ready in %s", a.out.bold(p.Title), p.Stage, p.Stages, a.out.bold(resp.Exercise.Title), res.Dir)
		if len(res.Reference) > 0 {
			copied := map[string]string{} // destination -> exercise
			for _, u := range resp.Uses {
				for _, c := range u.Copy {
					copied[c.To] = u.Exercise
				}
			}
			var restored []string
			for _, f := range res.Reference {
				if from, ok := copied[f]; ok {
					restored = append(restored, fmt.Sprintf("Restored %s, copied in from %s.", f, from))
				}
			}
			a.out.line("Laid down the reference solution of stage %d: %d files.", p.Stage-1, len(res.Reference)-len(restored))
			for _, line := range restored {
				a.out.line("%s", line)
			}
		}
		for _, f := range res.Kept {
			a.out.line("Kept your %s.", f)
		}
		a.printUses(res.Dir, resp.Uses)
	} else {
		a.out.line("%s is ready in %s", a.out.bold(resp.Exercise.Title), res.Dir)
		for _, f := range res.Kept {
			a.out.line("Kept your %s. To replace it with the starter file, run: zero start %s --force", f, id)
		}
	}
	a.out.blank()
	a.out.line("Next:")
	a.out.line("  cd %s", res.Dir)
	for _, u := range resp.Uses {
		if u.CopiesFiles() && !workspace.Present(res.Dir, u) {
			a.out.line("  zero use %s", u.Exercise)
		}
	}
	a.out.line("  zero test")
	return nil
}

// printUses says what the stage builds on. A prerequisite has nothing to
// copy. An entry with files says to copy them in with `zero use` before
// `zero test`, unless every file is in the project already.
func (a *app) printUses(dir string, uses []spec.Use) {
	for _, u := range uses {
		a.out.blank()
		switch {
		case !u.CopiesFiles():
			a.out.line("This stage builds on %s. There is nothing to copy.", u.Name())
		case workspace.Present(dir, u):
			a.out.line("This stage builds on your code from %s. It is already in the project:", u.Name())
			for _, c := range u.Copy {
				a.out.line("  %s", c.To)
			}
		default:
			a.out.line("This stage builds on your code from %s. Before zero test, copy it in with: zero use %s", u.Name(), u.Exercise)
			for _, c := range u.Copy {
				a.out.line("  %s -> %s", c.From, c.To)
			}
			a.out.line("This stage's code calls it, so without it the build fails.")
		}
	}
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
func (a *app) runTests(dir string, saved *spec.Saved, submitting bool) (spec.Report, error) {
	ex := saved.Exercise
	if ex.Checker != spec.CheckerGoTest {
		return spec.Report{}, fail("This exercise uses the checker %q, which zero %s does not know. Install the newest zero.", ex.Checker, a.Version)
	}
	rep, err := check.GoTest(dir, ex, saved.Project != nil, a.Version, a.Now)
	if errors.Is(err, check.ErrNoGo) {
		return rep, fail("The go command was not found. Install Go, then run: zero doctor")
	}
	if err != nil {
		return rep, err
	}
	var testStages map[string]int
	if saved.Project != nil {
		testStages = saved.Project.TestStages
	}
	a.printReport(ex, rep, testStages, submitting)
	return rep, nil
}

// printReport prints the exercise name, every task with its mark, the
// messages of the failing tests, and a one-line summary.
func (a *app) printReport(ex spec.Exercise, rep spec.Report, testStages map[string]int, submitting bool) {
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
	if len(rep.Regressions) > 0 {
		p.blank()
		p.line("%s:", brokeLine(rep.Regressions, testStages))
		for _, test := range rep.Regressions {
			label := test.Name
			if n := testStages[test.Name]; n > 0 {
				label = fmt.Sprintf("Stage %d: %s", n, test.Name)
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
	case len(rep.Regressions) > 0:
		p.line("%s %s. Fix it first, then run: zero test", count, brokeLine(rep.Regressions, testStages))
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

// brokeLine names the earlier stages that broke, for example "Stage 1 broke"
// or "Stages 1 and 2 broke". Without stage numbers it says "An earlier stage broke".
func brokeLine(regressions []spec.TestResult, testStages map[string]int) string {
	seen := map[int]bool{}
	var stages []int
	for _, t := range regressions {
		n := testStages[t.Name]
		if n == 0 {
			return "An earlier stage broke"
		}
		if !seen[n] {
			seen[n] = true
			stages = append(stages, n)
		}
	}
	sort.Ints(stages)
	if len(stages) == 1 {
		return fmt.Sprintf("Stage %d broke", stages[0])
	}
	names := make([]string, len(stages))
	for i, n := range stages {
		names[i] = strconv.Itoa(n)
	}
	return "Stages " + strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1] + " broke"
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
	rep, err := a.runTests(dir, saved, false)
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
	// Read the files first, so a missing one is named before the tests run.
	files, err := workspace.EditFiles(dir, saved)
	var missing *workspace.MissingError
	if errors.As(err, &missing) {
		return fail("Not submitted: %s is missing from %s. A project submits every file you edit in it. Put the file back, then run: zero submit", missing.Path, dir)
	}
	if err != nil {
		return fail("Cannot read your files: %v. Get the missing file back with: zero start %s", err, saved.Exercise.ID)
	}
	rep, err := a.runTests(dir, saved, true)
	if err != nil {
		return err
	}
	if !rep.OK {
		a.out.line("Not submitted: the tests do not pass yet.")
		return silent
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
	// With no path chosen, the server follows The Combined Path; say how to choose.
	if st, err := client.State(); err == nil && st.Path == "" {
		cfg, _ := a.settings()
		where := "the site"
		if cfg != nil {
			where = cfg.APIURL + "/path/"
		}
		if next == nil {
			a.out.line("You have not chosen a path yet. Choose one at %s, then run: zero next", where)
			return nil
		}
		a.printNext(next)
		a.out.line("You have not chosen a path yet, so this follows The Combined Path. To choose one, go to %s", where)
		return nil
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
