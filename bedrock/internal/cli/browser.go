// browser.go opens a page in the person's browser, for the bootstrap's GitHub Apps page.

package cli

import (
	"context"
	"os/exec"
	"runtime"

	"github.com/go-playground/errors/v5"
)

// openBrowser opens the address in the person's browser: xdg-open on Linux, open on
// macOS. Without either it answers the error, and the address is printed alone.
func openBrowser(ctx context.Context, url string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return errors.Wrapf(err, "no %s on this computer", name)
	}
	cmd := exec.CommandContext(ctx, path, url)
	if err := cmd.Start(); err != nil {
		return errors.Wrapf(err, "%s", name)
	}
	go func() {
		_ = cmd.Wait()
	}()

	return nil
}
