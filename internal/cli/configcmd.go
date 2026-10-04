package cli

import (
	"strings"

	"github.com/KalebHawkins/zero/internal/config"
)

// config runs `zero config [list|get <key>|set <key> <value>|unset <key>|path]`.
func (a *app) config() error {
	sub := "list"
	args := a.args
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "list":
		if len(args) != 0 {
			return usage("zero config list takes no arguments. Run: zero config list")
		}
		return a.configList()
	case "get":
		if len(args) != 1 {
			return usage("zero config get needs one key. Example: zero config get workspace")
		}
		return a.configGet(args[0])
	case "set":
		if len(args) != 2 {
			return usage("zero config set needs a key and a value. Example: zero config set workspace ~/code/zero")
		}
		return a.configSet(args[0], args[1])
	case "unset":
		if len(args) != 1 {
			return usage("zero config unset needs one key. Example: zero config unset workspace")
		}
		return a.configSet(args[0], "")
	case "path":
		if len(args) != 0 {
			return usage("zero config path takes no arguments. Run: zero config path")
		}
		return a.configPath()
	}
	return usage("zero config has no command named %q. Use list, get, set, unset or path.", sub)
}

func (a *app) configList() error {
	cfg, err := a.settings()
	if err != nil {
		return err
	}
	rows := [][3]string{}
	for _, e := range cfg.List() {
		rows = append(rows, [3]string{e.Key, shown(e.Value), string(e.Source)})
	}
	// The token row is about the current site only.
	token, tokenSource := "(not set)", "default"
	if cfg.Token != "" {
		token, tokenSource = "(set, hidden)", string(cfg.TokenSource)
	}
	rows = append(rows, [3]string{"token", token, tokenSource})
	rows = append(rows, [3]string{"config file", cfg.Path, string(cfg.PathSource)})

	widthKey, widthValue := 0, 0
	for _, r := range rows {
		widthKey = max(widthKey, len(r[0]))
		widthValue = max(widthValue, len(r[1]))
	}
	for _, r := range rows {
		a.out.line("%s  %s  %s",
			a.out.bold(pad(r[0], widthKey)), pad(r[1], widthValue), a.out.dim("from "+r[2]))
	}
	return nil
}

func (a *app) configGet(key string) error {
	cfg, err := a.settings()
	if err != nil {
		return err
	}
	e, err := cfg.Get(key)
	if err != nil {
		return fail("%s", err.Error())
	}
	a.out.line("%s", e.Value)
	return nil
}

// configSet reads only the file, so it can repair a file that Load refuses.
func (a *app) configSet(key, value string) error {
	path, _, err := config.ResolvePath(a.flags, a.Getenv)
	if err != nil {
		return fail("%s", err.Error())
	}
	file, err := config.ReadFile(path)
	if err != nil {
		return fail("%s", err.Error())
	}
	if err := file.Set(key, value); err != nil {
		return fail("%s", err.Error())
	}
	if err := config.WriteFile(path, file); err != nil {
		return fail("%s", err.Error())
	}
	if strings.TrimSpace(value) == "" {
		a.out.line("Removed %s from %s. It now uses its default.", key, path)
	} else {
		a.out.line("Saved %s in %s.", key, path)
	}

	// Say so when the saved value is not the one in effect.
	if cfg, err := config.Load(a.flags, a.Getenv); err == nil {
		switch src := cfg.Source(key); {
		case key == config.KeyEditor:
		case src == config.FromFlag:
			a.out.line("A flag sets %s for this command, so the saved value was not used here.", key)
		case src == config.FromEnv:
			a.out.line("The environment sets %s and wins over the config file. Run zero config list to see it.", key)
		}
	}
	return nil
}

func (a *app) configPath() error {
	path, _, err := config.ResolvePath(a.flags, a.Getenv)
	if err != nil {
		return fail("%s", err.Error())
	}
	a.out.line("%s", path)
	return nil
}

func shown(v string) string {
	if v == "" {
		return "(not set)"
	}
	return v
}

func pad(s string, width int) string {
	if n := width - len(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}
