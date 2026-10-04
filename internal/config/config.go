// Package config resolves the settings of the zero command.
//
// A value comes from the first place that has one: a flag, the environment,
// the config file, then the default. Config remembers which place won, so
// `zero config list` can show it.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// The keys that `zero config get` and `zero config set` accept.
const (
	KeyAPIURL    = "api_url"
	KeyWorkspace = "workspace"
	KeyBrowser   = "browser"
	KeyColor     = "color"
	KeyEditor    = "editor"
)

// Keys lists the settings in the order `zero config list` prints them.
var Keys = []string{KeyAPIURL, KeyWorkspace, KeyBrowser, KeyColor, KeyEditor}

// Defaults.
const (
	DefaultAPIURL    = "https://zeroseries.dev"
	DefaultWorkspace = "~/zero"
	DefaultColor     = ColorAuto
)

// Values of the color setting.
const (
	ColorAuto   = "auto"
	ColorAlways = "always"
	ColorNever  = "never"
)

// Environment variables.
const (
	EnvAPIURL    = "ZERO_API_URL"
	EnvWorkspace = "ZERO_WORKSPACE"
	EnvToken     = "ZERO_TOKEN"
	EnvConfig    = "ZERO_CONFIG"
	EnvNoBrowser = "ZERO_NO_BROWSER"
	EnvNoColor   = "NO_COLOR"
	EnvEditor    = "EDITOR"
)

// Source says where a value came from.
type Source string

const (
	FromFlag    Source = "flag"
	FromEnv     Source = "env"
	FromFile    Source = "file"
	FromDefault Source = "default"
	FromNowhere Source = ""
)

// File is the config file. It also holds the tokens, so it is written with
// mode 0600.
type File struct {
	APIURL    string `json:"api_url,omitempty"`
	Workspace string `json:"workspace,omitempty"`
	Browser   *bool  `json:"browser,omitempty"`
	Color     string `json:"color,omitempty"`
	Editor    string `json:"editor,omitempty"`

	// Tokens holds one sign-in per site, keyed by the site root without a
	// trailing slash. A token is only ever sent to the site it came from.
	Tokens map[string]SiteToken `json:"tokens,omitempty"`

	// Token and Login are the single sign-in that older files hold. ReadFile
	// moves them into Tokens; they are never written again.
	Token string `json:"token,omitempty"`
	Login string `json:"login,omitempty"`
}

// SiteToken is the sign-in for one site.
type SiteToken struct {
	Token string `json:"token"`
	Login string `json:"login"`
}

// SiteKey turns a site root into the key of File.Tokens.
func SiteKey(apiURL string) string {
	return strings.TrimRight(strings.TrimSpace(apiURL), "/")
}

// SetToken saves the sign-in for one site.
func (f *File) SetToken(apiURL, token, login string) {
	if f.Tokens == nil {
		f.Tokens = map[string]SiteToken{}
	}
	f.Tokens[SiteKey(apiURL)] = SiteToken{Token: token, Login: login}
}

// RemoveToken forgets the sign-in for one site and leaves the others. It
// reports whether there was one.
func (f *File) RemoveToken(apiURL string) bool {
	key := SiteKey(apiURL)
	if _, ok := f.Tokens[key]; !ok {
		return false
	}
	delete(f.Tokens, key)
	if len(f.Tokens) == 0 {
		f.Tokens = nil
	}
	return true
}

// migrate moves the single token of an older file into Tokens. The old
// format did not say which site the token was for, so it goes to the saved
// api_url, or to the default site when none is saved.
func (f *File) migrate() {
	if f.Token != "" {
		site := SiteKey(f.APIURL)
		if site == "" {
			site = DefaultAPIURL
		}
		if _, taken := f.Tokens[site]; !taken {
			f.SetToken(site, f.Token, f.Login)
		}
	}
	f.Token, f.Login = "", ""
}

// Flags holds the global flags. An empty string means the flag was not given.
type Flags struct {
	APIURL    string
	Workspace string
	Config    string
	NoBrowser bool
	NoColor   bool
}

// Entry is one row of `zero config list`.
type Entry struct {
	Key    string
	Value  string
	Source Source
}

// Config is the resolved settings.
type Config struct {
	Path       string // the config file
	PathSource Source

	APIURL    string // the site root, without a trailing slash
	Workspace string // an absolute folder path
	Browser   bool   // open a browser on login
	Color     string // auto, always or never
	Editor    string // may be empty

	Token       string // the token for APIURL; never the token of another site
	TokenSource Source // FromEnv, FromFile, or FromNowhere when there is no token
	Login       string // the login `zero login` saved for APIURL

	File    File // the config file as read
	sources map[string]Source
}

