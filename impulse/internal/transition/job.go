package transition

import (
	"context"
	"fmt"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/internal/check"
)

// Job scaffolds a command of the job process: the job's work in pkg/jobs, a function
// written empty, with the command the job process runs it under; the command's package
// under cmd/jobs, a cobra command whose Run runs the work over the data level; the root
// in cmd/jobs/main.go, which opens the data level once and registers every command,
// written by the first job and extended by the rest; and, with a schedule, the method
// marked @schedule in the rpc package that starts the job through the job process's
// starter, with the starter's wiring where the application lacks it. The work, its
// suite, and the schedule when none was given are handed to the agent. The file store's
// cleanup (impulse add files) is a job of this scaffold with work of its own.
type Job struct {
	// Name is the command's name as the job process takes it: lowercase words joined by
	// hyphens (prune-reports), with a group ahead of it when the name carries one
	// (files/cleanup makes the group files and the command cleanup under it).
	Name string
	// Schedule is the cron expression Cloud Scheduler starts the job on, read in UTC;
	// empty, nothing starts the job until a scheduled method or a caller does.
	Schedule string
	// kind is the job's sources: the stubs a scaffolded job takes, or the cleanup's own
	// work, command and method, which the file store's transition passes.
	kind jobKind
}

// The job process's shape: the library the command tree is built with, the root's
// variables as the sources write them, and the function the root registers in.
const (
	cobraImportPath = "github.com/spf13/cobra"
	jobsRootVar     = "root"
	jobsGroupVar    = "group"
	jobsDataVar     = "data"
	jobsRootFunc    = "run"
	jobsGroupFunc   = "Command"
	// jobNameMax is the longest name a job takes, its group included.
	jobNameMax = 64
)

// jobNameRE is one word of a job's name: lowercase letters and digits, hyphenated.
var jobNameRE = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

// cobraCommands are the commands cobra adds to every root, which no job may take.
var cobraCommands = []string{"help", "completion"}

// jobsPackageDoc opens the jobs package, written with its first job.
const jobsPackageDoc = `// Package jobs holds the work the application's job process runs apart from the served
// site: a function per job, with the command the job process runs it under. cmd/jobs is
// the command tree around it, and impulse add job <name> adds a job.
`

// jobKind is what differs between a scaffolded job and the file store's cleanup: the
// names beyond the derived ones, and the sources.
type jobKind interface {
	// names fills in the identifiers the job takes beyond the ones derived from its name.
	names(n *jobNames)
	// work is the job's file in pkg/jobs: the command and the work, opening the package
	// when the job is its first.
	work(s *scaffold, first bool) string
	// leaf is the command's package.
	leaf(s *scaffold) string
	// method is the scheduled method in the rpc package of the given name.
	method(s *scaffold, pkg string) string
	// whatWork and whatLeaf say what the two files hold, for the report.
	whatWork(n *jobNames) string
	whatLeaf(n *jobNames) string
}

// jobNames are the identifiers a job takes, derived from its name.
type jobNames struct {
	// group and name are the two words of the name; group is empty for a flat job.
	group, name string
	// pascal is the identifier the job takes (PruneReports, FilesCleanup).
	pascal string
	// pkg is the command's package, and pkgDir and leafFile where it lives.
	pkg, pkgDir, leafFile string
	// groupPkg, groupDir and groupFile are the group's command package, for a grouped job.
	groupPkg, groupDir, groupFile string
	// workFile is the job's file in pkg/jobs, workFunc the work, and commandConst the
	// command's name as the file declares it.
	workFile, workFunc, commandConst string
	// groupConst is the group's name as pkg/jobs declares it, in groupConstFile.
	groupConst, groupConstFile string
	// method is the scheduled method, outcome its answer, and methodFile the method's
	// file in the rpc package.
	method, outcome, methodFile string
}

