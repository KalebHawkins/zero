package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/KalebHawkins/zero/internal/config"

	"github.com/KalebHawkins/zero/spec"
)

func TestVersionAndHelp(t *testing.T) {
	h := newHarness(t)
	if out := h.ok("version"); out != "zero "+testVersion+"\n" {
		t.Errorf("version printed %q", out)
	}
	if out := h.ok("--version"); out != "zero "+testVersion+"\n" {
		t.Errorf("--version printed %q", out)
	}
	for _, args := range [][]string{{}, {"help"}, {"--help"}, {"test", "-h"}} {
		out := h.ok(args...)
		for _, command := range []string{"login", "logout", "whoami", "doctor", "start", "test", "run", "hint", "submit", "next",
			"config list", "config get", "config set", "config unset", "config path", "version", "help"} {
			if !strings.Contains(out, "\n  "+command) {
				t.Errorf("help (%v) does not list %q", args, command)
			}
		}
	}
}

func TestUnknownCommandAndArguments(t *testing.T) {
	h := newHarness(t)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"frobnicate"}, `zero has no command named "frobnicate". Run: zero help`},
		{[]string{"test", "--fast"}, `zero test takes no arguments, but got "--fast". Run: zero help`},
		{[]string{"start"}, "zero start needs one exercise id. Example: zero start hello-world"},
		{[]string{"config", "set", "color"}, "zero config set needs a key and a value."},
		{[]string{"config", "unset"}, "zero config unset needs one key."},
		{[]string{"whoami", "--api-url"}, "The flag --api-url needs a value."},
	} {
		code, _, stderr := h.run(tc.args...)
		if code != 2 || !strings.Contains(stderr, tc.want) {
			t.Errorf("zero %v: code %d, stderr %q; want code 2 and %q", tc.args, code, stderr, tc.want)
		}
	}
}

// Global flags work before and after the command name, as "--flag value"
// and as "--flag=value".
func TestGlobalFlagPositions(t *testing.T) {
	h := newHarness(t)
	delete(h.env, "ZERO_API_URL")
	for _, args := range [][]string{
		{"--api-url", "http://flag.example", "config", "get", "api_url"},
		{"config", "get", "api_url", "--api-url", "http://flag.example"},
		{"config", "--api-url=http://flag.example", "get", "api_url"},
		{"--api-url=http://flag.example/", "config", "get", "api_url"},
	} {
		if out := h.ok(args...); out != "http://flag.example\n" {
			t.Errorf("zero %v printed %q", args, out)
		}
	}
	// --config before and after.
	other := filepath.Join(h.home, "other.json")
	for _, args := range [][]string{
		{"--config", other, "config", "path"},
		{"config", "path", "--config", other},
	} {
		if out := h.ok(args...); out != other+"\n" {
			t.Errorf("zero %v printed %q", args, out)
		}
	}
}

