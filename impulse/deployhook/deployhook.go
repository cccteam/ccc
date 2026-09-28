// Package deployhook is the contract between an application's hooks program and the deploy
// pipeline bedrock renders for it. A hook is the application's own work at a fixed point of
// a deploy: data work after the migrations, a check against the new revision before traffic
// moves to it, a smoke test after. An application writes its hooks as a Go program at
// cmd/deployment/hooks, beside the migrate command:
//
//	func main() {
//		deployhook.Main(deployhook.Hooks{
//			AfterMigrate:  backfill,
//			BeforeTraffic: checkNextRevision,
//		})
//	}
//
// bedrock reads which stages the program implements from that literal, renders a pipeline
// step for each, and checks that the Dockerfile builds the program into the image as
// /hooks. The pipeline's image build takes /hooks out of the image it built, and each hook
// step runs it with the stage as its argument and the pipeline's facts in its environment,
// on the build worker as the build's deploy identity, in the checkout: the same place a hook
// script (infrastructure/hooks/<stage>.sh) runs, so a program and scripts mix, one or the
// other per stage.
//
// Only the stages after the image build take a program, because the program comes out of
// the image: before-build (the image does not exist yet) and after-down (a teardown builds
// no image) take a script. A hook has no database and no application secrets; work that
// needs the application's runtime belongs to its job process (cmd/jobs), which a hook may
// start.
package deployhook

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
)

// Stage is one point of the deploy sequence a hooks program may implement.
type Stage string

// The stages a program may implement, in the pipeline's order.
const (
	// BeforeMigrate runs after the image is built (Facts.ImageDigest names it) and
	// before the migrate job.
	BeforeMigrate Stage = "before-migrate"
	// AfterMigrate runs once the schema is migrated, before the service deploys: a
	// backfill, a reference-data reload, an index warm-up.
	AfterMigrate Stage = "after-migrate"
	// BeforeTraffic runs with the new revision deployed in every region and the old one
	// still serving; Facts.NextURL reaches the new one. A failure stops the build with the
	// old revision serving.
	BeforeTraffic Stage = "before-traffic"
	// AfterTraffic runs once traffic moved: a smoke test, a cache warm. A failure stops the
	// build before the deployment record, so the next environment's gate never sees the
	// release.
	AfterTraffic Stage = "after-traffic"
)

// Stages are the stages a program may implement, in the pipeline's order.
var Stages = []Stage{BeforeMigrate, AfterMigrate, BeforeTraffic, AfterTraffic}

// Func is one hook: it does the stage's work and answers an error to stop the build.
type Func func(ctx context.Context, f *Facts) error

// Hooks are an application's hooks, one function per stage it implements; a stage left nil
// has none. bedrock reads the stages from the literal passed to Main, so a program builds
// it in one place, keyed by field.
type Hooks struct {
	BeforeMigrate Func
	AfterMigrate  Func
	BeforeTraffic Func
	AfterTraffic  Func
}

// For is the hook for the stage, nil when the program implements none.
func (h Hooks) For(s Stage) Func {
	switch s {
	case BeforeMigrate:
		return h.BeforeMigrate
	case AfterMigrate:
		return h.AfterMigrate
	case BeforeTraffic:
		return h.BeforeTraffic
	case AfterTraffic:
		return h.AfterTraffic
	default:
		return nil
	}
}

// Facts are what the pipeline knows about the build, as the hook steps pass them in the
// environment: the pipeline's facts and every substitution of the build.
type Facts struct {
	// App and Environment are the application and the environment deployed to (_APP,
	// _ENV).
	App         string
	Environment string
	// Version is what the process reports (a release tag, or pr<N>@<commit>), and
	// Release the image's release name (the tag, or pr<N>-<commit>).
	Version string
	Release string
	// Image and ImageDigest are the image this build deploys (IMAGE, IMAGE_DIGEST).
	Image       string
	ImageDigest string
	// PullRequest is the pull request's number in a pull-request build, 0 in a release
	// build. SharedDB says a pull request runs against the environment's database, and
	// ReloadDB that its own database is recreated this build.
	PullRequest int
	SharedDB    bool
	ReloadDB    bool
	// RunMigrations says the migrate job runs in this build.
	RunMigrations bool
	// NextURL is the new revision's public URL through the load balancer before traffic
	// moves (empty in a pull-request build, whose environment is its own), and
	// RevisionURLs the tagged run.app URL of the new revision by region, which the
	// services answer from inside the project alone. Both are set from before-traffic on.
	NextURL      string
	RevisionURLs map[string]string

	lookup func(string) string
}