// parseJobName reads a job's name into its identifiers, refusing a name the job process
// cannot take.
func parseJobName(name string) (jobNames, error) {
	group, cmd, grouped := strings.Cut(name, "/")
	if !grouped {
		group, cmd = "", group
	}
	if strings.Contains(cmd, "/") {
		return jobNames{}, errors.Newf("job name %q: a name carries one group at most (files/cleanup)", name)
	}
	if len(name) > jobNameMax {
		return jobNames{}, errors.Newf("job name %q: a name is at most %d characters", name, jobNameMax)
	}
	words := []string{cmd}
	if grouped {
		words = []string{group, cmd}
	}
	for _, word := range words {
		if !jobNameRE.MatchString(word) {
			return jobNames{}, errors.Newf("job name %q: a name is lowercase words of letters and digits joined by single hyphens, opening with a letter, such as prune-reports, with a group ahead of it as files/cleanup", name)
		}
		if slices.Contains(cobraCommands, word) {
			return jobNames{}, errors.Newf("job name %q: %s is a command of every root (cobra adds it)", name, word)
		}
	}
	n := jobNames{group: group, name: cmd}
	n.pascal = pascalOf(group) + pascalOf(cmd)
	n.pkg = packageOf(cmd)
	n.pkgDir = path.Join(jobsCmdDir, n.pkg)
	snake := snakeOf(cmd)
	if group != "" {
		n.groupPkg = packageOf(group)
		n.groupDir = path.Join(jobsCmdDir, n.groupPkg)
		n.groupFile = path.Join(n.groupDir, n.groupPkg+".go")
		n.pkgDir = path.Join(n.groupDir, n.pkg)
		n.groupConst = pascalOf(group) + "Group"
		n.groupConstFile = path.Join(jobsPkgDir, snakeOf(group)+"_group.go")
		snake = snakeOf(group) + "_" + snake
	}
	n.leafFile = path.Join(n.pkgDir, n.pkg+".go")
	n.workFile = path.Join(jobsPkgDir, snake+".go")
	n.workFunc = n.pascal
	n.commandConst = n.pascal + "Command"
	n.method = n.pascal
	n.outcome = n.pascal + "Started"
	n.methodFile = snake + ".go"

	return n, nil
}

// pascalOf is a hyphenated word as an identifier (prune-reports: PruneReports).
func pascalOf(word string) string {
	var b strings.Builder
	for part := range strings.SplitSeq(word, "-") {
		if part != "" {
			b.WriteString(strings.ToUpper(part[:1]) + part[1:])
		}
	}

	return b.String()
}

// snakeOf is a hyphenated word as a file stem (prune-reports: prune_reports).
func snakeOf(word string) string {
	return strings.ReplaceAll(word, "-", "_")
}

// packageOf is a hyphenated word as a package name (prune-reports: prunereports).
func packageOf(word string) string {
	return strings.ReplaceAll(word, "-", "")
}

// commandPath is the command as the job process takes it: the group, then the name.
func (n *jobNames) commandPath() string {
	if n.group == "" {
		return n.name
	}

	return n.group + " " + n.name
}

// startArgs is the command as the scheduled method passes it to the starter, through
// the constants pkg/jobs declares under the given import name.
func (n *jobNames) startArgs(jobsName string) string {
	if n.group == "" {
		return jobsName + "." + n.commandConst
	}

	return jobsName + "." + n.groupConst + ", " + jobsName + "." + n.commandConst
}

// scaffold is a job's place in the application: its names and kind, the schedule, and
// the packages its sources name.
type scaffold struct {
	names                      jobNames
	kind                       jobKind
	schedule                   string
	modulePath                 string
	configDir, configPkg       string
	jobsPkg                    string
	resourcesDir, resourcesPkg string
}

// configImport is the configuration package's import line: aliased when the package's
// name is not its directory's.
func (s *scaffold) configImport() string {
	return importLine(s.configPkg, s.modulePath+"/"+s.configDir)
}

// jobsImport is the jobs package's import line.
func (s *scaffold) jobsImport() string {
	return importLine(s.jobsPkg, s.modulePath+"/"+jobsPkgDir)
}

// importLine is an import of the path, under its name when the name is not the path's
// last element.
func importLine(name, importPath string) string {
	if name != path.Base(importPath) {
		return name + " \"" + importPath + "\""
	}

	return "\"" + importPath + "\""
}

// Command is the impulse command line for the transition.
func (j Job) Command() string {
	if j.Schedule != "" {
		return fmt.Sprintf("impulse add job %s --schedule %q", j.Name, j.Schedule)
	}

	return "impulse add job " + j.Name
}

