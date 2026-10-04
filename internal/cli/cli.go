// Package cli holds the commands of zero.
package cli

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/KalebHawkins/zero/internal/api"
	"github.com/KalebHawkins/zero/internal/config"
	"github.com/KalebHawkins/zero/internal/workspace"
)

// Env is everything a command touches outside its arguments. The tests
// replace these to run commands in-process.
type Env struct {
	Version    string
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	Getenv     func(string) string
	Dir        string // the current folder
	IsTerminal bool   // Stdout is a terminal

	OpenBrowser func(url string) error // opens the login page
	Sleep       func(time.Duration)    // waits between login polls
	Now         func() time.Time
	HTTP        *http.Client // nil: a client with the standard timeout
}

// app is one invocation: the environment plus what the arguments said.
type app struct {
	Env
	flags config.Flags
	args  []string // the command's own arguments; command flags such as --force stay here
	out   *printer
}

// failure is an error whose text is ready for the learner. A failure with
// an empty message prints nothing: the command already explained itself.
type failure struct {
	msg  string
	code int
}

func (f *failure) Error() string { return f.msg }

// fail builds an error that says what went wrong and what to do.
func fail(format string, a ...any) error {
	return &failure{msg: fmt.Sprintf(format, a...), code: 1}
}

// usage builds an error for a command typed wrongly.
func usage(format string, a ...any) error {
	return &failure{msg: fmt.Sprintf(format, a...), code: 2}
}

// silent ends the command with exit code 1 and no further text.
var silent = &failure{code: 1}

// Run runs one command and returns the exit code.
func Run(args []string, env Env) int {
	a := &app{Env: env}
	command, err := a.parse(args)
	if err == nil {
		err = a.dispatch(command)
	}
	if err == nil {
		return 0
	}
	var f *failure
	if !errors.As(err, &f) {
		f = &failure{msg: explain(err), code: 1}
	}
	if f.msg != "" {
		fmt.Fprintln(env.Stderr, f.msg)
	}
	return f.code
}

// parse splits the arguments into global flags, the command name and the
// command's own arguments. Global flags may stand before or after the name.
func (a *app) parse(args []string) (string, error) {
	var rest []string
	help := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			rest = append(rest, args[i+1:]...)
			break
		}
		name, value, hasValue := strings.Cut(arg, "=")
		takeValue := func(example string) (string, error) {
			if hasValue {
				return value, nil
			}
			if i+1 >= len(args) {
				return "", usage("The flag %s needs a value. Example: zero %s %s", name, name, example)
			}
			i++
			return args[i], nil
		}
		var err error
		switch name {
		case "--api-url":
			a.flags.APIURL, err = takeValue("http://localhost:8090")
		case "--workspace":
			a.flags.Workspace, err = takeValue("~/zero")
		case "--config":
			a.flags.Config, err = takeValue("~/zero-config.json")
		case "--no-browser":
			a.flags.NoBrowser = true
		case "--no-color":
			a.flags.NoColor = true
		case "-h", "--help":
			help = true
		case "--version":
			rest = append([]string{"version"}, rest...)
		default:
			rest = append(rest, arg)
		}
		if err != nil {
			return "", err
		}
	}
	// Color needs the settings, but help and config repairs must work even
	// when the config file is damaged. Fall back to no color.
	a.out = newPrinter(a.Stdout, false)
	if cfg, err := config.Load(a.flags, a.Getenv); err == nil {
		a.out = newPrinter(a.Stdout, useColor(cfg.Color, a.IsTerminal, a.Getenv("TERM")))
	}
	if help || len(rest) == 0 {
		return "help", nil
	}
	a.args = rest[1:]
	return rest[0], nil
}

func (a *app) dispatch(command string) error {
	switch command {
	case "help":
		return a.help()
	case "version":
		return a.noArgs(command, a.version)
	case "config":
		return a.config()
	case "login":
		return a.noArgs(command, a.login)
	case "logout":
		return a.noArgs(command, a.logout)
	case "whoami":
		return a.noArgs(command, a.whoami)
	case "doctor":
		return a.doctor()
	case "start":
		return a.start()
	case "test":
		return a.noArgs(command, a.test)
	case "run":
		return a.noArgs(command, a.run)
	case "hint":
		return a.noArgs(command, a.hint)
	case "submit":
		return a.noArgs(command, a.submit)
	case "next":
		return a.noArgs(command, a.next)
	}
	return usage("zero has no command named %q. Run: zero help", command)
}