func TestConfigListShowsSources(t *testing.T) {
	h := newHarness(t)
	delete(h.env, "ZERO_API_URL")
	delete(h.env, "ZERO_WORKSPACE")
	h.ok("config", "set", "workspace", "~/code/zero")
	h.ok("config", "set", "color", "always")
	h.ok("config", "set", "api_url", "http://file.example")
	h.env["ZERO_NO_BROWSER"] = "1"
	h.env["ZERO_TOKEN"] = "abc"

	out := h.ok("config", "list", "--api-url", "http://flag.example", "--no-color")
	want := map[string][2]string{
		"api_url":     {"http://flag.example", "from flag"},
		"workspace":   {filepath.Join(h.home, "code", "zero"), "from file"},
		"browser":     {"false", "from env"},
		"color":       {"never", "from flag"},
		"editor":      {"(not set)", "from default"},
		"token":       {"(set, hidden)", "from env"},
		"config file": {h.configPath, "from env"},
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != len(want) {
		t.Fatalf("config list printed %d lines, want %d:\n%s", len(lines), len(want), out)
	}
	for _, line := range lines {
		found := false
		for key, w := range want {
			if strings.HasPrefix(line, key+" ") {
				found = true
				fields := strings.Fields(strings.TrimPrefix(line, key))
				gotSource := strings.Join(fields[len(fields)-2:], " ")
				gotValue := strings.Join(fields[:len(fields)-2], " ")
				if gotValue != w[0] || gotSource != w[1] {
					t.Errorf("%s: value %q %s, want %q %s", key, gotValue, gotSource, w[0], w[1])
				}
			}
		}
		if !found {
			t.Errorf("unexpected line %q", line)
		}
	}
	if strings.Contains(out, "abc") {
		t.Errorf("config list shows the token:\n%s", out)
	}

	// Without the flags the file values show.
	out = h.ok("config", "list")
	wantContains(t, "config list", out, "http://file.example", "always")

	// Bare "zero config" is "zero config list".
	if h.ok("config") != out {
		t.Errorf("zero config and zero config list differ")
	}
}

func TestConfigSetGet(t *testing.T) {
	h := newHarness(t)
	delete(h.env, "ZERO_WORKSPACE")

	out := h.ok("config", "set", "workspace", "~/practice")
	wantContains(t, "config set output", out, "Saved workspace in "+h.configPath)
	if got := h.ok("config", "get", "workspace"); got != filepath.Join(h.home, "practice")+"\n" {
		t.Errorf("config get workspace = %q", got)
	}
	h.ok("config", "set", "browser", "false")
	if got := h.ok("config", "get", "browser"); got != "false\n" {
		t.Errorf("config get browser = %q", got)
	}
	h.ok("config", "set", "editor", "code --wait")
	if got := h.ok("config", "get", "editor"); got != "code --wait\n" {
		t.Errorf("config get editor = %q", got)
	}

	// The file keeps its mode and its other keys.
	info, err := os.Stat(h.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("config file mode = %o, want 600", mode)
	}
	raw, _ := os.ReadFile(h.configPath)
	wantContains(t, "config file", string(raw), `"workspace": "~/practice"`, `"browser": false`, `"editor": "code --wait"`)

	// An empty value puts the default back.
	out = h.ok("config", "set", "workspace", "")
	wantContains(t, "config set output", out, "Removed workspace")
	if got := h.ok("config", "get", "workspace"); got != filepath.Join(h.home, "zero")+"\n" {
		t.Errorf("workspace after removing = %q", got)
	}

	// Bad values and keys say what is allowed.
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"config", "set", "color", "blue"}, "not auto, always or never"},
		{[]string{"config", "set", "browser", "maybe"}, "not true or false"},
		{[]string{"config", "set", "api_url", "localhost"}, "not an http or https address"},
		{[]string{"config", "set", "token", "x"}, "There is no setting named"},
		{[]string{"config", "get", "nope"}, "The settings are: api_url, workspace, browser, color, editor."},
	} {
		code, _, stderr := h.run(tc.args...)
		if code != 1 || !strings.Contains(stderr, tc.want) {
			t.Errorf("zero %v: code %d, stderr %q; want %q", tc.args, code, stderr, tc.want)
		}
	}

	// The environment wins over a saved value, and set says so.
	h.env["ZERO_WORKSPACE"] = "/from/env"
	out = h.ok("config", "set", "workspace", "~/again")
	wantContains(t, "config set output", out, "The environment sets workspace")
}

