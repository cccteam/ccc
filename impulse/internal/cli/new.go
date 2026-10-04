package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
	"golang.org/x/term"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/ci"
	"github.com/cccteam/ccc/impulse/internal/check"
	"github.com/cccteam/ccc/impulse/internal/handoff"
	"github.com/cccteam/ccc/impulse/internal/names"
	"github.com/cccteam/ccc/impulse/internal/skeleton"
	transition_ "github.com/cccteam/ccc/impulse/internal/transition"
)

// newFlags are impulse new's own flags; the composed options ride composedOptions and the
// handoff flags transitionFlags.
type newFlags struct {
	modulePath string
	name       string
	authName   string
	devRoot    string
	skipGit    bool
	oidcAzure  bool
	oidcGoogle bool
	authority  string
	opts       composedOptions
	transition transitionFlags
}

func (nf *newFlags) bind(cmd *cobra.Command) {
	cmd.Flags().StringVar(&nf.modulePath, "module", "", "module path of the application (required)")
	cmd.Flags().StringVar(&nf.name, "name", "", "the application's name: "+nameUse+" (default: the module path's last segment)")
	cmd.Flags().StringVar(&nf.authName, "auth", "", "the first auth's name: "+names.Guidance+" (asked when not given; no default)")
	cmd.Flags().StringVar(&nf.devRoot, "dev-root", "", "directory of cccteam checkouts to build against instead of the pins")
	cmd.Flags().BoolVar(&nf.skipGit, "skip-git", false, "do not initialize a git repository and make the first commit")
	cmd.Flags().BoolVar(&nf.oidcAzure, "oidc-azure", false, "the first auth's people sign in through the organization's Azure directory")
	cmd.Flags().BoolVar(&nf.oidcGoogle, "oidc-google", false, "the first auth's people sign in through the organization's Google Workspace directory")
	cmd.Flags().StringVar(&nf.authority, "authority", "", "who owns role membership for a directory-flavored first auth: directory (role claims synchronized at every login) or application (roles assigned in the application); asked when not given")
	cmd.Flags().BoolVar(&nf.opts.tenancy, "tenancy", false, "compose the tenancy option: tenant-scoped resources under a tenant segment, roles per tenant")
	cmd.Flags().StringVar(&nf.opts.tenantTable, "tenant-table", "Tenants", "the tenant-record table when --tenancy is given, PascalCase and plural")
	cmd.Flags().StringArrayVar(&nf.opts.outlets, "outlet", nil, "compose a session outlet, <name>=<prefix> (repeatable), such as portal=portal/api")
	cmd.Flags().StringArrayVar(&nf.opts.apiOutlets, "api-outlet", nil, "compose an API-key outlet, <name>=<prefix> (repeatable), such as machines=machines")
	cmd.Flags().StringArrayVar(&nf.opts.sites, "site", nil, "compose the sites layout: two or more site names (repeatable), the first being what the base site becomes under apps/")
	nf.transition.bindAgent(cmd)
	_ = cmd.MarkFlagRequired("module")
}

// newDeps are what impulse new reads besides its flags: the running impulse's build, whose
// version the new application's go.mod pins as its impulse tool, and the runner of the go
// command that resolves that pin through the module proxy. newNew passes the real ones;
// tests pass fakes.
type newDeps struct {
	build func() check.Build
	run   check.Execer
}

func newNew() *cobra.Command {
	return newNewWith(newDeps{build: check.RunningBuild, run: check.OSExec{}})
}

