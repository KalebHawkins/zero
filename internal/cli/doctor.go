package cli

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/KalebHawkins/zero/internal/api"
	"github.com/KalebHawkins/zero/internal/config"
	"github.com/KalebHawkins/zero/internal/workspace"
	"github.com/KalebHawkins/zero/spec"
)

// The oldest Go that the exercises accept.
const minGoMajor, minGoMinor = 1, 22

// doctor checks that this computer is ready. It starts only command-line
// tools with a version flag; it never opens a window.
func (a *app) doctor() error {
	report := a.takeFlag("--report")
	if len(a.args) > 0 {
		return usage("zero doctor takes only --report, but got %q. Run: zero doctor", strings.Join(a.args, " "))
	}
	cfg, err := a.settings()
	if err != nil {
		return err
	}
	client := a.client(cfg)

	site := checkAPI(cfg, client)
	doc := spec.Doctor{
		At:  a.Now().UTC().Format(time.RFC3339),
		CLI: a.Version,
		Checks: []spec.Check{
			checkGo(),
			checkGit(),
			checkPodman(),
			checkEditor(cfg),
			checkWorkspace(cfg),
			site,
		},
	}
	doc.OK = true
	failed := 0
	for _, c := range doc.Checks {
		if !c.OK && !c.Optional {
			doc.OK = false
			failed++
		}
	}

	if report {
		a.printDoctorReport(cfg, doc)
	} else {
		a.printDoctor(doc, failed)
	}

	switch {
	case cfg.Token != "" && site.OK:
		if err := client.PostDoctor(doc); err != nil {
			a.warn("the result was not sent to the site. %s", explain(err))
		}
	case cfg.Token != "":
		// The api check already says the site does not answer.
		a.warn("the result was not sent to the site, because the site does not answer.")
	case !report:
		a.out.line("Not signed in, so the site does not know this result. Run: zero login")
	}
	if !doc.OK {
		return silent
	}
	return nil
}

func (a *app) printDoctor(doc spec.Doctor, failed int) {
	width := 0
	for _, c := range doc.Checks {
		width = max(width, len(c.Name))
	}
	for _, c := range doc.Checks {
		mark := a.out.green(markPass)
		switch {
		case !c.OK && c.Optional:
			mark = a.out.dim(markNote)
		case !c.OK:
			mark = a.out.red(markFail)
		}
		detail := c.Detail
		if !c.OK && c.Optional {
			detail += " (optional)"
		}
		a.out.line("%s %s  %s", mark, a.out.bold(pad(c.Name, width)), detail)
		if c.Fix != "" {
			a.out.line("  %s  %s", pad("", width), c.Fix)
		}
	}
	a.out.blank()
	switch {
	case failed == 0:
		a.out.line("This computer is ready.")
	case failed == 1:
		a.out.line("1 check failed. Fix it, then run: zero doctor")
	default:
		a.out.line("%d checks failed. Fix them, then run: zero doctor", failed)
	}
}

// printDoctorReport prints a plain block to paste into an issue. It has no
// color and no token.
func (a *app) printDoctorReport(cfg *config.Config, doc spec.Doctor) {
	p := a.out
	p.line("zero doctor report")
	p.line("zero:    %s", a.Version)
	p.line("os:      %s/%s", runtime.GOOS, runtime.GOARCH)
	p.line("api_url: %s", cfg.APIURL)
	p.line("at:      %s", doc.At)
	result := "ok"
	if !doc.OK {
		result = "not ok"
	}
	p.line("result:  %s", result)
	for _, c := range doc.Checks {
		state := "[ok]  "
		switch {
		case !c.OK && c.Optional:
			state = "[note]"
		case !c.OK:
			state = "[fail]"
		}
		p.line("%s %s: %s", state, c.Name, c.Detail)
		if c.Fix != "" {
			p.line("       fix: %s", c.Fix)
		}
	}
}

// toolVersion runs `<name> <arg>` and returns its first output line.
func toolVersion(name, arg string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, arg).Output()
	if err != nil {
		return "", err
	}
	first, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return strings.TrimSpace(first), nil
}

var goVersionRE = regexp.MustCompile(`go(\d+)\.(\d+)(\.\d+|rc\d+|beta\d+)?`)

