// Package workspace reads and writes exercise folders.
//
// Layout of a plain exercise:
//
//	<workspace>/<id>/
//	  .zero/exercise.json   the exercise and the mode of every file
//	  .zero/hints           how many hints `zero hint` has shown
//	  README.txt
//	  the exercise files
//
// All stages of a project share one folder, <workspace>/<project>/. There
// .zero/exercise.json and README.txt describe the current stage.
package workspace

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/KalebHawkins/zero/spec"
)

// Names inside an exercise folder.
const (
	MetaDir      = ".zero"
	ExerciseFile = "exercise.json"
	HintsFile    = "hints"
	ReadmeFile   = "README.txt"
)

// ErrNotInExercise means no .zero/exercise.json was found.
var ErrNotInExercise = errors.New("this folder is not inside an exercise")

// ErrNoExerciseFolder means the folder of an exercise that a stage uses
// does not exist.
var ErrNoExerciseFolder = errors.New("the exercise folder does not exist")

// ExistsError means reference files were not written, because files the
// learner edits already exist with other content. Nothing was written.
type ExistsError struct {
	Dir   string
	Paths []string // forward slashes
}

func (e *ExistsError) Error() string {
	return fmt.Sprintf("these files already exist in %s: %s", e.Dir, strings.Join(e.Paths, ", "))
}

// MissingError means a file that must be read does not exist.
type MissingError struct {
	Path string // forward slashes, relative to its folder
}

func (e *MissingError) Error() string { return e.Path + " is missing" }

// WriteResult says what Write did. Paths use forward slashes.
type WriteResult struct {
	Dir       string   // the exercise folder, or the project folder of a stage
	Written   []string // files created or replaced
	Kept      []string // "edit" files left alone because they already exist
	Reference []string // reference files created or replaced before the stage's own files
}

// Write puts an exercise into <root>/<id>/, or a stage into its project
// folder <root>/<project>/. A file with mode "edit" that already exists is
// kept unless force is true. Every other file is replaced.
func Write(root string, resp *spec.ExerciseResponse, force bool) (*WriteResult, error) {
	return write(root, resp, force, false)
}

// WriteWithReference is Write for a stage, but it first lays down
// resp.Reference: the whole project as it stands after the previous stage.
// When a reference file that the learner edits already exists with other
// content, it writes nothing and returns an *ExistsError, unless force is
// true.
func WriteWithReference(root string, resp *spec.ExerciseResponse, force bool) (*WriteResult, error) {
	return write(root, resp, force, true)
}

