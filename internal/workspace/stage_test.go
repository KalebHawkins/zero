package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/KalebHawkins/zero/spec"
)

// stage builds the answer for stage n of a two-stage project "life".
func stage(n int) *spec.ExerciseResponse {
	resp := &spec.ExerciseResponse{
		Exercise: spec.Exercise{ID: "life-1", Title: "Rules", Checker: spec.CheckerGoTest, Edit: []string{"rules.go"}},
		Files: []spec.File{
			{Path: "go.mod", Content: "module life\n", Mode: spec.ModeGiven},
			{Path: "rules.go", Content: "stub rules\n", Mode: spec.ModeEdit},
			{Path: "stage1_test.go", Content: "test 1\n", Mode: spec.ModeGiven},
		},
		Readme:  "Rules\n",
		Project: &spec.Project{ID: "life", Title: "Life", Stage: 1, Stages: 2, EditAll: []string{"rules.go"}},
	}
	if n == 2 {
		resp.Exercise = spec.Exercise{ID: "life-2", Title: "Grid", Checker: spec.CheckerGoTest, Edit: []string{"rules.go", "grid.go"}}
		resp.Files = []spec.File{
			{Path: "grid.go", Content: "stub grid\n", Mode: spec.ModeEdit},
			{Path: "stage2_test.go", Content: "test 2\n", Mode: spec.ModeGiven},
		}
		resp.Readme = "Grid\n"
		resp.Project = &spec.Project{ID: "life", Title: "Life", Stage: 2, Stages: 2, EditAll: []string{"rules.go", "grid.go"}}
		resp.Uses = []spec.Use{{Exercise: "wrap", Copy: []spec.Copy{{From: "wrap.go", To: "wrap.go"}}}}
	}
	return resp
}

func put(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStagesShareTheProjectFolder(t *testing.T) {
	root := t.TempDir()
	res, err := Write(root, stage(1), false)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "life")
	if res.Dir != dir {
		t.Errorf("Dir = %q, want the project folder %q", res.Dir, dir)
	}
	if _, err := os.Stat(filepath.Join(root, "life-1")); err == nil {
		t.Error("a folder named after the stage was created")
	}
	put(t, filepath.Join(dir, "rules.go"), "my rules\n")
	put(t, filepath.Join(dir, "stage1_test.go"), "edited test\n")
	if err := SetHintsShown(dir, 1); err != nil {
		t.Fatal(err)
	}

	res, err = Write(root, stage(2), false)
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, dir, "rules.go"); got != "my rules\n" {
		t.Errorf("stage 2 replaced the learner's rules.go: %q", got)
	}
	// Files of earlier stages that this stage does not send stay as they are.
	if got := read(t, dir, "stage1_test.go"); got != "edited test\n" {
		t.Errorf("stage1_test.go = %q", got)
	}
	if got := read(t, dir, "grid.go"); got != "stub grid\n" {
		t.Errorf("grid.go = %q", got)
	}
	if got := read(t, dir, ReadmeFile); got != "Grid\n" {
		t.Errorf("README.txt = %q, want the stage 2 README", got)
	}
	if !reflect.DeepEqual(res.Written, []string{"grid.go", "stage2_test.go"}) || len(res.Kept) != 0 {
		t.Errorf("written %v, kept %v", res.Written, res.Kept)
	}
	if HintsShown(dir) != 0 {
		t.Error("the hint counter of stage 1 carried over to stage 2")
	}

	// A second start of stage 2 keeps the learner's grid.go and refreshes
	// the given test.
	put(t, filepath.Join(dir, "grid.go"), "my grid\n")
	put(t, filepath.Join(dir, "stage2_test.go"), "edited\n")
	res, err = Write(root, stage(2), false)
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, dir, "grid.go"); got != "my grid\n" {
		t.Errorf("grid.go = %q", got)
	}
	if got := read(t, dir, "stage2_test.go"); got != "test 2\n" {
		t.Errorf("the given test was not replaced: %q", got)
	}
	if !reflect.DeepEqual(res.Kept, []string{"grid.go"}) {
		t.Errorf("Kept = %v", res.Kept)
	}

	_, saved, err := Find(dir)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Exercise.ID != "life-2" || saved.Project == nil || saved.Project.Stage != 2 ||
		!reflect.DeepEqual(saved.Project.EditAll, []string{"rules.go", "grid.go"}) ||
		len(saved.Uses) != 1 || saved.Uses[0].Exercise != "wrap" {
		t.Errorf("saved = %+v, project %+v", saved, saved.Project)
	}
}

