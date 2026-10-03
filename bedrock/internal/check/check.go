// Package check compares a committed application stack with what the code declares: it
// renders the stack afresh and reports every owned file whose committed content differs,
// which is the drift between the code and the infrastructure.
package check

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/render"
)

// Finding is one file that is not as the render says it should be.
type Finding struct {
	// Path is the file's path relative to the stack directory, or to the application
	// root when Root is set.
	Path string
	// Root is true for a file at the application root (the pipeline files).
	Root bool
	// Missing reports a file the render produces that the directory lacks.
	Missing bool
	// Line is the first differing line (1-based), 0 when Missing.
	Line int
	// Want and Got are that line as rendered and as committed.
	Want string
	Got  string
}

// Authoritative is an authoritative IAM resource (a *_iam_binding or *_iam_policy)
// declared in one of the stack's files, other than a file store's bucket policy. The
// stack refuses them: such a resource replaces every member of its role or policy on
// each apply, so a pull-request stack applying one would remove the environment's
// members, and two pull requests each other's. A *_iam_member adds one member and
// removes only that one. The file stores' bucket policies
// (google_storage_bucket_iam_policy.<store>, as storage.tf declares them) are admitted
// by address: the stack sets each bucket's whole permission list on purpose, so that
// Cloud Storage's default grants to the project's basic roles are gone from it, and a
// pull-request stack makes buckets of its own, so its policies remove nobody else's
// members. A policy on any other bucket, and a binding on a file store's bucket, stay
// refused.
type Authoritative struct {
	// Path is the file's path relative to the stack directory.
	Path string
	// Line is the resource block's line (1-based).
	Line int
	// Address is the resource's type.name.
	Address string
}

// Report is the outcome of one check.
type Report struct {
	// Dir is the stack directory checked, and AppDir the application root its pipeline
	// files were checked at.
	Dir    string
	AppDir string
	// Checked counts the owned files compared.
	Checked int
	// Findings are the owned files that differ or are missing, in path order.
	Findings []Finding
	// Unseeded lists the seeded files the directory lacks: not drift, since the tool
	// writes them once and a person keeps them, but worth a line.
	Unseeded []string
	// Authoritative lists the authoritative IAM resources declared anywhere in the
	// stack, owned files and a person's alike, in path then line order, the file
	// stores' bucket policies excepted.
	Authoritative []Authoritative
	// Migrations are the problems with the schema migrations directory: a file that
	// is not a migration, an index with two up files, a gap in the sequence.
	Migrations []MigrationFinding
	// BuildSecrets are the build secrets the Dockerfile mounts as required that some
	// environment's placement does not declare.
	BuildSecrets []BuildSecretFinding
	// Binaries are the jobs whose command the Dockerfile does not build, Bundles the
	// browser bundles whose variable it does not set, and JobNames the instructions it
	// lacks for carrying the build's job to the site.
	Binaries []BinaryFinding
	Bundles  []BundleFinding
	JobNames []JobNameFinding
	// Stages are the instructions the Dockerfile's reserved stages hold beyond their
	// install, which the image build would export to the registry's cache for every
	// environment; MissingStages are the reserved stages the Dockerfile lacks, whose
	// downloads the image build then caches nothing of (a warning, not drift).
	Stages        []StageFinding
	MissingStages []string
	// ReleaseLines are the release-please settings under which a feature release would
	// not open a new hotfix line (a feature on the patch below 1.0).
	ReleaseLines []ReleaseLineFinding
	// Maintenance are the warnings about the maintenance windows: production without a
	// setting, a release file the checkout lacks, a dated slot that has passed. Warnings,
	// not drift: the check stays clean, so the refusal at run start is never the first
	// sign.
	Maintenance []MaintenanceFinding
}

// MaintenanceFinding is one warning about the maintenance windows.
type MaintenanceFinding struct {
	// Problem says what is missing or over, and the fix.
	Problem string
}

// Clean reports no drift, no refused resource, a sound migration sequence, every
// required build secret declared, every job's binary built, reserved stages holding
// their install alone, and release lines that a feature release opens.
func (r *Report) Clean() bool {
	return len(r.Findings) == 0 && len(r.Authoritative) == 0 && len(r.Migrations) == 0 && len(r.BuildSecrets) == 0 && len(r.Binaries) == 0 && len(r.Bundles) == 0 && len(r.JobNames) == 0 && len(r.Stages) == 0 && len(r.ReleaseLines) == 0
}

