// render.go renders the organization's layers from the embedded templates.

package org

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/render"
	"github.com/cccteam/ccc/impulse/ci"
)

//go:embed all:templates
var templates embed.FS

const (
	templateDir = "templates"
	templateExt = ".tmpl"
	tfvarsFile  = "terraform.tfvars"
	journalFile = "JOURNAL.md"
	ignoreFile  = ".gitignore"
	// workflowDir holds the layers workflow, the one GitHub workflow of the repository:
	// rendered and owned like the layers' files, compared by org check.
	workflowDir = ".github/workflows"
	// workflowName is the workflow file's name under workflowDir.
	workflowName = "layers.yml"
	// mappedPurpose is the attribute the boot project's identity provider maps a
	// GitHub token's event and ref to: plan for a pull request into the default branch,
	// apply for a push to it or a run started from it, none for anything else. Each
	// identity's federation binding selects one value.
	mappedPurpose = "attribute.purpose"
	// modelLabelWidth is the width of the model's longest label key,
	// terraform_source_path, which every labels block aligns to.
	modelLabelWidth = len("terraform_source_path")
	// seedLabelCount and blockLineCount are the model's own lines in the seed's label
	// list and in a labels block (its four labels plus the braces).
	seedLabelCount = 4
	blockLineCount = 6
)

// The model's layers by directory.
const (
	bootstrapLayer = "0-bootstrap"
	orgLayer       = "1-org"
	shrLayer       = "2-shr"
	spnLayer       = "2-spn"
	netLayer       = "2-net"
	envLayer       = "2-env"
)

// Layers are the model's layers, in apply order.
var Layers = []string{bootstrapLayer, orgLayer, shrLayer, spnLayer, netLayer, envLayer}

// WorkflowFile is the layers workflow's path under the repository root.
const WorkflowFile = workflowDir + "/" + workflowName

// layerProjects is the project each single-run layer's identities live in, by layer: the
// boot project for the two boot layers (both identities are named after their layer, boot
// and org), the layer's own project for the shared ones. 2-env's are the environments'.
var layerProjects = map[string]struct{ name, project string }{
	bootstrapLayer: {name: bootProject, project: bootProject},
	orgLayer:       {name: "org", project: bootProject},
	shrLayer:       {name: shrProject, project: shrProject},
	spnLayer:       {name: spnProject, project: spnProject},
	netLayer:       {name: netProject, project: netProject},
}

// seeded are the files the tool writes once, by base name.
var seeded = map[string]bool{
	tfvarsFile:  true,
	journalFile: true,
	ignoreFile:  true,
}

// view is what the templates see: the placement and the phrases derived from it.
type view struct {
	*Placement
}

// Environments is the model's environments in promotion order, for the templates.
func (*view) Environments() []string {
	return Environments
}

// Production is the last environment: the one a hotfix is based on, and the one whose
// backups the other environments restore from.
func (*view) Production() string {
	return Environments[len(Environments)-1]
}

// DeployIdentity is the application's deploy identity in the environment, as an IAM
// member; ApplyIdentity its apply identity. Both are 2-env's, named under the
// environment's project.
func (v *view) DeployIdentity(env, app string) string {
	return "serviceAccount:" + v.Prefix + "-" + env + "-gbl-" + app + "-deploy@" + v.Project(env) + ".iam.gserviceaccount.com"
}

func (v *view) ApplyIdentity(env, app string) string {
	return "serviceAccount:" + v.Prefix + "-" + env + "-gbl-" + app + "-tofu@" + v.Project(env) + ".iam.gserviceaccount.com"
}

// PlanIdentity is the application's plan identity in the environment as a member,
// 2-env's reader for the pull-request build's plan of that environment.
func (v *view) PlanIdentity(env, app string) string {
	return "serviceAccount:" + v.Prefix + "-" + env + "-gbl-" + app + "-plan@" + v.Project(env) + ".iam.gserviceaccount.com"
}

// Backend is the application's backend service in the environment, as the load
// balancer names it; PullRequestBackend the wildcard one in the first environment.
func (v *view) Backend(env, app string) string {
	return "projects/" + v.Project(env) + "/global/backendServices/" + v.Prefix + "-" + env + "-gbl-" + app + "-backend"
}

func (v *view) PullRequestBackend() string {
	return "projects/" + v.Project(Environments[0]) + "/global/backendServices/" + v.Prefix + "-" + Environments[0] + "-gbl-pr-backend"
}