// Validate checks the job against the application before anything is changed: the
// scaffold's premises, and with a schedule the method's.
func (j Job) Validate(a *app.App) error {
	if err := j.validateScaffold(a); err != nil {
		return err
	}
	if j.Schedule == "" {
		return nil
	}
	if err := validateSchedule(j.Schedule); err != nil {
		return err
	}
	n, err := j.names()
	if err != nil {
		return err
	}
	p := a.Profile()
	switch {
	case len(p.Sites) == 0:
		return errors.New("no generator program emits handlers; the application has no site to serve the scheduled method on")
	case p.Layout == app.LayoutSites:
		return errors.New("a scheduled job in an application in the sites layout is not supported yet")
	}
	rpcDir := p.Sites[0].Generator.RPCDir()
	if rpcDir == "" {
		rpcDir = rpcDirDefault
	}
	if rel := path.Join(rpcDir, n.methodFile); exists(a, rel) {
		return errors.Newf("%s exists: the scheduled method %s is there already", rel, n.method)
	}

	return nil
}

// names are the job's identifiers: the ones its name derives, as its kind fills them in.
func (j Job) names() (jobNames, error) {
	n, err := parseJobName(j.Name)
	if err != nil {
		return jobNames{}, err
	}
	if j.kind != nil {
		j.kind.names(&n)
	}

	return n, nil
}

// validateScaffold checks the scaffold's premises: the name, the module, the data
// level, and that nothing of the job's name is there yet.
func (j Job) validateScaffold(a *app.App) error {
	n, err := j.names()
	if err != nil {
		return err
	}
	if a.GoMod == nil || a.GoMod.Module == nil {
		return errors.New("go.mod has no module directive; the job process imports the application's own packages by module path")
	}
	configDir, err := dataConfigDir(a)
	if err != nil {
		return err
	}
	if configDir == "" {
		return errors.Newf("no file declares a %s struct, so there is no data level for the job process to open; the job process runs its commands over the data level the site opens", dataConfigType)
	}
	for _, word := range []string{n.pkg, n.groupPkg} {
		if word != "" && (word == packageNameOf(a, configDir) || slices.Contains(reservedJobPackages, word)) {
			return errors.Newf("job name %q: its package would be %s, which the job process imports already; choose another name", j.Name, word)
		}
	}
	if exists(a, n.pkgDir) {
		return errors.Newf("%s exists: the job %s is there already, or a package of its name", n.pkgDir, n.commandPath())
	}
	if exists(a, n.workFile) {
		return errors.Newf("%s exists: the job %s has its work there already", n.workFile, n.commandPath())
	}
	if n.group != "" && exists(a, n.groupDir) && !exists(a, n.groupFile) {
		return errors.Newf("%s exists but declares no group (%s), so %s cannot join it", n.groupDir, n.groupFile, n.commandPath())
	}

	return nil
}

// reservedJobPackages are the packages the job process's sources import, which a job's
// package cannot be named as without shadowing them.
var reservedJobPackages = []string{"main", "jobs", "cobra", "errors", "context", "log", "os", "signal", "fmt", "time", "resource"}

// dataConfigDir is the directory of the package declaring the data level, or empty when
// no file declares it.
func dataConfigDir(a *app.App) (string, error) {
	rel, _, _, err := findDeclaringFile(a, dataConfigType)
	if err != nil || rel == "" {
		return "", err
	}

	return path.Dir(rel), nil
}

// validateSchedule refuses a schedule that is not shaped as a cron expression: five
// fields separated by single spaces. The generator checks each field's values when it
// reads the method, and refuses what it cannot read.
func validateSchedule(schedule string) error {
	fields := strings.Split(schedule, " ")
	if len(fields) != 5 {
		return errors.Newf("schedule %q: a schedule is a cron expression of five fields separated by single spaces (minute, hour, day of the month, month, day of the week), such as \"0 3 * * *\"", schedule)
	}
	for _, field := range fields {
		if field == "" || strings.Trim(field, "0123456789*,/-ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz") != "" {
			return errors.Newf("schedule %q: the field %q is not a cron field (*, a value, a range or a list, optionally stepped)", schedule, field)
		}
	}

	return nil
}

