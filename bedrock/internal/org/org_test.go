package org

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/render"
)

const fixture = "testdata/imp"

// testPlacement reads the fixture organization's placement.
func testPlacement(t *testing.T) *Placement {
	t.Helper()

	p, err := ReadPlacement(filepath.Join(fixture, "placement.json"))
	if err != nil {
		t.Fatalf("ReadPlacement() error = %v", err)
	}

	return p
}

func TestPlacementValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(p *Placement)
		wantErr string
	}{
		{name: "the fixture", mutate: func(*Placement) {}},
		{name: "a long prefix", mutate: func(p *Placement) { p.Prefix = "impulse" }, wantErr: "prefix"},
		{name: "no organization domain", mutate: func(p *Placement) { p.OrganizationDomain = " " }, wantErr: "organizationDomain is empty"},
		{name: "no billing account", mutate: func(p *Placement) { p.BillingAccount = "" }, wantErr: "billingAccount is empty"},
		{name: "one region", mutate: func(p *Placement) { p.Regions = p.Regions[:1] }, wantErr: "regions has 1"},
		{name: "a long region code", mutate: func(p *Placement) { p.Regions[0].Code = "central" }, wantErr: "three-character code"},
		{name: "a contact domain without @", mutate: func(p *Placement) { p.ContactDomains = []string{"example.com"} }, wantErr: "@<domain>"},
		{name: "a long application", mutate: func(p *Placement) { p.Applications = []string{"lighthouse"} }, wantErr: `application "lighthouse"`},
		{name: "a label without a value", mutate: func(p *Placement) { p.Labels = map[string]string{"team": ""} }, wantErr: "label"},
		{name: "no spanner config", mutate: func(p *Placement) { p.Spanner.Config = "" }, wantErr: "spanner.config is empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := testPlacement(t)
			tt.mutate(p)
			err := p.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() error = %v, wantErr %q", err, tt.wantErr)
			}
		})
	}
}

// TestRenderGolden compares every rendered file, owned and seeded alike, with the
// fixture organization's committed one, and checks nothing committed goes unrendered.
func TestRenderGolden(t *testing.T) {
	t.Parallel()

	files, err := Render(testPlacement(t))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	rendered := map[string]bool{}
	for _, f := range files {
		rendered[f.Path] = true
		want, err := os.ReadFile(filepath.Join(fixture, filepath.FromSlash(f.Path)))
		if err != nil {
			t.Errorf("%s: rendered but not in the fixture: %v", f.Path, err)

			continue
		}
		if line, w, g, same := firstDifference(want, f.Content); !same {
			t.Errorf("%s differs from the fixture at line %d:\n  want: %s\n  got:  %s", f.Path, line, w, g)
		}
	}
	err = filepath.WalkDir(fixture, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(fixture, path)
		if err != nil {
			t.Fatalf("filepath.Rel() error = %v", err)
		}
		rel = filepath.ToSlash(rel)
		if rel != "placement.json" && !rendered[rel] {
			t.Errorf("%s is in the fixture but not rendered", rel)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir() error = %v", err)
	}
}

func TestRenderTiers(t *testing.T) {
	t.Parallel()

	files, err := Render(testPlacement(t))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	tiers := map[string]render.Tier{}
	for _, f := range files {
		tiers[f.Path] = f.Tier
	}
	tests := []struct {
		name string
		path string
		want render.Tier
	}{
		{name: "a layer's file is owned", path: "1-org/projects.tf", want: render.Owned},
		{name: "a layer's README is owned", path: "2-env/README.md", want: render.Owned},
		{name: "the root README is owned", path: "README.md", want: render.Owned},
		{name: "the OpenTofu version is owned", path: ".opentofu-version", want: render.Owned},
		{name: "a layer's values are seeded", path: "2-net/terraform.tfvars", want: render.Seeded},
		{name: "a layer's application values are owned", path: "2-shr/applications.auto.tfvars", want: render.Owned},
		{name: "the journal is seeded", path: "JOURNAL.md", want: render.Seeded},
		{name: "the ignore rules are seeded", path: ".gitignore", want: render.Seeded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := tiers[tt.path]
			if !ok {
				t.Fatalf("%s is not rendered", tt.path)
			}
			if got != tt.want {
				t.Errorf("%s tier = %s, want %s", tt.path, got, tt.want)
			}
		})
	}
}