// Host is the application's hostname in the environment: <app>-<env> under the apps
// domain, and the bare <app> in production.
func (v *view) Host(env, app string) string {
	if env == Environments[len(Environments)-1] {
		return app + "." + v.AppsDomain
	}

	return app + "-" + env + "." + v.AppsDomain
}

// HostRow is one entry of 2-net's hosts map: the quoted hostname, padded so the
// entries' equals signs align the way tofu fmt writes them, and its backend service.
type HostRow struct {
	Key   string
	Value string
}

// HostRows is every hostname the load balancer serves with its backend service, one
// row per application and environment in that order and the pull-request wildcard
// last, the keys padded to the longest so the rendered map is tofu fmt clean.
func (v *view) HostRows() []HostRow {
	rows := make([]HostRow, 0, 2*len(v.Applications)*len(Environments)+1)
	for _, app := range v.Applications {
		for _, env := range Environments {
			rows = append(rows,
				HostRow{Key: `"` + v.Host(env, app) + `"`, Value: v.Backend(env, app)},
				HostRow{Key: `"` + v.NextHost(env, app) + `"`, Value: v.NextBackend(env, app)},
			)
		}
	}
	rows = append(rows, HostRow{Key: `"*.` + v.AppsDomain + `"`, Value: v.PullRequestBackend()})

	width := 0
	for _, row := range rows {
		width = max(width, len(row.Key))
	}
	for i := range rows {
		rows[i].Key += strings.Repeat(" ", width-len(rows[i].Key))
	}

	return rows
}

// NextHost is the hostname of the application's next revision in the environment, the
// one the pipeline checks before traffic moves: the hostname's first label with -next.
func (v *view) NextHost(env, app string) string {
	host := v.Host(env, app)
	label, rest, _ := strings.Cut(host, ".")

	return label + "-next." + rest
}

// NextBackend is the backend service serving the application's next revision in the
// environment (the revision tag next), as the application's stack names it.
func (v *view) NextBackend(env, app string) string {
	return "projects/" + v.Project(env) + "/global/backendServices/" + v.Prefix + "-" + env + "-gbl-" + app + "-next-backend"
}

// Prd is the production environment, the last.
func (*view) Prd() string {
	return Environments[len(Environments)-1]
}

// LayerRun is one run of the layers workflow: a layer, the environment it is applied in
// (2-env alone; empty for the rest), the state prefix its init names (2-env alone), and
// the identities the run signs in as: the apply identity on the default branch, the plan
// identity on a pull request. Name is the run as the workflow and its comments call it.
type LayerRun struct {
	Name        string
	Layer       string
	Environment string
	StatePrefix string
	Apply       string
	Plan        string
}

// LayerRuns is every run of the layers workflow in layer order: the five single-run
// layers, then 2-env once per environment in promotion order. An identity under a project
// the placement does not record carries REPLACEME, which the workflow refuses to run.
func (v *view) LayerRuns() []LayerRun {
	runs := make([]LayerRun, 0, len(Layers)-1+len(Environments))
	for _, layer := range Layers {
		if layer == envLayer {
			for _, env := range Environments {
				runs = append(runs, LayerRun{
					Name: layer + " " + env, Layer: layer, Environment: env, StatePrefix: layer + "/" + env,
					Apply: v.identityEmail(env, env, "tofu"), Plan: v.identityEmail(env, env, "plan"),
				})
			}

			continue
		}
		p := layerProjects[layer]
		runs = append(runs, LayerRun{
			Name: layer, Layer: layer,
			Apply: v.identityEmail(p.name, p.project, "tofu"), Plan: v.identityEmail(p.name, p.project, "plan"),
		})
	}

	return runs
}

// identityEmail is a layer identity's email: <prefix>-<name>-gbl-<suffix> in the project
// under the key, as 0-bootstrap and 1-org name them.
func (v *view) identityEmail(name, projectKey, suffix string) string {
	return v.Prefix + "-" + name + "-gbl-" + suffix + "@" + v.Project(projectKey) + ".iam.gserviceaccount.com"
}

// BootProject is the boot project's id, or its REPLACEME form until the seed's values
// are recorded.
func (v *view) BootProject() string {
	return v.Project(bootProject)
}

