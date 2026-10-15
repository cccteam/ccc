package transition

import (
	"go/format"
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseJobName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    jobNames
		wantErr string
	}{
		{
			name: "a flat job",
			in:   "prune-reports",
			want: jobNames{name: "prune-reports", pascal: "PruneReports", pkg: "prunereports", pkgDir: "cmd/jobs/prunereports", leafFile: "cmd/jobs/prunereports/prunereports.go", workFile: "pkg/jobs/prune_reports.go", workFunc: "PruneReports", commandConst: "PruneReportsCommand", method: "PruneReports", outcome: "PruneReportsStarted", methodFile: "prune_reports.go"},
		},
		{
			name: "a grouped job",
			in:   "files/cleanup",
			want: jobNames{group: "files", name: "cleanup", pascal: "FilesCleanup", pkg: "cleanup", pkgDir: "cmd/jobs/files/cleanup", leafFile: "cmd/jobs/files/cleanup/cleanup.go", groupPkg: "files", groupDir: "cmd/jobs/files", groupFile: "cmd/jobs/files/files.go", workFile: "pkg/jobs/files_cleanup.go", workFunc: "FilesCleanup", commandConst: "FilesCleanupCommand", groupConst: "FilesGroup", groupConstFile: "pkg/jobs/files_group.go", method: "FilesCleanup", outcome: "FilesCleanupStarted", methodFile: "files_cleanup.go"},
		},
		{name: "a word with digits", in: "report2", want: jobNames{name: "report2", pascal: "Report2", pkg: "report2", pkgDir: "cmd/jobs/report2", leafFile: "cmd/jobs/report2/report2.go", workFile: "pkg/jobs/report2.go", workFunc: "Report2", commandConst: "Report2Command", method: "Report2", outcome: "Report2Started", methodFile: "report2.go"}},
		{name: "uppercase", in: "PruneReports", wantErr: "lowercase words"},
		{name: "an underscore", in: "prune_reports", wantErr: "lowercase words"},
		{name: "a leading hyphen", in: "-prune", wantErr: "lowercase words"},
		{name: "a double hyphen", in: "prune--reports", wantErr: "lowercase words"},
		{name: "a leading digit", in: "2prune", wantErr: "lowercase words"},
		{name: "empty", in: "", wantErr: "lowercase words"},
		{name: "an empty group", in: "/cleanup", wantErr: "lowercase words"},
		{name: "an empty name under a group", in: "files/", wantErr: "lowercase words"},
		{name: "two groups", in: "a/b/c", wantErr: "one group at most"},
		{name: "cobra's help", in: "help", wantErr: "help is a command of every root"},
		{name: "cobra's completion as a group", in: "completion/one", wantErr: "completion is a command of every root"},
		{name: "too long", in: strings.Repeat("a", 65), wantErr: "at most 64 characters"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseJobName(tt.in)
			switch {
			case tt.wantErr != "":
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parseJobName(%q) error = %v, want %q", tt.in, err, tt.wantErr)
				}
			case err != nil:
				t.Fatalf("parseJobName(%q) error = %v", tt.in, err)
			default:
				if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(jobNames{})); diff != "" {
					t.Errorf("parseJobName(%q) mismatch (-want +got):\n%s", tt.in, diff)
				}
			}
		})
	}
}

func TestValidateSchedule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		schedule string
		wantErr  string
	}{
		{name: "daily", schedule: "0 9 * * *"},
		{name: "stepped and ranged", schedule: "*/15 1-5 * JAN-MAR MON,FRI"},
		{name: "four fields", schedule: "0 9 * *", wantErr: "five fields"},
		{name: "two spaces", schedule: "0  9 * * *", wantErr: "five fields"},
		{name: "a stray character", schedule: "0 9 * * ?", wantErr: "not a cron field"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateSchedule(tt.schedule)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("validateSchedule(%q) error = %v", tt.schedule, err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Errorf("validateSchedule(%q) error = %v, want %q", tt.schedule, err, tt.wantErr)
			}
		})
	}
}

func TestJobValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		job     Job
		extra   map[string]string
		bare    bool
		wantErr string
	}{
		{name: "a flat job", job: Job{Name: "prune-reports"}},
		{name: "a scheduled job", job: Job{Name: "prune-reports", Schedule: "0 3 * * *"}},
		{name: "a bad name", job: Job{Name: "Prune"}, wantErr: "lowercase words"},
		{name: "a bad schedule", job: Job{Name: "prune-reports", Schedule: "daily"}, wantErr: "five fields"},
		{name: "the configuration package's name", job: Job{Name: "config"}, wantErr: "its package would be config, which the job process imports already"},
		{name: "a reserved package", job: Job{Name: "jobs"}, wantErr: "its package would be jobs"},
		{name: "a reserved group", job: Job{Name: "cobra/one"}, wantErr: "its package would be cobra"},
		{name: "no data level", job: Job{Name: "prune-reports"}, bare: true, wantErr: "no file declares a DataConfiguration struct"},
		{name: "the command's package exists", job: Job{Name: "prune-reports"}, extra: map[string]string{"cmd/jobs/prunereports/prunereports.go": "package prunereports\n"}, wantErr: "cmd/jobs/prunereports exists: the job prune-reports is there already"},
		{name: "the work exists", job: Job{Name: "prune-reports"}, extra: map[string]string{"pkg/jobs/prune_reports.go": "package jobs\n"}, wantErr: "pkg/jobs/prune_reports.go exists: the job prune-reports has its work there already"},
		{name: "a directory of the group's name that is no group", job: Job{Name: "files/cleanup"}, extra: map[string]string{"cmd/jobs/files/other.go": "package files\n"}, wantErr: "cmd/jobs/files exists but declares no group (cmd/jobs/files/files.go)"},
		{name: "the method exists", job: Job{Name: "prune-reports", Schedule: "0 3 * * *"}, extra: map[string]string{"pkg/rpc/prune_reports.go": "package rpc\n", "cmd/generate/resourcegenerator/main.go": rpcProgram}, wantErr: "pkg/rpc/prune_reports.go exists: the scheduled method PruneReports is there already"},
		{name: "a scheduled job in the sites layout", job: Job{Name: "prune-reports", Schedule: "0 3 * * *"}, extra: map[string]string{"cmd/generate/resourcegenerator/main.go": sitesProgram}, wantErr: "sites layout is not supported yet"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			files := map[string]string{}
			if !tt.bare {
				files = skeletonFiles(t)
			}
			for rel, text := range tt.extra {
				files[rel] = text
			}
			err := tt.job.Validate(beacon(t, files))
			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("Validate() error = %v", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Errorf("Validate() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

// formatted fails the test when a Go file the transition wrote does not parse or is not
// formatted.
func formatted(t *testing.T, rel, text string) {
	t.Helper()

	out, err := format.Source([]byte(text))
	if err != nil {
		t.Errorf("%s does not parse: %v", rel, err)
	} else if string(out) != text {
		t.Errorf("%s is not formatted (-formatted +got):\n%s", rel, cmp.Diff(string(out), text))
	}
}

func TestJobApply(t *testing.T) {
	t.Parallel()

	// An application from before the skeleton admitted the command tree's library.
	files := skeletonFiles(t)
	files[".golangci.yml"] = replaceOnce(t, files[".golangci.yml"], "            - github.com/spf13/cobra\n", "")
	a := beacon(t, files)
	exec := &fakeExec{}
	change, err := Job{Name: "prune-reports"}.Apply(t.Context(), a, exec)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	wantDid := []string{
		"pkg/jobs/prune_reports.go: PruneReportsCommand (prune-reports) and PruneReports, the work, empty until it is written",
		"cmd/jobs/main.go: the job process, a cobra root that opens the data level once and registers the commands",
		"cmd/jobs/prunereports/prunereports.go: the prune-reports command, Run over the data level",
		"cmd/jobs/main.go: prunereports.Command(data) registered on the root",
		".golangci.yml: github.com/spf13/cobra in the depguard allow list, the command tree's library",
		"go.mod: github.com/spf13/cobra, the command tree's library, required directly (go mod tidy)",
	}
	if diff := cmp.Diff(wantDid, change.Did); diff != "" {
		t.Errorf("Did mismatch (-want +got):\n%s", diff)
	}
	if len(change.Skipped) != 0 {
		t.Errorf("Skipped = %q, want none", change.Skipped)
	}
	wantLeft := []string{
		"pkg/jobs/prune_reports.go: PruneReports, the work (replace the error with the job's work over the resource client, drop the error's variable, and make the command's Short say what it does)",
		"test/integration: a suite driving PruneReports over the test database, as the file store's cleanup suite does",
		"nothing starts prune-reports yet: a method marked @schedule in the rpc package starts it through the Client's Jobs() (impulse add job --schedule writes one), or a caller starts it through the starter",
	}
	if diff := cmp.Diff(wantLeft, change.Left); diff != "" {
		t.Errorf("Left mismatch (-want +got):\n%s", diff)
	}
	if !strings.Contains(change.LeftNote, "PruneReports is written empty") {
		t.Errorf("LeftNote = %q", change.LeftNote)
	}
	if diff := cmp.Diff([]string{"go mod tidy"}, exec.calls); diff != "" {
		t.Errorf("exec calls mismatch (-want +got): no schedule, so no regeneration:\n%s", diff)
	}
	wantContains := map[string][]string{
		"pkg/jobs/prune_reports.go": {
			"// Package jobs holds the work the application's job process runs",
			`const PruneReportsCommand = "prune-reports"`,
			"the argument a scheduled method starts the job with, once one does",
			`var errPruneReportsNotWritten = errors.New("prune-reports: the job's work is not written yet")`,
			"func PruneReports(_ context.Context, _ resource.Client) error {\n\treturn errPruneReportsNotWritten\n}",
		},
		"cmd/jobs/main.go": {
			`"example.com/acme/beacon/cmd/jobs/prunereports"`,
			`"example.com/acme/beacon/pkg/config"`,
			"data, err := config.NewDataConfiguration(ctx)",
			"\troot.AddCommand(\n\t\tprunereports.Command(data),\n\t)\n",
		},
		"cmd/jobs/prunereports/prunereports.go": {
			"package prunereports",
			"func Command(data *config.DataConfiguration) *cobra.Command {",
			"Use:   jobs.PruneReportsCommand,",
			`Short: "Runs the prune-reports job",`,
			"return c.Run(cmd.Context())",
			"jobs.PruneReports(ctx, c.data.ResourceClient())",
		},
		".golangci.yml": {"            - google.golang.org/grpc/codes\n            - github.com/spf13/cobra\n"},
	}
	for rel, wants := range wantContains {
		text := read(t, a, rel)
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Errorf("%s lacks %q:\n%s", rel, want, text)
			}
		}
		if strings.HasSuffix(rel, ".go") {
			formatted(t, rel, text)
		}
	}
	for _, rel := range []string{"pkg/rpc/rpc.go", "pkg/rpc/prune_reports.go", "pkg/config/scheduled.go"} {
		if exists(a, rel) {
			t.Errorf("%s was written for a job with no schedule", rel)
		}
	}

	// A second job registers on the root the first wrote; the allowance holds; the
	// first job's name is refused.
	a = rediscovered(t, a)
	exec = &fakeExec{}
	change, err = Job{Name: "send-digests"}.Apply(t.Context(), a, exec)
	if err != nil {
		t.Fatalf("second Apply() error = %v", err)
	}
	wantDid = []string{
		"pkg/jobs/send_digests.go: SendDigestsCommand (send-digests) and SendDigests, the work, empty until it is written",
		"cmd/jobs/senddigests/senddigests.go: the send-digests command, Run over the data level",
		"cmd/jobs/main.go: senddigests.Command(data) registered on the root",
		"go.mod: github.com/spf13/cobra, the command tree's library, required directly (go mod tidy)",
	}
	if diff := cmp.Diff(wantDid, change.Did); diff != "" {
		t.Errorf("second Did mismatch (-want +got):\n%s", diff)
	}
	root := read(t, a, "cmd/jobs/main.go")
	if !strings.Contains(root, "\troot.AddCommand(\n\t\tprunereports.Command(data),\n\t\tsenddigests.Command(data),\n\t)\n") || strings.Count(root, "example.com/acme/beacon/cmd/jobs/") != 2 {
		t.Errorf("the root after two jobs:\n%s", root)
	}
	formatted(t, "cmd/jobs/main.go", root)
	if work := read(t, a, "pkg/jobs/send_digests.go"); strings.Contains(work, "// Package jobs") {
		t.Errorf("the second job's file opens the package again:\n%s", work)
	}
	if lint := read(t, a, ".golangci.yml"); strings.Count(lint, "github.com/spf13/cobra") != 1 {
		t.Errorf(".golangci.yml admits cobra %d times", strings.Count(lint, "github.com/spf13/cobra"))
	}
	if err := (Job{Name: "prune-reports"}).Validate(rediscovered(t, a)); err == nil || !strings.Contains(err.Error(), "is there already") {
		t.Errorf("Validate() of the first job again error = %v, want the refusal", err)
	}
}

func TestJobApplyGrouped(t *testing.T) {
	t.Parallel()

	a := beacon(t, skeletonFiles(t))
	change, err := Job{Name: "reports/materialize"}.Apply(t.Context(), a, &fakeExec{})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	wantDid := []string{
		"pkg/jobs/reports_materialize.go: ReportsMaterializeCommand (materialize) and ReportsMaterialize, the work, empty until it is written",
		"pkg/jobs/reports_group.go: ReportsGroup (reports), the group's name",
		"cmd/jobs/main.go: the job process, a cobra root that opens the data level once and registers the commands",
		"cmd/jobs/reports/reports.go: the reports group, a command holding its commands",
		"cmd/jobs/main.go: reports.Command(data) registered on the root",
		"cmd/jobs/reports/materialize/materialize.go: the reports materialize command, Run over the data level",
		"cmd/jobs/reports/reports.go: materialize.Command(data) registered on the reports group",
		"go.mod: github.com/spf13/cobra, the command tree's library, required directly (go mod tidy)",
	}
	if diff := cmp.Diff(wantDid, change.Did); diff != "" {
		t.Errorf("Did mismatch (-want +got):\n%s", diff)
	}
	wantContains := map[string][]string{
		"pkg/jobs/reports_group.go":                   {`const ReportsGroup = "reports"`},
		"pkg/jobs/reports_materialize.go":             {`const ReportsMaterializeCommand = "materialize"`, "the command's name under the reports group (ReportsGroup)"},
		"cmd/jobs/reports/reports.go":                 {"package reports", "Use:   jobs.ReportsGroup,", "\tgroup.AddCommand(\n\t\tmaterialize.Command(data),\n\t)\n", `"example.com/acme/beacon/cmd/jobs/reports/materialize"`},
		"cmd/jobs/reports/materialize/materialize.go": {"package materialize", "Use:   jobs.ReportsMaterializeCommand,", "jobs.ReportsMaterialize(ctx, c.data.ResourceClient())"},
		"cmd/jobs/main.go":                            {"\troot.AddCommand(\n\t\treports.Command(data),\n\t)\n", `"example.com/acme/beacon/cmd/jobs/reports"`},
	}
	for rel, wants := range wantContains {
		text := read(t, a, rel)
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Errorf("%s lacks %q:\n%s", rel, want, text)
			}
		}
		formatted(t, rel, text)
	}

	// A second job of the group joins it: no group written, nothing on the root.
	change, err = Job{Name: "reports/prune"}.Apply(t.Context(), rediscovered(t, a), &fakeExec{})
	if err != nil {
		t.Fatalf("second Apply() error = %v", err)
	}
	for _, d := range change.Did {
		if strings.Contains(d, "the reports group, a command") || strings.Contains(d, "registered on the root") || strings.Contains(d, "ReportsGroup (reports)") {
			t.Errorf("the second job of the group did %q", d)
		}
	}
	if !slicesContains(change.Did, "cmd/jobs/reports/reports.go: prune.Command(data) registered on the reports group") {
		t.Errorf("Did lacks the group registration:\n%s", strings.Join(change.Did, "\n"))
	}
	if group := read(t, a, "cmd/jobs/reports/reports.go"); !strings.Contains(group, "\tgroup.AddCommand(\n\t\tmaterialize.Command(data),\n\t\tprune.Command(data),\n\t)\n") {
		t.Errorf("the group after two jobs:\n%s", group)
	}
}

