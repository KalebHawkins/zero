package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/KalebHawkins/zero/spec"
)

// A stage with several uses names the copies on one line, leaves their
// zero use commands to the Next block, and lists the uses with nothing to
// copy on one line.
func TestPrintUsesSaysEachThingOnce(t *testing.T) {
	var b bytes.Buffer
	a := &app{out: newPrinter(&b, false)}
	a.printUses(t.TempDir(), []spec.Use{
		{Exercise: "vectors", Title: "Vectors", Copy: []spec.Copy{{From: "vec.go", To: "internal/v/vec.go"}}},
		{Exercise: "bezier", Title: "Curves", Copy: []spec.Copy{{From: "bezier.go", To: "internal/v/bezier.go"}}},
		{Exercise: "scatter", Title: "Scattering from a seed"},
		{Exercise: "the-game-loop", Title: "The game loop"},
	})
	out := b.String()
	for _, want := range []string{
		"This stage builds on your code from: Vectors, Curves. Copy them in before zero test, or the build fails.\n",
		"This stage builds on Scattering from a seed and The game loop. There is nothing to copy from them.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	for _, once := range []string{"the build fails", "Vectors", "Curves"} {
		if n := strings.Count(out, once); n != 1 {
			t.Errorf("%q appears %d times, want 1:\n%s", once, n, out)
		}
	}
	if strings.Contains(out, "zero use") {
		t.Errorf("printUses names a zero use command; the Next block lists them:\n%s", out)
	}
}