// WorkflowPool is the id of the boot project's workload identity pool for the layers
// workflow, as 0-bootstrap names it; WorkflowProvider the full name of its provider,
// which the workflow's sign-in step takes, or empty until the placement records the boot
// project's number.
func (v *view) WorkflowPool() string {
	return v.Prefix + "-boot-github"
}

func (v *view) WorkflowProvider() string {
	number, ok := v.ProjectNumbers[bootProject]
	if !ok {
		return ""
	}

	return "projects/" + number + "/locations/global/workloadIdentityPools/" + v.WorkflowPool() + "/providers/github"
}

// WorkflowPath is the workflow file's path, for the provider's condition and the READMEs.
func (*view) WorkflowPath() string {
	return WorkflowFile
}

// MappedPurpose is the provider attribute the federation bindings select on.
func (*view) MappedPurpose() string {
	return mappedPurpose
}

// InfrastructureRepository is this repository under its GitHub organization, the one
// repository whose tokens the boot project's provider trusts.
func (v *view) InfrastructureRepository() string {
	return v.GithubOrganization + "/" + v.SourceRepo
}

// LayerOrderProse spells the apply order: 0-bootstrap, 1-org, 2-shr, 2-spn and 2-net,
// then 2-env for tst, stg and prd.
func (*view) LayerOrderProse() string {
	return prose(Layers[:len(Layers)-1]) + ", then " + Layers[len(Layers)-1] + " for " + prose(Environments)
}

// SingleRunLayers are the layers applied once, in order: every one but 2-env.
func (*view) SingleRunLayers() []string {
	return Layers[:len(Layers)-1]
}

// Layers is the model's layers in apply order, for the templates.
func (*view) Layers() []string {
	return Layers
}

// WorkflowName is the workflow file's name, which the workflow names itself by when it
// asks GitHub for its earlier runs.
func (*view) WorkflowName() string {
	return workflowName
}

// FirstEnvironment is the first environment in promotion order, where the pull-request
// build runs and its trigger is named.
func (*view) FirstEnvironment() string {
	return Environments[0]
}

// ImpulseChecks is the job ids every application's CI workflow carries, as impulse
// renders the workflow and GitHub Actions reports its jobs (title, go, image, secrets,
// migrations), in the workflow's order: the check names the branch rulesets require
// beside bedrock check and the pull-request build, read from impulse so the names have
// one source. The browser jobs (angular-<workspace>, one per browser workspace) are not
// among them, since the placement does not carry each application's workspaces.
func (*view) ImpulseChecks() []string {
	return ci.FixedChecks
}

// ImpulseChecksProse is the same job ids as prose, for a .tf comment: title, go, image,
// secrets and migrations.
func (*view) ImpulseChecksProse() string {
	return prose(ci.FixedChecks)
}

// ImpulseChecksProseQuoted is the job ids as prose, backticked for a README: `title`,
// `go`, `image`, `secrets` and `migrations`.
func (*view) ImpulseChecksProseQuoted() string {
	return prose(backticked(ci.FixedChecks))
}

// BootstrapRoles are the roles the bootstrap administrator holds for the seed and the
// first applies, org preflight's table, which 0-bootstrap's README and the root README
// list, so the READMEs name what org preflight checks.
func (*view) BootstrapRoles() []BootstrapRole {
	return BootstrapRoles
}

// EnvironmentsList is the environments as an HCL list, and EnvironmentsProse the same
// as prose, backticked: `tst`, `stg` and `prd`.
func (*view) EnvironmentsList() string {
	return hclList(Environments)
}

func (*view) EnvironmentsProse() string {
	return prose(backticked(Environments))
}

// ApprovalEnvironmentsList is the approval environments as an HCL list, and
// ApprovalEnvironmentsProse the same as prose, backticked: `stg` and `prd`.
func (v *view) ApprovalEnvironmentsList() string {
	return hclList(v.ApprovalEnvironments())
}

func (v *view) ApprovalEnvironmentsProse() string {
	return prose(backticked(v.ApprovalEnvironments()))
}

// TeamGroupLines are the environments' team groups as the lines of an HCL map's body,
// one per environment in promotion order, indented for a variable's default.
func (v *view) TeamGroupLines() string {
	lines := make([]string, 0, len(Environments))
	for _, env := range Environments {
		lines = append(lines, fmt.Sprintf("    %s = %q", env, v.TeamGroupAddress(env)))
	}

	return strings.Join(lines, "\n")
}

