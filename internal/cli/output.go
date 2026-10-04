package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/KalebHawkins/zero/internal/config"
)

// Marks. They are plain characters, not emoji.
const (
	markPass = "✓"
	markFail = "✗"
	markNote = "-"
)

// printer writes to standard output, with color when color is on.
type printer struct {
	w     io.Writer
	color bool
}

func newPrinter(w io.Writer, color bool) *printer { return &printer{w: w, color: color} }

// useColor decides whether to color: never and always are fixed; auto colors
// only a terminal that can show it.
func useColor(setting string, isTerminal bool, term string) bool {
	switch setting {
	case config.ColorNever:
		return false
	case config.ColorAlways:
		return true
	}
	return isTerminal && term != "dumb"
}

func (p *printer) line(format string, a ...any) {
	fmt.Fprintf(p.w, format+"\n", a...)
}

func (p *printer) blank() { fmt.Fprintln(p.w) }

func (p *printer) raw(s string) { io.WriteString(p.w, s) }

func (p *printer) paint(code, s string) string {
	if !p.color {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (p *printer) green(s string) string { return p.paint("32", s) }
func (p *printer) red(s string) string   { return p.paint("31", s) }
func (p *printer) bold(s string) string  { return p.paint("1", s) }
func (p *printer) dim(s string) string   { return p.paint("2", s) }

// indent puts prefix before every line of s.
func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = prefix + l
		}
	}
	return strings.Join(lines, "\n")
}

// warn prints one warning line to standard error.
func (a *app) warn(format string, args ...any) {
	fmt.Fprintf(a.Stderr, "Warning: "+format+"\n", args...)
}