// Error is a problem with a setting. Its text says what to do.
type Error struct{ Message string }

func (e *Error) Error() string { return e.Message }

func errorf(format string, a ...any) error {
	return &Error{Message: fmt.Sprintf(format, a...)}
}

// ResolvePath returns the config file path and where the path came from.
func ResolvePath(flags Flags, getenv func(string) string) (string, Source, error) {
	if flags.Config != "" {
		return expandHome(flags.Config, getenv), FromFlag, nil
	}
	if v := getenv(EnvConfig); v != "" {
		return expandHome(v, getenv), FromEnv, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", FromDefault, errorf("Cannot find the folder for the config file: %v. Set %s to a file path.", err, EnvConfig)
	}
	return filepath.Join(dir, "zero", "config.json"), FromDefault, nil
}

// Load resolves every setting from flags, the environment, the config file
// and the defaults, in that order.
func Load(flags Flags, getenv func(string) string) (*Config, error) {
	path, pathSource, err := ResolvePath(flags, getenv)
	if err != nil {
		return nil, err
	}
	file, err := ReadFile(path)
	if err != nil {
		return nil, err
	}
	c := &Config{Path: path, PathSource: pathSource, File: file, sources: map[string]Source{}}

	// api_url
	switch {
	case flags.APIURL != "":
		c.APIURL, c.sources[KeyAPIURL] = flags.APIURL, FromFlag
	case getenv(EnvAPIURL) != "":
		c.APIURL, c.sources[KeyAPIURL] = getenv(EnvAPIURL), FromEnv
	case file.APIURL != "":
		c.APIURL, c.sources[KeyAPIURL] = file.APIURL, FromFile
	default:
		c.APIURL, c.sources[KeyAPIURL] = DefaultAPIURL, FromDefault
	}
	if c.APIURL, err = cleanAPIURL(c.APIURL); err != nil {
		return nil, errorf("%s %s", err.Error(), whereToFix(KeyAPIURL, c.sources[KeyAPIURL], "--api-url", EnvAPIURL))
	}

	// workspace
	switch {
	case flags.Workspace != "":
		c.Workspace, c.sources[KeyWorkspace] = flags.Workspace, FromFlag
	case getenv(EnvWorkspace) != "":
		c.Workspace, c.sources[KeyWorkspace] = getenv(EnvWorkspace), FromEnv
	case file.Workspace != "":
		c.Workspace, c.sources[KeyWorkspace] = file.Workspace, FromFile
	default:
		c.Workspace, c.sources[KeyWorkspace] = DefaultWorkspace, FromDefault
	}
	c.Workspace = expandHome(c.Workspace, getenv)
	if abs, err := filepath.Abs(c.Workspace); err == nil {
		c.Workspace = abs
	}

	// browser
	switch {
	case flags.NoBrowser:
		c.Browser, c.sources[KeyBrowser] = false, FromFlag
	case truthy(getenv(EnvNoBrowser)):
		c.Browser, c.sources[KeyBrowser] = false, FromEnv
	case file.Browser != nil:
		c.Browser, c.sources[KeyBrowser] = *file.Browser, FromFile
	default:
		c.Browser, c.sources[KeyBrowser] = true, FromDefault
	}

	// color
	switch {
	case flags.NoColor:
		c.Color, c.sources[KeyColor] = ColorNever, FromFlag
	case getenv(EnvNoColor) != "":
		c.Color, c.sources[KeyColor] = ColorNever, FromEnv
	case file.Color != "":
		c.Color, c.sources[KeyColor] = file.Color, FromFile
	default:
		c.Color, c.sources[KeyColor] = DefaultColor, FromDefault
	}
	if !validColor(c.Color) {
		return nil, errorf("The config file has color %q. Use auto, always or never. Run: zero config set color auto", c.Color)
	}

	// editor: the setting first, then $EDITOR. It has no flag.
	switch {
	case file.Editor != "":
		c.Editor, c.sources[KeyEditor] = file.Editor, FromFile
	case getenv(EnvEditor) != "":
		c.Editor, c.sources[KeyEditor] = getenv(EnvEditor), FromEnv
	default:
		c.Editor, c.sources[KeyEditor] = "", FromDefault
	}

	// token: ZERO_TOKEN, or the token saved for this site and no other.
	saved := file.Tokens[SiteKey(c.APIURL)]
	c.Login = saved.Login
	switch {
	case getenv(EnvToken) != "":
		c.Token, c.TokenSource = getenv(EnvToken), FromEnv
	case saved.Token != "":
		c.Token, c.TokenSource = saved.Token, FromFile
	}
	return c, nil
}