// Apply makes the deterministic half of the transition: the work, the command, the
// root, the registration, and with a schedule the method and the starter's wiring.
func (j Job) Apply(ctx context.Context, a *app.App, exec check.Execer) (*Change, error) {
	if err := j.Validate(a); err != nil {
		return nil, err
	}
	ch := &Change{Command: j.Command()}
	s, err := j.scaffold(a)
	if err != nil {
		return nil, err
	}
	if err := s.write(ctx, a, exec, ch); err != nil {
		return nil, err
	}
	if j.Schedule != "" {
		g := a.Profile().Sites[0].Generator
		w := starterWiring{method: s.names.method, command: jobsCmdDir + " " + s.names.commandPath()}
		if err := w.wire(a, s, g, ch); err != nil {
			return nil, err
		}
		generate(ctx, a, exec, ch,
			fmt.Sprintf("go generate ./... failed, so %s's handler and scheduled route are not generated yet; fix the cause and run it:", s.names.method),
			fmt.Sprintf("ran go generate ./..., which emitted the %s handler, its scheduled route under /_scheduled, and the router's SchedulerAuth requirement", s.names.method))
	}
	j.leave(ch, s)

	return ch, nil
}

// scaffold reads the job's place in the application.
func (j Job) scaffold(a *app.App) (*scaffold, error) {
	n, err := j.names()
	if err != nil {
		return nil, err
	}
	kind := j.kind
	if kind == nil {
		kind = stubJob{}
	}
	configDir, err := dataConfigDir(a)
	if err != nil {
		return nil, err
	}
	s := &scaffold{
		names:      n,
		kind:       kind,
		schedule:   j.Schedule,
		modulePath: a.GoMod.Module.Mod.Path,
		configDir:  configDir,
		configPkg:  packageNameOf(a, configDir),
		jobsPkg:    packageNameOf(a, jobsPkgDir),
	}
	if p := a.Profile(); len(p.Sites) > 0 {
		s.resourcesDir = p.Sites[0].Generator.ResourcePackageDir
		s.resourcesPkg = packageNameOf(a, s.resourcesDir)
	}

	return s, nil
}

// write lays the job in: its work, the root when the application has none, its group
// when the name carries one and the application has none, its command, the
// registration, the lint configuration's allowance of the command tree's library, and
// the library as a direct requirement.
func (s *scaffold) write(ctx context.Context, a *app.App, exec check.Execer, ch *Change) error {
	n := &s.names
	first := !exists(a, jobsPkgDir)
	if err := writeGo(a, n.workFile, s.kind.work(s, first)); err != nil {
		return err
	}
	ch.didf("%s: %s (%s) and %s, %s", n.workFile, n.commandConst, n.name, n.workFunc, s.kind.whatWork(n))
	if n.group != "" && !exists(a, n.groupConstFile) {
		if err := writeGo(a, n.groupConstFile, s.groupConstSource()); err != nil {
			return err
		}
		ch.didf("%s: %s (%s), the group's name", n.groupConstFile, n.groupConst, n.group)
	}
	rootFile := path.Join(jobsCmdDir, jobsMainFile)
	if !exists(a, rootFile) {
		if err := writeGo(a, rootFile, s.rootSource()); err != nil {
			return err
		}
		ch.didf("%s: the job process, a cobra root that opens the data level once and registers the commands", rootFile)
	}
	owner, ownerFile, ownerFunc, ownerVar := "root", rootFile, jobsRootFunc, jobsRootVar
	if n.group != "" {
		if !exists(a, n.groupFile) {
			if err := writeGo(a, n.groupFile, s.groupSource()); err != nil {
				return err
			}
			ch.didf("%s: the %s group, a command holding its commands", n.groupFile, n.group)
			if err := s.register(a, ch, rootFile, jobsRootFunc, jobsRootVar, s.modulePath+"/"+n.groupDir, n.groupPkg+"."+jobsGroupFunc+"("+jobsDataVar+")", "root"); err != nil {
				return err
			}
		}
		owner, ownerFile, ownerFunc, ownerVar = n.group+" group", n.groupFile, jobsGroupFunc, jobsGroupVar
	}
	if err := writeGo(a, n.leafFile, s.kind.leaf(s)); err != nil {
		return err
	}
	ch.didf("%s: the %s command, %s", n.leafFile, n.commandPath(), s.kind.whatLeaf(n))
	if err := s.register(a, ch, ownerFile, ownerFunc, ownerVar, s.modulePath+"/"+n.pkgDir, n.pkg+"."+jobsGroupFunc+"("+jobsDataVar+")", owner); err != nil {
		return err
	}
	if err := allowImport(a, ch, cobraImportPath, "the command tree's library"); err != nil {
		return err
	}
	s.requireCobra(ctx, a, exec, ch)

	return nil
}