// SharedInstanceEntitlementLines are the environments on the shared instance as the
// lines of an HCL map's body, each with its team group's address and whether a grant
// there waits for an approval, for 2-spn's Spanner entitlements.
func (v *view) SharedInstanceEntitlementLines() string {
	envs := v.SharedInstanceEnvironments()
	lines := make([]string, 0, len(envs))
	for _, env := range envs {
		lines = append(lines, fmt.Sprintf("    %s = { group = %q, approval = %t }", env, v.TeamGroupAddress(env), slices.Contains(v.ApprovalEnvironments(), env)))
	}

	return strings.Join(lines, "\n")
}

// SharedInstanceEnvironmentsProse is the environments on the shared instance as prose,
// backticked: `stg` and `prd`.
func (v *view) SharedInstanceEnvironmentsProse() string {
	return prose(backticked(v.SharedInstanceEnvironments()))
}

// EntitlementDefaultsProse spells the entitlements' default longest grants, for the
// READMEs: the secret operator an hour, ...
func (*view) EntitlementDefaultsProse() string {
	parts := make([]string, 0, len(Entitlements))
	for _, name := range Entitlements {
		parts = append(parts, entitlementWords[name]+" "+durationWords(entitlementDefaults[name]))
	}

	return prose(parts)
}

// entitlementWords names each entitlement the way the READMEs do.
var entitlementWords = map[string]string{
	EntitlementSecretOperator:     "the secret operator",
	EntitlementSpannerAdmin:       "the Spanner admin",
	EntitlementSpannerViewer:      "the Spanner viewer",
	EntitlementLayerAdministrator: "the layer administrator",
}

// durationWords spells a whole number of hours or minutes: an hour, two hours, 90
// minutes.
func durationWords(d time.Duration) string {
	if d%time.Hour == 0 {
		hours := int(d / time.Hour)
		if hours == 1 {
			return "an hour"
		}
		if word, ok := numberWords[hours]; ok {
			return word + " hours"
		}

		return strconv.Itoa(hours) + " hours"
	}

	return strconv.Itoa(int(d/time.Minute)) + " minutes"
}

// numberWords spell the small counts the prose uses.
var numberWords = map[int]string{2: "two", 3: "three", 4: "four", 5: "five", 6: "six", 7: "seven", 8: "eight"}

// hclList is the items as an HCL list of strings.
func hclList(items []string) string {
	return `["` + strings.Join(items, `", "`) + `"]`
}

// ApplicationsList is the applications as an HCL list.
func (v *view) ApplicationsList() string {
	if len(v.Applications) == 0 {
		return "[]"
	}

	return `["` + strings.Join(v.Applications, `", "`) + `"]`
}

// ExampleApp is the application the layers' examples name: the first one, or app.
func (v *view) ExampleApp() string {
	if len(v.Applications) == 0 {
		return "app"
	}

	return v.Applications[0]
}

// contactDomains are the domains Essential Contacts may belong to: the placement's, or
// the organization's own domain when it names none.
func (v *view) contactDomains() []string {
	if len(v.ContactDomains) == 0 {
		return []string{"@" + v.OrganizationDomain}
	}

	return v.ContactDomains
}

// ContactDomainsList is the contact domains as an HCL list.
func (v *view) ContactDomainsList() string {
	return hclList(v.contactDomains())
}

// ContactDomainsProse is the contact domains as the root README names them: contact
// domain `@example.com`, or contact domains `@example.com` and `@example.org`.
func (v *view) ContactDomainsProse() string {
	domains := v.contactDomains()
	noun := "contact domains "
	if len(domains) == 1 {
		noun = "contact domain "
	}

	return noun + prose(backticked(domains))
}

// ExtraLabels reports whether the placement adds labels of its own.
func (v *view) ExtraLabels() bool {
	return len(v.Labels) > 0
}

// ExtraLabelsProse spells the placement's labels as `key = "value"`, joined.
func (v *view) ExtraLabelsProse() string {
	parts := make([]string, 0, len(v.Labels))
	for _, k := range v.labelKeys() {
		parts = append(parts, "`"+k+` = "`+v.Labels[k]+`"`+"`")
	}

	return strings.Join(parts, ", ")
}

// ExtraLabelKeys spells the placement's label keys in backticks, joined.
func (v *view) ExtraLabelKeys() string {
	parts := make([]string, 0, len(v.Labels))
	for _, k := range v.labelKeys() {
		parts = append(parts, "`"+k+"`")
	}

	return strings.Join(parts, ", ")
}