// A file the server sends as given, but that an earlier stage lets the
// learner edit, is the learner's work.
func TestStageKeepsFilesInEditAll(t *testing.T) {
	root := t.TempDir()
	if _, err := Write(root, stage(1), false); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "life")
	put(t, filepath.Join(dir, "rules.go"), "my rules\n")
	resp := stage(2)
	resp.Files = append(resp.Files, spec.File{Path: "rules.go", Content: "other\n", Mode: spec.ModeGiven})
	if _, err := Write(root, resp, false); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dir, "rules.go"); got != "my rules\n" {
		t.Errorf("rules.go = %q", got)
	}
}

func reference() []spec.File {
	return []spec.File{
		{Path: "go.mod", Content: "module life\n", Mode: spec.ModeGiven},
		{Path: "rules.go", Content: "solved rules\n", Mode: spec.ModeEdit},
		{Path: "stage1_test.go", Content: "test 1\n", Mode: spec.ModeGiven},
	}
}

func TestReferenceOnAnEmptyFolder(t *testing.T) {
	root := t.TempDir()
	resp := stage(2)
	resp.Reference = reference()
	res, err := WriteWithReference(root, resp, false)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "life")
	for path, want := range map[string]string{
		"go.mod": "module life\n", "rules.go": "solved rules\n", "stage1_test.go": "test 1\n",
		"grid.go": "stub grid\n", "stage2_test.go": "test 2\n",
	} {
		if got := read(t, dir, path); got != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
	if len(res.Reference) != 3 {
		t.Errorf("Reference = %v", res.Reference)
	}
	// Write without the reference flag ignores resp.Reference.
	other := t.TempDir()
	if _, err := Write(other, resp, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(other, "life", "rules.go")); err == nil {
		t.Error("Write laid down the reference")
	}
}

func TestReferenceRefusesToReplaceWork(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "life")
	put(t, filepath.Join(dir, "rules.go"), "my rules\n")
	resp := stage(2)
	resp.Reference = reference()
	_, err := WriteWithReference(root, resp, false)
	var exists *ExistsError
	if !errors.As(err, &exists) || !reflect.DeepEqual(exists.Paths, []string{"rules.go"}) {
		t.Fatalf("err = %v, want an ExistsError naming rules.go", err)
	}
	if got := read(t, dir, "rules.go"); got != "my rules\n" {
		t.Errorf("rules.go = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		t.Error("files were written before the refusal")
	}

	// A file that already holds the reference content is no conflict.
	put(t, filepath.Join(dir, "rules.go"), "solved rules\n")
	if _, err := WriteWithReference(root, resp, false); err != nil {
		t.Errorf("same content refused: %v", err)
	}

	// force replaces the work.
	put(t, filepath.Join(dir, "rules.go"), "my rules\n")
	if _, err := WriteWithReference(root, resp, true); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dir, "rules.go"); got != "solved rules\n" {
		t.Errorf("rules.go after force = %q", got)
	}
}

func TestStageRefusesBadPaths(t *testing.T) {
	for name, edit := range map[string]func(*spec.ExerciseResponse){
		"project id":     func(r *spec.ExerciseResponse) { r.Project.ID = "../x" },
		"uses exercise":  func(r *spec.ExerciseResponse) { r.Uses[0].Exercise = "a/b" },
		"copy from":      func(r *spec.ExerciseResponse) { r.Uses[0].Copy[0].From = "../x.go" },
		"copy to":        func(r *spec.ExerciseResponse) { r.Uses[0].Copy[0].To = ".zero/exercise.json" },
		"reference path": func(r *spec.ExerciseResponse) { r.Reference = []spec.File{{Path: "/etc/x"}} },
	} {
		root := t.TempDir()
		resp := stage(2)
		edit(resp)
		if _, err := WriteWithReference(root, resp, false); err == nil {
			t.Errorf("%s: accepted", name)
		}
		if entries, _ := os.ReadDir(root); len(entries) != 0 {
			t.Errorf("%s: files were written", name)
		}
	}
}

