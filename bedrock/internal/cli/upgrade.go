// upgrade.go is the upgrade command: move the application to a bedrock release.

package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/release"
)

// upgradeCommand is the name of the upgrade command.
const upgradeCommand = "upgrade"

func newUpgrade(d deps) *cobra.Command {
	var (
		appFlag   string
		dirFlag   string
		placement string
	)

	cmd := &cobra.Command{
		Use:   upgradeCommand + " [version] [--app <dir>] [--dir <dir>]",
		Short: "Move the application to a bedrock release",
		Long: `upgrade moves the application's bedrock pin: the version and the checksum in placement.json
(bedrockVersion, bedrockSha256) that its pipeline and its infrastructure workflow download and
verify before running. The version is the one given (v0.4.0), else the latest release; a
pre-release is moved to by name. The checksum is read from the release's checksums.txt, never
typed. The release is then fetched for this machine (into the user's cache directory, verified
against the same file) and the stack and the pipeline are rendered with it, so the committed files
come from the bedrock the pin names. Commit placement.json with the rendered files: from that
commit the pipeline's first step and the infrastructure check download and verify that release,
and every bedrock deploy step runs it. The pin is the one place a bedrock version appears in the
application.

render and check refuse a placement pinned to another release than the bedrock running them, so
the installed bedrock is kept at the pinned version; upgrade is the command that moves it. Run from
anywhere inside the repository, it finds the stack and the application as render does; --dir,
--app and --placement override.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			appDir, dir, err := d.stack(appFlag, dirFlag)
			if err != nil {
				return err
			}
			if placement == "" {
				placement = filepath.Join(dir, placementFile)
			}

			return d.upgrade(cmd, args, appDir, dir, placement)
		},
	}

	cmd.Flags().StringVar(&appFlag, "app", "", "application root, the directory holding go.mod (default: the repository root when the stack is in its infrastructure directory, else the working directory)")
	cmd.Flags().StringVar(&dirFlag, "dir", "", "the stack directory (default: the application's stack, found from the working directory)")
	cmd.Flags().StringVar(&placement, "placement", "", "placement file (default: placement.json in the stack directory)")
	_ = cmd.MarkFlagDirname("app")
	_ = cmd.MarkFlagDirname("dir")

	return cmd
}

// upgrade moves the pin in the placement and renders with the release it names.
func (d deps) upgrade(cmd *cobra.Command, args []string, appDir, dir, placement string) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()
	p, err := derive.ReadPlacement(placement)
	if err != nil {
		return err
	}
	src := d.releases()
	version, err := targetVersion(ctx, src, args)
	if err != nil {
		return err
	}
	sums, err := src.Checksums(ctx, version)
	if err != nil {
		return err
	}
	pipelineSum, ok := sums[release.PipelineAsset()]
	if !ok {
		return errors.Newf("bedrock %s publishes no %s, the binary the pipeline runs: its %s does not list it", version, release.PipelineAsset(), release.ChecksumsFile)
	}
	asset := release.Asset(runtime.GOOS, runtime.GOARCH)
	sum, ok := sums[asset]
	if !ok {
		return errors.Newf("bedrock %s publishes no binary for %s/%s (%s): upgrade runs on a platform the release covers", version, runtime.GOOS, runtime.GOARCH, asset)
	}
	if p.BedrockVersion == version && p.BedrockSHA256 == pipelineSum {
		fmt.Fprintf(out, "%s already pins bedrock %s.\n", placement, version)

		return nil
	}
	bin, err := d.fetched(ctx, src, version, asset, sum)
	if err != nil {
		return err
	}
	was := "no bedrock"
	if p.Pinned() {
		was = "bedrock " + p.BedrockVersion
	}
	p.BedrockVersion, p.BedrockSHA256 = version, pipelineSum
	if err := derive.WritePlacement(placement, p); err != nil {
		return err
	}
	fmt.Fprintf(out, "Moved %s from %s to bedrock %s (%s %s).\n", placement, was, version, release.PipelineAsset(), pipelineSum)
	fmt.Fprintf(out, "Rendering with bedrock %s (%s):\n", version, bin)
	render := exec.CommandContext(ctx, bin, renderCommand, "--app", appDir, "--out", dir, "--placement", placement)
	render.Stdout, render.Stderr = out, cmd.ErrOrStderr()
	if err := render.Run(); err != nil {
		return errors.Newf("rendering with bedrock %s failed (%v): what it said is above", version, err)
	}
	fmt.Fprintf(out, "Commit %s with the rendered files: from that commit the pipeline and the infrastructure check run bedrock %s.\n", placement, version)

	return nil
}

// targetVersion is the release to move to: the one named, else the latest.
func targetVersion(ctx context.Context, src *release.Source, args []string) (string, error) {
	if len(args) == 0 {
		return src.Latest(ctx)
	}
	if !release.IsVersion(args[0]) {
		return "", errors.Newf("%q is not a release version: v0.4.0, the tag bedrock/v0.4.0 without its prefix", args[0])
	}

	return args[0], nil
}

// fetched is the release's binary for this machine in the cache directory: the copy
// there when it verifies, else fetched afresh and verified.
func (d deps) fetched(ctx context.Context, src *release.Source, version, asset, sum string) (string, error) {
	dir := d.cacheDir
	if dir == "" {
		userCache, err := os.UserCacheDir()
		if err != nil {
			return "", errors.Wrap(err, "os.UserCacheDir()")
		}
		dir = filepath.Join(userCache, release.Name)
	}
	path := filepath.Join(dir, version, asset)
	ok, err := release.Verify(path, sum)
	if err != nil {
		return "", err
	}
	if ok {
		return path, nil
	}
	if err := src.Fetch(ctx, version, asset, sum, path); err != nil {
		return "", err
	}

	return path, nil
}