// Run renders the model and compares the owned files with the directory's, and the
// owned files at the application root with appDir's. It also reads the schema migrations
// directory and the seed directory beside it for a sequence the migrate command could not
// apply in order.
func Run(m *derive.Model, dir, appDir string) (*Report, error) {
	files, err := render.Render(m)
	if err != nil {
		return nil, err
	}
	r := &Report{Dir: dir, AppDir: appDir}
	for _, f := range files {
		in := dir
		if f.Root {
			in = appDir
		}
		committed, err := os.ReadFile(filepath.Join(in, filepath.FromSlash(f.Path)))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, errors.Wrapf(err, "os.ReadFile(): %s", f.Path)
		}
		if f.Tier == render.Seeded {
			if err != nil {
				r.Unseeded = append(r.Unseeded, f.Path)
			}

			continue
		}
		r.Checked++
		if err != nil {
			r.Findings = append(r.Findings, Finding{Path: f.Path, Root: f.Root, Missing: true})

			continue
		}
		if line, want, got, same := firstDifference(f.Content, committed); !same {
			r.Findings = append(r.Findings, Finding{Path: f.Path, Root: f.Root, Line: line, Want: want, Got: got})
		}
	}
	authoritative, err := scanAuthoritative(dir, fileStorePolicies(m))
	if err != nil {
		return nil, err
	}
	r.Authoritative = authoritative
	for _, d := range migrationDirs(m) {
		migrations, err := scanMigrations(filepath.Join(appDir, filepath.FromSlash(d)), d)
		if err != nil {
			return nil, err
		}
		r.Migrations = append(r.Migrations, migrations...)
	}
	buildSecrets, err := scanBuildSecrets(appDir, dir, m.Placement.Environments)
	if err != nil {
		return nil, err
	}
	r.BuildSecrets = buildSecrets
	if err := r.scanImage(appDir, m); err != nil {
		return nil, err
	}
	releaseLines, err := scanReleaseLines(appDir)
	if err != nil {
		return nil, err
	}
	r.ReleaseLines = releaseLines
	r.Maintenance = scanMaintenance(m, appDir, time.Now())

	return r, nil
}

// scanImage reads the Dockerfile against the stack: the binaries its jobs run, the
// bundles its site serves, the job name it carries, and its reserved stages.
func (r *Report) scanImage(appDir string, m *derive.Model) error {
	binaries, err := scanBinaries(appDir, m)
	if err != nil {
		return err
	}
	r.Binaries = binaries
	r.Bundles = scanBundles(m)
	jobNames, err := scanJobName(appDir, m)
	if err != nil {
		return err
	}
	r.JobNames = jobNames
	stages, missing, err := scanReservedStages(appDir)
	if err != nil {
		return err
	}
	r.Stages, r.MissingStages = stages, missing

	return nil
}

