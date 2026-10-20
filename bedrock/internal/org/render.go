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

	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/release"
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
	// StepsVersion names the shape of the triggers' inline steps this bedrock renders
	// (0-bootstrap/cloud-build.tf): the substitutions they carry and what each step
	// does. The steps also follow the pin's kind (a release pin renders FetchBedrock
	// alone, a commit pin adds BuildBedrock), so what both triggers pass as
	// _BEDROCK_STEPS is StepsShape, this version and the kind, and bedrock org plan and
	// apply compare it with their own, since a trigger's steps lag one apply behind the
	// repository: a change to them is rendered by the new bedrock, planned and applied
	// by a run on the old steps, and the next run is the first on the new ones. It
	// changes when the steps change in a way the commands have to know about.
	StepsVersion = "2"
	// modelLabelWidth is the width of the model's longest label key,
	// terraform_source_path, which every labels block aligns to.
	modelLabelWidth = len("terraform_source_path")
	// seedLabelCount and blockLineCount are the model's own lines in the seed's label
	// list and in a labels block (its four labels plus the braces).
	seedLabelCount = 5
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

// Layers are the model's layers, in apply order: 2-env before 2-net, since 2-net's URL
// map names tst's pull-request backend, which 2-env tst makes, while nothing in 2-env
// waits on 2-net (its two load balancer principals are derived from 1-org until 2-net
// exists, and the apps domain is the placement's).
var Layers = []string{bootstrapLayer, orgLayer, shrLayer, spnLayer, envLayer, netLayer}

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

// FirebaseBrowserKey is the display name of the API key Firebase creates when Identity
// Platform is initialized, which bedrock org apply restricts after each apply of 2-env.
func (*view) FirebaseBrowserKey() string {
	return FirebaseBrowserKey
}

// SignInAPITargets is the API restriction bedrock org apply sets on Firebase's browser
// key, as gcloud services api-keys update takes it for the same restriction by hand: one
// --api-target per sign-in API.
func (*view) SignInAPITargets() string {
	targets := make([]string, 0, len(SignInAPIs))
	for _, api := range SignInAPIs {
		targets = append(targets, "--api-target=service="+api)
	}

	return strings.Join(targets, " ")
}

// SignInAPIsProse names the sign-in APIs, joined with "and".
func (*view) SignInAPIsProse() string {
	return prose(SignInAPIs)
}

// SignInAPIsCode names the sign-in APIs as SignInAPIsProse does, each in backticks, for
// Markdown.
func (*view) SignInAPIsCode() string {
	return prose(backticked(SignInAPIs))
}

// Production is the last environment: the one a patch release is based on, and the one whose
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

// PullRequestBackend is the wildcard backend in the first environment, which serves
// every pull-request stack's services by the hostname's first label.
func (v *view) PullRequestBackend() string {
	return "projects/" + v.Project(Environments[0]) + "/global/backendServices/" + v.Prefix + "-" + Environments[0] + "-gbl-pr-backend"
}

// HostRow is one entry of 2-net's hosts map: the quoted hostname, padded so the
// entries' equals signs align the way tofu fmt writes them, and its backend service.
type HostRow struct {
	Key   string
	Value string
}

