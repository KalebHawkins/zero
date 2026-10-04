package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/KalebHawkins/zero/internal/workspace"
	"github.com/KalebHawkins/zero/spec"
)

// use copies files from a finished exercise's folder into the current
// project, as the stage's uses entry says. With no argument it lists what
// the stage can use.
func (a *app) use() error {
	force := a.takeFlag("--force")
	if len(a.args) > 1 || (len(a.args) == 1 && strings.HasPrefix(a.args[0], "-")) {
		return usage("zero use takes one exercise id, or none to list what this stage uses. Example: zero use the-game-loop")
	}
	dir, saved, err := workspace.Find(a.Dir)
	if errors.Is(err, workspace.ErrNotInExercise) {
		return fail("zero use works only inside a project stage, and this folder is not inside one. Go to the project folder, or get a stage with: zero start <id>")
	}
	if err != nil {
		return fail("%s. Get the exercise again with: zero start <id>", capitalize(err.Error()))
	}
	if saved.Project == nil {
		return fail("zero use works only inside a project stage. %s is a plain exercise.", saved.Exercise.Title)
	}
	if len(a.args) == 0 {
		return a.listUses(dir, saved)
	}
	id := a.args[0]
	var entry *spec.Use
	for i := range saved.Uses {
		if saved.Uses[i].Exercise == id {
			entry = &saved.Uses[i]
		}
	}
	if entry == nil {
		return fail("This stage does not use %s. To see what it uses, run: zero use", id)
	}
	if !entry.CopiesFiles() {
		a.out.line("This stage builds on what %s taught, so there is nothing to copy.", entry.Name())
		return nil
	}
	cfg, err := a.settings()
	if err != nil {
		return err
	}
	source := filepath.Join(cfg.Workspace, id)
	if !force && !isDir(source) && workspace.Present(dir, *entry) {
		var to []string
		for _, c := range entry.Copy {
			to = append(to, c.To)
		}
		a.out.line("%s The files from %s are already in the project: %s", a.out.green(markPass), entry.Name(), strings.Join(to, ", "))
		a.out.blank()
		a.out.line("Next: zero test")
		return nil
	}
	res, err := workspace.Use(dir, source, *entry, force)
	var missing *workspace.MissingError
	switch {
	case errors.Is(err, workspace.ErrNoExerciseFolder):
		return fail("The folder %s does not exist, so there is nothing to copy. Get the exercise and finish it: zero start %s. If you passed it already (on another machine, say), get the reference copy instead: zero start %s --reference", source, id, saved.Exercise.ID)
	case errors.As(err, &missing):
		return fail("%s has no %s. Get the file back with: zero start %s", source, missing.Path, id)
	case err != nil:
		return fail("Cannot copy the files of %s: %v.", id, err)
	}
	from := map[string]string{}
	for _, c := range entry.Copy {
		from[c.To] = c.From
	}
	for _, to := range res.Copied {
		a.out.line("%s Copied %s/%s to %s", a.out.green(markPass), id, from[to], to)
	}
	for _, to := range res.Same {
		a.out.line("%s %s already matches %s/%s", a.out.green(markPass), to, id, from[to])
	}
	for _, to := range res.Kept {
		a.out.line("%s Kept your %s. It differs from %s/%s.", a.out.red(markFail), to, id, from[to])
	}
	if len(res.Kept) > 0 {
		a.out.blank()
		a.out.line("To replace your files with the copies, run: zero use %s --force", id)
		return silent
	}
	a.out.blank()
	a.out.line("Next: zero test")
	return nil
}

func (a *app) listUses(dir string, saved *spec.Saved) error {
	if len(saved.Uses) == 0 {
		a.out.line("This stage uses no other exercise.")
		return nil
	}
	a.out.line("This stage can use:")
	next := ""
	for _, u := range saved.Uses {
		if !u.CopiesFiles() {
			a.out.line("  %s  builds on %s, nothing to copy", a.out.bold(u.Exercise), u.Name())
			continue
		}
		present := workspace.Present(dir, u)
		if present {
			a.out.line("  %s  your code from %s, already in the project", a.out.bold(u.Exercise), u.Name())
		} else {
			a.out.line("  %s  your code from %s", a.out.bold(u.Exercise), u.Name())
			if next == "" {
				next = u.Exercise
			}
		}
		for _, c := range u.Copy {
			a.out.line("    %s -> %s", c.From, c.To)
		}
	}
	a.out.blank()
	if next == "" {
		a.out.line("Nothing to copy. Next: zero test")
		return nil
	}
	a.out.line("To copy the files, run: zero use %s", next)
	return nil
}

// isDir reports whether path is a folder.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