// Substitution is a substitution of the build by name (_HOSTNAME, a declared _NAME), or
// any fact the pipeline exports; empty when the build has none.
func (f *Facts) Substitution(name string) string {
	if f.lookup == nil {
		return ""
	}

	return f.lookup(name)
}

// FactsFrom reads the facts through lookup, os.Getenv in a hook step.
func FactsFrom(lookup func(string) string) (*Facts, error) {
	f := &Facts{
		App: lookup("_APP"), Environment: lookup("_ENV"), Version: lookup("VERSION"), Release: lookup("RELEASE"),
		Image: lookup("IMAGE"), ImageDigest: lookup("IMAGE_DIGEST"),
		SharedDB: lookup("SHARED_DB") == set, ReloadDB: lookup("RELOAD_DB") == set, RunMigrations: lookup("RUN_MIGRATIONS") == set,
		NextURL: lookup("NEXT_URL"), RevisionURLs: map[string]string{}, lookup: lookup,
	}
	if pr := lookup("_PR_NUMBER"); pr != "" {
		n, err := strconv.Atoi(pr)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("_PR_NUMBER %q is not a pull request number", pr)
		}
		f.PullRequest = n
	}
	for _, pair := range strings.Split(lookup("REVISION_URLS"), ",") {
		if region, url, ok := strings.Cut(pair, "="); ok && region != "" {
			f.RevisionURLs[region] = url
		}
	}

	return f, nil
}

// Run runs the hook for the stage, refusing a stage the program does not implement.
func Run(ctx context.Context, h Hooks, s Stage, f *Facts) error {
	fn := h.For(s)
	if fn == nil {
		return fmt.Errorf("this hooks program implements no %s hook; the pipeline runs a stage the program's Hooks literal names, so render the pipeline again", s)
	}

	return fn(ctx, f)
}

// The exit codes Main answers, and how the pipeline writes a fact that is set.
const (
	exitFailed = 1
	exitUsage  = 2
	set        = "true"
)

// Main runs the stage its one argument names with the facts in the environment, and exits:
// 0 when the hook succeeds, 1 when it fails (the build stops at the stage), 2 when the
// program is run without a stage it implements. The context ends when the step is stopped.
func Main(h Hooks) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, h, os.Args[1:], os.Getenv, os.Stdout)
	stop()
	os.Exit(code)
}

// run is Main without the process: the arguments after the program's name, the lookup the
// facts are read through, and where it reports.
func run(ctx context.Context, h Hooks, args []string, lookup func(string) string, out io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintf(out, "usage: hooks <stage>, one of %v\n", Stages)

		return exitUsage
	}
	stage := Stage(args[0])
	if h.For(stage) == nil {
		fmt.Fprintf(out, "hooks: %s is not a stage this program implements (it implements %v)\n", stage, implemented(h))

		return exitUsage
	}
	f, err := FactsFrom(lookup)
	if err != nil {
		fmt.Fprintf(out, "hooks: %v\n", err)

		return exitUsage
	}
	if err := Run(ctx, h, stage, f); err != nil {
		fmt.Fprintf(out, "hooks: the %s hook failed: %v\n", stage, err)

		return exitFailed
	}

	return 0
}

// implemented are the stages the hooks implement.
func implemented(h Hooks) []Stage {
	var stages []Stage
	for _, s := range Stages {
		if h.For(s) != nil {
			stages = append(stages, s)
		}
	}

	return stages
}
