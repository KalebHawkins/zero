package spec

import (
	"bytes"
	"encoding/json"
	"testing"
)

// compact removes the whitespace of a JSON text.
func compact(t *testing.T, s string) string {
	t.Helper()
	var b bytes.Buffer
	if err := json.Compact(&b, []byte(s)); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// The examples below are copied from the platform's docs/SPEC.md. Decoding
// one and encoding it again must give the same JSON: same field names, same
// order, nothing lost.
func TestExamplesRoundTrip(t *testing.T) {
	for name, tc := range map[string]struct {
		in string
		v  any
	}{
		"Report": {`{ "exercise": "hello-world", "checker": "go-test", "ok": false,
			"at": "2026-10-04T15:04:05Z", "duration_ms": 812, "cli": "0.1.0",
			"build_error": "",
			"tasks": [ { "n": 1, "title": "Say hello", "status": "fail",
				"tests": [ { "name": "TestHello", "status": "fail",
					"message": "Hello() = \"Goodbye, World\", want \"Hello, World\"" } ] } ] }`, &Report{}},
		"Doctor": {`{ "ok": true, "at": "2026-10-04T15:04:05Z", "cli": "0.1.0",
			"checks": [ { "name": "go", "ok": true, "detail": "go1.27.1", "fix": "" } ] }`, &Doctor{}},
		"User":   {`{"id": "github:123", "login": "kryo", "name": "Kaleb", "avatar": "https://example.org/a.png"}`, &User{}},
		"File":   {`{ "path": "hello.go", "content": "package hello\n", "mode": "edit" }`, &File{}},
		"Next":   {`{ "kind": "exercise", "id": "variables", "title": "Variables" }`, &Next{}},
		"Error":  {`{"error": "not_passing", "message": "The tests do not pass."}`, &Error{}},
		"Device": {`{"device_code":"d","user_code":"ABCD-EFGH","verification_uri":"https://kryolabs.duckdns.org/zero/cli/","verification_uri_complete":"https://kryolabs.duckdns.org/zero/cli/?code=ABCD-EFGH","interval":2,"expires_in":600}`, &DeviceResponse{}},
		"Exercise": {`{ "id": "hello-world", "title": "Hello, World", "lead": "One sentence.",
			"minutes": 5, "checker": "go-test", "edit": ["hello.go"], "run": ["go", "run", "./cmd/hello"],
			"tasks": [ { "n": 1, "title": "Say hello", "text": "Plain text.", "tests": ["TestHello"] } ],
			"hints": [ { "title": "Where to look", "text": "Plain text." } ] }`, &Exercise{}},
		"State": {`{ "user": {"id": "github:123", "login": "kryo", "name": "Kaleb", "avatar": ""}, "path": "combined",
			"setup": { "go_confirmed": true, "cli_linked": true, "doctor": null },
			"exercises": { "hello-world": { "status": "started", "started_at": "2026-10-04T15:04:05Z", "passed_at": "", "last_run": null } } }`, &State{}},
		"Report with a regression": {`{ "exercise": "game-of-life-4", "checker": "go-test", "ok": false,
			"at": "2026-10-04T15:04:05Z", "duration_ms": 812, "cli": "0.2.0",
			"build_error": "",
			"tasks": [ { "n": 1, "title": "Draw the grid", "status": "pass",
				"tests": [ { "name": "TestDraw", "status": "pass", "message": "" } ] } ],
			"regressions": [ { "name": "TestStep", "status": "fail", "message": "Step() left 3 cells alive, want 4" } ] }`, &Report{}},
		"Stage answer": {`{ "exercise": { "id": "game-of-life-4", "title": "A window", "lead": "One sentence.",
				"minutes": 30, "checker": "go-test", "edit": ["cmd/life-window/main.go"], "run": ["go", "run", "./cmd/life-window"],
				"tasks": [ { "n": 1, "title": "Draw the grid", "text": "Plain text.", "tests": ["TestDraw"] } ],
				"hints": [] },
			"files": [ { "path": "stage4_test.go", "content": "package life\n", "mode": "given" } ],
			"readme": "A window\n",
			"project": { "id": "game-of-life", "title": "Game of Life", "stage": 4, "stages": 6,
				"edit_all": ["internal/life/life.go", "cmd/life-term/main.go"] },
			"uses": [ { "exercise": "the-game-loop",
				"copy": [ { "from": "loop.go", "to": "cmd/life-window/loop.go" } ] } ],
			"reference": [ { "path": "internal/life/life.go", "content": "package life\n", "mode": "edit" } ] }`, &ExerciseResponse{}},
		"Saved stage": {`{ "exercise": { "id": "game-of-life-4", "title": "A window", "lead": "",
				"minutes": 30, "checker": "go-test", "edit": ["cmd/life-window/main.go"], "run": null,
				"tasks": [], "hints": [] },
			"files": [ { "path": "stage4_test.go", "mode": "given" } ],
			"project": { "id": "game-of-life", "title": "Game of Life", "stage": 4, "stages": 6,
				"edit_all": ["internal/life/life.go"] },
			"uses": [ { "exercise": "the-game-loop", "copy": [ { "from": "loop.go", "to": "cmd/life-window/loop.go" } ] } ] }`, &Saved{}},
		"Submit answer with nothing next": {`{"passed": true, "next": null}`, &SubmitResponse{}},
	} {
		if err := json.Unmarshal([]byte(tc.in), tc.v); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		out, err := json.Marshal(tc.v)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got, want := string(out), compact(t, tc.in); got != want {
			t.Errorf("%s round trip:\n got %s\nwant %s", name, got, want)
		}
	}
}