func TestJobApplyScheduled(t *testing.T) {
	t.Parallel()

	// A fresh application: the job brings the starter's whole wiring.
	a := beacon(t, skeletonFiles(t))
	exec := &fakeExec{}
	change, err := Job{Name: "prune-reports", Schedule: "0 3 * * *"}.Apply(t.Context(), a, exec)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	wantDid := []string{
		`pkg/rpc: the rpc package, its Client carrying the job process's starter (Jobs()), and PruneReports (@rpc, @schedule("0 3 * * *")), which starts cmd/jobs prune-reports through it`,
		"pkg/config/site.go: SiteConfiguration gained the fields, built from the environment; pkg/config/scheduled.go: Scheduler() and Jobs(), which the app's Configurer asks for, and Close(), which releases the driver with the level",
		"app/app.go: App gained Scheduler() and Jobs() on Configurer, the scheduler field, its construction, and the RPC client built over the starter; app/scheduled.go: SchedulerAuth, the middleware the generated router mounts the scheduled routes behind, and RPCClient(), the dependencies of the RPC methods",
		"the test configurers gained a nil guard and a fake starter: test/authz/harness_test.go (testConfigurer: Scheduler() and Jobs()); test/integration/harness_test.go (servedConfigurer: Scheduler() and Jobs())",
		`cmd/generate/resourcegenerator/main.go: WithRPC("pkg/rpc"), so the generator reads the rpc package`,
		".envrc.template: APP_JOBS_TEMPLATE documented in the site block, unset",
		"ran go generate ./..., which emitted the PruneReports handler, its scheduled route under /_scheduled, and the router's SchedulerAuth requirement",
	}
	for _, want := range wantDid {
		if !slicesContains(change.Did, want) {
			t.Errorf("Did lacks %q:\n%s", want, strings.Join(change.Did, "\n"))
		}
	}
	if len(change.Skipped) != 0 {
		t.Errorf("Skipped = %q, want none", change.Skipped)
	}
	if diff := cmp.Diff([]string{"go mod tidy", "go generate ./..."}, exec.calls); diff != "" {
		t.Errorf("exec calls mismatch (-want +got):\n%s", diff)
	}
	for _, left := range change.Left {
		if strings.Contains(left, "nothing starts") {
			t.Errorf("Left names the schedule as missing: %q", left)
		}
	}
	wantContains := map[string][]string{
		"pkg/rpc/prune_reports.go": {
			"// @rpc\n\t// @schedule(\"0 3 * * *\")\n\tPruneReports struct{}",
			"PruneReportsStarted struct {",
			"func (m *PruneReports) Execute(ctx context.Context, _ resource.ReadWriteTransaction, c *Client) (*PruneReportsStarted, error)",
			"c.Jobs().Start(ctx, appjobs.PruneReportsCommand)",
		},
		"pkg/rpc/rpc.go":             {"func (c *Client) Jobs() jobs.Starter"},
		"pkg/jobs/prune_reports.go":  {"the argument the scheduled PruneReports method starts the job with"},
		"test/authz/harness_test.go": {"func (c *testConfigurer) Jobs() jobs.Starter {\n\treturn jobs.NewFake()\n}"},
		".envrc.template":            {"# export APP_JOBS_TEMPLATE=\n"},
	}
	for rel, wants := range wantContains {
		text := read(t, a, rel)
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Errorf("%s lacks %q:\n%s", rel, want, text)
			}
		}
	}
	if text := read(t, a, "test/integration/harness_test.go"); strings.Contains(text, "filestore") {
		t.Errorf("a job's wiring gave the harness the file store:\n%s", text)
	}
}

