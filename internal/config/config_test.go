package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// env builds a getenv from pairs. HOME is always set so nothing reads the
// real home directory.
func env(home string, pairs ...string) func(string) string {
	m := map[string]string{"HOME": home}
	for i := 0; i+1 < len(pairs); i += 2 {
		m[pairs[i]] = pairs[i+1]
	}
	return func(k string) string { return m[k] }
}

func writeConfig(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDefaults(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "none.json")
	c, err := Load(Flags{Config: path}, env(home))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		KeyAPIURL:    DefaultAPIURL,
		KeyWorkspace: filepath.Join(home, "zero"),
		KeyBrowser:   "true",
		KeyColor:     "auto",
		KeyEditor:    "",
	}
	for _, e := range c.List() {
		if e.Value != want[e.Key] {
			t.Errorf("%s = %q, want %q", e.Key, e.Value, want[e.Key])
		}
		if e.Source != FromDefault {
			t.Errorf("%s source = %q, want default", e.Key, e.Source)
		}
	}
	if c.Token != "" || c.TokenSource != FromNowhere {
		t.Errorf("token = %q from %q, want none", c.Token, c.TokenSource)
	}
	if c.PathSource != FromFlag || c.Path != path {
		t.Errorf("path = %q from %q", c.Path, c.PathSource)
	}
}

// TestPrecedence walks each setting down the order flag, env, file, default.
func TestPrecedence(t *testing.T) {
	home := t.TempDir()
	path := writeConfig(t, home, `{
		"api_url": "https://file.example",
		"workspace": "/from/file",
		"browser": false,
		"color": "always",
		"editor": "file-editor",
		"token": "file-token",
		"login": "kryo"
	}`)
	empty := filepath.Join(home, "empty.json")

	type want struct {
		value  string
		source Source
	}
	tests := []struct {
		name  string
		flags Flags
		env   []string
		want  map[string]want
	}{
		{
			name:  "flags win over file",
			flags: Flags{Config: path, APIURL: "https://flag.example/", Workspace: "/from/flag", NoBrowser: true, NoColor: true},
			want: map[string]want{
				KeyAPIURL:    {"https://flag.example", FromFlag},
				KeyWorkspace: {"/from/flag", FromFlag},
				KeyBrowser:   {"false", FromFlag},
				KeyColor:     {"never", FromFlag},
			},
		},
		{
			name:  "flags win when env is also set",
			flags: Flags{Config: path, APIURL: "https://flag.example", Workspace: "/from/flag", NoBrowser: true, NoColor: true},
			env:   []string{EnvAPIURL, "https://env.example", EnvWorkspace, "/from/env", EnvNoBrowser, "1", EnvNoColor, "1"},
			want: map[string]want{
				KeyAPIURL:    {"https://flag.example", FromFlag},
				KeyWorkspace: {"/from/flag", FromFlag},
				KeyBrowser:   {"false", FromFlag},
				KeyColor:     {"never", FromFlag},
			},
		},
		{
			name:  "env wins over file",
			flags: Flags{Config: path},
			env:   []string{EnvAPIURL, "https://env.example", EnvWorkspace, "/from/env", EnvNoBrowser, "1", EnvNoColor, "1"},
			want: map[string]want{
				KeyAPIURL:    {"https://env.example", FromEnv},
				KeyWorkspace: {"/from/env", FromEnv},
				KeyBrowser:   {"false", FromEnv},
				KeyColor:     {"never", FromEnv},
			},
		},
		{
			name:  "file wins over defaults",
			flags: Flags{Config: path},
			want: map[string]want{
				KeyAPIURL:    {"https://file.example", FromFile},
				KeyWorkspace: {"/from/file", FromFile},
				KeyBrowser:   {"false", FromFile},
				KeyColor:     {"always", FromFile},
				KeyEditor:    {"file-editor", FromFile},
			},
		},
		{
			name:  "defaults",
			flags: Flags{Config: empty},
			want: map[string]want{
				KeyAPIURL:    {DefaultAPIURL, FromDefault},
				KeyWorkspace: {filepath.Join(home, "zero"), FromDefault},
				KeyBrowser:   {"true", FromDefault},
				KeyColor:     {"auto", FromDefault},
				KeyEditor:    {"", FromDefault},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Load(tc.flags, env(home, tc.env...))
			if err != nil {
				t.Fatal(err)
			}
			for key, w := range tc.want {
				e, err := c.Get(key)
				if err != nil {
					t.Fatal(err)
				}
				if e.Value != w.value || e.Source != w.source {
					t.Errorf("%s = %q from %q, want %q from %q", key, e.Value, e.Source, w.value, w.source)
				}
			}
		})
	}
}