// register adds a command's constructor to the AddCommand call of a root or a group,
// with the import of its package; a file without the call is left to the agent.
func (*scaffold) register(a *app.App, ch *Change, rel, funcName, variable, importPath, argument, owner string) error {
	src, mode, err := readFile(a, rel)
	if err != nil {
		return err
	}
	edited, err := app.AppendCallArgument(rel, src, funcName, variable+".AddCommand", argument)
	switch {
	case errors.Is(err, app.ErrNoAnchor):
		ch.skipf("%s: no %s with a %s.AddCommand call to register %s on; register it on the %s", rel, funcName, variable, argument, owner)

		return nil
	case err != nil:
		return err
	}
	edited, err = app.AddImport(rel, edited, importPath)
	if err != nil {
		return err
	}
	if err := os.WriteFile(a.Abs(rel), edited, mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	ch.didf("%s: %s registered on the %s", rel, argument, owner)

	return nil
}

// requireCobra makes the command tree's library a direct requirement of the module, with
// go mod tidy, where go.mod does not require it so yet; a failed tidy is recorded as the
// agent's, as a failed regeneration is.
func (*scaffold) requireCobra(ctx context.Context, a *app.App, exec check.Execer, ch *Change) {
	for _, r := range a.GoMod.Require {
		if r.Mod.Path == cobraImportPath && !r.Indirect {
			return
		}
	}
	out, err := exec.Run(ctx, a.Root, nil, "go", "mod", "tidy")
	if err != nil {
		ch.skipf("go mod tidy failed, so go.mod does not require %s directly yet; fix the cause and run it:\n%s", cobraImportPath, strings.TrimSpace(string(out)))

		return
	}
	ch.didf("go.mod: %s, the command tree's library, required directly (go mod tidy)", cobraImportPath)
}

// leave hands the agent what the scaffold cannot write: the work and its suite, and the
// schedule when the job has none.
func (j Job) leave(ch *Change, s *scaffold) {
	n := &s.names
	ch.LeftNote = fmt.Sprintf("The job's work is yours: %s is written empty and answers an error until it is, the command around it is written, and the suite that proves the work is the application's.", n.workFunc)
	ch.Left = append(ch.Left,
		fmt.Sprintf("%s: %s, the work (replace the error with the job's work over the resource client, drop the error's variable, and make the command's Short say what it does)", n.workFile, n.workFunc),
		fmt.Sprintf("test/integration: a suite driving %s over the test database, as the file store's cleanup suite does", n.workFunc),
	)
	if j.Schedule == "" {
		ch.Left = append(ch.Left, fmt.Sprintf("nothing starts %s yet: a method marked @schedule in the rpc package starts it through the Client's Jobs() (impulse add job --schedule writes one), or a caller starts it through the starter", n.commandPath()))
	}
}

// Meaning explains the job process in this framework and names what is left to do.
func (j Job) Meaning() string {
	n, err := j.names()
	if err != nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "The job process is the application's second binary (`%s`): the work that runs apart from the served site, over the same data level the site opens, one cobra command per job. The root opens the data level once and registers every command; a command is a package under `%s` whose `Run` runs the job's work, the function in `%s` named for it, over the resource client; and bedrock deploys the binary as the Cloud Run job beside the service (the Dockerfile it seeds builds `/jobs`), started by the service alone. A method marked `@schedule` in the rpc package starts one execution through the job driver (`resource/jobs/cloudrun`) with the command as the container's arguments, Cloud Scheduler calling the method under the scheduler's token, so a traffic rollback rolls the job back too. Nothing starts a job by hand or on a schedule of its own, and a job whose execution fails is retried by Cloud Run, so the work is idempotent.\n\n", jobsCmdDir, jobsCmdDir, jobsPkgDir)
	b.WriteString("Left to wire, in this order:\n\n")
	items := []string{
		fmt.Sprintf("The work. `%s` in `%s` answers an error until it is written: replace the body with the job's work over the client (reads and writes through the resource client, the file stores off it), drop the error's variable, keep the work idempotent, and make the command's `Short` in `%s` say what the job does. Flags the job takes are declared on the command there, bound to fields of its `command`, and passed to the work as options, as the file store's cleanup passes `--window` and `--dry-run`.", n.workFunc, n.workFile, n.leafFile),
		fmt.Sprintf("Its suite. A test in `test/integration` drives `%s` over the test database, so the work is proven without the job process; the command is the frame around it and needs no test of its own.", n.workFunc),
	}
	if j.Schedule == "" {
		items = append(items, fmt.Sprintf("Starting it. A job is started by the running service: a method marked `@rpc` and `@schedule(\"<cron>\")` in the rpc package whose `Execute` calls `c.Jobs().Start(ctx, %s)` starts it on the schedule (`impulse add job %s --schedule \"<cron>\"` would have written it, and the file store's `CleanUpFiles` is the shape), or a method a person calls starts it the same way. The starter is the configuration's `Jobs()`, which `impulse add files` wires; an application without it wires the job driver into the site level and the `Client` by hand.", n.startArgs("appjobs"), j.Name))
	} else {
		items = append(items, fmt.Sprintf("The schedule. `%s` (`@schedule(%q)`, UTC) is the method Cloud Scheduler calls; the stack reads it from the router's release file and makes the scheduler job per environment, under the invoker identity it names in `APP_SCHEDULER_INVOKER`. A zone other than UTC is `@schedule(%q, zone: \"<IANA zone>\")`.", n.method, j.Schedule, j.Schedule))
	}
	items = append(items, "Deployment. Nothing by hand: bedrock sees `cmd/jobs`, builds `/jobs` into the image, makes the job of each build from the template job the stack names to the service (`APP_JOBS_TEMPLATE`), and makes the scheduler jobs; in development and in a pull-request stack no job is configured, and a start says so.")
	for i, item := range items {
		fmt.Fprintf(&b, "%d. %s\n", i+1, item)
	}

	return b.String()
}

