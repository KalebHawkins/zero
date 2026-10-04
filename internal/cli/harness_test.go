package cli

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The hello-world exercise, copied from the platform's content folder.
// starter/go.mod is stored as go.mod.txt because a folder with a go.mod is a
// module of its own and cannot be embedded.
//
//go:embed all:testdata/hello-world
var fixtures embed.FS

func helloWorld(t *testing.T) fs.FS {
	t.Helper()
	sub, err := fs.Sub(fixtures, "testdata/hello-world")
	if err != nil {
		t.Fatal(err)
	}
	return sub
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := fs.ReadFile(helloWorld(t), name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const testVersion = "0.1.0"

// harness runs commands in-process against a fake API. Nothing touches the
// real home directory, and no browser is ever started: OpenBrowser only
// records the address.
type harness struct {
	t    *testing.T
	fake *fakeAPI
	srv  *httptest.Server

	home       string
	workspace  string
	configPath string
	env        map[string]string
	dir        string // the current folder of the next command
	isTerminal bool

	clock  time.Time
	slept  []time.Duration
	opened []string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	fake, err := newFakeAPI(helloWorld(t), "/zero-next")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)
	fake.siteRoot = srv.URL + fake.prefix

	home := t.TempDir()
	h := &harness{
		t: t, fake: fake, srv: srv,
		home:       home,
		workspace:  filepath.Join(home, "zero"),
		configPath: filepath.Join(home, "config", "zero", "config.json"),
		dir:        home,
		clock:      time.Date(2026, 10, 4, 15, 4, 5, 0, time.UTC),
	}
	h.env = map[string]string{
		"HOME":           home,
		"ZERO_CONFIG":    h.configPath,
		"ZERO_API_URL":   fake.siteRoot,
		"ZERO_WORKSPACE": h.workspace,
	}
	return h
}

// run runs one command and returns its exit code, standard output and
// standard error.
func (h *harness) run(args ...string) (int, string, string) {
	h.t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(args, Env{
		Version:    testVersion,
		Stdin:      strings.NewReader(""),
		Stdout:     &stdout,
		Stderr:     &stderr,
		Getenv:     func(k string) string { return h.env[k] },
		Dir:        h.dir,
		IsTerminal: h.isTerminal,
		OpenBrowser: func(url string) error {
			h.opened = append(h.opened, url)
			return nil
		},
		// The fake clock moves only when the command sleeps.
		Sleep: func(d time.Duration) {
			h.slept = append(h.slept, d)
			h.clock = h.clock.Add(d)
		},
		Now: func() time.Time { return h.clock },
	})
	// Take the fake's lock once, so the test reads what the handlers wrote.
	h.fake.mu.Lock()
	defer h.fake.mu.Unlock()
	return code, stdout.String(), stderr.String()
}

// ok runs a command that must succeed and returns its standard output.
func (h *harness) ok(args ...string) string {
	h.t.Helper()
	code, stdout, stderr := h.run(args...)
	if code != 0 {
		h.t.Fatalf("zero %s: exit code %d\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), code, stdout, stderr)
	}
	return stdout
}

// signIn puts a valid token for the fake site into the config file without
// the login flow.
func (h *harness) signIn() {
	h.t.Helper()
	if err := os.MkdirAll(filepath.Dir(h.configPath), 0o700); err != nil {
		h.t.Fatal(err)
	}
	content := `{"tokens": {"` + h.fake.siteRoot + `": {"token": "` + h.fake.token + `", "login": "kryo"}}}`
	if err := os.WriteFile(h.configPath, []byte(content), 0o600); err != nil {
		h.t.Fatal(err)
	}
}

func wantContains(t *testing.T, what, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("%s does not contain %q:\n%s", what, w, got)
		}
	}
}