func write(root string, resp *spec.ExerciseResponse, force, reference bool) (*WriteResult, error) {
	id := resp.Exercise.ID
	if !folderName(id) {
		return nil, fmt.Errorf("the server sent the exercise id %q, which cannot be a folder name", id)
	}
	name := id
	edit := map[string]bool{}
	for _, p := range resp.Exercise.Edit {
		edit[p] = true
	}
	stage := resp.Project != nil
	if stage {
		name = resp.Project.ID
		if !folderName(name) {
			return nil, fmt.Errorf("the server sent the project id %q, which cannot be a folder name", name)
		}
		for _, p := range resp.Project.EditAll {
			edit[p] = true
		}
	}
	dir := filepath.Join(root, name)
	res := &WriteResult{Dir: dir}

	// Check every path before writing anything.
	saved := spec.Saved{Exercise: resp.Exercise, Files: []spec.FileRef{}, Project: resp.Project, Uses: resp.Uses}
	for _, f := range resp.Files {
		if err := checkPath(f.Path); err != nil {
			return nil, err
		}
		mode := f.Mode
		if mode != spec.ModeEdit && mode != spec.ModeGiven {
			mode = spec.ModeGiven
			if edit[f.Path] {
				mode = spec.ModeEdit
			}
		}
		// In a project, a file that any stage lets the learner edit is the
		// learner's work, whatever mode the server sent.
		if stage && edit[f.Path] {
			mode = spec.ModeEdit
		}
		saved.Files = append(saved.Files, spec.FileRef{Path: f.Path, Mode: mode})
	}
	for _, u := range resp.Uses {
		if !folderName(u.Exercise) {
			return nil, fmt.Errorf("the server sent the exercise id %q, which cannot be a folder name", u.Exercise)
		}
		for _, c := range u.Copy {
			if err := checkPath(c.From); err != nil {
				return nil, err
			}
			if err := checkPath(c.To); err != nil {
				return nil, err
			}
		}
	}
	var ref []spec.File
	if reference && stage {
		ref = resp.Reference
	}
	var exists []string
	for _, f := range ref {
		if err := checkPath(f.Path); err != nil {
			return nil, err
		}
		if force || (f.Mode != spec.ModeEdit && !edit[f.Path]) {
			continue
		}
		data, err := f.Data()
		if err != nil {
			return nil, err
		}
		old, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f.Path)))
		if errors.Is(err, fs.ErrNotExist) || (err == nil && bytes.Equal(old, data)) {
			continue
		}
		exists = append(exists, f.Path)
	}
	if len(exists) > 0 {
		return nil, &ExistsError{Dir: dir, Paths: exists}
	}

	if err := os.MkdirAll(filepath.Join(dir, MetaDir), 0o755); err != nil {
		return nil, err
	}
	for _, f := range ref {
		data, err := f.Data()
		if err != nil {
			return nil, err
		}
		if err := writeFile(filepath.Join(dir, filepath.FromSlash(f.Path)), data); err != nil {
			return nil, err
		}
		res.Reference = append(res.Reference, f.Path)
	}
	for i, f := range resp.Files {
		target := filepath.Join(dir, filepath.FromSlash(f.Path))
		if saved.Files[i].Mode == spec.ModeEdit && !force {
			if _, err := os.Lstat(target); err == nil {
				res.Kept = append(res.Kept, f.Path)
				continue
			}
		}
		data, err := f.Data()
		if err != nil {
			return nil, err
		}
		if err := writeFile(target, data); err != nil {
			return nil, err
		}
		res.Written = append(res.Written, f.Path)
	}
	if err := writeFile(filepath.Join(dir, ReadmeFile), []byte(resp.Readme)); err != nil {
		return nil, err
	}
	// The hint counter belongs to one stage. A new stage starts at its
	// first hint.
	if stage {
		var old spec.Saved
		b, err := os.ReadFile(filepath.Join(dir, MetaDir, ExerciseFile))
		if err == nil && json.Unmarshal(b, &old) == nil && old.Exercise.ID != id {
			os.Remove(filepath.Join(dir, MetaDir, HintsFile))
		}
	}
	b, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := writeFile(filepath.Join(dir, MetaDir, ExerciseFile), append(b, '\n')); err != nil {
		return nil, err
	}
	return res, nil
}

// AheadError means the project folder holds a later stage than the one being
// started, so starting it would put older files back.
type AheadError struct {
	Dir          string
	Have, Starts int // the stage in the folder, the stage being started
}

func (e *AheadError) Error() string {
	return fmt.Sprintf("Stage %d is ahead of this one in %s; starting stage %d would replace newer files. Use --force to do it anyway.", e.Have, e.Dir, e.Starts)
}

// CheckNotAhead returns an *AheadError when the project folder of resp, a stage,
// already holds a later stage of the same project. A plain exercise, a missing
// or unreadable folder, and the same or an earlier stage are fine.
func CheckNotAhead(root string, resp *spec.ExerciseResponse) error {
	p := resp.Project
	if p == nil || !folderName(p.ID) {
		return nil
	}
	dir := filepath.Join(root, p.ID)
	b, err := os.ReadFile(filepath.Join(dir, MetaDir, ExerciseFile))
	if err != nil {
		return nil
	}
	var old spec.Saved
	if json.Unmarshal(b, &old) != nil || old.Project == nil || old.Project.ID != p.ID {
		return nil
	}
	if old.Project.Stage > p.Stage {
		return &AheadError{Dir: dir, Have: old.Project.Stage, Starts: p.Stage}
	}
	return nil
}