func newNewWith(d newDeps) *cobra.Command {
	var nf newFlags

	cmd := &cobra.Command{
		Use:   "new <dir>",
		Short: "Create an Impulse application: the base skeleton under your module path, its first auth named",
		Long: `new renders the base skeleton (flat layout, one auth, nothing else on) into a new or empty
directory under the module path you name, and names the application's first auth: the
population that signs in, as a lowercase plural (staff, members, partners, devices). The
auth is a package, pkg/auth/<name>, and its name is also the prefix of its tables, its
cookie, and the stem of its role file, so it is asked for when --auth is not given, and
there is no default: a default word would land in every application whose author skipped
the question.

The application is named after the module path's last segment (github.com/acme/beacon names
beacon) unless --name says otherwise; the name is the web package (<name>-web), APP_SERVICE_NAME,
and the development Spanner project, instance, and database in .envrc.template.

The rendered tree is committed as the application's first commit (--skip-git leaves it
uncommitted), so impulse add can start from a clean tree. With --dev-root, new also writes
a go.work that uses the framework checkouts under that directory (laid out by repository:
<root>/ccc/resource, <root>/session, ...); it is ignored by git and never committed.

Options are added afterwards, each as one reviewable change: impulse add tenancy, impulse
add outlet <name>, impulse add auth <name>, impulse add site <name>. Or they are composed
into the creation: --tenancy, --outlet <name>=<prefix> (repeatable; --api-outlet for a
machine surface), and --site <name> (two or more, the first being what the base site
becomes under apps/) apply the same transitions to the fresh tree in order, tenancy first
and the sites last, and end in one check and one handoff brief carrying every obligation,
so the agent wires the whole shape in one sitting. The first commit is the base alone, so the composed options are
one reviewable diff on top of it.

The first auth signs in with a password unless --oidc-azure or --oidc-google says its people
sign in through the organization's directory; --authority then says who owns role
membership (directory or application) and is asked when not given, as for impulse add auth.
The flavor is composed the same way, first, and the auth is born in the directory's shape:
its session migrations are written in that shape rather than moved to it later.

The application's go.mod pins its impulse tool (the tool directive CI's go tool impulse
check runs) at the impulse running new: a release's version, or the pseudo-version of the
commit an impulse installed with go install github.com/cccteam/ccc/impulse@<commit> was
built from. new resolves the pin with go get -tool, through the module proxy, so the
templates the application starts from and the check its CI runs are the same code. An
impulse built from a checkout (go build, go run) names no commit the proxy serves, so new
refuses it before writing anything. With --dev-root the template's pin stands.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pin, err := toolPin(d.build(), nf.devRoot)
			if err != nil {
				return err
			}
			reserved, err := skeleton.Reserved(skeleton.Base)
			if err != nil {
				return err
			}
			if nf.authName == "" {
				nf.authName, err = askAuthName(cmd)
				if err != nil {
					return err
				}
			}
			if err := names.ValidateAuth(nf.authName, reserved); err != nil {
				return err
			}
			nf.opts.authName = nf.authName
			nf.opts.flavor, nf.opts.authority, err = firstAuthFlavor(cmd, nf.oidcAzure, nf.oidcGoogle, nf.authority)
			if err != nil {
				return err
			}
			appName, err := appName(nf.name, nf.modulePath)
			if err != nil {
				return err
			}
			transitions, err := nf.opts.transitions()
			if err != nil {
				return err
			}
			if len(transitions) > 0 && nf.skipGit {
				return errors.New("composed options need the first commit to build on; drop --skip-git, or add them afterwards with impulse add (impulse swap auth for the flavor)")
			}

			dir := args[0]
			base := scaffold{
				dir: dir, modulePath: nf.modulePath, name: appName, authName: nf.authName, devRoot: nf.devRoot,
				pin: pin, skipGit: nf.skipGit, composing: len(transitions) > 0,
			}
			if err := renderBase(cmd, d.run, &base); err != nil {
				return err
			}
			if len(transitions) == 0 {
				return nil
			}

			return composeOptions(cmd, &nf.transition, dir, &nf.opts, transitions)
		},
	}
	nf.bind(cmd)

	return cmd
}

// scaffold is the base rendering impulse new makes: where, under which module path and
// names, and the impulse pin its go.mod takes.
type scaffold struct {
	dir, modulePath, name, authName, devRoot string
	// pin is the version go.mod pins the impulse tool at (toolPin); empty keeps the
	// template's.
	pin string
	// skipGit leaves the tree uncommitted; composing says options are composed after the
	// base, which needs the first commit.
	skipGit, composing bool
}

// renderBase renders the base skeleton with its first auth, pins its impulse tool, makes
// the first commit unless told not to, and writes the report. With options to compose, the
// commit is required, since the transitions build on it.
func renderBase(cmd *cobra.Command, run check.Execer, s *scaffold) error {
	got, err := skeleton.Render(&skeleton.Options{Candidate: skeleton.Base, Dir: s.dir, ModulePath: s.modulePath, Name: s.name, DevRoot: s.devRoot, Auth: s.authName})
	if err != nil {
		return err
	}
	if s.pin != "" {
		if err := pinTool(cmd.Context(), run, s.dir, s.pin); err != nil {
			return err
		}
	}
	// The owned files are written from the code before the first commit, so the
	// application owns them from its first commit; the base's committed workflow equals
	// the rendering, and a composed option that changes the workspaces rewrites it.
	if err := writeOwned(s.dir); err != nil {
		return err
	}
	gitNote, committed := "", false
	if !s.skipGit {
		gitNote, committed = initRepo(cmd.Context(), s.dir, s.modulePath, s.authName)
	}
	if s.composing && !committed {
		return errors.Newf("the base was rendered but not committed (%s), so the options were not added; commit it and add them with impulse add", gitNote)
	}

	port, emulator, firestore := templatePorts(s.dir)
	goProcs, err := goProcesses(s.dir)
	if err != nil {
		return err
	}
	web, err := webWorkspaces(s.dir)
	if err != nil {
		return err
	}
	report := renderReport{
		headline: fmt.Sprintf("Created %s at %s with the %s auth (%d files).", s.modulePath, s.dir, s.authName, got.Files),
		gitNote:  gitNote, options: !s.composing, pin: s.pin,
		candidate: skeleton.Base, dir: s.dir, modulePath: s.modulePath, name: s.name, devRoot: s.devRoot,
		rendered: got, port: port, emulator: emulator, firestore: firestore, goProcs: goProcs, web: web,
		styled: isTerminal(cmd.OutOrStdout()),
	}
	report.write(cmd.OutOrStdout())

	return nil
}

// goModFile is the module file at an application's root.
const goModFile = "go.mod"

// toolPin is the version the new application's go.mod pins impulse at: the version of the
// impulse that scaffolds it, so the templates the application starts from and the go tool
// impulse check its CI runs are the same code. A release pins its own version, and an
// impulse installed from a commit (go install <module>@<commit>) the pseudo-version of that
// commit. A build that cannot name its commit, one built from a checkout (go build, go
// run, whatever version control stamped on it) or reporting (devel), is refused before
// anything is written: no module proxy serves it, so no pin could fetch it. With a dev
// root nothing is pinned and the template's pin stands, as it always has there: that
// application builds against local checkouts, and the impulse scaffolding it is usually
// built from one.
func toolPin(b check.Build, devRoot string) (string, error) {
	if devRoot != "" {
		return "", nil
	}
	if !b.FromModule || !semver.IsValid(b.Version) {
		return "", errors.Newf("a new application pins its impulse tool at the impulse that creates it, and this impulse is a build from a checkout (%s), which names no commit the module proxy serves: install impulse from a release or a pushed commit (go install %s@<version|commit>) and run impulse new again; nothing was written", b.Version, check.ImpulseModule)
	}

	return b.Version, nil
}

// pinTool pins the application's impulse tool at version: it writes the version into
// go.mod as the impulse require, beside the tool directive, then resolves it with go get
// -tool through the module proxy, which fetches that impulse, verifies it against Go's
// checksum database, raises any requirement of the application that impulse needs higher,
// and records the checksums in go.sum. GOWORK=off keeps a workspace above the directory
// out of the resolution.
func pinTool(ctx context.Context, run check.Execer, dir, version string) error {
	// The rendered tree is read and written through a root scoped to it, as rendering
	// writes it.
	root, err := os.OpenRoot(dir)
	if err != nil {
		return errors.Wrap(err, "os.OpenRoot()")
	}
	defer root.Close()

	info, err := root.Stat(goModFile)
	if err != nil {
		return errors.Wrap(err, "os.Root.Stat()")
	}
	data, err := root.ReadFile(goModFile)
	if err != nil {
		return errors.Wrap(err, "os.Root.ReadFile()")
	}
	mod, err := modfile.Parse(filepath.Join(dir, goModFile), data, nil)
	if err != nil {
		return errors.Wrap(err, "modfile.Parse()")
	}
	if err := mod.AddRequire(check.ImpulseModule, version); err != nil {
		return errors.Wrap(err, "modfile.File.AddRequire()")
	}
	if err := mod.AddTool(check.ImpulseModule); err != nil {
		return errors.Wrap(err, "modfile.File.AddTool()")
	}
	pinned, err := mod.Format()
	if err != nil {
		return errors.Wrap(err, "modfile.File.Format()")
	}
	if err := root.WriteFile(goModFile, pinned, info.Mode().Perm()); err != nil {
		return errors.Wrap(err, "os.Root.WriteFile()")
	}
	at := check.ImpulseModule + "@" + version
	if out, err := run.Run(ctx, dir, []string{"GOWORK=off"}, "go", "get", "-tool", at); err != nil {
		return errors.Newf("go get -tool %s could not resolve the impulse pin through the module proxy (%v): %s. %s is rendered but not committed: once the proxy answers, remove it and run impulse new again", at, errors.Cause(err), strings.TrimSpace(string(out)), dir)
	}

	return nil
}

// writeOwned writes the files impulse owns from the code of the application at dir.
func writeOwned(dir string) error {
	a, err := app.Discover(dir)
	if err != nil {
		return err
	}
	if _, err := ci.Write(a); err != nil {
		return err
	}

	return nil
}

// composeOptions runs the composed options' transitions on the fresh tree: the same ones
// add would run one at a time, in order, ending in one check and one brief.
func composeOptions(cmd *cobra.Command, f *transitionFlags, dir string, opts *composedOptions, transitions []transition) error {
	fmt.Fprintf(cmd.OutOrStdout(), "\nAdding %s.\n\n", opts.describe())
	f.appDir = dir
	a, err := app.Discover(dir)
	if err != nil {
		return err
	}
	repo := handoff.For(a, check.OSExec{})
	if err := repo.Check(cmd.Context()); err != nil {
		return err
	}

	return runTransitions(cmd, f, repo, transitions, opts.reference())
}

// firstAuthFlavor resolves the first auth's directory flavor from the flags: at most one
// flavor, and for a flavor the authority, asked when not given, as impulse add auth asks
// it. Without a flavor the base's password auth stands and --authority has no meaning.
func firstAuthFlavor(cmd *cobra.Command, oidcAzure, oidcGoogle bool, authority string) (flavor, resolvedAuthority string, err error) {
	switch {
	case oidcAzure && oidcGoogle:
		return "", "", errors.New("--oidc-azure and --oidc-google are two flavors; pick one")
	case oidcAzure:
		flavor = transition_.FlavorOIDCAzure
	case oidcGoogle:
		flavor = transition_.FlavorOIDCGoogle
	case authority != "":
		return "", "", errors.New("--authority goes with --oidc-azure or --oidc-google; a password auth's role membership is the application's")
	default:
		return "", "", nil
	}
	if authority == "" {
		authority, err = askAuthority(cmd)
		if err != nil {
			return "", "", err
		}
	}

	return flavor, authority, nil
}

// composedOptions are the options impulse new composes into the creation.
type composedOptions struct {
	// authName, flavor, and authority describe the first auth when a directory flavor is
	// composed; flavor empty means the base's password auth stands.
	authName    string
	flavor      string
	authority   string
	tenancy     bool
	tenantTable string
	outlets     []string
	apiOutlets  []string
	sites       []string
}

// transitions returns the transitions the options ask for, in the order add would run
// them: the first auth's flavor first, since it swaps the base's handler seams where they
// stand, then tenancy, since an outlet's members may be tenant-scoped, then the outlets,
// then the sites, since promotion moves what the others laid in.
func (o *composedOptions) transitions() ([]transition, error) {
	var ts []transition
	if o.flavor != "" {
		ts = append(ts, transition_.AuthFlavor{Name: o.authName, Flavor: o.flavor, Authority: o.authority, Fresh: true})
	}
	if o.tenancy {
		ts = append(ts, transition_.Tenancy{Table: o.tenantTable})
	}
	for _, kind := range []struct {
		specs    []string
		sessions bool
		flag     string
	}{{o.outlets, true, "--outlet"}, {o.apiOutlets, false, "--api-outlet"}} {
		for _, spec := range kind.specs {
			name, prefix, ok := strings.Cut(spec, "=")
			if !ok || name == "" || prefix == "" {
				return nil, errors.Newf("%s %q: name the outlet and its prefix as <name>=<prefix>, such as portal=portal/api", kind.flag, spec)
			}
			ts = append(ts, transition_.Outlet{Name: name, Prefix: prefix, Sessions: kind.sessions})
		}
	}
	if len(o.sites) == 1 {
		return nil, errors.Newf("--site %s: name at least two sites, the first being what the base site becomes under apps/ (an application with one site stays flat)", o.sites[0])
	}
	for i, site := range o.sites {
		if i == 0 {
			continue
		}
		s := transition_.Site{Name: site}
		if i == 1 {
			s.Existing = o.sites[0]
		}
		ts = append(ts, s)
	}

	return ts, nil
}

// describe names the composed options for the report.
func (o *composedOptions) describe() string {
	var parts []string
	if o.flavor != "" {
		parts = append(parts, "the "+o.flavor+" flavor for the "+o.authName+" auth, role membership the "+o.authority+"'s")
	}
	if o.tenancy {
		parts = append(parts, "tenancy ("+o.tenantTable+")")
	}
	for _, spec := range o.outlets {
		parts = append(parts, "the session outlet "+spec)
	}
	for _, spec := range o.apiOutlets {
		parts = append(parts, "the API-key outlet "+spec)
	}
	if len(o.sites) > 1 {
		parts = append(parts, "the sites "+strings.Join(o.sites, ", ")+" (the base site becomes "+o.sites[0]+")")
	}

	return strings.Join(parts, ", ")
}

// reference picks the finished application the brief points the agent at: the one with
// the most of the composed options wired.
func (o *composedOptions) reference() string {
	switch {
	case len(o.sites) > 1:
		return transition_.SitesReference
	case len(o.outlets)+len(o.apiOutlets) > 0 || o.flavor != "":
		// The outlets candidate also holds the reference directory auth.
		return transition_.ReferenceCandidate
	default:
		return transition_.TenancyReferenceCandidate
	}
}

// authQuestion is what a person at the terminal is asked when --auth is not given.
const authQuestion = `Name the application's first auth: the population that signs in, plural, lowercase, one
word (staff, members, partners, devices). It names the package pkg/auth/<name>, the
tables, the cookie, and the role file. There is no default.
Auth name: `

// askAuthName asks for the first auth's name when the flag did not give it and a person is
// at the terminal. Without a terminal the flag is required.
func askAuthName(cmd *cobra.Command) (string, error) {
	if !stdinIsTerminal() {
		return "", errors.New("--auth is required: name the application's first auth, " + names.Guidance + ". There is no default")
	}
	fmt.Fprint(cmd.ErrOrStderr(), authQuestion)
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && line == "" {
		return "", errors.Wrap(err, "reading the answer")
	}

	return strings.TrimSpace(line), nil
}

// stdinIsTerminal reports whether a person is at the terminal to answer a question.
func stdinIsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// initRepo makes the rendered tree a git repository with its first commit, so impulse add
// starts from a clean tree. It returns a note for the report and whether the commit was
// made: a missing git or an unset identity is the person's to sort out, not a reason to
// undo the render.
func initRepo(ctx context.Context, dir, modulePath, authName string) (note string, committed bool) {
	steps := [][]string{
		{"init", "-q"},
		{"add", "-A"},
		{"commit", "-q", "-m", fmt.Sprintf("impulse new: %s with the %s auth", modulePath, authName)},
	}
	for _, step := range steps {
		cmd := exec.CommandContext(ctx, "git", step...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Sprintf("git %s failed (%v): %s. Commit the tree yourself before impulse add.", step[0], err, strings.TrimSpace(string(out))), false
		}
	}

	return "Committed the tree as the application's first commit.", true
}
