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

	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/layers"
	"github.com/cccteam/ccc/bedrock/internal/org"
	"github.com/cccteam/ccc/bedrock/internal/release"
)

// upgradeCommand is the name of the upgrade command; the options are those the pinned
// bedrock is told where to render with.
const (
	upgradeCommand  = "upgrade"
	appOption       = "--app"
	dirOption       = "--dir"
	outOption       = "--out"
	placementOption = "--placement"
	// yesTyped is the word typed back before upgrade applies 0-bootstrap.
	yesTyped = "yes"
)

func newUpgrade(d deps) *cobra.Command {
	var (
		appFlag   string
		dirFlag   string
		placement string
		yes       bool
	)

	cmd := &cobra.Command{
		Use:   upgradeCommand + " [version|commit|branch] [--app <dir>] [--dir <dir>]",
		Short: "Move the application to a bedrock release or a pushed commit",
		Long: `upgrade moves the application's bedrock pin in placement.json (bedrockVersion, and for a release
bedrockSha256), which its pipeline and its stack's triggers run. In the organization's
infrastructure repository (the one whose placement.json at the root names the organization,
organizationId) it moves the organization's pin instead, which the Cloud Build triggers' runs
fetch, and renders the layers with it (bedrock org render). The pin is one of two kinds.

A release: the version given (v0.4.0), else the latest release; a pre-release is moved to by name.
The checksum of its linux/amd64 binary is read from the release's checksums.txt, never typed, and
the pipeline and the stack's triggers download that binary and verify it against the checksum.
The release is fetched for this machine (into the user's cache directory, verified against the
same file) to render with.

A commit pin: a commit of github.com/cccteam/ccc (a full or abbreviated hash), a branch (its head
commit, read from the GitHub API) or the pseudo-version Go gives a commit. The Go module proxy
names its version (v0.0.0-lab.1.0.20260928222237-58b211dce544); the placement holds that version
and no checksum, and the pipeline and the stack's triggers build it with go install, which
verifies it against Go's checksum database. The commit must be pushed first: the proxy knows only
pushed commits, and remembers for about 30 minutes that it did not know one asked about too soon.
A commit a release tag names moves to that release. A commit named by its hash or its
pseudo-version must be on a branch of github.com/cccteam/ccc, since GitHub also serves the
commits of the repository's forks by hash; the triggers' first step checks the same with git
before building a commit pin. A commit pin needs Go on this machine: the commit is built
with go install into the user's cache directory to render with.

Either way the stack and the pipeline are then rendered with that bedrock, so the committed files
come from the bedrock the pin names. Commit placement.json with the rendered files: from that
commit the pipeline's first step and the stack's triggers get the pinned bedrock, and every
bedrock deploy step runs it. The pin is the one place a bedrock version appears in the
application.

In the organization's repository a move that changes the pin's kind, a release to a commit or a
commit to a release, changes the shape of the Cloud Build triggers' steps (0-bootstrap/cloud-build.tf:
a release pin renders the fetch step alone, a commit pin adds the step that builds it), and
0-bootstrap is the one layer no trigger applies: the triggers as applied stay in the old shape,
and in the release shape the fetch step refuses a commit pin. So upgrade says so, asks for yes
typed back (--yes stands in for it where there is no terminal; without either the run is refused
and nothing is moved), and after the render applies 0-bootstrap as you, with your sign-in, the way
the bootstrap administrator applies the layer by hand: the plan updates the two triggers in place.
A move that keeps the kind applies nothing.

Upgrade impulse first, then bedrock, and land both in one change: bedrock's checks read what
impulse's walk writes (the drivers' settings the configuration embeds, the release file, the
processes), and the walk reads nothing bedrock renders. An application behind impulse's
provider-drivers step is refused here, and the refusal names impulse upgrade as the remedy.

render and check refuse a placement pinned to another bedrock than a release or an installed
commit running them, so the installed bedrock is kept at the pin; upgrade is the command that
moves it. Run from anywhere inside the repository, it finds the stack and the application as
render does; --dir, --app and --placement override. In the organization's repository --dir
is the repository root.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, p, err := d.orgRepository(dirFlag)
			if err != nil {
				return err
			}
			if p != nil {
				if appFlag != "" {
					return errors.New("--app names an application: the organization's repository has none, and upgrade moves its own pin here")
				}
				if placement == "" {
					placement = filepath.Join(root, orgPlacementFile)
				}

				return d.upgrade(cmd, args, orgTarget(p, root, placement), yes)
			}
			appDir, dir, err := d.stack(appFlag, dirFlag)
			if err != nil {
				return err
			}
			if placement == "" {
				placement = filepath.Join(dir, placementFile)
			}
			t, err := appTarget(appDir, dir, placement)
			if err != nil {
				return err
			}

			return d.upgrade(cmd, args, t, yes)
		},
	}

	cmd.Flags().StringVar(&appFlag, "app", "", "application root, the directory holding go.mod (default: the repository root when the stack is in its infrastructure directory, else the working directory)")
	cmd.Flags().StringVar(&dirFlag, "dir", "", "the stack directory (default: the application's stack, found from the working directory)")
	cmd.Flags().StringVar(&placement, "placement", "", "placement file (default: placement.json in the stack directory)")
	cmd.Flags().BoolVar(&yes, confirmFlag, false, "in the organization's repository, when the pin changes kind: apply 0-bootstrap without yes typed back, where there is no terminal to type it at")
	_ = cmd.MarkFlagDirname("app")
	_ = cmd.MarkFlagDirname("dir")

	return cmd
}

// upgradeTarget is the placement upgrade moves, an application's or the organization's:
// where it is, its pin as it stands, how the new pin is written, the arguments the
// pinned bedrock renders with, and who runs the pin from the commit, for the messages.
type upgradeTarget struct {
	placement string
	version   string
	sum       string
	write     func(version, sum string) error
	render    []string
	// runner is who runs the pin from the commit; builds says who builds a commit pin.
	runner string
	builds string
	// root is the organization repository's root, whose 0-bootstrap upgrade applies when
	// the pin changes kind; empty for an application, whose pin changes no trigger's
	// steps (its stack trigger always carries the build step).
	root string
}

// appTarget is an application's placement: the stack is rendered with the pinned
// bedrock (bedrock render --app --out --placement).
func appTarget(appDir, dir, placement string) (*upgradeTarget, error) {
	p, err := derive.ReadPlacement(placement)
	if err != nil {
		return nil, err
	}

	return &upgradeTarget{
		placement: placement, version: p.BedrockVersion, sum: p.BedrockSHA256,
		write: func(version, sum string) error {
			p.BedrockVersion, p.BedrockSHA256 = version, sum

			return derive.WritePlacement(placement, p)
		},
		render: []string{renderCommand, appOption, appDir, outOption, dir, placementOption, placement},
		runner: "the pipeline and the stack's triggers",
		builds: "the pipeline builds it",
	}, nil
}

// orgTarget is the organization's placement: the layers are rendered with the pinned
// bedrock (bedrock org render --dir --placement).
func orgTarget(p *org.Placement, root, placement string) *upgradeTarget {
	return &upgradeTarget{
		placement: placement, version: p.BedrockVersion, sum: p.BedrockSHA256,
		write: func(version, sum string) error {
			p.BedrockVersion, p.BedrockSHA256 = version, sum

			return p.Write(placement)
		},
		render: []string{orgCommand, renderCommand, dirOption, root, placementOption, placement},
		runner: "the Cloud Build triggers' runs",
		builds: "the triggers' runs build it",
		root:   root,
	}
}

// shapeChange is the change of kind the move to version makes to the organization's
// pin, release to commit or commit to release, which changes the shape of the Cloud
// Build triggers' steps; nil when the kind stays, for an application, and for a first
// pin, before the triggers exist.
func (t *upgradeTarget) shapeChange(version string) *shapeChange {
	if t.root == "" || t.version == "" {
		return nil
	}
	from, to := org.PinKind(t.version), org.PinKind(version)
	if from == to {
		return nil
	}

	return &shapeChange{from: from, to: to}
}

// shapeChange is a move of the organization's pin across kinds (org.PinKind), which
// changes the shape of the Cloud Build triggers' steps: 0-bootstrap, the one layer no
// trigger applies, is applied here after the render.
type shapeChange struct {
	from string
	to   string
}

// announcement says what the move does to the triggers: the kinds, the shapes, what
// the triggers as applied do with the new pin, and that 0-bootstrap is applied here.
func (c *shapeChange) announcement() string {
	const applies = "bedrock upgrade applies 0-bootstrap after the render%s, as you, with your sign-in, the way the bootstrap administrator applies the layer by hand (in 0-bootstrap: tofu init, plan and apply, at its backend)%s; the plan updates the two triggers in place."
	if c.to == org.CommitKind {
		return "The pin moves from a release to a commit, and the Cloud Build triggers' steps change shape with it (0-bootstrap/cloud-build.tf): a release pin renders the fetch step alone, a commit pin adds the step that builds it with go install. The triggers are applied in the release shape, whose fetch step refuses a commit pin, so no pull request with this pin can be planned until 0-bootstrap is applied in the commit shape. " +
			fmt.Sprintf(applies, "", "")
	}

	return "The pin moves from a commit to a release, and the Cloud Build triggers' steps change shape with it (0-bootstrap/cloud-build.tf): a commit pin renders the fetch step and the step that builds it with go install, a release pin the fetch step alone. The triggers are applied in the commit shape, whose steps run a release pin too (the build step has nothing to do), so the pull request's plan runs either way. " +
		fmt.Sprintf(applies, " all the same", ", so the triggers and the repository are in one shape")
}

// pinMove is one move of the pin, ready to be made once the bedrock to render with is in
// hand: the version and the checksum to write (none for a commit), that bedrock, and the
// two lines that say what moved and what to commit.
type pinMove struct {
	version string
	sum     string
	bin     string
	moved   string
	closing string
}

// upgrade moves the pin in the placement and renders with the bedrock it names.
func (d deps) upgrade(cmd *cobra.Command, args []string, t *upgradeTarget, yes bool) error {
	ctx := cmd.Context()
	src := d.releases()
	version, err := targetVersion(ctx, src, args)
	if err != nil {
		return err
	}
	if release.IsCommitPin(version) {
		return d.upgradeToCommit(cmd, t, version, yes)
	}

	return d.upgradeToRelease(cmd, src, t, version, yes)
}

// upgradeToRelease moves the pin to a release: its version and its pipeline binary's
// checksum, rendering with its binary for this machine.
func (d deps) upgradeToRelease(cmd *cobra.Command, src *release.Source, t *upgradeTarget, version string, yes bool) error {
	ctx := cmd.Context()
	sums, pipelineSum, err := releaseChecksums(ctx, src, version)
	if err != nil {
		return err
	}
	asset := release.Asset(runtime.GOOS, runtime.GOARCH)
	sum, ok := sums[asset]
	if !ok {
		return errors.Newf("bedrock %s publishes no binary for %s/%s (%s): upgrade runs on a platform the release covers", version, runtime.GOOS, runtime.GOARCH, asset)
	}
	if t.version == version && t.sum == pipelineSum {
		fmt.Fprintf(cmd.OutOrStdout(), "%s already pins bedrock %s.\n", t.placement, version)

		return nil
	}
	change, ok, err := d.confirmShapeChange(cmd, t, version, yes)
	if err != nil || !ok {
		return err
	}
	bin, err := d.fetched(ctx, src, version, asset, sum)
	if err != nil {
		return err
	}

	return d.move(cmd, t, &pinMove{
		version: version, sum: pipelineSum, bin: bin,
		moved:   fmt.Sprintf("Moved %s from %s to bedrock %s (%s %s).", t.placement, pinName(t.version), version, release.PipelineAsset(), pipelineSum),
		closing: fmt.Sprintf("Commit %s with the rendered files: from that commit %s run bedrock %s.", t.placement, t.runner, version),
	}, change)
}

// move makes the move once it is confirmed and the bedrock to render with is in hand:
// the pin written and announced, the render with that bedrock, the apply of 0-bootstrap
// when the triggers' shape changes, and the closing line.
func (d deps) move(cmd *cobra.Command, t *upgradeTarget, m *pinMove, change *shapeChange) error {
	out := cmd.OutOrStdout()
	if err := t.write(m.version, m.sum); err != nil {
		return err
	}
	fmt.Fprintln(out, m.moved)
	if err := renderWith(cmd, m.bin, m.version, t.render); err != nil {
		return err
	}
	if change != nil {
		if err := d.applyShapeChange(cmd, t, change); err != nil {
			return err
		}
	}
	fmt.Fprintln(out, m.closing)

	return nil
}

// confirmShapeChange is the change of the triggers' shape the move to version makes,
// confirmed before anything is fetched, installed or written: when there is one, it is
// printed and yes is asked for at the terminal; --yes stands in for it, and a run with
// no terminal and no --yes is refused, with nothing moved. ok is false when something
// else was typed, which stops the move. No change asks nothing.
func (d deps) confirmShapeChange(cmd *cobra.Command, t *upgradeTarget, version string, yes bool) (change *shapeChange, ok bool, err error) {
	change = t.shapeChange(version)
	if change == nil {
		return nil, true, nil
	}
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, change.announcement())
	if yes {
		return change, true, nil
	}
	if !d.interactive() {
		return nil, false, errors.Newf("bedrock %s applies %s when the pin changes kind, after yes typed back at the terminal, and this run has none: run it again with --%s, which stands in for it; nothing was moved", upgradeCommand, layers.BootstrapLayer, confirmFlag)
	}
	typed, err := d.asker(cmd).Line(fmt.Sprintf("Apply %s after the render, as you? Type %s to go on; anything else stops:", layers.BootstrapLayer, yesTyped))
	if err != nil {
		return nil, false, err
	}
	if typed != yesTyped {
		fmt.Fprintln(out, "Stopped: nothing was moved.")

		return nil, false, nil
	}

	return change, true, nil
}

// applyShapeChange applies 0-bootstrap in the repository as the person running the
// command, once the render has put the layer in the new shape, and says what the plan
// and the apply did.
func (d deps) applyShapeChange(cmd *cobra.Command, t *upgradeTarget, change *shapeChange) error {
	out := cmd.OutOrStdout()
	layer := layers.BootstrapLayer
	fmt.Fprintf(out, "Applying %s as you, with your sign-in (in %s: tofu init, plan and apply):\n", layer, filepath.Join(t.root, layer))
	var runner deploy.Runner
	if d.deploy != nil {
		runner = d.deploy.Exec
	}
	applied, err := layers.ApplyByHand(cmd.Context(), runner, t.root, layer, out)
	if err != nil {
		return errors.Newf("the apply of %s failed (%v): %s and the rendered files are in place, so fix what failed and apply %s by hand (in %s: tofu init, then tofu apply) before this change's pull request can be planned", layer, errors.Cause(err), t.placement, layer, layer)
	}
	fmt.Fprintf(out, "Planned %s: %s\nApplied %s: %s The triggers are in the %s shape.\n", layer, applied.PlanSummary, layer, applied.ApplySummary, change.to)

	return nil
}

// releaseChecksums reads a release's checksums file, and in it the checksum of the
// binary the pipeline runs, which a release pin carries (bedrockSha256); a release whose
// file does not list that binary is refused.
func releaseChecksums(ctx context.Context, src *release.Source, version string) (sums release.Checksums, pipelineSum string, err error) {
	sums, err = src.Checksums(ctx, version)
	if err != nil {
		return nil, "", err
	}
	pipelineSum, ok := sums[release.PipelineAsset()]
	if !ok {
		return nil, "", errors.Newf("bedrock %s publishes no %s, the binary the pipeline runs: its %s does not list it", version, release.PipelineAsset(), release.ChecksumsFile)
	}

	return sums, pipelineSum, nil
}

// upgradeToCommit moves the pin to a commit: its pseudo-version and no checksum, which also
// clears the checksum a release pin left. The commit is installed before the placement is
// written, so a failed install leaves the placement as it was, and after the move is
// confirmed, so a refused one installs nothing.
func (d deps) upgradeToCommit(cmd *cobra.Command, t *upgradeTarget, version string, yes bool) error {
	if t.version == version && t.sum == "" {
		fmt.Fprintf(cmd.OutOrStdout(), "%s already pins bedrock %s.\n", t.placement, version)

		return nil
	}
	change, ok, err := d.confirmShapeChange(cmd, t, version, yes)
	if err != nil || !ok {
		return err
	}
	bin, err := d.installed(cmd.Context(), version)
	if err != nil {
		return err
	}

	return d.move(cmd, t, &pinMove{
		version: version, sum: "", bin: bin,
		moved:   fmt.Sprintf("Moved %s from %s to bedrock %s, a commit pin: %s with go install and Go's checksum database verifies it.", t.placement, pinName(t.version), version, t.builds),
		closing: fmt.Sprintf("Commit %s with the rendered files: from that commit %s build bedrock %s with go install and run it.", t.placement, t.runner, version),
	}, change)
}

// pinName names the placement's pin before a move, for the message that says what moved.
func pinName(version string) string {
	if version == "" {
		return "no bedrock"
	}

	return "bedrock " + version
}

// renderWith runs the render of the bedrock at bin, the one the pin now names, with the
// target's arguments.
func renderWith(cmd *cobra.Command, bin, version string, args []string) error {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Rendering with bedrock %s (%s):\n", version, bin)
	render := exec.CommandContext(cmd.Context(), bin, args...)
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
