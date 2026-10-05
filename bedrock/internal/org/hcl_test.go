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

// TestZonePreventDestroy holds 2-net's zones to prevent_destroy unless the placement sets
// zoneReplacement: Cloud DNS assigns a zone its name servers when it creates the zone, and
// a zone made again can land on a different set, so the apps zone and every parked zone a
// registration points at refuse a recreation unless the value lifts the rule for one.
// The registration keeps its own rule and keeps ignoring its DNS and contact settings,
// which the provider cannot change in place, in both renders.
func TestZonePreventDestroy(t *testing.T) {
	t.Parallel()

	registration := "google_clouddomains_registration.this"
	tests := []struct {
		name            string
		zoneReplacement bool
		path            string
		address         string
		wantRule        bool
		// wantIgnored are the attributes the resource's ignore_changes lists; none means
		// it has no ignore_changes.
		wantIgnored []string
	}{
		{name: "the apps zone, by default", path: "2-net/dns.tf", address: "google_dns_managed_zone.apps", wantRule: true},
		{name: "every parked zone, by default", path: "2-net/domains.tf", address: "google_dns_managed_zone.parked", wantRule: true},
		{name: "the registration, by default", path: "2-net/domains.tf", address: registration, wantRule: true, wantIgnored: []string{"contact_settings", "dns_settings"}},
		{name: "the apps zone, zoneReplacement set", zoneReplacement: true, path: "2-net/dns.tf", address: "google_dns_managed_zone.apps"},
		{name: "every parked zone, zoneReplacement set", zoneReplacement: true, path: "2-net/domains.tf", address: "google_dns_managed_zone.parked"},
		{name: "the registration, zoneReplacement set", zoneReplacement: true, path: "2-net/domains.tf", address: registration, wantRule: true, wantIgnored: []string{"contact_settings", "dns_settings"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := testPlacement(t)
			p.ZoneReplacement = tt.zoneReplacement
			files, err := Render(p)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			var content []byte
			for _, f := range files {
				if f.Path == tt.path {
					content = f.Content
				}
			}
			if content == nil {
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
			lifecycle := findLifecycle(t, body, tt.address)
			rule := false
			if lifecycle != nil {
				if attr, ok := lifecycle.Attributes["prevent_destroy"]; ok {
					v, diags := attr.Expr.Value(nil)
					rule = !diags.HasErrors() && v.True()
				}
			}
			if rule != tt.wantRule {
				t.Errorf("%s: prevent_destroy = %v, want %v", tt.address, rule, tt.wantRule)
			}
			var ignored []string
			if lifecycle != nil {
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
			}
			if !slices.Equal(ignored, tt.wantIgnored) {
				t.Errorf("%s ignores %v, want %v", tt.address, ignored, tt.wantIgnored)
			}
		})
	}
}

// TestMakingAZoneAgain holds 2-net's README to the recreation as three renders and one
// hand step: zoneReplacement set and rendered, the recreating change rendered, the
// re-pointing by hand that bedrock domain check prints, and the value cleared and
// rendered; with what a zone made again moves, and the step in Cloud Domains.
func TestMakingAZoneAgain(t *testing.T) {
	t.Parallel()

	readme := renderedFile(t, "2-net/README.md")
	_, section, found := strings.Cut(readme, "## Making a zone again")
	if !found {
		t.Fatal("2-net/README.md has no \"Making a zone again\"")
	}
	section, _, found = strings.Cut(section, "\n## A domain registered here")
	if !found {
		t.Fatal("2-net/README.md has no \"A domain registered here\" after \"Making a zone again\"")
	}
	tests := []struct {
		name string
		want string
	}{
		{name: "three renders and one hand step", want: "is a deliberate change of three renders and one hand step. The\nrule is lifted by a value in `placement.json`, `zoneReplacement`, never by\nediting the files, which are bedrock's"},
		{name: "the first render sets the value", want: "1. In the infrastructure repository: set `\"zoneReplacement\": true` in\n   `placement.json` and run `bedrock org render`."},
		{name: "the second render makes the recreating change", want: "2. In the infrastructure repository: make the change that recreates the\n   zone and run `bedrock org render`, the value still set"},
		{name: "the hand step re-points, as domain check prints", want: "3. By hand, where each step happens: point everything that pointed at the\n   old set at the new one (below). `bedrock domain check` prints each step"},
		{name: "the third render clears the value", want: "4. In the infrastructure repository: remove `zoneReplacement` from\n   `placement.json` (or set it to `false`) and run `bedrock org render`. The\n   rule is written back."},
		{name: "what a zone made again moves", want: "- **The registration's name servers**"},
		{name: "the certificate's record", want: "- **The certificate's authorization record.**"},
		{name: "a delegation a person made", want: "- **Any delegation a person made**"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if !strings.Contains(section, tt.want) {
				t.Errorf("2-net/README.md's \"Making a zone again\" lacks %q:\n%s", tt.want, section)
			}
		})
	}
	if strings.Contains(section, "removes the\n   `prevent_destroy` rule") {
		t.Error("2-net/README.md still has the rule removed by editing the files")
	}
	if want := "`gcloud domains registrations configure dns\n   <domain> --cloud-dns-zone=<zone> --project=<network project>`"; !strings.Contains(readme, want) {
		t.Errorf("2-net/README.md lacks %q", want)
	}
}

// findLifecycle is the lifecycle block of the resource named type.name, or nil when the
// resource has none.
func findLifecycle(t *testing.T, body *hclsyntax.Body, address string) *hclsyntax.Body {
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

		return nil
	}
	t.Fatalf("no resource %s", address)

	return nil
}