func TestJobApplyAfterFiles(t *testing.T) {
	t.Parallel()

	// The file store first: its cleanup wrote the root and the whole wiring. A scheduled
	// job then joins the root and the rpc package, and the wiring is found in place.
	a := beacon(t, skeletonFiles(t))
	if _, err := (Files{}).Apply(t.Context(), a, &fakeExec{}); err != nil {
		t.Fatalf("Files.Apply() error = %v", err)
	}
	// The tidy the cleanup ran left cobra a direct requirement.
	if err := os.WriteFile(a.Abs("go.mod"), []byte("module example.com/acme/beacon\n\ngo 1.26\n\nrequire github.com/spf13/cobra v1.10.2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a = rediscovered(t, a)
	exec := &fakeExec{}
	change, err := Job{Name: "prune-reports", Schedule: "0 3 * * *"}.Apply(t.Context(), a, exec)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	wantDid := []string{
		"pkg/jobs/prune_reports.go: PruneReportsCommand (prune-reports) and PruneReports, the work, empty until it is written",
		"cmd/jobs/prunereports/prunereports.go: the prune-reports command, Run over the data level",
		"cmd/jobs/main.go: prunereports.Command(data) registered on the root",
		`pkg/rpc/prune_reports.go: PruneReports (@rpc, @schedule("0 3 * * *")), which starts cmd/jobs prune-reports through the Client's Jobs()`,
		"pkg/config/site.go: SiteConfiguration gained nothing: the guard and the starter were wired already",
		"app/app.go: App exposed the guard and the starter already",
		"ran go generate ./..., which emitted the PruneReports handler, its scheduled route under /_scheduled, and the router's SchedulerAuth requirement",
	}
	if diff := cmp.Diff(wantDid, change.Did); diff != "" {
		t.Errorf("Did mismatch (-want +got):\n%s", diff)
	}
	if len(change.Skipped) != 0 {
		t.Errorf("Skipped = %q, want none: the cleanup's wiring carries the starter", change.Skipped)
	}
	if diff := cmp.Diff([]string{"go generate ./..."}, exec.calls); diff != "" {
		t.Errorf("exec calls mismatch (-want +got): cobra is required already:\n%s", diff)
	}
	root := read(t, a, "cmd/jobs/main.go")
	if !strings.Contains(root, "\troot.AddCommand(\n\t\tcleanupfiles.Command(data),\n\t\tprunereports.Command(data),\n\t)\n") {
		t.Errorf("the root after the cleanup and a job:\n%s", root)
	}
	for rel, once := range map[string]string{"app/app.go": "rpcClient   *rpc.Client", "pkg/rpc/rpc.go": "func (c *Client) Jobs() jobs.Starter", "test/authz/harness_test.go": "func (c *testConfigurer) Jobs() jobs.Starter"} {
		if text := read(t, a, rel); strings.Count(text, once) != 1 {
			t.Errorf("%s has %d of %q, want one:\n%s", rel, strings.Count(text, once), once, text)
		}
	}
	if work := read(t, a, "pkg/jobs/prune_reports.go"); strings.Contains(work, "// Package jobs") {
		t.Errorf("the job's file opens the package the cleanup opened:\n%s", work)
	}
}

func TestJobCommandAndMeaning(t *testing.T) {
	t.Parallel()

	if got := (Job{Name: "prune-reports"}).Command(); got != "impulse add job prune-reports" {
		t.Errorf("Command() = %q", got)
	}
	if got := (Job{Name: "prune-reports", Schedule: "0 3 * * *"}).Command(); got != `impulse add job prune-reports --schedule "0 3 * * *"` {
		t.Errorf("Command() = %q", got)
	}
	meaning := (Job{Name: "prune-reports"}).Meaning()
	for _, want := range []string{"cmd/jobs", "Left to wire, in this order:", "1. The work.", "PruneReports", "3. Starting it.", "c.Jobs().Start(ctx, appjobs.PruneReportsCommand)", "4. Deployment."} {
		if !strings.Contains(meaning, want) {
			t.Errorf("Meaning() lacks %q:\n%s", want, meaning)
		}
	}
	scheduled := (Job{Name: "files/cleanup", Schedule: "0 9 * * *"}).Meaning()
	for _, want := range []string{"3. The schedule.", `@schedule("0 9 * * *")`, "FilesCleanup"} {
		if !strings.Contains(scheduled, want) {
			t.Errorf("Meaning() with a schedule lacks %q:\n%s", want, scheduled)
		}
	}
	if (Job{Name: "Bad"}).Meaning() != "" {
		t.Error("Meaning() of a bad name is not empty")
	}
}

func TestAdmitInAllowList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		text      string
		want      string
		wantAdded bool
	}{
		{
			name:      "after the last entry",
			text:      "    depguard:\n      rules:\n        main:\n          allow:\n            - $gostd\n            - github.com/cccteam\n    exhaustive:\n",
			want:      "    depguard:\n      rules:\n        main:\n          allow:\n            - $gostd\n            - github.com/cccteam\n            - github.com/spf13/cobra\n    exhaustive:\n",
			wantAdded: true,
		},
		{name: "admitted already", text: "          allow:\n            - github.com/spf13/cobra\n", want: "          allow:\n            - github.com/spf13/cobra\n"},
		{name: "admitted by a prefix", text: "          allow:\n            - github.com/spf13\n", want: "          allow:\n            - github.com/spf13\n"},
		{name: "an empty list", text: "          allow:\n    exhaustive:\n", want: "          allow:\n            - github.com/spf13/cobra\n    exhaustive:\n", wantAdded: true},
		{name: "no list", text: "linters:\n  enable:\n    - depguard\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, added := admitInAllowList(tt.text, "github.com/spf13/cobra")
			if added != tt.wantAdded || got != tt.want {
				t.Errorf("admitInAllowList() = %q, %v; want %q, %v", got, added, tt.want, tt.wantAdded)
			}
		})
	}
}