// parseGoVersion finds the version in the output of `go version`, for
// example "go version go1.26.3 linux/amd64".
func parseGoVersion(out string) (version string, major, minor int, ok bool) {
	m := goVersionRE.FindStringSubmatch(out)
	if m == nil {
		return "", 0, 0, false
	}
	major, _ = strconv.Atoi(m[1])
	minor, _ = strconv.Atoi(m[2])
	return m[0], major, minor, true
}

func goIsNewEnough(major, minor int) bool {
	return major > minGoMajor || (major == minGoMajor && minor >= minGoMinor)
}

func checkGo() spec.Check {
	c := spec.Check{Name: "go"}
	fix := fmt.Sprintf("Install Go %d.%d or newer from https://go.dev/dl/ and open a new terminal.", minGoMajor, minGoMinor)
	out, err := toolVersion("go", "version")
	if err != nil {
		c.Detail, c.Fix = "the go command was not found", fix
		return c
	}
	version, major, minor, ok := parseGoVersion(out)
	if !ok {
		c.Detail, c.Fix = fmt.Sprintf("cannot read the version in %q", out), fix
		return c
	}
	c.Detail = version
	if !goIsNewEnough(major, minor) {
		c.Detail = fmt.Sprintf("%s is older than go%d.%d", version, minGoMajor, minGoMinor)
		c.Fix = fix
		return c
	}
	c.OK = true
	return c
}

func checkGit() spec.Check {
	c := spec.Check{Name: "git"}
	out, err := toolVersion("git", "--version")
	if err != nil {
		c.Detail = "the git command was not found"
		c.Fix = "Install git from https://git-scm.com/downloads and open a new terminal."
		return c
	}
	c.OK, c.Detail = true, out
	return c
}

func checkPodman() spec.Check {
	c := spec.Check{Name: "podman", Optional: true}
	out, err := toolVersion("podman", "--version")
	if err != nil {
		c.Detail = "the podman command was not found"
		c.Fix = "Install podman from https://podman.io when an exercise asks for containers."
		return c
	}
	c.OK, c.Detail = true, out
	return c
}

// checkEditor looks for an editor: the editor setting, then $EDITOR, then
// the code command of VS Code. It only looks on PATH; it starts nothing.
func checkEditor(cfg *config.Config) spec.Check {
	c := spec.Check{Name: "editor", Optional: true}
	command, from := cfg.Editor, "the editor setting"
	if cfg.Source(config.KeyEditor) == config.FromEnv {
		from = "$EDITOR"
	}
	if command == "" {
		command, from = "code", "found on PATH"
	}
	fields := strings.Fields(command)
	if len(fields) > 0 {
		if _, err := exec.LookPath(fields[0]); err == nil {
			c.OK, c.Detail = true, fmt.Sprintf("%s (%s)", command, from)
			return c
		}
	}
	if cfg.Editor == "" {
		c.Detail = "no editor is set and the code command was not found"
	} else {
		c.Detail = fmt.Sprintf("%s (%s) was not found", command, from)
	}
	c.Fix = "Install VS Code from https://code.visualstudio.com or name your editor with: zero config set editor <command>"
	return c
}

func checkWorkspace(cfg *config.Config) spec.Check {
	c := spec.Check{Name: "workspace"}
	if err := workspace.CheckWritable(cfg.Workspace); err != nil {
		c.Detail = fmt.Sprintf("cannot write in %s: %v", cfg.Workspace, err)
		c.Fix = "Choose a folder you can write with: zero config set workspace <folder>"
		return c
	}
	c.OK, c.Detail = true, cfg.Workspace+" is writable"
	return c
}

func checkAPI(cfg *config.Config, client *api.Client) spec.Check {
	c := spec.Check{Name: "api"}
	if _, err := client.ServerConfig(); err != nil {
		var netErr *api.NetError
		if errors.As(err, &netErr) {
			c.Detail = fmt.Sprintf("cannot reach %s: %v", cfg.APIURL, netErr.Err)
		} else {
			c.Detail = fmt.Sprintf("%s answered with an error: %s", cfg.APIURL, err.Error())
		}
		c.Fix = "Check your connection, or set the site address with: zero config set api_url <url>"
		return c
	}
	c.OK, c.Detail = true, cfg.APIURL+" answers"
	return c
}
