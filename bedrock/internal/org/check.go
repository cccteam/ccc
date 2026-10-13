// check.go compares a committed organization repository with what the placement
// renders: the drift between the placement and the layers, the files a layer holds that
// no render produces, and the files an earlier bedrock rendered and this one retired.

package org

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/go-playground/errors/v5"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"

	"github.com/cccteam/ccc/bedrock/internal/render"
)

// Finding is one file that is not as the render says it should be: an owned file that
// differs or is missing, a file in a layer that no render produces, a retired file still
// there, or a value a person's file carries that the placement carries now.
type Finding struct {
	// Path is the file's path relative to the repository root.
	Path string
	// Missing reports a file the render produces that the repository lacks.
	Missing bool
	// Line is the first differing line (1-based), 0 when Missing or when Reason says
	// what is wrong.
	Line int
	// Want and Got are that line as rendered and as committed.
	Want string
	Got  string
	// Reason, when set, says why the file is refused: a .tf file no render produces, an
	// override file, a retired file, or a value the placement carries now.
	Reason string
}

// Report is the outcome of one check.
type Report struct {
	// Dir is the repository directory checked.
	Dir string
	// Checked counts the owned files compared.
	Checked int
	// Findings are the owned files that differ or are missing, in path order, then the
	// files refused with a reason.
	Findings []Finding
	// Unseeded lists the seeded files the repository lacks: not drift, since the tool
	// writes them once and a person keeps them, but worth a line.
	Unseeded []string
}

// Clean reports no drift and nothing refused.
func (r *Report) Clean() bool {
	return len(r.Findings) == 0
}

// The files a person owns beside the rendered ones in a layer: the values, the provider
// lock, and the values bedrock itself renders (*.auto.tfvars are owned, compared above).
var personOwned = []string{tfvarsFile, ".terraform.lock.hcl"}

// retiredValues are the attributes of 2-env's values that placement.json carries now,
// each with the placement key that carries it: a values file that still sets one is
// reported.
var retiredValues = []struct {
	attribute string
	key       string
}{
	{attribute: "github_app_installation_id", key: "cloudBuildInstallationId"},
	{attribute: "github_oauth_token_secret_version", key: "cloudBuildTokenSecretVersion"},
	{attribute: "github_deployer_app_id", key: "githubDeployerAppId"},
	{attribute: "github_deployer_key_secret_versions", key: "githubDeployerKeyVersion"},
}

// Check renders the placement and compares the owned files with the repository's; then
// it refuses, in each layer, every .tf file the render does not produce and every
// override file (OpenTofu merges an override.tf or *_override.tf into the layer, so a
// pull request could change what the layers declare without touching a rendered file),
// the retired files an earlier bedrock rendered, and a value in 2-env's terraform.tfvars
// that placement.json carries now.
func Check(p *Placement, dir string) (*Report, error) {
	files, err := Render(p)
	if err != nil {
		return nil, err
	}
	r := &Report{Dir: dir}
	rendered := map[string]bool{}
	for _, f := range files {
		rendered[f.Path] = true
		committed, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f.Path)))
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
			r.Findings = append(r.Findings, Finding{Path: f.Path, Missing: true})

			continue
		}
		if line, want, got, same := firstDifference(f.Content, committed); !same {
			r.Findings = append(r.Findings, Finding{Path: f.Path, Line: line, Want: want, Got: got})
		}
	}
	unknown, err := unknownFiles(dir, rendered)
	if err != nil {
		return nil, err
	}
	r.Findings = append(r.Findings, unknown...)
	retired, err := RetiredFiles(dir)
	if err != nil {
		return nil, err
	}
	for _, path := range retired {
		r.Findings = append(r.Findings, Finding{Path: path, Reason: retiredReason(path)})
	}
	values, err := retiredValueFindings(dir)
	if err != nil {
		return nil, err
	}
	r.Findings = append(r.Findings, values...)

	return r, nil
}

// retiredReason says what a retired file was and what replaced it.
func retiredReason(path string) string {
	if path == RetiredWorkflowFile {
		return "the layers workflow is retired: the Cloud Build triggers 0-bootstrap renders plan and apply the layers (0-bootstrap/README.md, \"The Cloud Build triggers\"); bedrock org render removes it"
	}

	return "retired: 1-org/runners.tf carries its resources now, and a .tf file left beside it declares them twice; bedrock org render removes it"
}

// unknownFiles lists, in each layer, every .tf and .tf.json file the render does not
// produce and every override file, each with the reason it is refused, in path order.
func unknownFiles(dir string, rendered map[string]bool) ([]Finding, error) {
	var findings []Finding
	for _, layer := range Layers {
		entries, err := os.ReadDir(filepath.Join(dir, layer))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}

			return nil, errors.Wrapf(err, "os.ReadDir(): %s", layer)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			path := layer + "/" + entry.Name()
			if slices.Contains(retiredFiles, path) {
				continue
			}
			if reason := refusal(entry.Name(), rendered[path]); reason != "" {
				findings = append(findings, Finding{Path: path, Reason: reason})
			}
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		return findings[i].Path < findings[j].Path
	})

	return findings, nil
}

// refusal is why a file in a layer is refused, or empty when it is allowed: a rendered
// file, a person's values and lock file, and anything that is not OpenTofu
// configuration pass; an override file and a .tf or .tf.json file the render does not
// produce do not.
func refusal(name string, rendered bool) string {
	base := strings.TrimSuffix(name, ".json")
	switch {
	case base == "override.tf" || strings.HasSuffix(base, "_override.tf"):
		return "an override file: OpenTofu merges it into the layer, so it changes what the layer declares without a rendered file changing; the layers hold no override files"
	case rendered || slices.Contains(personOwned, name):
		return ""
	case strings.HasSuffix(base, ".tf"):
		return "not rendered by bedrock: every .tf file of a layer comes from the placement, and a file added by hand would run as the layer's identity; move what it declares into the placement, or remove it"
	}

	return ""
}

// retiredValueFindings reports each attribute of 2-env's terraform.tfvars that
// placement.json carries now, naming the key that carries it. A values file that is
// absent or does not parse is left to the layer's own init to report.
func retiredValueFindings(dir string) ([]Finding, error) {
	path := envLayer + "/" + tfvarsFile
	src, err := os.ReadFile(filepath.Join(dir, envLayer, tfvarsFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, errors.Wrapf(err, "os.ReadFile(): %s", path)
	}
	f, diags := hclsyntax.ParseConfig(src, path, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, nil
	}
	body, ok := f.Body.(*hclsyntax.Body)
	if !ok {
		return nil, nil
	}
	var findings []Finding
	for _, retired := range retiredValues {
		if _, set := body.Attributes[retired.attribute]; set {
			findings = append(findings, Finding{Path: path, Reason: "sets " + retired.attribute + ", which placement.json carries now as " + retired.key + ": record it there (bedrock org render reads it) and remove it here"})
		}
	}

	return findings, nil
}

// firstDifference finds the first line where the rendered and committed contents part.
func firstDifference(want, got []byte) (line int, wantLine, gotLine string, same bool) {
	if bytes.Equal(want, got) {
		return 0, "", "", true
	}
	wantLines := bytes.Split(want, []byte("\n"))
	gotLines := bytes.Split(got, []byte("\n"))
	for i := range max(len(wantLines), len(gotLines)) {
		var w, g []byte
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if !bytes.Equal(w, g) {
			return i + 1, string(w), string(g), false
		}
	}

	return 0, "", "", true
}