// scanMaintenance warns about the maintenance windows at now: production without a
// setting (a breaking release to it is refused at run start until one is written), no
// release file in the router package (no outlet declares an oldest answered release, so
// no release is breaking and the window never holds a run), a release file that does not
// read, and a dated slot that has passed.
func scanMaintenance(m *derive.Model, appDir string, now time.Time) []MaintenanceFinding {
	var findings []MaintenanceFinding
	p := m.Placement
	if _, ok := p.MaintenanceSetting(p.Production()); !ok {
		findings = append(findings, MaintenanceFinding{Problem: fmt.Sprintf("%s has no maintenance setting (placement.json \"maintenance\": {%q: ...}): a breaking release to %s is refused at the start of its run until one is written; %q is a setting, and so are the client's windows", p.Production(), p.Production(), p.Production(), derive.MaintenanceAnytime)})
	}
	for _, env := range p.Environments {
		setting, ok := p.MaintenanceSetting(env)
		if !ok {
			continue
		}
		for _, s := range setting.PassedDates(now) {
			findings = append(findings, MaintenanceFinding{Problem: fmt.Sprintf("maintenance.%s: the dated slot on %s (%s to %s) has passed; remove it", env, s.On, s.From, s.To)})
		}
	}
	switch _, err := derive.ReadReleaseFile(filepath.Join(appDir, filepath.FromSlash(m.RouterDir))); {
	case m.RouterDir == "":
		findings = append(findings, MaintenanceFinding{Problem: "the site generator declares no routes directory (GenerateRoutes), so there is no release file to read and no outlet declares an oldest answered release: no release is breaking, and the maintenance window never holds a run"})
	case errors.Is(err, os.ErrNotExist):
		findings = append(findings, MaintenanceFinding{Problem: fmt.Sprintf("no release file at %s: no outlet declares an oldest answered release, so no release is breaking and the maintenance window never holds a run; the resource generator writes it beside the generated router (go generate ./...)", path.Join(m.RouterDir, derive.ReleaseFileName))})
	case err != nil:
		findings = append(findings, MaintenanceFinding{Problem: fmt.Sprintf("the release file does not read, and the release check will refuse the run: %v", errors.Cause(err))})
	}

	return findings
}

// migrationDirs is the schema migrations directory and the seed directory beside it
// (schema/devseed, the data migrations the migrate command applies with -seed), both
// root-relative; none when the application has no schema.
func migrationDirs(m *derive.Model) []string {
	if m.Schema.MigrationsDir == "" {
		return nil
	}

	return []string{m.Schema.MigrationsDir, path.Join(path.Dir(m.Schema.MigrationsDir), derive.SeedDir)}
}

// authoritativeResource matches the opening line of an authoritative IAM resource
// block: resource "<type>_iam_binding" "<name>" or resource "<type>_iam_policy" "<name>".
var authoritativeResource = regexp.MustCompile(`^\s*resource\s+"([A-Za-z0-9_]+_iam_(?:binding|policy))"\s+"([^"]+)"`)

// fileStorePolicies are the file stores' bucket policies by address: the authoritative
// IAM resources the stack declares on purpose, which the scan admits.
func fileStorePolicies(m *derive.Model) []string {
	policies := make([]string, 0, len(m.FileStores))
	for i := range m.FileStores {
		policies = append(policies, m.FileStores[i].PolicyAddress())
	}

	return policies
}

// ScanAuthoritative finds the authoritative IAM resources in every .tf file of the
// directory, a person's files included, other than the ones at the admitted addresses
// (the file stores' bucket policies): what check refuses, and what the pipeline's test
// of a stack's plan refuses before the apply.
func ScanAuthoritative(dir string, admitted []string) ([]Authoritative, error) {
	return scanAuthoritative(dir, admitted)
}

// scanAuthoritative finds the authoritative IAM resources in every .tf file of the
// directory, a person's files included, other than the ones at the admitted addresses.
func scanAuthoritative(dir string, admitted []string) ([]Authoritative, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.tf"))
	if err != nil {
		return nil, errors.Wrap(err, "filepath.Glob()")
	}
	var found []Authoritative
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, errors.Wrapf(err, "os.ReadFile(): %s", path)
		}
		for i, line := range strings.Split(string(data), "\n") {
			m := authoritativeResource.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			address := m[1] + "." + m[2]
			if slices.Contains(admitted, address) {
				continue
			}
			found = append(found, Authoritative{Path: filepath.Base(path), Line: i + 1, Address: address})
		}
	}

	return found, nil
}

// firstDifference finds the first line where the two texts part, or reports them the
// same.
func firstDifference(want, got []byte) (line int, wantLine, gotLine string, same bool) {
	if bytes.Equal(want, got) {
		return 0, "", "", true
	}
	wantLines := strings.Split(string(want), "\n")
	gotLines := strings.Split(string(got), "\n")
	for i := range max(len(wantLines), len(gotLines)) {
		var w, g string
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if w != g || i >= len(wantLines) || i >= len(gotLines) {
			return i + 1, w, g, false
		}
	}

	return len(wantLines), "", "", false
}

