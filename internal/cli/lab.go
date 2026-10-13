package cli

import (
	"context"
	"errors"
	"os"
	"os/signal"

	"github.com/KalebHawkins/zero/internal/check"
	"github.com/KalebHawkins/zero/spec"
)

// runLab runs the checks of a lab stage (docs/SPEC.md section 5). Ctrl+C
// stops the tests, and the stage's container, volume and image are still
// removed by name.
func (a *app) runLab(dir string, ex spec.Exercise) (spec.Report, error) {
	if ex.Lab == nil {
		return spec.Report{}, fail("This stage runs in a container, but %s has no lab settings saved. Get the stage again with: zero start %s", dir, ex.ID)
	}
	run := a.Runner
	if run == nil {
		run = check.Exec
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	a.out.line("This stage builds and runs a container with podman. The first run downloads the base images and can take a few minutes.")
	a.out.blank()
	rep, err := check.Lab(ctx, dir, ex, run, a.Version, a.Now)
	var podman *check.PodmanError
	switch {
	case errors.Is(err, check.ErrNoPodman):
		return rep, fail("%s", noPodman)
	case errors.As(err, &podman):
		return rep, fail(podmanDown, indent(podman.Output, "    "))
	case errors.Is(err, context.Canceled):
		return rep, fail("Stopped. The stage's container, volume and image were removed: %s, %s and %s. Run: zero test",
			ex.Lab.Names.Container, ex.Lab.Names.Volume, ex.Lab.Names.Image)
	}
	return rep, err
}

// noPodman says how to install podman. No report is posted.
const noPodman = `This stage runs your server in a container, and the podman command was not found.
Install podman: https://podman.io/docs/installation
On macOS and Windows, podman runs in a small Linux machine. Make it once, then start it:
    podman machine init
    podman machine start
Then run: zero test`

// podmanDown says that podman is installed and does not answer.
const podmanDown = `podman is installed, but podman info failed:
%s
On macOS and Windows, start podman's machine with: podman machine start
Then run: zero test`
