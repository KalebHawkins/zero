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
			"checks": [ { "name": "go", "ok": true, "detail": "go1.26.3", "fix": "" } ] }`, &Doctor{}},
		"User":   {`{"id": "github:123", "login": "kryo", "name": "Kaleb", "avatar": "https://example.org/a.png"}`, &User{}},
		"File":   {`{ "path": "hello.go", "content": "package hello\n", "mode": "edit" }`, &File{}},
		"Next":   {`{ "kind": "exercise", "id": "variables", "title": "Variables" }`, &Next{}},
		"Error":  {`{"error": "not_passing", "message": "The tests do not pass."}`, &Error{}},
		"Device": {`{"device_code":"d","user_code":"ABCD-EFGH","verification_uri":"https://zeroseries.dev/cli/","verification_uri_complete":"https://zeroseries.dev/cli/?code=ABCD-EFGH","interval":2,"expires_in":600}`, &DeviceResponse{}},
		"Exercise": {`{ "id": "hello-world", "title": "Hello, World", "lead": "One sentence.",
			"minutes": 5, "checker": "go-test", "edit": ["hello.go"], "run": ["go", "run", "./cmd/hello"],
			"tasks": [ { "n": 1, "title": "Say hello", "text": "Plain text.", "tests": ["TestHello"] } ],
			"hints": [ { "title": "Where to look", "text": "Plain text." } ] }`, &Exercise{}},
		"State": {`{ "user": {"id": "github:123", "login": "kryo", "name": "Kaleb", "avatar": ""}, "path": "combined",
			"setup": { "go_confirmed": true, "cli_linked": true, "doctor": null },
			"exercises": { "hello-world": { "status": "started", "started_at": "2026-10-04T15:04:05Z", "passed_at": "", "last_run": null } } }`, &State{}},
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