// Write prints the report the way a person reads it: one line per file, the first
// differing line under each.
func (r *Report) Write(w io.Writer) {
	if len(r.Findings) == 0 {
		fmt.Fprintf(w, "%s (and the pipeline at %s): %d owned file(s) match the code\n", r.Dir, r.AppDir, r.Checked)
	} else {
		fmt.Fprintf(w, "%s (and the pipeline at %s): %d of %d owned file(s) differ from the code\n", r.Dir, r.AppDir, len(r.Findings), r.Checked)
	}
	for _, f := range r.Findings {
		where := ""
		if f.Root {
			where = " (at the application root)"
		}
		if f.Missing {
			fmt.Fprintf(w, "  missing  %s%s\n", f.Path, where)

			continue
		}
		fmt.Fprintf(w, "  differs  %s:%d%s\n", f.Path, f.Line, where)
		fmt.Fprintf(w, "           code:      %s\n", f.Want)
		fmt.Fprintf(w, "           committed: %s\n", f.Got)
	}
	for _, path := range r.Unseeded {
		fmt.Fprintf(w, "  unseeded %s (bedrock render creates it once)\n", path)
	}
	for _, a := range r.Authoritative {
		fmt.Fprintf(w, "  refused  %s:%d %s: an authoritative IAM resource replaces every member on each apply; declare a *_iam_member per member instead (a file store's bucket policy, storage.tf's, is the one admitted)\n", a.Path, a.Line, a.Address)
	}
	for _, mf := range r.Migrations {
		fmt.Fprintf(w, "  refused  %s: %s\n", mf.Path, mf.Problem)
	}
	for _, bs := range r.BuildSecrets {
		fmt.Fprintf(w, "  refused  Dockerfile mounts build secret %s as required; %s declare%s no such secret (terraform.tfvars build_secrets)\n", bs.ID, joinEnvironments(bs.Missing), pluralS(len(bs.Missing)))
	}
	for _, b := range r.Binaries {
		fmt.Fprintf(w, "  refused  Dockerfile builds no %s, the command %s: go build -o /build%s ./%s in the Go stage, with /build copied into the runtime image\n", b.Binary, b.Runs, b.Binary, b.Dir)
	}
	for _, b := range r.Bundles {
		fmt.Fprintf(w, "  refused  Dockerfile sets no %s: the bundle %s is built in a browser stage of its workspace, copied under the working directory and named by an ENV %s=<path>, as the seeded Dockerfile does\n", b.Var, b.Path, b.Var)
	}
	for _, j := range r.JobNames {
		fmt.Fprintf(w, "  refused  Dockerfile lacks %s: the pipeline passes the job of each build as the build argument JOBS_JOB, and the runtime stage sets %s from it (ARG JOBS_JOB, then ENV %s=\"${JOBS_JOB}\"), as the seeded Dockerfile does, so the site starts the job of its own build\n", j.Missing, j.Var, j.Var)
	}
	for _, s := range r.Stages {
		fmt.Fprintf(w, "  refused  Dockerfile:%d stage %s %s (%q): the image build exports this stage's layers to the registry's cache, which every environment's build reads, so the stage holds its install and nothing else\n", s.Line, s.Stage, s.Problem, s.Instruction)
	}
	for _, rl := range r.ReleaseLines {
		fmt.Fprintf(w, "  refused  %s: %s\n", rl.Path, rl.Problem)
	}
	for _, name := range r.MissingStages {
		fmt.Fprintf(w, "  warning  Dockerfile has no %s stage: the image build caches nothing for %s, and every environment's build repeats it; the seeded Dockerfile (bedrock render into an empty directory) shows the stage\n", name, reserved[name].what)
	}
	for _, mf := range r.Maintenance {
		fmt.Fprintf(w, "  warning  %s\n", mf.Problem)
	}
}

// joinEnvironments writes environments the way a sentence lists them: "stg", "stg and
// prd", "tst, stg and prd".
func joinEnvironments(envs []string) string {
	switch len(envs) {
	case 0:
		return ""
	case 1:
		return envs[0]
	default:
		return strings.Join(envs[:len(envs)-1], ", ") + " and " + envs[len(envs)-1]
	}
}

// pluralS is the verb's ending for one environment ("declares") or several ("declare").
func pluralS(n int) string {
	if n == 1 {
		return "s"
	}

	return ""
}
