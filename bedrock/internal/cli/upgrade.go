// upgrade.go is the upgrade command: move the application to a bedrock release or to a
// pushed commit of bedrock.

package cli

import (
	"bytes"
	"context"
	"debug/buildinfo"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

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
		Use:   upgradeCommand + " [version|commit|branch] [--app <dir>] [--dir <dir>]",
		Short: "Move the application to a bedrock release or a pushed commit",
		Long: `upgrade moves the application's bedrock pin in placement.json (bedrockVersion, and for a release
bedrockSha256), which its pipeline and its infrastructure workflow run. The pin is one of two kinds.

A release: the version given (v0.4.0), else the latest release; a pre-release is moved to by name.
The checksum of its linux/amd64 binary is read from the release's checksums.txt, never typed, and
the pipeline and the infrastructure check download that binary and verify it against the checksum.
The release is fetched for this machine (into the user's cache directory, verified against the
same file) to render with.

A commit pin: a commit of github.com/cccteam/ccc (a full or abbreviated hash), a branch (its head
commit, read from the GitHub API) or the pseudo-version Go gives a commit. The Go module proxy
names its version (v0.0.0-lab.1.0.20260928222237-58b211dce544); the placement holds that version
and no checksum, and the pipeline and the infrastructure check build it with go install, which
verifies it against Go's checksum database. The commit must be pushed first: the proxy knows only
pushed commits, and remembers for about 30 minutes that it did not know one asked about too soon.
A commit a release tag names moves to that release. A commit pin needs Go on this machine: the
commit is built with go install into the user's cache directory to render with.

Either way the stack and the pipeline are then rendered with that bedrock, so the committed files
come from the bedrock the pin names. Commit placement.json with the rendered files: from that
commit the pipeline's first step and the infrastructure check get the pinned bedrock, and every
bedrock deploy step runs it. The pin is the one place a bedrock version appears in the
application.

render and check refuse a placement pinned to another bedrock than a release or an installed
commit running them, so the installed bedrock is kept at the pin; upgrade is the command that
moves it. Run from anywhere inside the repository, it finds the stack and the application as
render does; --dir, --app and --placement override.`,
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

// upgrade moves the pin in the placement and renders with the bedrock it names.
func (d deps) upgrade(cmd *cobra.Command, args []string, appDir, dir, placement string) error {
	ctx := cmd.Context()
	p, err := derive.ReadPlacement(placement)
	if err != nil {
		return err
	}
	src := d.releases()
	version, err := targetVersion(ctx, src, args)
	if err != nil {
		return err
	}
	if release.IsCommitPin(version) {
		return d.upgradeToCommit(cmd, p, version, appDir, dir, placement)
	}

	return d.upgradeToRelease(cmd, src, p, version, appDir, dir, placement)
}

// upgradeToRelease moves the pin to a release: its version and its pipeline binary's
// checksum, rendering with its binary for this machine.
func (d deps) upgradeToRelease(cmd *cobra.Command, src *release.Source, p *derive.Placement, version, appDir, dir, placement string) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()
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
	was := pinName(p)
	p.BedrockVersion, p.BedrockSHA256 = version, pipelineSum
	if err := derive.WritePlacement(placement, p); err != nil {
		return err
	}
	fmt.Fprintf(out, "Moved %s from %s to bedrock %s (%s %s).\n", placement, was, version, release.PipelineAsset(), pipelineSum)
	if err := renderWith(cmd, bin, version, appDir, dir, placement); err != nil {
		return err
	}
	fmt.Fprintf(out, "Commit %s with the rendered files: from that commit the pipeline and the infrastructure check run bedrock %s.\n", placement, version)

	return nil
}

// upgradeToCommit moves the pin to a commit: its pseudo-version and no checksum, which also
// clears the checksum a release pin left. The commit is installed before the placement is
// written, so a failed install leaves the placement as it was.
func (d deps) upgradeToCommit(cmd *cobra.Command, p *derive.Placement, version, appDir, dir, placement string) error {
	out := cmd.OutOrStdout()
	if p.BedrockVersion == version && p.BedrockSHA256 == "" {
		fmt.Fprintf(out, "%s already pins bedrock %s.\n", placement, version)

		return nil
	}
	bin, err := d.installed(cmd.Context(), version)
	if err != nil {
		return err
	}
	was := pinName(p)
	p.BedrockVersion, p.BedrockSHA256 = version, ""
	if err := derive.WritePlacement(placement, p); err != nil {
		return err
	}
	fmt.Fprintf(out, "Moved %s from %s to bedrock %s, a commit pin: the pipeline builds it with go install and Go's checksum database verifies it.\n", placement, was, version)
	if err := renderWith(cmd, bin, version, appDir, dir, placement); err != nil {
		return err
	}
	fmt.Fprintf(out, "Commit %s with the rendered files: from that commit the pipeline and the infrastructure check build bedrock %s with go install and run it.\n", placement, version)

	return nil
}