// HostRows is every hostname the load balancer serves with its backend service, one
// row per application and environment in that order (the recorded entries in name
// order, else the default rule's hostname and its -next twin), an environment out of
// service (Placement.OutOfService) left out, and the pull-request wildcard last, the
// keys padded to the longest so the rendered map is tofu fmt clean.
func (v *view) HostRows() []HostRow {
	served := v.Served()
	rows := make([]HostRow, 0, 2*len(served)*len(Environments)+1)
	for _, app := range served {
		for _, env := range Environments {
			if !v.Routed(app, env) {
				continue
			}
			hosts := v.ApplicationHosts(app, env)
			for _, host := range SortedHosts(hosts) {
				rows = append(rows, HostRow{Key: `"` + host + `"`, Value: hosts[host]})
			}
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

// Prd is the production environment, the last.
func (*view) Prd() string {
	return Environments[len(Environments)-1]
}

// InfrastructureRepository is this repository under its GitHub organization, the one
// repository the boot project's connection links and the triggers watch.
func (v *view) InfrastructureRepository() string {
	return v.GithubOrganization + "/" + v.SourceRepo
}

// StepsShape is what the triggers pass as _BEDROCK_STEPS: StepsVersion and the kind of
// pin the steps were rendered for (2-release, 2-commit), since a release pin renders
// the fetch step alone and a commit pin the fetch and build steps.
func StepsShape(p *Placement) string {
	if release.IsCommitPin(p.BedrockVersion) {
		return StepsVersion + "-commit"
	}

	return StepsVersion + "-release"
}

// StepsVersion is the shape of the triggers' steps, passed as _BEDROCK_STEPS.
func (v *view) StepsVersion() string {
	return StepsShape(v.Placement)
}

// CommitPin reports a commit pin, which the triggers' second step builds with go
// install; a release pin renders no such step, since its binary is downloaded whole.
func (v *view) CommitPin() bool {
	return release.IsCommitPin(v.BedrockVersion)
}

// MarkerPrefix is where bedrock org apply keeps its markers in the state bucket.
func (*view) MarkerPrefix() string {
	return MarkerPrefix
}

// CloudBuildInstallationIDHCL is the installation id as an HCL value: the number, or
// null until the authorization is recorded. CloudBuildTokenSecretVersionHCL is the token
// secret version's resource name, quoted, or null.
func (v *view) CloudBuildInstallationIDHCL() string {
	if v.CloudBuildInstallationID == "" {
		return hclNull
	}

	return v.CloudBuildInstallationID
}

func (v *view) CloudBuildTokenSecretVersionHCL() string {
	return hclStringOrNull(v.CloudBuildTokenSecretVersion)
}

// GithubDeployerAppIDHCL is the deployer app's App ID as an HCL value, or null until the
// app is recorded; DeployerKeyVersionNameHCL the pinned version of its key by resource
// name, quoted, or null until one is pinned.
func (v *view) GithubDeployerAppIDHCL() string {
	if v.GithubDeployerAppID == "" {
		return hclNull
	}

	return v.GithubDeployerAppID
}

func (v *view) DeployerKeyVersionNameHCL() string {
	return hclStringOrNull(v.DeployerKeyVersionName())
}

// ReleaseKeyVersionNameHCL is the pinned version of the release app's key by resource
// name, quoted, or null until one is pinned.
func (v *view) ReleaseKeyVersionNameHCL() string {
	return hclStringOrNull(v.ReleaseKeyVersionName())
}

// hclNull is HCL's null, the value of what the placement does not record yet.
const hclNull = "null"

// hclStringOrNull is the value as a quoted HCL string, or null when empty.
func hclStringOrNull(value string) string {
	if value == "" {
		return hclNull
	}

	return strconv.Quote(value)
}

// EnvironmentProjectNumbersHCL is the environment projects' numbers the placement
// records as an HCL map by environment, {} when it records none: 0-bootstrap grants
// their Cloud Build service agents the read of the connections' token secret.
func (v *view) EnvironmentProjectNumbersHCL() string {
	var lines []string
	for _, env := range Environments {
		if number, ok := v.ProjectNumbers[env]; ok {
			lines = append(lines, fmt.Sprintf("    %s = %q", env, number))
		}
	}
	if len(lines) == 0 {
		return emptyHCLMap
	}

	return "{\n" + strings.Join(lines, "\n") + "\n  }"
}

// BedrockModule is the module go install builds for a commit pin.
func (*view) BedrockModule() string {
	return release.Module
}

// The images the triggers' steps run in, the application pipeline's: the gcloud image
// (python3 and curl, for the fetch), the Go image a commit pin is built in, pinned by
// digest, and the OpenTofu image the check and the commands run in.
func (*view) GcloudImage() string {
	return gcloudImage
}

func (*view) GoImage() string {
	return goImage
}

func (*view) OpenTofuImage() string {
	return openTofuImage
}

// GoInstallEnvHCL is the environment go install builds a commit pin in, the application
// pipeline's, as an HCL list of KEY=value strings.
func (*view) GoInstallEnvHCL() string {
	return hclList(append([]string{"GOBIN=" + bedrockHome}, goInstallEnv...))
}

// BedrockRepository is where the first step clones the repository's branches from, to
// check a commit pin against.
func (*view) BedrockRepository() string {
	return release.CloneURL()
}

// FetchBedrockScript is the first step's script, which reads the pin off placement.json
// in the checkout and checks a commit pin against the repository's branches;
// BuildBedrockScript the second's, which builds a commit pin.
func (*view) FetchBedrockScript() string {
	return stepScript(fetchBedrockScript)
}

func (*view) BuildBedrockScript() string {
	return stepScript(buildBedrockScript)
}

// BedrockPath is where the steps leave the fetched bedrock, and the entrypoint of the
// steps that run it.
func (*view) BedrockPath() string {
	return bedrockPath
}

// The steps' timeouts, in seconds: the fetch (a release's download, three tries), the
// build of a commit pin, the check, a plan of every layer side by side, and the apply of
// every layer in order; and the whole build's, which holds the steps.
func (*view) FetchSeconds() int {
	return fetchSeconds
}

func (*view) BuildSeconds() int {
	return buildSeconds
}

func (*view) CheckSeconds() int {
	return checkSeconds
}

func (*view) PlanSeconds() int {
	return planSeconds
}

func (*view) ApplySeconds() int {
	return applySeconds
}

func (*view) PlanBuildSeconds() int {
	return fetchSeconds + buildSeconds + checkSeconds + planSeconds
}

func (*view) ApplyBuildSeconds() int {
	return fetchSeconds + buildSeconds + checkSeconds + applySeconds
}

// LayerOrderProse spells the apply order: 0-bootstrap, 1-org, 2-shr and 2-spn, then
// 2-env for tst, stg and prd, then 2-net.
func (*view) LayerOrderProse() string {
	i := slices.Index(Layers, envLayer)
	text := prose(Layers[:i]) + ", then " + envLayer + " for " + prose(Environments)
	if after := Layers[i+1:]; len(after) > 0 {
		text += ", then " + prose(after)
	}

	return text
}

// SingleRunLayers are the layers applied once, in order: every one but 2-env.
func (*view) SingleRunLayers() []string {
	return slices.DeleteFunc(slices.Clone(Layers), func(layer string) bool { return layer == envLayer })
}

// Layers is the model's layers in apply order, for the templates.
func (*view) Layers() []string {
	return Layers
}

// FirstEnvironment is the first environment in promotion order, where the pull-request
// build runs and its trigger is named.
func (*view) FirstEnvironment() string {
	return Environments[0]
}

// ImpulseChecks is the job ids every application's CI workflow carries, as impulse
// renders the workflow and GitHub Actions reports its jobs (title, go, web, image,
// secrets, migrations), in the workflow's order: the check names the branch rulesets
// require beside bedrock check, read from impulse so the names have one source. The
// browser jobs (angular-<workspace>, one per browser workspace) are not among them,
// since they are named per application; web, the gate over them, is.
func (*view) ImpulseChecks() []string {
	return ci.FixedChecks
}

// ImpulseChecksProse is the same job ids as prose, for a .tf comment: title, go, web,
// image, secrets and migrations.
func (*view) ImpulseChecksProse() string {
	return prose(ci.FixedChecks)
}

// ApprovingReviewsProse spells the approving reviews a pull request needs before it
// merges, for the READMEs: one approving review, two approving reviews, no approving
// review.
func (v *view) ApprovingReviewsProse() string {
	count := v.ApprovingReviewCount()
	switch count {
	case 0:
		return "no approving review"
	case 1:
		return "one approving review"
	}
	if word, ok := numberWords[count]; ok {
		return word + " approving reviews"
	}

	return strconv.Itoa(count) + " approving reviews"
}

// ImpulseChecksProseQuoted is the job ids as prose, backticked for a README: `title`,
// `go`, `web`, `image`, `secrets` and `migrations`.
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
	return v.entitlementLines(v.SharedInstanceEnvironments())
}

// EnvironmentEntitlementLines are every environment, in promotion order, as the same
// lines, for 1-org's layer state entitlements on the boot project.
func (v *view) EnvironmentEntitlementLines() string {
	return v.entitlementLines(Environments)
}

// entitlementLines are the environments as the lines of an HCL map's body, each with
// its team group's address and whether a grant there waits for an approval.
func (v *view) entitlementLines(envs []string) string {
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

// emptyHCLMap is an HCL map with nothing in it, and emptyHCLList a list.
const (
	emptyHCLMap  = "{}"
	emptyHCLList = "[]"
)

// ApplicationsList is the applications' codes as an HCL list.
func (v *view) ApplicationsList() string {
	if len(v.Applications) == 0 {
		return emptyHCLList
	}

	return `["` + strings.Join(v.Codes(), `", "`) + `"]`
}

// ApplicationRepositoriesHCL is the applications' repository names by code as an HCL
// map, the values in one column; {} with no application.
func (v *view) ApplicationRepositoriesHCL() string {
	if len(v.Applications) == 0 {
		return emptyHCLMap
	}
	width := 0
	for _, a := range v.Applications {
		width = max(width, len(a.Code))
	}
	var b strings.Builder
	b.WriteString("{\n")
	for _, a := range v.Applications {
		fmt.Fprintf(&b, "  %-*s = %q\n", width, a.Code, a.Repository)
	}
	b.WriteString("}")

	return b.String()
}

// ApplicationCodePattern is the one rule for an application code, for the layers'
// validations (derive.ApplicationCodePattern).
func (*view) ApplicationCodePattern() string {
	return derive.ApplicationCodePattern
}

// AppliedList is the applications registered in full (Served) as an HCL list: those whose
// stacks have had their first apply, so their repositories' rulesets may require the
// stack's check trigger.
func (v *view) AppliedList() string {
	served := v.Served()
	if len(served) == 0 {
		return emptyHCLList
	}

	return `["` + strings.Join(served, `", "`) + `"]`
}

// ExampleApp is the application the layers' examples name: the first one, or app.
func (v *view) ExampleApp() string {
	if len(v.Applications) == 0 {
		return "app"
	}

	return v.Applications[0].Code
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

// VanityDomainsList is the vanity domains as an HCL list, empty when none is declared.
func (v *view) VanityDomainsList() string {
	if len(v.VanityDomains) == 0 {
		return emptyHCLList
	}

	return hclList(v.VanityDomains)
}

// VanityDomainsProse names the vanity domains for a README, each in backticks, joined
// with "and"; empty when none is declared.
func (v *view) VanityDomainsProse() string {
	return prose(backticked(v.VanityDomains))
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
	parts = append(parts, "terraform=true", "terraform_source_path=0-bootstrap", "source_repo="+v.SourceRepo, "organization="+v.Prefix, "environment=boot")
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
		fmt.Sprintf("    %-*s = %q", width, "organization", v.Prefix),
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

// Written is what Write did: the files written.
type Written struct {
	*render.Written
}

// Write puts the files into the repository directory, creating it and the layer
// directories when absent: owned files always, seeded files only when absent.
func Write(files []render.File, dir string) (*Written, error) {
	for _, layer := range Layers {
		if err := os.MkdirAll(filepath.Join(dir, layer), 0o750); err != nil {
			return nil, errors.Wrapf(err, "os.MkdirAll(): %s", layer)
		}
	}
	written, err := render.Write(files, dir, dir)
	if err != nil {
		return nil, err
	}

	return &Written{Written: written}, nil
}