// TestDeployRecordsGrants pins the deploy identity's two grants on its own environment's
// records bucket: it creates records and reads them (the stale-database check of a
// pull-request build, the environment's live version), each for every application;
// neither role overwrites or deletes a record.
func TestDeployRecordsGrants(t *testing.T) {
	t.Parallel()

	files, err := Render(testPlacement(t))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	var identities string
	for _, f := range files {
		if f.Path == "2-env/identities.tf" {
			identities = string(f.Content)
		}
	}
	if identities == "" {
		t.Fatal("2-env/identities.tf is not rendered")
	}
	tests := []struct {
		name     string
		resource string
		role     string
	}{
		{name: "creates records", resource: "deploy_records", role: "roles/storage.objectCreator"},
		{name: "reads records", resource: "deploy_records_viewer", role: "roles/storage.objectViewer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			want := "resource \"google_storage_bucket_iam_member\" \"" + tt.resource + "\" {\n" +
				"  for_each = local.apps\n\n" +
				"  bucket = google_storage_bucket.records.name\n" +
				"  role   = \"" + tt.role + "\"\n" +
				"  member = google_service_account.deploy[each.key].member\n}\n"
			if !strings.Contains(identities, want) {
				t.Errorf("2-env/identities.tf lacks the grant:\n%s", want)
			}
		})
	}
}