func TestEditFilesOfAProject(t *testing.T) {
	root := t.TempDir()
	if _, err := Write(root, stage(1), false); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(root, stage(2), false); err != nil {
		t.Fatal(err)
	}
	dir, saved, err := Find(filepath.Join(root, "life"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := EditFiles(dir, saved)
	if err != nil {
		t.Fatal(err)
	}
	want := []spec.File{
		{Path: "rules.go", Content: "stub rules\n", Mode: spec.ModeEdit},
		{Path: "grid.go", Content: "stub grid\n", Mode: spec.ModeEdit},
	}
	if !reflect.DeepEqual(files, want) {
		t.Errorf("EditFiles = %+v, want every edit_all file", files)
	}
	os.Remove(filepath.Join(dir, "rules.go"))
	_, err = EditFiles(dir, saved)
	var missing *MissingError
	if !errors.As(err, &missing) || missing.Path != "rules.go" {
		t.Errorf("err = %v, want a MissingError naming rules.go", err)
	}
}

func TestUse(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "life")
	exercise := filepath.Join(root, "wrap")
	use := spec.Use{Exercise: "wrap", Copy: []spec.Copy{
		{From: "wrap.go", To: "wrap.go"},
		{From: "more/util.go", To: "internal/util/util.go"},
	}}

	// The exercise folder is missing.
	if _, err := Use(project, exercise, use, false); !errors.Is(err, ErrNoExerciseFolder) {
		t.Errorf("missing folder: err = %v", err)
	}

	// A source file is missing: nothing is copied.
	put(t, filepath.Join(exercise, "wrap.go"), "wrap v1\n")
	_, err := Use(project, exercise, use, false)
	var missing *MissingError
	if !errors.As(err, &missing) || missing.Path != "more/util.go" {
		t.Errorf("missing file: err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, "wrap.go")); err == nil {
		t.Error("a file was copied before the refusal")
	}

	// The happy path.
	put(t, filepath.Join(exercise, "more", "util.go"), "util\n")
	res, err := Use(project, exercise, use, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Copied, []string{"wrap.go", "internal/util/util.go"}) {
		t.Errorf("Copied = %v", res.Copied)
	}
	if got := read(t, project, "internal", "util", "util.go"); got != "util\n" {
		t.Errorf("util.go = %q", got)
	}

	// Again: the files match, nothing to do.
	res, err = Use(project, exercise, use, false)
	if err != nil || len(res.Same) != 2 || len(res.Copied) != 0 {
		t.Errorf("second use: %+v, %v", res, err)
	}

	// An existing file with other content is kept without force.
	put(t, filepath.Join(project, "wrap.go"), "my wrap\n")
	res, err = Use(project, exercise, use, false)
	if err != nil || !reflect.DeepEqual(res.Kept, []string{"wrap.go"}) {
		t.Errorf("kept: %+v, %v", res, err)
	}
	if got := read(t, project, "wrap.go"); got != "my wrap\n" {
		t.Errorf("wrap.go was overwritten without force: %q", got)
	}
	res, err = Use(project, exercise, use, true)
	if err != nil || !reflect.DeepEqual(res.Copied, []string{"wrap.go"}) {
		t.Errorf("force: %+v, %v", res, err)
	}
	if got := read(t, project, "wrap.go"); got != "wrap v1\n" {
		t.Errorf("wrap.go after force = %q", got)
	}

	// Paths outside the folders are refused.
	bad := spec.Use{Exercise: "wrap", Copy: []spec.Copy{{From: "wrap.go", To: "../evil.go"}}}
	if _, err := Use(project, exercise, bad, true); err == nil {
		t.Error("a destination outside the project was accepted")
	}
}

func TestCheckNotAhead(t *testing.T) {
	root := t.TempDir()
	stage := func(project string, n int) *spec.ExerciseResponse {
		return &spec.ExerciseResponse{
			Exercise: spec.Exercise{ID: project + "-" + strconv.Itoa(n), Edit: []string{"a.go"}},
			Files:    []spec.File{{Path: "a.go", Content: "package a\n", Mode: spec.ModeEdit}},
			Project:  &spec.Project{ID: project, Stage: n, Stages: 3, EditAll: []string{"a.go"}},
		}
	}
	if err := CheckNotAhead(root, stage("p", 1)); err != nil {
		t.Fatalf("no folder yet: %v", err)
	}
	if _, err := Write(root, stage("p", 3), false); err != nil {
		t.Fatal(err)
	}
	var ahead *AheadError
	if err := CheckNotAhead(root, stage("p", 1)); !errors.As(err, &ahead) || ahead.Have != 3 || ahead.Starts != 1 ||
		err.Error() != "Stage 3 is ahead of this one in "+filepath.Join(root, "p")+"; starting stage 1 would replace newer files. Use --force to do it anyway." {
		t.Errorf("stage 1 over stage 3: %v", err)
	}
	for _, n := range []int{3, 4} {
		if err := CheckNotAhead(root, stage("p", n)); err != nil {
			t.Errorf("stage %d over stage 3: %v", n, err)
		}
	}
	if err := CheckNotAhead(root, &spec.ExerciseResponse{Exercise: spec.Exercise{ID: "p"}}); err != nil {
		t.Errorf("a plain exercise: %v", err)
	}
}