// noArgs runs a command that takes no arguments of its own.
func (a *app) noArgs(command string, run func() error) error {
	if len(a.args) > 0 {
		return usage("zero %s takes no arguments, but got %q. Run: zero help", command, strings.Join(a.args, " "))
	}
	return run()
}

// takeFlag removes a command flag such as --force from the arguments and
// says whether it was there.
func (a *app) takeFlag(name string) bool {
	found := false
	kept := a.args[:0:0]
	for _, arg := range a.args {
		if arg == name {
			found = true
			continue
		}
		kept = append(kept, arg)
	}
	a.args = kept
	return found
}

// settings loads the configuration.
func (a *app) settings() (*config.Config, error) {
	cfg, err := config.Load(a.flags, a.Getenv)
	if err != nil {
		return nil, fail("%s", err.Error())
	}
	return cfg, nil
}

func (a *app) client(cfg *config.Config) *api.Client {
	c := api.New(cfg.APIURL, cfg.Token, a.Version)
	if a.HTTP != nil {
		c.HTTP = a.HTTP
	}
	return c
}

// signedIn loads the settings and makes sure there is a token.
func (a *app) signedIn() (*config.Config, *api.Client, error) {
	cfg, err := a.settings()
	if err != nil {
		return nil, nil, err
	}
	if cfg.Token == "" {
		return nil, nil, fail("Not signed in. Run: zero login")
	}
	return cfg, a.client(cfg), nil
}

// explain turns any error into text that says what went wrong and what to do.
func explain(err error) string {
	var apiErr *api.Error
	var netErr *api.NetError
	var cfgErr *config.Error
	switch {
	case errors.As(err, &apiErr):
		if apiErr.Status == http.StatusUnauthorized {
			return joinSentence(apiErr.Message, "Run: zero login")
		}
		return apiErr.Message
	case errors.As(err, &netErr):
		return fmt.Sprintf("Cannot reach %s (%v). Check your connection, or set the address with: zero config set api_url <url>", netErr.BaseURL, netErr.Err)
	case errors.As(err, &cfgErr):
		return cfgErr.Message
	case errors.Is(err, workspace.ErrNotInExercise):
		return "This folder is not inside an exercise. Go to an exercise folder, or get one with: zero start <id>"
	}
	return joinSentence(capitalize(err.Error()), "Run zero doctor to check your setup.")
}

// joinSentence puts advice after a message, adding the period the message
// may lack.
func joinSentence(msg, advice string) string {
	msg = strings.TrimSpace(msg)
	if msg != "" && !strings.ContainsAny(msg[len(msg)-1:], ".!?") {
		msg += "."
	}
	return strings.TrimSpace(msg + " " + advice)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func (a *app) version() error {
	a.out.line("zero %s", a.Version)
	return nil
}

func (a *app) help() error {
	a.out.raw(helpText)
	return nil
}

const helpText = `zero is the command for Zero Series practice.

Usage:
  zero <command> [flags]

Set up:
  login                      sign in to the site from this computer
  logout                     end the sign-in for the current site
  whoami                     show who is signed in
  doctor [--report]          check that this computer is ready
                             --report prints a block to paste into an issue

Practice:
  start <id> [--force]       get an exercise
                             --force replaces your files with the starter files
  test                       run the tests of the exercise in this folder
  run                        run the program of the exercise in this folder
  hint                       show the next hint
  submit                     finish the exercise when every test passes
  next                       show what comes next on your path

Settings:
  config list                show every setting and where its value comes from
  config get <key>           show one setting
  config set <key> <value>   save one setting in the config file
  config unset <key>         remove one setting from the config file,
                             so it uses its default again
  config path                show where the config file is
  version                    show the version
  help                       show this text

Setting keys:
  api_url      the site address                         default https://zeroseries.dev
  workspace    the folder that holds your exercises     default ~/zero
  browser      open a browser on login: true or false   default true
  color        auto, always or never                    default auto
  editor       the command that starts your editor      default $EDITOR

Flags, before or after the command:
  --api-url <url>      the site address               (or ZERO_API_URL)
  --workspace <dir>    the workspace folder           (or ZERO_WORKSPACE)
  --config <file>      the config file                (or ZERO_CONFIG)
  --no-browser         do not open a browser on login (or ZERO_NO_BROWSER=1)
  --no-color           do not use color               (or NO_COLOR)

A flag wins over the environment. The environment wins over the config file.
A sign-in belongs to one site address. Another api_url needs its own login.
`
