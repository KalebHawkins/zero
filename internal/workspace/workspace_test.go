package workspace

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/KalebHawkins/zero/spec"
)

func response() *spec.ExerciseResponse {
	return &spec.ExerciseResponse{
		Exercise: spec.Exercise{
			ID: "hello-world", Title: "Hello, World", Checker: spec.CheckerGoTest,
			Edit:  []string{"hello.go"},
			Run:   []string{"go", "run", "./cmd/hello"},
			Tasks: []spec.Task{{N: 1, Title: "Say hello", Text: "Fix it.", Tests: []string{"TestHello"}}},
			Hints: []spec.Hint{{Title: "Where to look", Text: "Open hello.go."}},
		},
		Files: []spec.File{
			{Path: "cmd/hello/main.go", Content: "package main\n", Mode: spec.ModeGiven},
			{Path: "go.mod", Content: "module hello\n", Mode: spec.ModeGiven},
			{Path: "hello.go", Content: "starter\n", Mode: spec.ModeEdit},
			{Path: "hello_test.go", Content: "test v1\n", Mode: spec.ModeGiven},
		},
		Readme: "Hello, World\n",
	}
}

func read(t *testing.T, parts ...string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(parts...))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestWriteCreatesTheFolder(t *testing.T) {
	root := filepath.Join(t.TempDir(), "zero") // does not exist yet
	res, err := Write(root, response(), false)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "hello-world")
	if res.Dir != dir {
		t.Errorf("Dir = %q, want %q", res.Dir, dir)
	}
	if len(res.Written) != 4 || len(res.Kept) != 0 {
		t.Errorf("written %v, kept %v", res.Written, res.Kept)
	}
	for path, want := range map[string]string{
		"cmd/hello/main.go": "package main\n",
		"go.mod":            "module hello\n",
		"hello.go":          "starter\n",
		"hello_test.go":     "test v1\n",
		"README.txt":        "Hello, World\n",
	} {
		if got := read(t, dir, filepath.FromSlash(path)); got != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}

	var saved spec.Saved
	if err := json.Unmarshal([]byte(read(t, dir, MetaDir, ExerciseFile)), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Exercise.ID != "hello-world" || len(saved.Exercise.Tasks) != 1 || len(saved.Exercise.Hints) != 1 {
		t.Errorf("saved exercise = %+v", saved.Exercise)
	}
	wantFiles := []spec.FileRef{
		{Path: "cmd/hello/main.go", Mode: spec.ModeGiven},
		{Path: "go.mod", Mode: spec.ModeGiven},
		{Path: "hello.go", Mode: spec.ModeEdit},
		{Path: "hello_test.go", Mode: spec.ModeGiven},
	}
	if len(saved.Files) != len(wantFiles) {
		t.Fatalf("saved files = %+v", saved.Files)
	}
	for i, w := range wantFiles {
		if saved.Files[i] != w {
			t.Errorf("saved file %d = %+v, want %+v", i, saved.Files[i], w)
		}
	}
}

func TestWriteKeepsEditedFiles(t *testing.T) {
	root := t.TempDir()
	if _, err := Write(root, response(), false); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "hello-world")
	// The learner edits both a file to edit and a given file.
	for _, name := range []string{"hello.go", "hello_test.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("my work\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// The server now sends a newer test file.
	resp := response()
	resp.Files[3].Content = "test v2\n"
	res, err := Write(root, resp, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, dir, "hello.go"); got != "my work\n" {
		t.Errorf("hello.go was overwritten without --force: %q", got)
	}
	if got := read(t, dir, "hello_test.go"); got != "test v2\n" {
		t.Errorf("the given file was not refreshed: %q", got)
	}
	if len(res.Kept) != 1 || res.Kept[0] != "hello.go" {
		t.Errorf("Kept = %v, want [hello.go]", res.Kept)
	}
	if len(res.Written) != 3 {
		t.Errorf("Written = %v, want the three given files", res.Written)
	}

	// --force replaces the edited file.
	res, err = Write(root, resp, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, dir, "hello.go"); got != "starter\n" {
		t.Errorf("--force did not replace hello.go: %q", got)
	}
	if len(res.Kept) != 0 || len(res.Written) != 4 {
		t.Errorf("with force: written %v, kept %v", res.Written, res.Kept)
	}
}