// Source returns where the value of key came from.
func (c *Config) Source(key string) Source { return c.sources[key] }

// Get returns one setting.
func (c *Config) Get(key string) (Entry, error) {
	e := Entry{Key: key, Source: c.sources[key]}
	switch key {
	case KeyAPIURL:
		e.Value = c.APIURL
	case KeyWorkspace:
		e.Value = c.Workspace
	case KeyBrowser:
		e.Value = strconv.FormatBool(c.Browser)
	case KeyColor:
		e.Value = c.Color
	case KeyEditor:
		e.Value = c.Editor
	default:
		return Entry{}, unknownKey(key)
	}
	return e, nil
}

// List returns every setting in the order of Keys.
func (c *Config) List() []Entry {
	out := make([]Entry, 0, len(Keys))
	for _, k := range Keys {
		e, _ := c.Get(k)
		out = append(out, e)
	}
	return out
}

// Set changes one key of a File after checking the value. An empty value
// removes the key, so the setting goes back to its default.
func (f *File) Set(key, value string) error {
	value = strings.TrimSpace(value)
	switch key {
	case KeyAPIURL:
		if value != "" {
			clean, err := cleanAPIURL(value)
			if err != nil {
				return errorf("%s Example: zero config set api_url %s", err.Error(), DefaultAPIURL)
			}
			value = clean
		}
		f.APIURL = value
	case KeyWorkspace:
		f.Workspace = value
	case KeyBrowser:
		if value == "" {
			f.Browser = nil
			return nil
		}
		b, err := parseBool(value)
		if err != nil {
			return errorf("browser is %q, which is not true or false. Run: zero config set browser false", value)
		}
		f.Browser = &b
	case KeyColor:
		if value != "" && !validColor(value) {
			return errorf("color is %q, which is not auto, always or never. Run: zero config set color auto", value)
		}
		f.Color = value
	case KeyEditor:
		f.Editor = value
	default:
		return unknownKey(key)
	}
	return nil
}

// ReadFile reads the config file. A missing file is an empty File. A token
// in the older single-token format is moved to its site.
func ReadFile(path string) (File, error) {
	var f File
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return f, errorf("Cannot read the config file %s: %v. Check the file's permissions.", path, err)
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return f, nil
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return f, errorf("The config file %s is not valid JSON: %v. Fix the file or delete it.", path, err)
	}
	f.migrate()
	return f, nil
}

// WriteFile writes the config file with mode 0600. It writes a temporary
// file first and renames it, so a crash never leaves half a file.
func WriteFile(path string, f File) error {
	f.migrate()
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return errorf("Cannot create the folder %s: %v. Set %s to a file in a folder you can write.", dir, err, EnvConfig)
	}
	tmp, err := os.CreateTemp(dir, ".config-*.json")
	if err != nil {
		return errorf("Cannot write in the folder %s: %v. Set %s to a file in a folder you can write.", dir, err, EnvConfig)
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return errorf("Cannot write the config file %s: %v. Check the file's permissions.", path, err)
	}
	return nil
}

func unknownKey(key string) error {
	return errorf("There is no setting named %q. The settings are: %s.", key, strings.Join(Keys, ", "))
}

func whereToFix(key string, src Source, flag, env string) string {
	switch src {
	case FromFlag:
		return "Check the value of " + flag + "."
	case FromEnv:
		return "Check the value of " + env + "."
	default:
		return "Run: zero config set " + key + " <value>"
	}
}

// cleanAPIURL checks a site root and removes its trailing slash.
func cleanAPIURL(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("api_url is %q, which is not an http or https address.", raw)
	}
	return raw, nil
}

func validColor(v string) bool {
	return v == ColorAuto || v == ColorAlways || v == ColorNever
}

func parseBool(v string) (bool, error) {
	switch strings.ToLower(v) {
	case "true", "yes", "on", "1":
		return true, nil
	case "false", "no", "off", "0":
		return false, nil
	}
	return false, fmt.Errorf("not a boolean: %q", v)
}

// truthy reports whether an environment switch such as ZERO_NO_BROWSER is on.
func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "0", "false", "no", "off":
		return false
	}
	return true
}

// expandHome replaces a leading ~ with the home directory.
func expandHome(p string, getenv func(string) string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") && !strings.HasPrefix(p, `~\`) {
		return p
	}
	home := homeDir(getenv)
	if home == "" {
		return p
	}
	return filepath.Join(home, p[1:])
}

func homeDir(getenv func(string) string) string {
	for _, k := range []string{"HOME", "USERPROFILE"} {
		if v := getenv(k); v != "" {
			return v
		}
	}
	h, _ := os.UserHomeDir()
	return h
}
