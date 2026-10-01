// render.go renders the organization's layers from the embedded templates.

package org

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/render"
)

//go:embed all:templates
var templates embed.FS

const (
	templateDir = "templates"
	templateExt = ".tmpl"
	tfvarsFile  = "terraform.tfvars"
	journalFile = "JOURNAL.md"
	ignoreFile  = ".gitignore"
	// modelLabelWidth is the width of the model's longest label key,
	// terraform_source_path, which every labels block aligns to.
	modelLabelWidth = len("terraform_source_path")
	// seedLabelCount and blockLineCount are the model's own lines in the seed's label
	// list and in a labels block (its four labels plus the braces).
	seedLabelCount = 4
	blockLineCount = 6
)

// Layers are the model's layers, in apply order.
var Layers = []string{"0-bootstrap", "1-org", "2-shr", "2-spn", "2-net", "2-env"}

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

// DeployIdentity is the application's deploy identity in the environment, as an IAM
// member; ApplyIdentity its apply identity. Both are 2-env's, named under the
// environment's project.
func (v *view) DeployIdentity(env, app string) string {
	return "serviceAccount:" + v.Prefix + "-" + env + "-gbl-" + app + "-deploy@" + v.Project(env) + ".iam.gserviceaccount.com"
}

func (v *view) ApplyIdentity(env, app string) string {
	return "serviceAccount:" + v.Prefix + "-" + env + "-gbl-" + app + "-tofu@" + v.Project(env) + ".iam.gserviceaccount.com"
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

// ContactDomainsList is the contact domains as an HCL list.
func (v *view) ContactDomainsList() string {
	if len(v.ContactDomains) == 0 {
		return `["@` + v.OrganizationDomain + `"]`
	}

	return `["` + strings.Join(v.ContactDomains, `", "`) + `"]`
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