// rootSource is the job process's root: the data level opened once, and the commands
// registered over it.
func (s *scaffold) rootSource() string {
	return fmt.Sprintf(`// Command jobs is the application's job process: the work that runs apart from the
// served site, over the same data level the site opens, one command per job:
//
//	go run ./%[1]s <command> [flags]
//
// The root opens the data level once and registers every command over it; a command is
// a package under %[1]s around its work in %[2]s, and impulse add job <name> adds one.
// On Cloud Run the same binary runs as the job the scheduled methods start: Cloud
// Scheduler calls the service, and the service starts the job deployed with it, with
// the command as the container's arguments.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	%[3]s
	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"
)

func main() {
	if err := %[4]s(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func %[4]s(ctx context.Context) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	%[5]s, err := %[6]s.%[7]s(ctx)
	if err != nil {
		return errors.Wrap(err, "%[6]s.%[7]s()")
	}
	defer %[5]s.Close()

	%[8]s := &cobra.Command{
		Use:           "jobs",
		Short:         "The application's job process: one command per job, over the data level",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	%[8]s.AddCommand()
	if err := %[8]s.ExecuteContext(ctx); err != nil {
		return errors.Wrap(err, "cobra.Command.ExecuteContext()")
	}

	return nil
}
`, jobsCmdDir, jobsPkgDir, s.configImport(), jobsRootFunc, jobsDataVar, s.configPkg, dataConstructor, jobsRootVar)
}

// groupSource is a group's command: the commands under one word.
func (s *scaffold) groupSource() string {
	n := &s.names

	return fmt.Sprintf(`// Package %[1]s is the %[2]s group of the job process: its commands run as
//
//	go run ./%[3]s %[2]s <command> [flags]
package %[1]s

import (
	%[4]s
	%[5]s
	"github.com/spf13/cobra"
)

// %[6]s is the %[2]s group over the data level the root opened, with its commands.
func %[6]s(%[7]s *%[8]s.%[9]s) *cobra.Command {
	%[10]s := &cobra.Command{
		Use:   %[11]s.%[12]s,
		Short: "The %[2]s commands",
	}
	%[10]s.AddCommand()

	return %[10]s
}
`, n.groupPkg, n.group, jobsCmdDir, s.configImport(), s.jobsImport(), jobsGroupFunc, jobsDataVar, s.configPkg, dataConfigType, jobsGroupVar, s.jobsPkg, n.groupConst)
}

