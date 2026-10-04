// Package workspace reads and writes exercise folders.
//
// Layout:
//
//	<workspace>/<id>/
//	  .zero/exercise.json   the exercise and the mode of every file
//	  .zero/hints           how many hints `zero hint` has shown
//	  README.txt
//	  the exercise files
package workspace

import (
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

// WriteResult says what Write did. Paths use forward slashes.
type WriteResult struct {
	Dir     string   // the exercise folder
	Written []string // files created or replaced
	Kept    []string // "edit" files left alone because they already exist
}

// Write puts an exercise into <root>/<id>/. A file with mode "edit" that
// already exists is kept unless force is true. Every other file is replaced.
func Write(root string, resp *spec.ExerciseResponse, force bool) (*WriteResult, error) {
	id := resp.Exercise.ID
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, `/\`) {
		return nil, fmt.Errorf("the server sent the exercise id %q, which cannot be a folder name", id)
	}
	dir := filepath.Join(root, id)
	res := &WriteResult{Dir: dir}

	edit := map[string]bool{}
	for _, p := range resp.Exercise.Edit {
		edit[p] = true
	}

	// Check every path before writing anything.
	saved := spec.Saved{Exercise: resp.Exercise, Files: []spec.FileRef{}}
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
		saved.Files = append(saved.Files, spec.FileRef{Path: f.Path, Mode: mode})
	}

	if err := os.MkdirAll(filepath.Join(dir, MetaDir), 0o755); err != nil {
		return nil, err
	}
	for i, f := range resp.Files {
		target := filepath.Join(dir, filepath.FromSlash(f.Path))
		if saved.Files[i].Mode == spec.ModeEdit && !force {
			if _, err := os.Lstat(target); err == nil {
				res.Kept = append(res.Kept, f.Path)
				continue
			}
		}
		if err := writeFile(target, []byte(f.Content)); err != nil {
			return nil, err
		}
		res.Written = append(res.Written, f.Path)
	}
	if err := writeFile(filepath.Join(dir, ReadmeFile), []byte(resp.Readme)); err != nil {
		return nil, err
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

// EditFiles reads the current content of every "edit" file. These are the
// files `zero submit` uploads.
func EditFiles(dir string, s *spec.Saved) ([]spec.File, error) {
	var paths []string
	for _, f := range s.Files {
		if f.Mode == spec.ModeEdit {
			paths = append(paths, f.Path)
		}
	}
	if len(paths) == 0 {
		paths = s.Exercise.Edit
	}
	out := make([]spec.File, 0, len(paths))
	for _, p := range paths {
		if err := checkPath(p); err != nil {
			return nil, err
		}
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
		if err != nil {
			return nil, err
		}
		out = append(out, spec.File{Path: p, Content: string(b), Mode: spec.ModeEdit})
	}
	return out, nil
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