// A damaged value in the file can be repaired with config set.
func TestConfigSetRepairsABadFile(t *testing.T) {
	h := newHarness(t)
	if err := os.MkdirAll(filepath.Dir(h.configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, h.configPath, `{"color": "blue", "token": "keep-me"}`)
	code, _, stderr := h.run("whoami")
	if code != 1 || !strings.Contains(stderr, "zero config set color auto") {
		t.Errorf("code %d, stderr %q", code, stderr)
	}
	h.ok("config", "set", "color", "auto")
	if got := h.ok("config", "get", "color"); got != "auto\n" {
		t.Errorf("color = %q", got)
	}
	raw, _ := os.ReadFile(h.configPath)
	wantContains(t, "config file", string(raw), "keep-me")
}

func TestLoginOpensTheBrowserUnlessDisabled(t *testing.T) {
	complete := func(h *harness) string { return h.fake.siteRoot + "/cli/?code=ABCD-EFGH" }

	t.Run("default opens verification_uri_complete", func(t *testing.T) {
		h := newHarness(t)
		out := h.ok("login")
		if len(h.opened) != 1 || h.opened[0] != complete(h) {
			t.Errorf("opened %v, want [%s]", h.opened, complete(h))
		}
		wantContains(t, "login output", out, "Your code is: ABCD-EFGH", h.fake.siteRoot+"/cli/")
	})
	t.Run("--no-browser", func(t *testing.T) {
		h := newHarness(t)
		h.ok("login", "--no-browser")
		if len(h.opened) != 0 {
			t.Errorf("opened %v", h.opened)
		}
	})
	t.Run("ZERO_NO_BROWSER=1", func(t *testing.T) {
		h := newHarness(t)
		h.env["ZERO_NO_BROWSER"] = "1"
		h.ok("login")
		if len(h.opened) != 0 {
			t.Errorf("opened %v", h.opened)
		}
	})
	t.Run("browser false in the file", func(t *testing.T) {
		h := newHarness(t)
		h.ok("config", "set", "browser", "false")
		h.ok("login")
		if len(h.opened) != 0 {
			t.Errorf("opened %v", h.opened)
		}
		// login keeps the other settings in the file.
		if got := h.ok("config", "get", "browser"); got != "false\n" {
			t.Errorf("browser after login = %q", got)
		}
	})
}

func TestLoginExpires(t *testing.T) {
	t.Run("410 from the server", func(t *testing.T) {
		h := newHarness(t)
		h.fake.expired = true
		code, _, stderr := h.run("login", "--no-browser")
		if code != 1 || strings.TrimSpace(stderr) != "The code expired before it was approved. Run: zero login" {
			t.Errorf("code %d, stderr %q", code, stderr)
		}
		if _, err := os.Stat(h.configPath); err == nil {
			t.Error("a config file was written without a token")
		}
	})
	t.Run("expires_in passes", func(t *testing.T) {
		h := newHarness(t)
		h.fake.pendingPolls = 1 << 30 // never approved
		code, _, stderr := h.run("login", "--no-browser")
		if code != 1 || !strings.Contains(stderr, "The code expired") {
			t.Errorf("code %d, stderr %q", code, stderr)
		}
		// 600 seconds at one poll every 2 seconds.
		if len(h.slept) != 300 || h.fake.polls != 300 {
			t.Errorf("slept %d times and polled %d times, want 300", len(h.slept), h.fake.polls)
		}
	})
}

func TestNotSignedIn(t *testing.T) {
	h := newHarness(t)
	for _, args := range [][]string{{"whoami"}, {"start", "hello-world"}, {"next"}} {
		code, _, stderr := h.run(args...)
		if code != 1 || strings.TrimSpace(stderr) != "Not signed in. Run: zero login" {
			t.Errorf("zero %v: code %d, stderr %q", args, code, stderr)
		}
	}
}

func TestAPIErrorsPrintTheServerMessage(t *testing.T) {
	h := newHarness(t)
	h.signIn()
	code, _, stderr := h.run("start", "no-such-exercise")
	if code != 1 || !strings.HasPrefix(stderr, `There is no exercise with the id "no-such-exercise".`) {
		t.Errorf("code %d, stderr %q", code, stderr)
	}

	// A token the server refuses: the server's message, then what to do.
	h.env["ZERO_TOKEN"] = "stale"
	code, _, stderr = h.run("next")
	if code != 1 || strings.TrimSpace(stderr) != "This sign-in is not valid. Run: zero login" {
		t.Errorf("code %d, stderr %q", code, stderr)
	}
}

func TestSiteDown(t *testing.T) {
	h := newHarness(t)
	h.signIn()
	h.srv.Close()
	code, _, stderr := h.run("next")
	if code != 1 || !strings.HasPrefix(stderr, "Cannot reach "+h.fake.siteRoot) || !strings.Contains(stderr, "zero config set api_url") {
		t.Errorf("code %d, stderr %q", code, stderr)
	}
}

func TestOutsideAnExercise(t *testing.T) {
	h := newHarness(t)
	for _, command := range []string{"test", "run", "hint", "submit"} {
		code, _, stderr := h.run(command)
		if code != 1 || !strings.HasPrefix(stderr, "This folder is not inside an exercise.") {
			t.Errorf("zero %s: code %d, stderr %q", command, code, stderr)
		}
	}
}

func TestHintsOnePerCall(t *testing.T) {
	h := newHarness(t)
	h.signIn()
	h.fake.exercise.Hints = []spec.Hint{
		{Title: "First", Text: "Text one."},
		{Title: "Second", Text: "Text two."},
	}
	h.ok("start", "hello-world")
	h.dir = filepath.Join(h.workspace, "hello-world")

	out := h.ok("hint")
	wantContains(t, "hint 1", out, "Hint 1 of 2: First", "Text one.", "For the next hint, run: zero hint")
	out = h.ok("hint")
	wantContains(t, "hint 2", out, "Hint 2 of 2: Second", "Text two.")
	if strings.Contains(out, "First") {
		t.Errorf("the second call printed the first hint again:\n%s", out)
	}
	out = h.ok("hint")
	wantContains(t, "hint 3", out, "You have seen every hint. Starting again from the first.", "Hint 1 of 2: First")
	if got, _ := os.ReadFile(filepath.Join(h.dir, ".zero", "hints")); strings.TrimSpace(string(got)) != "1" {
		t.Errorf(".zero/hints = %q, want 1", got)
	}
}

func TestColor(t *testing.T) {
	const esc = "\x1b["
	h := newHarness(t)

	if out := h.ok("config", "list"); strings.Contains(out, esc) {
		t.Error("color was used although stdout is not a terminal")
	}
	h.isTerminal = true
	if out := h.ok("config", "list"); !strings.Contains(out, esc) {
		t.Error("color was not used on a terminal")
	}
	if out := h.ok("config", "list", "--no-color"); strings.Contains(out, esc) {
		t.Error("--no-color did not turn color off")
	}
	h.env["NO_COLOR"] = "1"
	if out := h.ok("config", "list"); strings.Contains(out, esc) {
		t.Error("NO_COLOR did not turn color off")
	}
	delete(h.env, "NO_COLOR")
	h.env["TERM"] = "dumb"
	if out := h.ok("config", "list"); strings.Contains(out, esc) {
		t.Error("color was used on a dumb terminal")
	}
	delete(h.env, "TERM")

	h.isTerminal = false
	h.ok("config", "set", "color", "always")
	if out := h.ok("config", "list"); !strings.Contains(out, esc) {
		t.Error("color always did not force color")
	}
	h.ok("config", "set", "color", "never")
	h.isTerminal = true
	if out := h.ok("config", "list"); strings.Contains(out, esc) {
		t.Error("color never did not turn color off")
	}
}

func TestDoctor(t *testing.T) {
	h := newHarness(t)
	_, out, _ := h.run("doctor")
	wantContains(t, "doctor output", out,
		"go ", "git ", "podman ", "cc ", "editor ", "workspace ", "api ",
		h.workspace+" is writable",
		"Not signed in, so the site does not know this result. Run: zero login")
	if len(h.fake.doctors) != 0 {
		t.Errorf("doctor posted without a sign-in")
	}

	// A site that does not answer fails the api check, with a fix.
	h.srv.Close()
	code, out, _ := h.run("doctor", "--report")
	if code != 1 {
		t.Errorf("doctor with the site down: exit code %d, want 1", code)
	}
	wantContains(t, "doctor report", out,
		"result:  not ok",
		"[fail] api: cannot reach "+h.fake.siteRoot,
		"       fix: Check your connection, or set the site address with: zero config set api_url <url>")
}

func TestParseGoVersion(t *testing.T) {
	for _, tc := range []struct {
		in      string
		version string
		enough  bool
		ok      bool
	}{
		{"go version go1.26.3 linux/amd64", "go1.26.3", true, true},
		{"go version go1.25.0 darwin/arm64", "go1.25.0", true, true},
		{"go version go1.25 windows/amd64", "go1.25", true, true},
		{"go version go1.24.9 linux/amd64", "go1.24.9", false, true},
		{"go version go1.21.13 linux/amd64", "go1.21.13", false, true},
		{"go version go1.9.7 linux/amd64", "go1.9.7", false, true},
		{"go version go1.27rc1 linux/amd64", "go1.27rc1", true, true},
		{"go version devel go1.27-abc123 linux/amd64", "go1.27", true, true},
		{"go version go2.0.1 linux/amd64", "go2.0.1", true, true},
		{"something else", "", false, false},
	} {
		version, major, minor, ok := parseGoVersion(tc.in)
		if ok != tc.ok || version != tc.version || (ok && goIsNewEnough(major, minor) != tc.enough) {
			t.Errorf("parseGoVersion(%q) = %q, %d.%d, %v; want %q, new enough %v", tc.in, version, major, minor, ok, tc.version, tc.enough)
		}
	}
}

func TestOpenBrowserRefusesOtherSchemes(t *testing.T) {
	// These return before any program starts.
	for _, bad := range []string{"", "file:///etc/passwd", "javascript:alert(1)", "--help", "localhost:8090/cli/"} {
		if err := OpenBrowser(bad); err == nil {
			t.Errorf("OpenBrowser(%q) was accepted", bad)
		}
	}
}

// siteB is a second site. It records the Authorization header of every
// request and answers like a site nobody is signed in to.
type siteB struct {
	mu    sync.Mutex
	paths []string
	auths []string
}

func (b *siteB) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	b.paths = append(b.paths, r.Method+" "+r.URL.Path)
	b.auths = append(b.auths, r.Header.Get("Authorization"))
	b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/api/config":
		w.Write([]byte(`{"dev_login": false, "github": true}`))
	case r.Header.Get("Authorization") == "":
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "unauthorized", "message": "Sign in first."}`))
	default:
		w.Write([]byte(`{"next": null}`))
	}
}

// A token is sent only to the site it came from. After a login on site A,
// requests to site B carry no Authorization header.
func TestTokenIsNotSentToAnotherSite(t *testing.T) {
	h := newHarness(t) // the fake API is site A
	h.env["ZERO_NO_BROWSER"] = "1"
	h.ok("login")
	if out := h.ok("whoami"); !strings.Contains(out, "Signed in as kryo") {
		t.Fatalf("whoami on site A printed %q", out)
	}

	b := &siteB{}
	srvB := httptest.NewServer(b)
	defer srvB.Close()

	// Each way of switching api_url to site B.
	switchers := map[string]func() (undo func()){
		"flag": func() func() {
			return func() {}
		},
		"env": func() func() {
			old := h.env["ZERO_API_URL"]
			h.env["ZERO_API_URL"] = srvB.URL
			return func() { h.env["ZERO_API_URL"] = old }
		},
		"config file": func() func() {
			old := h.env["ZERO_API_URL"]
			delete(h.env, "ZERO_API_URL")
			h.ok("config", "set", "api_url", srvB.URL+"/")
			return func() {
				h.ok("config", "unset", "api_url")
				h.env["ZERO_API_URL"] = old
			}
		},
	}
	for name, switchTo := range switchers {
		var extra []string
		if name == "flag" {
			extra = []string{"--api-url", srvB.URL}
		}
		undo := switchTo()
		before := len(b.auths)

		// doctor sends GET /api/config whether signed in or not.
		h.run(append([]string{"doctor"}, extra...)...)
		// Commands that need a sign-in say there is none for this site.
		for _, command := range [][]string{{"next"}, {"whoami"}, {"start", "hello-world"}} {
			code, _, stderr := h.run(append(command, extra...)...)
			if code != 1 || strings.TrimSpace(stderr) != "Not signed in. Run: zero login" {
				t.Errorf("%s: zero %v on site B: code %d, stderr %q", name, command, code, stderr)
			}
		}
		out := h.ok(append([]string{"config", "list"}, extra...)...)
		if !strings.Contains(out, "(not set)") || strings.Contains(out, "(set, hidden)") {
			t.Errorf("%s: config list on site B shows a token:\n%s", name, out)
		}
		undo()

		b.mu.Lock()
		if len(b.auths) == before {
			t.Errorf("%s: site B got no request, so the test proves nothing", name)
		}
		for i := before; i < len(b.auths); i++ {
			if b.auths[i] != "" {
				t.Errorf("%s: %s on site B carried Authorization %q", name, b.paths[i], b.auths[i])
			}
		}
		b.mu.Unlock()
	}

	// Site A still has its sign-in, and config list shows it there.
	if out := h.ok("whoami"); !strings.Contains(out, "Signed in as kryo") {
		t.Errorf("whoami on site A after the switch printed %q", out)
	}
	if out := h.ok("config", "list"); !strings.Contains(out, "(set, hidden)") {
		t.Errorf("config list on site A shows no token:\n%s", out)
	}

	// ZERO_TOKEN still overrides on any site.
	h.env["ZERO_TOKEN"] = "explicit"
	h.ok("next", "--api-url", srvB.URL)
	b.mu.Lock()
	if last := b.auths[len(b.auths)-1]; last != "Bearer explicit" {
		t.Errorf("with ZERO_TOKEN, site B got Authorization %q", last)
	}
	b.mu.Unlock()
}

// readTokens returns the tokens in the config file.
func readTokens(t *testing.T, h *harness) map[string]config.SiteToken {
	t.Helper()
	raw, err := os.ReadFile(h.configPath)
	if err != nil {
		t.Fatal(err)
	}
	var f config.File
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	return f.Tokens
}

func TestLogout(t *testing.T) {
	const other = "https://other.example"
	// twoSites saves a sign-in for the fake site and for another site.
	twoSites := func(t *testing.T) *harness {
		h := newHarness(t)
		var f config.File
		f.SetToken(h.fake.siteRoot, h.fake.token, "kryo")
		f.SetToken(other, "other-token", "someone")
		if err := config.WriteFile(h.configPath, f); err != nil {
			t.Fatal(err)
		}
		return h
	}
	// onlyOtherLeft checks that logout removed the fake site's entry and
	// nothing else.
	onlyOtherLeft := func(t *testing.T, h *harness) {
		t.Helper()
		tokens := readTokens(t, h)
		if _, ok := tokens[h.fake.siteRoot]; ok {
			t.Errorf("the token of %s is still saved", h.fake.siteRoot)
		}
		if len(tokens) != 1 || tokens[other].Token != "other-token" {
			t.Errorf("tokens after logout = %+v, want only %s", tokens, other)
		}
	}

	t.Run("ends the token on the site and removes only this site", func(t *testing.T) {
		h := twoSites(t)
		out := h.ok("logout")
		wantContains(t, "logout output", out, "Signed out of "+h.fake.siteRoot)
		if len(h.fake.logouts) != 1 || h.fake.logouts[0] != "Bearer "+h.fake.token {
			t.Errorf("logout requests = %q, want one with the saved token", h.fake.logouts)
		}
		onlyOtherLeft(t, h)

		// The site no longer accepts the token.
		h.env["ZERO_TOKEN"] = h.fake.token
		code, _, stderr := h.run("next")
		if code != 1 || !strings.Contains(stderr, "This sign-in is not valid") {
			t.Errorf("next with the ended token: code %d, stderr %q", code, stderr)
		}
		delete(h.env, "ZERO_TOKEN")

		// A second logout has nothing to do and asks the site nothing.
		out = h.ok("logout")
		wantContains(t, "second logout", out, "No sign-in is saved for "+h.fake.siteRoot)
		if len(h.fake.logouts) != 1 {
			t.Errorf("the second logout sent a request")
		}
	})

	t.Run("site down", func(t *testing.T) {
		h := twoSites(t)
		h.srv.Close()
		code, out, stderr := h.run("logout")
		if code != 0 || stderr != "" {
			t.Errorf("code %d, stderr %q; a network error must be ignored", code, stderr)
		}
		wantContains(t, "logout output", out, "Signed out of "+h.fake.siteRoot)
		onlyOtherLeft(t, h)
	})

	t.Run("401 from the site", func(t *testing.T) {
		h := twoSites(t)
		h.fake.revoked = true // the site already forgot the token
		code, _, stderr := h.run("logout")
		if code != 0 || stderr != "" {
			t.Errorf("code %d, stderr %q; a 401 must be ignored", code, stderr)
		}
		if len(h.fake.logouts) != 1 {
			t.Errorf("logout requests = %d, want 1", len(h.fake.logouts))
		}
		onlyOtherLeft(t, h)
	})

	t.Run("another error warns and still removes the token", func(t *testing.T) {
		h := twoSites(t)
		h.fake.logoutStatus = http.StatusInternalServerError
		code, _, stderr := h.run("logout")
		if code != 0 || !strings.HasPrefix(stderr, "Warning: ") || !strings.Contains(stderr, "The site cannot end tokens right now.") {
			t.Errorf("code %d, stderr %q", code, stderr)
		}
		onlyOtherLeft(t, h)
	})

	t.Run("ZERO_TOKEN is not ended and not forgotten", func(t *testing.T) {
		h := newHarness(t)
		h.env["ZERO_TOKEN"] = h.fake.token
		code, out, stderr := h.run("logout")
		if code != 0 || len(h.fake.logouts) != 0 {
			t.Errorf("code %d, %d logout requests; want 0 and none", code, len(h.fake.logouts))
		}
		wantContains(t, "logout output", out, "No sign-in is saved for")
		wantContains(t, "logout warning", stderr, "ZERO_TOKEN is still set")
	})

	t.Run("the saved token is ended even when ZERO_TOKEN is set", func(t *testing.T) {
		h := twoSites(t)
		h.env["ZERO_TOKEN"] = "from-env"
		h.ok("logout")
		if len(h.fake.logouts) != 1 || h.fake.logouts[0] != "Bearer "+h.fake.token {
			t.Errorf("logout requests = %q, want the saved token", h.fake.logouts)
		}
		onlyOtherLeft(t, h)
	})
}

// An old config file with one token works after the upgrade, and login
// rewrites it in the new shape.
func TestOldConfigFileStillSignsIn(t *testing.T) {
	h := newHarness(t)
	delete(h.env, "ZERO_API_URL")
	if err := os.MkdirAll(filepath.Dir(h.configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	old := `{"api_url": "` + h.fake.siteRoot + `/", "token": "` + h.fake.token + `", "login": "kryo"}`
	if err := os.WriteFile(h.configPath, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := h.ok("whoami"); !strings.Contains(out, "Signed in as kryo") {
		t.Errorf("whoami printed %q", out)
	}
	// Any write moves the token under its site.
	h.ok("config", "set", "color", "never")
	raw, _ := os.ReadFile(h.configPath)
	if strings.Contains(string(raw), `"token": "`+h.fake.token+`",`) && !strings.Contains(string(raw), `"tokens"`) {
		t.Errorf("the file still has the old shape: %s", raw)
	}
	want := config.SiteToken{Token: h.fake.token, Login: "kryo"}
	if tokens := readTokens(t, h); len(tokens) != 1 || tokens[h.fake.siteRoot] != want {
		t.Errorf("tokens = %+v", tokens)
	}
}

func TestConfigUnset(t *testing.T) {
	h := newHarness(t)
	delete(h.env, "ZERO_WORKSPACE")
	delete(h.env, "ZERO_API_URL")
	h.signIn()
	h.ok("config", "set", "workspace", "~/practice")
	h.ok("config", "set", "browser", "false")
	h.ok("config", "set", "color", "never")
	h.ok("config", "set", "editor", "vim")
	h.ok("config", "set", "api_url", "http://localhost:8090")

	defaults := map[string]string{
		"workspace": filepath.Join(h.home, "zero"),
		"browser":   "true",
		"color":     "auto",
		"editor":    "",
		"api_url":   config.DefaultAPIURL,
	}
	for key, want := range defaults {
		out := h.ok("config", "unset", key)
		wantContains(t, "config unset output", out, "Removed "+key+" from "+h.configPath, "default")
		if got := h.ok("config", "get", key); got != want+"\n" {
			t.Errorf("%s after unset = %q, want %q", key, got, want)
		}
	}
	out := h.ok("config", "list")
	for _, key := range config.Keys {
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(line, key+" ") && !strings.HasSuffix(line, "from default") {
				t.Errorf("after unset: %q", line)
			}
		}
	}
	// unset leaves the sign-in alone and is fine to repeat.
	if tokens := readTokens(t, h); tokens[h.fake.siteRoot].Token != h.fake.token {
		t.Errorf("unset removed the token: %+v", tokens)
	}
	h.ok("config", "unset", "color")

	code, _, stderr := h.run("config", "unset", "token")
	if code != 1 || !strings.Contains(stderr, "There is no setting named") {
		t.Errorf("config unset token: code %d, stderr %q", code, stderr)
	}
}