func TestLabelsBlock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		labels map[string]string
		layer  string
		env    string
		want   string
	}{
		{
			name: "the model's labels alone", layer: "2-shr", env: "local.environment",
			want: "  labels = {\n    terraform             = \"true\"\n    terraform_source_path = \"2-shr\"\n    source_repo           = \"acme-infrastructure\"\n    environment           = local.environment\n  }",
		},
		{
			name: "a short extra label keeps the model's width", labels: map[string]string{"team": "core"}, layer: "1-org", env: "\"org\"",
			want: "  labels = {\n    terraform             = \"true\"\n    terraform_source_path = \"1-org\"\n    source_repo           = \"acme-infrastructure\"\n    environment           = \"org\"\n    team                  = \"core\"\n  }",
		},
		{
			name: "a long extra label widens the block", labels: map[string]string{"cost_center_and_owner_key": "x"}, layer: "2-net", env: "local.environment",
			want: "  labels = {\n    terraform                 = \"true\"\n    terraform_source_path     = \"2-net\"\n    source_repo               = \"acme-infrastructure\"\n    environment               = local.environment\n    cost_center_and_owner_key = \"x\"\n  }",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			v := &view{Placement: &Placement{SourceRepo: "acme-infrastructure", Labels: tt.labels}}
			if got := v.LabelsBlock(tt.layer, tt.env); got != tt.want {
				t.Errorf("LabelsBlock() =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

func TestViewPhrases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		placement Placement
		check     func(v *view) string
		want      string
	}{
		{name: "no applications", placement: Placement{}, check: func(v *view) string { return v.ApplicationsList() + " " + v.ExampleApp() }, want: "[] app"},
		{name: "two applications", placement: Placement{Applications: []string{"harbor", "beacon"}}, check: func(v *view) string { return v.ApplicationsList() + " " + v.ExampleApp() }, want: `["harbor", "beacon"] harbor`},
		{name: "no contact domains", placement: Placement{OrganizationDomain: "acme.com"}, check: func(v *view) string { return v.ContactDomainsList() }, want: `["@acme.com"]`},
		{name: "seed labels", placement: Placement{SourceRepo: "acme-infrastructure", Labels: map[string]string{"team": "core"}}, check: func(v *view) string { return v.SeedLabels() }, want: "terraform=true,terraform_source_path=0-bootstrap,source_repo=acme-infrastructure,environment=boot,team=core"},
		{name: "label prose", placement: Placement{Labels: map[string]string{"team": "core", "cost": "a"}}, check: func(v *view) string { return v.ExtraLabelsProse() + " / " + v.ExtraLabelKeys() }, want: "`cost = \"a\"`, `team = \"core\"` / `cost`, `team`"},
		{name: "the bucket before the seed", placement: Placement{Prefix: "acme"}, check: func(v *view) string { return v.Bucket() }, want: "acme-boot-gbl-state-REPLACEME"},
		{name: "the bucket after the seed", placement: Placement{Prefix: "acme", StateBucket: "acme-boot-gbl-state-1a2b"}, check: func(v *view) string { return v.Bucket() }, want: "acme-boot-gbl-state-1a2b"},
		{
			name:      "identities, backends and hosts under the recorded projects, REPLACEME for the rest",
			placement: Placement{Prefix: "acme", AppsDomain: "acme.dev", Projects: map[string]string{"tst": "acme-tst-gbl-core-1a2b"}},
			check: func(v *view) string {
				return v.DeployIdentity("tst", "quill") + " " + v.ApplyIdentity("stg", "quill") + " " + v.Backend("tst", "quill") + " " + v.Host("tst", "quill") + " " + v.Host("prd", "quill") + " " + v.PullRequestBackend()
			},
			want: "serviceAccount:acme-tst-gbl-quill-deploy@acme-tst-gbl-core-1a2b.iam.gserviceaccount.com serviceAccount:acme-stg-gbl-quill-tofu@acme-stg-gbl-core-REPLACEME.iam.gserviceaccount.com projects/acme-tst-gbl-core-1a2b/global/backendServices/acme-tst-gbl-quill-backend quill-tst.acme.dev quill.acme.dev projects/acme-tst-gbl-core-1a2b/global/backendServices/acme-tst-gbl-pr-backend",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := tt.placement
			if got := tt.check(&view{Placement: &p}); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		mutate       func(t *testing.T, dir string)
		wantClean    bool
		wantFinding  string
		wantUnseeded int
	}{
		{name: "the fixture is clean", mutate: func(*testing.T, string) {}, wantClean: true},
		{
			name: "a changed line is drift",
			mutate: func(t *testing.T, dir string) {
				t.Helper()
				path := filepath.Join(dir, "1-org", "folders.tf")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, append(data, []byte("# a hand edit\n")...), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantFinding: "1-org/folders.tf",
		},
		{
			name: "a missing owned file is drift, a missing seeded file is not",
			mutate: func(t *testing.T, dir string) {
				t.Helper()
				for _, f := range []string{"2-spn/outputs.tf", "2-spn/terraform.tfvars"} {
					if err := os.Remove(filepath.Join(dir, filepath.FromSlash(f))); err != nil {
						t.Fatal(err)
					}
				}
			},
			wantFinding:  "2-spn/outputs.tf",
			wantUnseeded: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			files, err := Render(testPlacement(t))
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			if _, err := Write(files, dir); err != nil {
				t.Fatalf("Write() error = %v", err)
			}
			tt.mutate(t, dir)
			r, err := Check(testPlacement(t), dir)
			if err != nil {
				t.Fatalf("Check() error = %v", err)
			}
			if r.Clean() != tt.wantClean {
				t.Errorf("Clean() = %t, want %t: %+v", r.Clean(), tt.wantClean, r.Findings)
			}
			if tt.wantFinding != "" && (len(r.Findings) != 1 || r.Findings[0].Path != tt.wantFinding) {
				t.Errorf("Findings = %+v, want one for %s", r.Findings, tt.wantFinding)
			}
			if len(r.Unseeded) != tt.wantUnseeded {
				t.Errorf("Unseeded = %v, want %d", r.Unseeded, tt.wantUnseeded)
			}
		})
	}
}

func TestWriteKeepsSeeded(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	files, err := Render(testPlacement(t))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	first, err := Write(files, dir)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if first.Owned == 0 || len(first.Seeded) != 8 || len(first.Kept) != 0 {
		t.Fatalf("first write = %+v, want owned files, 8 seeded, none kept", first)
	}
	values := filepath.Join(dir, "2-spn", "terraform.tfvars")
	if err := os.WriteFile(values, []byte("processing_units = 200\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := Write(files, dir)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if len(second.Seeded) != 0 || len(second.Kept) != 8 {
		t.Errorf("second write = %+v, want nothing seeded and 8 kept", second)
	}
	data, err := os.ReadFile(values)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "processing_units = 200\n" {
		t.Errorf("the seeded values were rewritten: %q", data)
	}
}

func TestPlacementRegister(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		existing []string
		app      string
		want     []string
		wantErr  string
	}{
		{name: "a new application is appended", existing: []string{"harbor"}, app: "beacon", want: []string{"harbor", "beacon"}},
		{name: "the first application", app: "quill", want: []string{"quill"}},
		{name: "a registered application is refused", existing: []string{"harbor"}, app: "harbor", wantErr: `application "harbor" is registered already`},
		{name: "a code of the wrong shape is refused", app: "Harbor-1", wantErr: `application "Harbor-1" is not 1 to 6 lowercase alphanumeric characters`},
		{name: "a code too long is refused", app: "sevench", wantErr: "is not 1 to 6"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := Placement{Applications: tt.existing}
			err := p.Register(tt.app)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Register() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Register() error = %v", err)
			}
			if strings.Join(p.Applications, ",") != strings.Join(tt.want, ",") {
				t.Errorf("Applications = %v, want %v", p.Applications, tt.want)
			}
		})
	}
}

func TestPlacementProjects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		projects map[string]string
		wantErr  string
		// wantMissing are the environments ProjectsMissing names.
		wantMissing string
	}{
		{name: "none recorded yet", wantMissing: "tst,stg,prd"},
		{name: "all three recorded", projects: map[string]string{"tst": "imp-tst-gbl-core-1a2b", "stg": "imp-stg-gbl-core-3c4d", "prd": "imp-prd-gbl-core-5e6f"}},
		{name: "one recorded", projects: map[string]string{"tst": "imp-tst-gbl-core-1a2b"}, wantMissing: "stg,prd"},
		{name: "an environment the model lacks is refused", projects: map[string]string{"qa": "imp-qa-gbl-core-1a2b"}, wantErr: `projects names "qa", which is not one of tst, stg, prd`},
		{name: "an empty project is refused", projects: map[string]string{"tst": " "}, wantErr: "projects.tst is empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := testPlacement(t)
			p.Projects = tt.projects
			err := p.Validate()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Validate() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if got := strings.Join(p.ProjectsMissing(), ","); got != tt.wantMissing {
				t.Errorf("ProjectsMissing() = %q, want %q", got, tt.wantMissing)
			}
		})
	}
}