// pinName names the placement's pin before a move, for the message that says what moved.
func pinName(p *derive.Placement) string {
	if !p.Pinned() {
		return "no bedrock"
	}

	return "bedrock " + p.BedrockVersion
}

// renderWith runs the render of the bedrock at bin, the one the pin now names.
func renderWith(cmd *cobra.Command, bin, version, appDir, dir, placement string) error {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Rendering with bedrock %s (%s):\n", version, bin)
	render := exec.CommandContext(cmd.Context(), bin, renderCommand, "--app", appDir, "--out", dir, "--placement", placement)
	render.Stdout, render.Stderr = out, cmd.ErrOrStderr()
	if err := render.Run(); err != nil {
		return errors.Newf("rendering with bedrock %s failed (%v): what it said is above", version, err)
	}

	return nil
}

// targetVersion is the bedrock to move to: the latest release when none is named, the
// release named, else what the Go module proxy gives the commit, the branch or the
// pseudo-version named, which is a release when a release tag names that commit.
func targetVersion(ctx context.Context, src *release.Source, args []string) (string, error) {
	if len(args) == 0 {
		return src.Latest(ctx)
	}
	arg := args[0]
	if release.IsVersion(arg) {
		return arg, nil
	}
	if release.IsVersion("v" + arg) {
		return "", errors.Newf("%q is not a version: did you mean v%s? (a version carries its v, as the tag bedrock/v%s does)", arg, arg, arg)
	}

	return src.Resolve(ctx, arg)
}

// cache is the directory upgrade keeps the bedrock binaries it fetches and installs in,
// one directory per version.
func (d deps) cache() (string, error) {
	if d.cacheDir != "" {
		return d.cacheDir, nil
	}
	userCache, err := os.UserCacheDir()
	if err != nil {
		return "", errors.Wrap(err, "os.UserCacheDir()")
	}

	return filepath.Join(userCache, release.Name), nil
}

// fetched is the release's binary for this machine in the cache directory: the copy
// there when it verifies, else fetched afresh and verified.
func (d deps) fetched(ctx context.Context, src *release.Source, version, asset, sum string) (string, error) {
	dir, err := d.cache()
	if err != nil {
		return "", err
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

// installed is the commit pin's binary in the cache directory: the copy there when its
// build info says go install built it at that version, else installed afresh.
func (d deps) installed(ctx context.Context, version string) (string, error) {
	cache, err := d.cache()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cache, version)
	bin := filepath.Join(dir, release.Name)
	if installedAt(bin, version) {
		return bin, nil
	}
	install := d.install
	if install == nil {
		install = goInstall
	}
	if err := install(ctx, version, dir); err != nil {
		return "", err
	}

	return bin, nil
}

// installedAt reports whether the binary at path is the bedrock module at the version, as
// go install builds it: its build info names the module, the version and the module's sum.
func installedAt(path, version string) bool {
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return false
	}

	return info.Main.Path == release.Module && info.Main.Version == version && info.Main.Sum != ""
}

// goInstall builds the bedrock module at a version into dir with go install, as the
// pipeline's first step does: a static binary (CGO_ENABLED=0) without the build directory
// in it (-trimpath), and the module verified against Go's checksum database whatever this
// machine's Go settings say (GOSUMDB set; GONOSUMDB, GOPRIVATE and GOINSECURE cleared).
func goInstall(ctx context.Context, version, dir string) error {
	if _, err := exec.LookPath("go"); err != nil {
		return errors.Newf("a commit pin needs Go on this machine, to build bedrock %s with go install: %v", version, err)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return errors.Wrap(err, "os.MkdirAll()")
	}
	install := exec.CommandContext(ctx, "go", "install", "-ldflags=-s -w", release.Module+"@"+version)
	install.Env = append(os.Environ(),
		"GOBIN="+dir, "CGO_ENABLED=0", "GOFLAGS=-trimpath",
		"GOSUMDB=sum.golang.org", "GONOSUMDB=", "GOPRIVATE=", "GOINSECURE=",
	)
	var output bytes.Buffer
	install.Stdout, install.Stderr = &output, &output
	if err := install.Run(); err != nil {
		return errors.Newf("go install %s@%s failed (%v): %s", release.Module, version, err, strings.TrimSpace(output.String()))
	}

	return nil
}