// A file without a mode takes it from the exercise's edit list.
func TestWriteModeFromEditList(t *testing.T) {
	root := t.TempDir()
	resp := response()
	for i := range resp.Files {
		resp.Files[i].Mode = ""
	}
	if _, err := Write(root, resp, false); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "hello-world")
	if err := os.WriteFile(filepath.Join(dir, "hello.go"), []byte("my work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(root, resp, false); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dir, "hello.go"); got != "my work\n" {
		t.Errorf("hello.go was overwritten: %q", got)
	}
}

func TestWriteRefusesPathsOutsideTheFolder(t *testing.T) {
	for _, bad := range []string{"../evil.go", "/etc/passwd", "a/../../evil.go", "", ".zero/exercise.json", `a\b.go`} {
		root := t.TempDir()
		resp := response()
		resp.Files = append(resp.Files, spec.File{Path: bad, Content: "x", Mode: spec.ModeGiven})
		if _, err := Write(root, resp, true); err == nil {
			t.Errorf("path %q was accepted", bad)
		}
		if _, err := os.Stat(filepath.Join(root, "hello-world")); err == nil {
			t.Errorf("path %q: files were written before the check", bad)
		}
	}
	for _, bad := range []string{"", "..", "a/b", `a\b`} {
		resp := response()
		resp.Exercise.ID = bad
		if _, err := Write(t.TempDir(), resp, false); err == nil {
			t.Errorf("exercise id %q was accepted", bad)
		}
	}
}

func TestFindWalksUp(t *testing.T) {
	root := t.TempDir()
	if _, err := Write(root, response(), false); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "hello-world")
	for _, start := range []string{dir, filepath.Join(dir, "cmd"), filepath.Join(dir, "cmd", "hello")} {
		got, saved, err := Find(start)
		if err != nil {
			t.Fatalf("Find(%s): %v", start, err)
		}
		if got != dir || saved.Exercise.ID != "hello-world" {
			t.Errorf("Find(%s) = %q, %q", start, got, saved.Exercise.ID)
		}
	}
	if _, _, err := Find(root); !errors.Is(err, ErrNotInExercise) {
		t.Errorf("Find outside an exercise: %v, want ErrNotInExercise", err)
	}
}

func TestEditFiles(t *testing.T) {
	root := t.TempDir()
	if _, err := Write(root, response(), false); err != nil {
		t.Fatal(err)
	}
	dir, saved, err := Find(filepath.Join(root, "hello-world"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hello.go"), []byte("my work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := EditFiles(dir, saved)
	if err != nil {
		t.Fatal(err)
	}
	want := spec.File{Path: "hello.go", Content: "my work\n", Mode: spec.ModeEdit}
	if len(files) != 1 || files[0] != want {
		t.Errorf("EditFiles = %+v, want [%+v]", files, want)
	}
}

func TestHintsCounter(t *testing.T) {
	root := t.TempDir()
	if _, err := Write(root, response(), false); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "hello-world")
	if n := HintsShown(dir); n != 0 {
		t.Errorf("HintsShown at the start = %d", n)
	}
	if err := SetHintsShown(dir, 2); err != nil {
		t.Fatal(err)
	}
	if n := HintsShown(dir); n != 2 {
		t.Errorf("HintsShown = %d, want 2", n)
	}
	// Starting the exercise again keeps the counter.
	if _, err := Write(root, response(), false); err != nil {
		t.Fatal(err)
	}
	if n := HintsShown(dir); n != 2 {
		t.Errorf("HintsShown after a second start = %d, want 2", n)
	}
}

func TestCheckWritable(t *testing.T) {
	root := filepath.Join(t.TempDir(), "new", "zero")
	if err := CheckWritable(root); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Errorf("the folder holds %d files after the check, err %v", len(entries), err)
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckWritable(file); err == nil {
		t.Error("a plain file passed as a workspace folder")
	}
}
