package org

import (
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

// TestRenderedOrganizationParsesAsHCL parses every rendered .tf file of the organization's layers under
// testdata, so a template whose rendering is not valid HCL fails here and not in an apply.
func TestRenderedOrganizationParsesAsHCL(t *testing.T) {
	t.Parallel()

	tests := make([]struct {
		name string
		dir  string
	}, 0, len(Layers))
	for _, layer := range Layers {
		tests = append(tests, struct {
			name string
			dir  string
		}{name: layer, dir: filepath.Join("testdata", "imp", layer)})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var files []string
			err := filepath.WalkDir(tt.dir, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() && strings.HasSuffix(path, ".tf") {
					files = append(files, path)
				}

				return nil
			})
			if err != nil {
				t.Fatalf("walk %s: %v", tt.dir, err)
			}
			if len(files) == 0 {
				t.Fatalf("no .tf files under %s", tt.dir)
			}
			parser := hclparse.NewParser()
			for _, file := range files {
				if _, diags := parser.ParseHCLFile(file); diags.HasErrors() {
					t.Errorf("%s: %s", file, diags.Error())
				}
			}
		})
	}
}

// TestZonePreventDestroy holds 2-net's zones to prevent_destroy: Cloud DNS assigns a zone
// its name servers when it creates the zone, and a zone made again can land on a different
// set, so the apps zone and every parked zone a registration points at refuse a recreation
// unless the rule is removed first. The registration keeps ignoring its DNS and contact
// settings, which the provider cannot change in place, and the README walks the
// recreation's steps and the step in Cloud Domains that points a registration at the zone.
func TestZonePreventDestroy(t *testing.T) {
	t.Parallel()

	files, err := Render(testPlacement(t))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	byPath := map[string][]byte{}
	for _, f := range files {
		byPath[f.Path] = f.Content
	}
	tests := []struct {
		name    string
		path    string
		address string
		// wantIgnored are the attributes the resource's ignore_changes lists; none means
		// it has no ignore_changes.
		wantIgnored []string
	}{
		{name: "the apps zone", path: "2-net/dns.tf", address: "google_dns_managed_zone.apps"},
		{name: "every parked zone", path: "2-net/domains.tf", address: "google_dns_managed_zone.parked"},
		{
			name: "the registration, still ignoring the settings the provider cannot change in place", path: "2-net/domains.tf",
			address: "google_clouddomains_registration.this", wantIgnored: []string{"contact_settings", "dns_settings"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			content, ok := byPath[tt.path]
			if !ok {
				t.Fatalf("%s is not rendered", tt.path)
			}
			f, diags := hclparse.NewParser().ParseHCL(content, tt.path)
			if diags.HasErrors() {
				t.Fatalf("%s: %s", tt.path, diags.Error())
			}
			body, ok := f.Body.(*hclsyntax.Body)
			if !ok {
				t.Fatalf("%s: not native syntax", tt.path)
			}
			lifecycle := lifecycleOf(t, body, tt.address)
			preventDestroy, ok := lifecycle.Attributes["prevent_destroy"]
			if !ok {
				t.Fatalf("%s has no prevent_destroy", tt.address)
			}
			if v, diags := preventDestroy.Expr.Value(nil); diags.HasErrors() || !v.True() {
				t.Errorf("%s: prevent_destroy is not true", tt.address)
			}
			var ignored []string
			if attr, ok := lifecycle.Attributes["ignore_changes"]; ok {
				exprs, diags := hcl.ExprList(attr.Expr)
				if diags.HasErrors() {
					t.Fatalf("%s: ignore_changes is not a list", tt.address)
				}
				for _, expr := range exprs {
					traversal, diags := hcl.AbsTraversalForExpr(expr)
					if diags.HasErrors() {
						t.Fatalf("%s: ignore_changes holds an entry that is not a name", tt.address)
					}
					ignored = append(ignored, traversal.RootName())
				}
			}
			if !slices.Equal(ignored, tt.wantIgnored) {
				t.Errorf("%s ignores %v, want %v", tt.address, ignored, tt.wantIgnored)
			}
		})
	}
	readme := string(byPath["2-net/README.md"])
	for _, want := range []string{
		"## Making a zone again",
		"1. In the infrastructure repository, a pull request that removes the\n   `prevent_destroy` rule",
		"- **The registration's name servers**",
		"- **The certificate's authorization record.**",
		"- **Any delegation a person made**",
		"`gcloud domains registrations configure dns\n   <domain> --cloud-dns-zone=<zone> --project=<network project>`",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("2-net/README.md lacks %q", want)
		}
	}
}

// lifecycleOf is the lifecycle block of the resource named type.name.
func lifecycleOf(t *testing.T, body *hclsyntax.Body, address string) *hclsyntax.Body {
	t.Helper()

	for _, b := range body.Blocks {
		if b.Type != "resource" || len(b.Labels) != 2 || b.Labels[0]+"."+b.Labels[1] != address {
			continue
		}
		for _, inner := range b.Body.Blocks {
			if inner.Type == "lifecycle" {
				return inner.Body
			}
		}
		t.Fatalf("%s has no lifecycle block", address)
	}
	t.Fatalf("no resource %s", address)

	return nil
}