// groupConstSource declares a group's name in pkg/jobs, for the commands under it and
// the methods that start them.
func (s *scaffold) groupConstSource() string {
	n := &s.names

	return fmt.Sprintf(`package %[1]s

// %[2]s is the job process's %[3]s group: the word ahead of its commands' names
// (%[4]s %[3]s <command>).
const %[2]s = %[3]q
`, s.jobsPkg, n.groupConst, n.group, jobsCmdDir)
}

// stubJob is a scaffolded job: its work written empty, its command plain.
type stubJob struct{}

func (stubJob) names(*jobNames) {}

func (stubJob) whatWork(*jobNames) string {
	return "the work, empty until it is written"
}

func (stubJob) whatLeaf(*jobNames) string {
	return "Run over the data level"
}

// work is the job's file: the command's name, and the work, which refuses until it is
// written.
func (stubJob) work(s *scaffold, first bool) string {
	n := &s.names
	doc := ""
	if first {
		doc = jobsPackageDoc
	}
	started := "the argument a scheduled method starts the job with, once one does"
	if s.schedule != "" {
		started = fmt.Sprintf("the argument the scheduled %s method starts the job with", n.method)
	}
	if n.group != "" {
		started = fmt.Sprintf("the command's name under the %s group (%s), %s", n.group, n.groupConst, started)
	}

	return fmt.Sprintf(`%[1]spackage %[2]s

import (
	"context"

	"github.com/cccteam/ccc/resource"
	"github.com/go-playground/errors/v5"
)

// %[3]s is the job process's command for the %[4]s job: %[5]s.
const %[3]s = %[6]q

// err%[7]sNotWritten is the empty work's answer: a start of the job fails until the
// work is written.
var err%[7]sNotWritten = errors.New("%[4]s: the job's work is not written yet")

// %[7]s is the %[4]s job's work, run by the job process's command (%[8]s) over the
// resource client the data level built. impulse add job wrote it empty: it answers
// err%[7]sNotWritten until the work is written, and what the job does, with the suite
// that proves it, is the application's.
func %[7]s(_ context.Context, _ resource.Client) error {
	return err%[7]sNotWritten
}
`, doc, s.jobsPkg, n.commandConst, n.commandPath(), started, n.name, n.workFunc, n.pkgDir)
}

// leaf is the command: a cobra command whose Run runs the work over the data level.
func (stubJob) leaf(s *scaffold) string {
	n := &s.names

	return fmt.Sprintf(`// Package %[1]s is the %[2]s command of the job process: the command around the
// work in %[3]s (%[4]s), run as
//
//	go run ./%[5]s %[2]s
package %[1]s

import (
	"context"

	%[6]s
	%[7]s
	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"
)

// %[8]s is the %[2]s command over the data level the root opened.
func %[8]s(%[9]s *%[10]s.%[11]s) *cobra.Command {
	c := &command{%[9]s: %[9]s}

	return &cobra.Command{
		Use:   %[12]s.%[13]s,
		Short: "Runs the %[2]s job",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return c.Run(cmd.Context())
		},
	}
}

// command is the %[2]s command: the data level it runs over, and its flags once it
// takes any.
type command struct {
	%[9]s *%[10]s.%[11]s
}

// Run runs the job's work over the resource client.
func (c *command) Run(ctx context.Context) error {
	if err := %[12]s.%[4]s(ctx, c.%[9]s.ResourceClient()); err != nil {
		return errors.Wrap(err, "%[12]s.%[4]s()")
	}

	return nil
}
`, n.pkg, n.commandPath(), jobsPkgDir, n.workFunc, jobsCmdDir, s.configImport(), s.jobsImport(), jobsGroupFunc, jobsDataVar, s.configPkg, dataConfigType, s.jobsPkg, n.commandConst)
}

