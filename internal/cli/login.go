package cli

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/KalebHawkins/zero/internal/api"
	"github.com/KalebHawkins/zero/internal/config"
	"github.com/KalebHawkins/zero/spec"
)

// login links this computer to an account with the device flow: the server
// gives a code, the learner approves it on the site, and the command polls
// until the server hands over a token.
func (a *app) login() error {
	cfg, err := a.settings()
	if err != nil {
		return err
	}
	client := a.client(cfg)
	client.Token = "" // the device flow needs no sign-in

	hostname, _ := os.Hostname()
	dev, err := client.Device(spec.DeviceRequest{Hostname: hostname, OS: runtime.GOOS, Version: a.Version})
	if err != nil {
		return err
	}
	if dev.DeviceCode == "" || dev.UserCode == "" {
		return fail("%s did not send a sign-in code. Check that api_url is the site address: zero config list", cfg.APIURL)
	}
	// The complete link carries the code, so the page opens with it filled in.
	page := dev.VerificationURIComplete
	if page == "" {
		page = dev.VerificationURI
	}
	if page == "" {
		page = cfg.APIURL + "/cli/?code=" + url.QueryEscape(dev.UserCode)
	}

	a.out.line("Your code is: %s", a.out.bold(dev.UserCode))
	a.out.line("Approve it on this page: %s", page)
	if cfg.Browser && dev.VerificationURIComplete != "" && a.OpenBrowser != nil {
		if err := a.OpenBrowser(dev.VerificationURIComplete); err == nil {
			a.out.line("A browser is opening that page with the code filled in.")
		}
	}
	a.out.line("Waiting for you to approve the code. Press Ctrl+C to stop.")

	interval := time.Duration(dev.Interval) * time.Second
	if interval <= 0 {
		interval = 2 * time.Second
	}
	expires := time.Duration(dev.ExpiresIn) * time.Second
	if expires <= 0 {
		expires = 10 * time.Minute
	}
	deadline := a.Now().Add(expires)

	netFailures := 0
	for {
		a.Sleep(interval)
		tok, err := client.PollToken(dev.DeviceCode)
		var netErr *api.NetError
		switch {
		case err == nil:
			return a.saveLogin(cfg, tok)
		case errors.Is(err, api.ErrExpired):
			return fail("The code expired before it was approved. Run: zero login")
		case errors.Is(err, api.ErrPending):
			netFailures = 0
		case errors.As(err, &netErr):
			// One dropped connection should not end the login.
			if netFailures++; netFailures >= 3 {
				return err
			}
		default:
			return err
		}
		if !a.Now().Before(deadline) {
			return fail("The code expired before it was approved. Run: zero login")
		}
	}
}

func (a *app) saveLogin(cfg *config.Config, tok *spec.TokenResponse) error {
	if tok.Token == "" {
		return fail("%s approved the code but sent no token. Run: zero login", cfg.APIURL)
	}
	// The token is saved for this site only. Other sites keep theirs.
	file := cfg.File
	file.SetToken(cfg.APIURL, tok.Token, tok.User.Login)
	if err := config.WriteFile(cfg.Path, file); err != nil {
		return fail("%s", err.Error())
	}
	a.out.line("%s Signed in as %s.", a.out.green(markPass), a.out.bold(tok.User.Login))
	if cfg.TokenSource == config.FromEnv {
		a.warn("%s is set and wins over the saved sign-in. Unset it to use this sign-in.", config.EnvToken)
	}
	a.out.line("Next: zero doctor")
	return nil
}

// logout ends the sign-in for the current site. It asks the site to end the
// token, then removes the token from the config file. The request is best
// effort: when the site cannot be reached or no longer knows the token, the
// token is still removed here. Tokens of other sites are left alone.
func (a *app) logout() error {
	cfg, err := a.settings()
	if err != nil {
		return err
	}
	file := cfg.File
	saved, ok := file.Tokens[config.SiteKey(cfg.APIURL)]
	if !ok {
		a.out.line("No sign-in is saved for %s. There is nothing to forget.", cfg.APIURL)
	} else {
		if saved.Token != "" {
			client := a.client(cfg)
			client.Token = saved.Token // the saved token, not ZERO_TOKEN
			err := client.Logout()
			var apiErr *api.Error
			var netErr *api.NetError
			switch {
			case err == nil, errors.As(err, &netErr):
			case errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized:
			default:
				a.warn("%s did not end the token. %s It is removed from this computer anyway.", cfg.APIURL, joinSentence(explain(err), ""))
			}
		}
		file.RemoveToken(cfg.APIURL)
		if err := config.WriteFile(cfg.Path, file); err != nil {
			return fail("%s", err.Error())
		}
		a.out.line("Signed out of %s. The token is removed from %s.", cfg.APIURL, cfg.Path)
	}
	if cfg.TokenSource == config.FromEnv {
		a.warn("%s is still set, so commands stay signed in. Unset it to sign out.", config.EnvToken)
	}
	return nil
}

func (a *app) whoami() error {
	cfg, client, err := a.signedIn()
	if err != nil {
		return err
	}
	user, err := client.Me()
	var apiErr *api.Error
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized {
		return fail("%s does not accept the saved sign-in. Run: zero login", cfg.APIURL)
	}
	if err != nil {
		return err
	}
	if user.Name != "" && user.Name != user.Login {
		a.out.line("Signed in as %s (%s) on %s.", a.out.bold(user.Login), user.Name, cfg.APIURL)
	} else {
		a.out.line("Signed in as %s on %s.", a.out.bold(user.Login), cfg.APIURL)
	}
	return nil
}

// OpenBrowser opens a web page in the default browser. It is the Env.OpenBrowser
// of the real command. It does not wait for the browser.
func OpenBrowser(page string) error {
	u, err := url.Parse(page)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("not a web address")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", page)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", page)
	default:
		cmd = exec.Command("xdg-open", page)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // collect the helper process when it ends
	return nil
}
