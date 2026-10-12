package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/KalebHawkins/zero/spec"
)

// A stage with several uses says why the copies are needed once, and lists
// the uses with nothing to copy on one line.
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
		"copy it in with: zero use vectors\n  vec.go -> internal/v/vec.go\n",
		"copy it in with: zero use bezier\n  bezier.go -> internal/v/bezier.go\n",
		"This stage's code calls each of them, so without them the build fails.",
		"This stage builds on Scattering from a seed and The game loop. There is nothing to copy from them.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "the build fails"); n != 1 {
		t.Errorf("\"the build fails\" appears %d times, want 1:\n%s", n, out)
	}
}