// SeedLabels is the label list the seed step's gcloud commands pass.
func (v *view) SeedLabels() string {
	keys := v.labelKeys()
	parts := make([]string, 0, seedLabelCount+len(keys))
	parts = append(parts, "terraform=true", "terraform_source_path=0-bootstrap", "source_repo="+v.SourceRepo, "environment=boot")
	for _, k := range keys {
		parts = append(parts, k+"="+v.Labels[k])
	}

	return strings.Join(parts, ",")
}

// LabelsBlock is a layer's labels block, the environment given as an expression.
func (v *view) LabelsBlock(layer, environment string) string {
	keys := v.labelKeys()
	width := modelLabelWidth
	for _, k := range keys {
		width = max(width, len(k))
	}
	lines := make([]string, 0, blockLineCount+len(keys))
	lines = append(lines,
		"  labels = {",
		fmt.Sprintf("    %-*s = %q", width, "terraform", "true"),
		fmt.Sprintf("    %-*s = %q", width, "terraform_source_path", layer),
		fmt.Sprintf("    %-*s = %q", width, "source_repo", v.SourceRepo),
		fmt.Sprintf("    %-*s = %s", width, "environment", environment),
	)
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("    %-*s = %q", width, k, v.Labels[k]))
	}
	lines = append(lines, "  }")

	return strings.Join(lines, "\n")
}

// LabelsBlockQuoted is a layer's labels block, the environment given as a value.
func (v *view) LabelsBlockQuoted(layer, environment string) string {
	return v.LabelsBlock(layer, fmt.Sprintf("%q", environment))
}

// prose lists the items the way a sentence does: a, b and c. One item is itself; none
// is empty.
func prose(items []string) string {
	if len(items) <= 1 {
		return strings.Join(items, "")
	}

	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

// backticked wraps each item in backticks, for Markdown.
func backticked(items []string) []string {
	quoted := make([]string, 0, len(items))
	for _, item := range items {
		quoted = append(quoted, "`"+item+"`")
	}

	return quoted
}

// Render renders every file of the organization's repository: the layers' owned files,
// each layer's seeded values, and the root files.
func Render(p *Placement) ([]render.File, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	v := &view{Placement: p}
	var files []render.File
	root, err := renderDir(templateDir, "", v)
	if err != nil {
		return nil, err
	}
	files = append(files, root...)
	workflow, err := renderDir(templateDir+"/"+workflowDir, workflowDir+"/", v)
	if err != nil {
		return nil, err
	}
	files = append(files, workflow...)
	for _, layer := range Layers {
		rendered, err := renderDir(templateDir+"/"+layer, layer+"/", v)
		if err != nil {
			return nil, err
		}
		files = append(files, rendered...)
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})

	return files, nil
}

// renderDir renders the templates of one embedded directory; prefix is the path the
// files take under the repository root.
func renderDir(dir, prefix string, v *view) ([]render.File, error) {
	entries, err := fs.ReadDir(templates, dir)
	if err != nil {
		return nil, errors.Wrapf(err, "fs.ReadDir(): %s", dir)
	}
	files := make([]render.File, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), templateExt) {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), templateExt)
		src, err := templates.ReadFile(dir + "/" + entry.Name())
		if err != nil {
			return nil, errors.Wrapf(err, "embed.FS.ReadFile(): %s", entry.Name())
		}
		t, err := template.New(entry.Name()).Option("missingkey=error").Parse(string(src))
		if err != nil {
			return nil, errors.Wrapf(err, "template.Parse(): %s", entry.Name())
		}
		var buf bytes.Buffer
		if err := t.Execute(&buf, v); err != nil {
			return nil, errors.Wrapf(err, "template.Execute(): %s%s", prefix, name)
		}
		tier := render.Owned
		if seeded[name] {
			tier = render.Seeded
		}
		files = append(files, render.File{Path: prefix + name, Tier: tier, Content: render.Formatted(name, buf.Bytes())})
	}

	return files, nil
}

// Write puts the files into the repository directory, creating it and the layer
// directories when absent: owned files always, seeded files only when absent.
func Write(files []render.File, dir string) (*render.Written, error) {
	for _, layer := range Layers {
		if err := os.MkdirAll(filepath.Join(dir, layer), 0o750); err != nil {
			return nil, errors.Wrapf(err, "os.MkdirAll(): %s", layer)
		}
	}

	return render.Write(files, dir, dir)
}