// folderName reports whether an id from the server can name a folder in the
// workspace.
func folderName(id string) bool {
	return id != "" && id != "." && id != ".." && !strings.ContainsAny(id, `/\`)
}

// checkPath refuses a file path that would land outside the exercise folder
// or inside .zero/.
func checkPath(p string) error {
	local := filepath.FromSlash(p)
	if p == "" || strings.Contains(p, `\`) || !filepath.IsLocal(local) {
		return fmt.Errorf("the server sent the file path %q, which is not inside the exercise folder", p)
	}
	if first, _, _ := strings.Cut(p, "/"); first == MetaDir {
		return fmt.Errorf("the server sent the file path %q, which is inside %s", p, MetaDir)
	}
	return nil
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Find walks up from start to the nearest folder that holds
// .zero/exercise.json. It returns that folder and the saved exercise.
func Find(start string) (string, *spec.Saved, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", nil, err
	}
	for {
		b, err := os.ReadFile(filepath.Join(dir, MetaDir, ExerciseFile))
		if err == nil {
			var s spec.Saved
			if err := json.Unmarshal(b, &s); err != nil {
				return "", nil, fmt.Errorf("%s is damaged: %v", filepath.Join(dir, MetaDir, ExerciseFile), err)
			}
			return dir, &s, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", nil, err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil, ErrNotInExercise
		}
		dir = parent
	}
}

// EditFiles reads the current content of the files `zero submit` uploads.
// For a plain exercise these are its "edit" files. For a stage they are
// every file in the project's edit_all; one that does not exist is a
// *MissingError.
func EditFiles(dir string, s *spec.Saved) ([]spec.File, error) {
	var paths []string
	project := s.Project != nil && len(s.Project.EditAll) > 0
	if project {
		paths = s.Project.EditAll
	} else {
		for _, f := range s.Files {
			if f.Mode == spec.ModeEdit {
				paths = append(paths, f.Path)
			}
		}
		if len(paths) == 0 {
			paths = s.Exercise.Edit
		}
	}
	out := make([]spec.File, 0, len(paths))
	for _, p := range paths {
		if err := checkPath(p); err != nil {
			return nil, err
		}
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
		if project && errors.Is(err, fs.ErrNotExist) {
			return nil, &MissingError{Path: p}
		}
		if err != nil {
			return nil, err
		}
		out = append(out, spec.File{Path: p, Content: string(b), Mode: spec.ModeEdit})
	}
	return out, nil
}

// UseResult says what Use did. Paths are destinations in the project
// folder, with forward slashes.
type UseResult struct {
	Copied []string // files created or replaced
	Kept   []string // existing files with other content, left alone
	Same   []string // existing files that already hold the same content
}

// Use copies the files of one `uses` entry from exerciseDir, the folder of a
// finished exercise, into projectDir. An existing file is kept unless force
// is true. Use reads every source file before it writes anything: a missing
// exercise folder is ErrNoExerciseFolder, a missing source file is a
// *MissingError.
func Use(projectDir, exerciseDir string, use spec.Use, force bool) (*UseResult, error) {
	if info, err := os.Stat(exerciseDir); errors.Is(err, fs.ErrNotExist) || (err == nil && !info.IsDir()) {
		return nil, ErrNoExerciseFolder
	} else if err != nil {
		return nil, err
	}
	content := make([][]byte, len(use.Copy))
	for i, c := range use.Copy {
		if err := checkPath(c.From); err != nil {
			return nil, err
		}
		if err := checkPath(c.To); err != nil {
			return nil, err
		}
		b, err := os.ReadFile(filepath.Join(exerciseDir, filepath.FromSlash(c.From)))
		if errors.Is(err, fs.ErrNotExist) {
			return nil, &MissingError{Path: c.From}
		}
		if err != nil {
			return nil, err
		}
		content[i] = b
	}
	res := &UseResult{}
	for i, c := range use.Copy {
		target := filepath.Join(projectDir, filepath.FromSlash(c.To))
		old, err := os.ReadFile(target)
		switch {
		case err == nil && string(old) == string(content[i]):
			res.Same = append(res.Same, c.To)
			continue
		case err == nil && !force:
			res.Kept = append(res.Kept, c.To)
			continue
		case err != nil && !errors.Is(err, fs.ErrNotExist):
			return nil, err
		}
		if err := writeFile(target, content[i]); err != nil {
			return nil, err
		}
		res.Copied = append(res.Copied, c.To)
	}
	return res, nil
}

// Present reports whether every file a uses entry copies already exists in
// projectDir, restored by --reference or copied earlier. An entry with
// nothing to copy is never present: it has no files.
func Present(projectDir string, use spec.Use) bool {
	if len(use.Copy) == 0 {
		return false
	}
	for _, c := range use.Copy {
		if checkPath(c.To) != nil {
			return false
		}
		info, err := os.Stat(filepath.Join(projectDir, filepath.FromSlash(c.To)))
		if err != nil || info.IsDir() {
			return false
		}
	}
	return true
}

// HintsShown returns how many hints `zero hint` has shown in dir.
func HintsShown(dir string) int {
	b, err := os.ReadFile(filepath.Join(dir, MetaDir, HintsFile))
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// SetHintsShown records how many hints have been shown.
func SetHintsShown(dir string, n int) error {
	return writeFile(filepath.Join(dir, MetaDir, HintsFile), []byte(strconv.Itoa(n)+"\n"))
}

// CheckWritable makes sure root exists and a file can be created in it.
func CheckWritable(root string) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(root, ".zero-write-test-*")
	if err != nil {
		return err
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}