// method is the scheduled method that starts the job.
func (stubJob) method(s *scaffold, pkg string) string {
	n := &s.names

	return fmt.Sprintf(`package %[1]s

import (
	"context"

	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/logger"
	"github.com/go-playground/errors/v5"

	appjobs "%[2]s"
)

type (
	// %[3]s is the scheduled method that starts the %[4]s job. On its schedule
	// Cloud Scheduler calls it, and it starts one execution of the application's job
	// process with the job's command (%[5]s %[4]s), through the job driver the
	// configuration built from the template job the stack sets (APP_JOBS_TEMPLATE,
	// resource/jobs/cloudrun) and the version. The service starts its job, the job
	// deployed with this revision, and the work never runs inside a request; where no
	// job is configured (development, a pull-request stack) the start is refused and
	// the call says so.
	//
	// It takes no input and runs as the application; it answers the execution it
	// started, which the call's log carries, and the execution's own outcome is Cloud
	// Run's to show.
	//
	// @rpc
	// @schedule(%[6]q)
	%[3]s struct{}

	// %[7]s is the method's outcome: the execution started.
	%[7]s struct {
		Execution string
	}
)

// Execute starts the job process on the job's command.
func (m *%[3]s) Execute(ctx context.Context, _ resource.ReadWriteTransaction, c *Client) (*%[7]s, error) {
	execution, err := c.Jobs().Start(ctx, %[8]s)
	if err != nil {
		return nil, errors.Wrap(err, "jobs.Starter.Start()")
	}
	logger.FromCtx(ctx).Infof("%[4]s: the job started as %%s", execution)

	return &%[7]s{Execution: execution}, nil
}
`, pkg, s.modulePath+"/"+jobsPkgDir, n.method, n.commandPath(), jobsCmdDir, s.schedule, n.outcome, n.startArgs("appjobs"))
}

// lintConfigFile is the lint configuration the skeleton carries; lintConfigs are the
// configurations the allowance is written into, the YAML forms alone.
const lintConfigFile = ".golangci.yml"

var lintConfigs = []string{lintConfigFile, ".golangci.yaml"}

// allowImport admits an import in the lint configuration's depguard allow list, so the
// application's own lint takes the package; a configuration that admits it, by the path
// or a prefix of it, is left as it is, and an application without the configuration
// has no lint to admit it in.
func allowImport(a *app.App, ch *Change, importPath, what string) error {
	for _, rel := range lintConfigs {
		if !exists(a, rel) {
			continue
		}
		data, mode, err := readFile(a, rel)
		if err != nil {
			return err
		}
		text, added := admitInAllowList(string(data), importPath)
		if !added {
			if text == "" {
				ch.skipf("%s: no depguard allow list to admit %s in; admit it where imports are allowed, or the lint refuses the job process", rel, importPath)
			}

			return nil
		}
		if err := os.WriteFile(a.Abs(rel), []byte(text), mode); err != nil {
			return errors.Wrap(err, "os.WriteFile()")
		}
		ch.didf("%s: %s in the depguard allow list, %s", rel, importPath, what)

		return nil
	}

	return nil
}

// admitInAllowList adds the path to the first depguard allow list of the configuration,
// after its last entry and at the entries' indentation. It answers the text unchanged
// and false when the list admits the path already, and empty and false when the
// configuration has no list.
func admitInAllowList(text, importPath string) (string, bool) {
	lines := strings.Split(text, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "allow:" {
			start = i

			break
		}
	}
	if start < 0 {
		return "", false
	}
	last, indent := start, ""
	for i := start + 1; i < len(lines); i++ {
		entry, ok := allowListEntry(lines[i])
		if !ok {
			if strings.TrimSpace(lines[i]) == "" {
				continue
			}

			break
		}
		if entry == importPath || !strings.HasPrefix(entry, "$") && strings.HasPrefix(importPath, entry+"/") {
			return text, false
		}
		last, indent = i, leadingSpace(lines[i])
	}
	if last == start {
		indent = leadingSpace(lines[start]) + "  "
	}
	out := slices.Insert(slices.Clone(lines), last+1, indent+"- "+importPath)

	return strings.Join(out, "\n"), true
}

// leadingSpace is the line's indentation.
func leadingSpace(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}

// allowListEntry reads one "- path" list item, as depguard's allow list writes them.
func allowListEntry(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "- ") {
		return "", false
	}

	return strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")), true
}