func TestListShowsEveryKeyInOrder(t *testing.T) {
	home := t.TempDir()
	path := writeConfig(t, home, `{"workspace": "~/code", "color": "never"}`)
	c, err := Load(Flags{Config: path, APIURL: "http://localhost:8090"}, env(home, EnvNoBrowser, "1"))
	if err != nil {
		t.Fatal(err)
	}
	got := c.List()
	want := []Entry{
		{KeyAPIURL, "http://localhost:8090", FromFlag},
		{KeyWorkspace, filepath.Join(home, "code"), FromFile},
		{KeyBrowser, "false", FromEnv},
		{KeyColor, "never", FromFile},
		{KeyEditor, "", FromDefault},
	}
	if len(got) != len(want) {
		t.Fatalf("List has %d entries, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestToken(t *testing.T) {
	home := t.TempDir()
	path := writeConfig(t, home, `{"tokens": {"https://zeroseries.dev": {"token": "file-token", "login": "kryo"}}}`)

	c, err := Load(Flags{Config: path}, env(home))
	if err != nil {
		t.Fatal(err)
	}
	if c.Token != "file-token" || c.TokenSource != FromFile || c.Login != "kryo" {
		t.Errorf("token = %q from %q, login %q", c.Token, c.TokenSource, c.Login)
	}

	c, err = Load(Flags{Config: path}, env(home, EnvToken, "env-token"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Token != "env-token" || c.TokenSource != FromEnv {
		t.Errorf("token = %q from %q, want env-token from env", c.Token, c.TokenSource)
	}
}

func TestEditor(t *testing.T) {
	home := t.TempDir()
	empty := filepath.Join(home, "empty.json")
	c, err := Load(Flags{Config: empty}, env(home, EnvEditor, "vim"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Editor != "vim" || c.Source(KeyEditor) != FromEnv {
		t.Errorf("editor = %q from %q, want vim from env", c.Editor, c.Source(KeyEditor))
	}

	// zero's own setting wins over the shared $EDITOR.
	path := writeConfig(t, home, `{"editor": "code --wait"}`)
	c, err = Load(Flags{Config: path}, env(home, EnvEditor, "vim"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Editor != "code --wait" || c.Source(KeyEditor) != FromFile {
		t.Errorf("editor = %q from %q, want code --wait from file", c.Editor, c.Source(KeyEditor))
	}
}

func TestConfigPath(t *testing.T) {
	home := t.TempDir()
	p, src, err := ResolvePath(Flags{Config: "~/a.json"}, env(home, EnvConfig, "/env/b.json"))
	if err != nil || p != filepath.Join(home, "a.json") || src != FromFlag {
		t.Errorf("flag: %q from %q, %v", p, src, err)
	}
	p, src, err = ResolvePath(Flags{}, env(home, EnvConfig, "/env/b.json"))
	if err != nil || p != "/env/b.json" || src != FromEnv {
		t.Errorf("env: %q from %q, %v", p, src, err)
	}
	p, src, err = ResolvePath(Flags{}, env(home))
	if err != nil || src != FromDefault || !strings.HasSuffix(filepath.ToSlash(p), "/zero/config.json") {
		t.Errorf("default: %q from %q, %v", p, src, err)
	}
}

func TestWorkspaceExpandsHome(t *testing.T) {
	home := t.TempDir()
	empty := filepath.Join(home, "empty.json")
	for in, want := range map[string]string{
		"~":          home,
		"~/zero":     filepath.Join(home, "zero"),
		"~/a/b":      filepath.Join(home, "a", "b"),
		"/abs/path":  "/abs/path",
		"~other/dir": "", // not this user's home: left alone, made absolute
	} {
		c, err := Load(Flags{Config: empty, Workspace: in}, env(home))
		if err != nil {
			t.Fatal(err)
		}
		if want == "" {
			if !filepath.IsAbs(c.Workspace) || !strings.HasSuffix(filepath.ToSlash(c.Workspace), "/~other/dir") {
				t.Errorf("workspace %q became %q", in, c.Workspace)
			}
			continue
		}
		if runtime.GOOS != "windows" && c.Workspace != want {
			t.Errorf("workspace %q became %q, want %q", in, c.Workspace, want)
		}
	}
}

func TestAPIURLIsCleanedAndChecked(t *testing.T) {
	home := t.TempDir()
	empty := filepath.Join(home, "empty.json")
	c, err := Load(Flags{Config: empty, APIURL: "https://kryolabs.duckdns.org/zero-next/"}, env(home))
	if err != nil {
		t.Fatal(err)
	}
	if c.APIURL != "https://kryolabs.duckdns.org/zero-next" {
		t.Errorf("api_url = %q", c.APIURL)
	}
	if _, err := Load(Flags{Config: empty}, env(home, EnvAPIURL, "localhost:8090")); err == nil {
		t.Error("an address without http:// was accepted")
	} else if !strings.Contains(err.Error(), EnvAPIURL) {
		t.Errorf("the error does not name %s: %v", EnvAPIURL, err)
	}
}

func TestNoBrowserEnvValues(t *testing.T) {
	home := t.TempDir()
	empty := filepath.Join(home, "empty.json")
	for value, wantBrowser := range map[string]bool{"1": false, "true": false, "yes": false, "": true, "0": true, "false": true} {
		c, err := Load(Flags{Config: empty}, env(home, EnvNoBrowser, value))
		if err != nil {
			t.Fatal(err)
		}
		if c.Browser != wantBrowser {
			t.Errorf("%s=%q: browser = %v, want %v", EnvNoBrowser, value, c.Browser, wantBrowser)
		}
	}
}

func TestSetChecksValues(t *testing.T) {
	var f File
	good := [][2]string{
		{KeyAPIURL, "http://localhost:8090/"},
		{KeyWorkspace, "~/code/zero"},
		{KeyBrowser, "false"},
		{KeyColor, "never"},
		{KeyEditor, "code --wait"},
	}
	for _, kv := range good {
		if err := f.Set(kv[0], kv[1]); err != nil {
			t.Errorf("Set(%s, %s): %v", kv[0], kv[1], err)
		}
	}
	if f.APIURL != "http://localhost:8090" || f.Workspace != "~/code/zero" || f.Browser == nil || *f.Browser ||
		f.Color != "never" || f.Editor != "code --wait" {
		t.Errorf("file after Set = %+v", f)
	}
	bad := [][2]string{
		{KeyAPIURL, "not a url"},
		{KeyBrowser, "maybe"},
		{KeyColor, "blue"},
		{"token", "abc"},
		{"nope", "x"},
	}
	for _, kv := range bad {
		if err := f.Set(kv[0], kv[1]); err == nil {
			t.Errorf("Set(%s, %s) was accepted", kv[0], kv[1])
		}
	}
	// An empty value removes the key.
	for _, k := range Keys {
		if err := f.Set(k, ""); err != nil {
			t.Errorf("Set(%s, \"\"): %v", k, err)
		}
	}
	if f.APIURL != "" || f.Workspace != "" || f.Browser != nil || f.Color != "" || f.Editor != "" {
		t.Errorf("file after clearing = %+v", f)
	}
}

func TestWriteFileModeAndRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "zero", "config.json")
	no := false
	in := File{APIURL: "http://localhost:8090", Browser: &no}
	in.SetToken("http://localhost:8090/", "secret", "kryo")
	if err := WriteFile(path, in); err != nil {
		t.Fatal(err)
	}
	// Writing again over an existing file keeps the mode.
	if err := WriteFile(path, in); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Errorf("config file mode = %o, want 600", mode)
		}
	}
	out, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := SiteToken{Token: "secret", Login: "kryo"}
	if out.APIURL != in.APIURL || out.Tokens["http://localhost:8090"] != want || out.Browser == nil || *out.Browser {
		t.Errorf("read back %+v", out)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("the folder holds %d files, want only config.json", len(entries))
	}
}

func TestBadFile(t *testing.T) {
	home := t.TempDir()
	path := writeConfig(t, home, `{"color": `)
	if _, err := Load(Flags{Config: path}, env(home)); err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("broken JSON: %v", err)
	}
	path = writeConfig(t, home, `{"color": "blue"}`)
	if _, err := Load(Flags{Config: path}, env(home)); err == nil || !strings.Contains(err.Error(), "zero config set color") {
		t.Errorf("bad color: %v", err)
	}
}

// A token belongs to one site. Another api_url never gets it.
func TestTokenIsPerSite(t *testing.T) {
	home := t.TempDir()
	path := writeConfig(t, home, `{
		"api_url": "http://localhost:8090",
		"tokens": {
			"http://localhost:8090": {"token": "local-token", "login": "dev"},
			"https://zeroseries.dev": {"token": "prod-token", "login": "kryo"}
		}
	}`)
	for _, tc := range []struct {
		name   string
		flags  Flags
		env    []string
		token  string
		login  string
		source Source
	}{
		{"the saved api_url", Flags{}, nil, "local-token", "dev", FromFile},
		{"a trailing slash is the same site", Flags{APIURL: "http://localhost:8090/"}, nil, "local-token", "dev", FromFile},
		{"another site by flag", Flags{APIURL: "https://zeroseries.dev"}, nil, "prod-token", "kryo", FromFile},
		{"another site by env", Flags{}, []string{EnvAPIURL, "https://zeroseries.dev/"}, "prod-token", "kryo", FromFile},
		{"a site with no sign-in", Flags{APIURL: "https://other.example"}, nil, "", "", FromNowhere},
		{"a path prefix is another site", Flags{APIURL: "http://localhost:8090/zero-next"}, nil, "", "", FromNowhere},
		{"ZERO_TOKEN overrides on any site", Flags{APIURL: "https://other.example"}, []string{EnvToken, "env-token"}, "env-token", "", FromEnv},
		{"ZERO_TOKEN overrides a saved token", Flags{}, []string{EnvToken, "env-token"}, "env-token", "dev", FromEnv},
	} {
		tc.flags.Config = path
		c, err := Load(tc.flags, env(home, tc.env...))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if c.Token != tc.token || c.Login != tc.login || c.TokenSource != tc.source {
			t.Errorf("%s: token %q, login %q, from %q; want %q, %q, from %q",
				tc.name, c.Token, c.Login, c.TokenSource, tc.token, tc.login, tc.source)
		}
	}
}

// An older file holds one token and does not say which site it is for.
func TestOldTokenMigrates(t *testing.T) {
	home := t.TempDir()

	// With a saved api_url the token goes to that site, and to no other.
	path := writeConfig(t, home, `{"api_url": "http://localhost:8090/", "token": "old-token", "login": "kryo"}`)
	f, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := SiteToken{Token: "old-token", Login: "kryo"}
	if len(f.Tokens) != 1 || f.Tokens["http://localhost:8090"] != want || f.Token != "" || f.Login != "" {
		t.Errorf("migrated file = %+v", f)
	}
	c, err := Load(Flags{Config: path}, env(home))
	if err != nil {
		t.Fatal(err)
	}
	if c.Token != "old-token" || c.Login != "kryo" || c.TokenSource != FromFile {
		t.Errorf("token on the saved site = %q, login %q, from %q", c.Token, c.Login, c.TokenSource)
	}
	c, err = Load(Flags{Config: path, APIURL: DefaultAPIURL}, env(home))
	if err != nil {
		t.Fatal(err)
	}
	if c.Token != "" {
		t.Errorf("the old token was offered to another site: %q", c.Token)
	}

	// Without a saved api_url the token goes to the default site.
	path = writeConfig(t, home, `{"token": "old-token", "login": "kryo"}`)
	f, err = ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Tokens) != 1 || f.Tokens[DefaultAPIURL] != want {
		t.Errorf("migrated file = %+v", f)
	}
	c, err = Load(Flags{Config: path, APIURL: "http://localhost:8090"}, env(home))
	if err != nil {
		t.Fatal(err)
	}
	if c.Token != "" {
		t.Errorf("the old token was offered to localhost: %q", c.Token)
	}

	// The next write drops the old fields from the file.
	if err := WriteFile(path, f); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var onDisk map[string]json.RawMessage
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatal(err)
	}
	if _, ok := onDisk["token"]; ok {
		t.Errorf("the old token field was written again: %s", raw)
	}
	if _, ok := onDisk["login"]; ok {
		t.Errorf("the old login field was written again: %s", raw)
	}
	if _, ok := onDisk["tokens"]; !ok {
		t.Errorf("tokens is missing: %s", raw)
	}

	// A newer entry for the same site is not replaced by the old token.
	path = writeConfig(t, home, `{"token": "old-token", "tokens": {"https://zeroseries.dev": {"token": "new-token", "login": "kryo"}}}`)
	f, err = ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if f.Tokens[DefaultAPIURL].Token != "new-token" || f.Token != "" {
		t.Errorf("migrated file = %+v", f)
	}
}

func TestRemoveTokenLeavesOtherSites(t *testing.T) {
	var f File
	f.SetToken("http://localhost:8090/", "a", "dev")
	f.SetToken("https://zeroseries.dev", "b", "kryo")
	if !f.RemoveToken("http://localhost:8090") {
		t.Error("RemoveToken found nothing")
	}
	if len(f.Tokens) != 1 || f.Tokens["https://zeroseries.dev"].Token != "b" {
		t.Errorf("tokens = %+v", f.Tokens)
	}
	if f.RemoveToken("http://localhost:8090") {
		t.Error("RemoveToken removed a site twice")
	}
	f.RemoveToken("https://zeroseries.dev/")
	if f.Tokens != nil {
		t.Errorf("tokens = %+v, want none", f.Tokens)
	}
}
