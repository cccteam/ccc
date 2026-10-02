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

// renderedFile renders the fixture organization and returns one file's text.
func renderedFile(t *testing.T, path string) string {
	t.Helper()

	files, err := Render(testPlacement(t))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	for _, f := range files {
		if f.Path == path {
			return string(f.Content)
		}
	}
	t.Fatalf("%s is not rendered", path)

	return ""
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
		{name: "no release app", mutate: func(p *Placement) { p.GithubReleaseAppID = "" }, wantErr: "githubReleaseAppId is empty"},
		{name: "a release app named by slug", mutate: func(p *Placement) { p.GithubReleaseAppID = "imp-release" }, wantErr: "githubReleaseAppId \"imp-release\" is not an App ID"},
		{name: "no default branch", mutate: func(p *Placement) { p.GithubDefaultBranch = " " }, wantErr: "githubDefaultBranch is empty"},
		{name: "no infrastructure team", mutate: func(p *Placement) { p.GithubInfrastructureTeam = "" }},
		{name: "project numbers for the environments", mutate: func(p *Placement) { p.ProjectNumbers = map[string]string{"tst": "123456789012"} }},
		{name: "a project number in an unknown environment", mutate: func(p *Placement) { p.ProjectNumbers = map[string]string{"qa": "1"} }, wantErr: `projectNumbers names "qa", which is not one of tst, stg, prd`},
		{name: "a project number that is not a number", mutate: func(p *Placement) { p.ProjectNumbers = map[string]string{"tst": "imp-tst"} }, wantErr: `projectNumbers.tst "imp-tst" is not a project number (digits)`},
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
// pull-request build today; the environment's live version once a step needs it), each
// for every application; neither role overwrites or deletes a record.
func TestDeployRecordsGrants(t *testing.T) {
	t.Parallel()

	identities := renderedFile(t, "2-env/identities.tf")
	tests := []struct {
		name     string
		resource string
		role     string
		// identity is the service account resource granted: deploy, or plan.
		identity string
	}{
		{name: "creates records", resource: "deploy_records", role: "roles/storage.objectCreator", identity: "deploy"},
		{name: "reads records", resource: "deploy_records_viewer", role: "roles/storage.objectViewer", identity: "deploy"},
		{name: "the plan identity reads records, for the hotfix preview", resource: "plan_records", role: "roles/storage.objectViewer", identity: "plan"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			want := "resource \"google_storage_bucket_iam_member\" \"" + tt.resource + "\" {\n" +
				"  for_each = local.apps\n\n" +
				"  bucket = google_storage_bucket.records.name\n" +
				"  role   = \"" + tt.role + "\"\n" +
				"  member = google_service_account." + tt.identity + "[each.key].member\n}\n"
			if !strings.Contains(identities, want) {
				t.Errorf("2-env/identities.tf lacks the grant:\n%s", want)
			}
		})
	}
}

// TestDeployProjectRoles pins the deploy identity's roles on the environment project: what
// a build needs and no bundle. roles/cloudbuild.builds.builder reaches every bucket in the
// project (the deployment records, the applications' file stores), so the identity reads
// the build it runs in through cloudBuildBuildReader and, in tst alone, runs the sweep
// trigger through cloudBuildTriggerRunner.
func TestDeployProjectRoles(t *testing.T) {
	t.Parallel()

	locals := renderedFile(t, "2-env/locals.tf")
	tests := []struct {
		name   string
		text   string
		absent bool
	}{
		{
			name: "the roles a build needs",
			text: "  deploy_project_roles = concat([\n" +
				"    \"roles/serviceusage.serviceUsageConsumer\",\n" +
				"    \"roles/run.developer\",\n" +
				"    \"roles/logging.logWriter\",\n" +
				"    \"roles/monitoring.viewer\",\n" +
				"    local.org.run_job_policy_admin_role,\n" +
				"    local.org.cloud_build_build_reader_role,\n" +
				"  ], local.is_tst ? [local.org.cloud_build_trigger_runner_role] : [])\n",
		},
		{name: "no builder bundle", text: "\"roles/cloudbuild.builds.builder\"", absent: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			found := strings.Contains(locals, tt.text)
			switch {
			case tt.absent && found:
				t.Errorf("2-env/locals.tf grants %s", tt.text)
			case !tt.absent && !found:
				t.Errorf("2-env/locals.tf lacks:\n%s", tt.text)
			}
		})
	}
}

// TestPlanProjectRoles pins the plan identity's roles on the environment project: the reads a
// plan needs and no bundle. roles/viewer reads data as well as resources (the rows of a
// database in the project, the application's uploaded files through the bucket's default
// grants to project viewers), so the identity reads the stack's resources through
// applicationPlanReader, their IAM policies through securityReviewer, and nothing else.
func TestPlanProjectRoles(t *testing.T) {
	t.Parallel()

	locals := renderedFile(t, "2-env/locals.tf")
	tests := []struct {
		name   string
		text   string
		absent bool
	}{
		{
			name: "the roles a plan needs",
			text: "  plan_project_roles = [\n" +
				"    \"roles/serviceusage.serviceUsageConsumer\",\n" +
				"    local.org.application_plan_reader_role,\n" +
				"    \"roles/iam.securityReviewer\",\n" +
				"  ]\n",
		},
		{name: "no viewer bundle", text: "\"roles/viewer\"", absent: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			found := strings.Contains(locals, tt.text)
			switch {
			case tt.absent && found:
				t.Errorf("2-env/locals.tf grants %s", tt.text)
			case !tt.absent && !found:
				t.Errorf("2-env/locals.tf lacks:\n%s", tt.text)
			}
		})
	}
}

// TestCustomRolePermissions pins what 1-org's custom roles carry: each is the exact
// permissions one identity needs where the cloud's own roles carry more.
func TestCustomRolePermissions(t *testing.T) {
	t.Parallel()

	roles := renderedFile(t, "1-org/custom-roles.tf")
	tests := []struct {
		name        string
		resource    string
		roleID      string
		permissions []string
	}{
		{
			name:        "reads a build",
			resource:    "cloud_build_build_reader",
			roleID:      "cloudBuildBuildReader",
			permissions: []string{"cloudbuild.builds.get"},
		},
		{
			name:     "runs a trigger",
			resource: "cloud_build_trigger_runner",
			roleID:   "cloudBuildTriggerRunner",
			permissions: []string{
				"cloudbuild.builds.create", "cloudbuild.builds.get", "cloudbuild.builds.list",
				"cloudbuild.triggers.get", "cloudbuild.triggers.list",
			},
		},
		{
			name:        "sets a job's policy",
			resource:    "run_job_policy_admin",
			roleID:      "runJobPolicyAdmin",
			permissions: []string{"run.jobs.getIamPolicy", "run.jobs.setIamPolicy"},
		},
		{
			name:     "creates a database on an instance",
			resource: "spanner_database_creator",
			roleID:   "spannerDatabaseCreator",
			permissions: []string{
				"spanner.backupOperations.list", "spanner.backups.list", "spanner.databaseOperations.list",
				"spanner.databases.create", "spanner.databases.list", "spanner.instances.get",
			},
		},
		{
			name:     "plans an application stack",
			resource: "application_plan_reader",
			roleID:   "applicationPlanReader",
			permissions: []string{
				"cloudbuild.builds.get", "cloudscheduler.jobs.get", "cloudtasks.queues.get",
				"compute.backendServices.get", "compute.regionNetworkEndpointGroups.get",
				"datastore.databases.getMetadata", "logging.buckets.get", "logging.sinks.get",
				"run.jobs.get", "run.services.get", "run.services.listTagBindings",
				"secretmanager.secrets.get", "secretmanager.versions.get",
				"storage.buckets.get",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			header := "resource \"google_organization_iam_custom_role\" \"" + tt.resource + "\" {\n" +
				"  org_id      = local.org_id\n" +
				"  role_id     = \"" + tt.roleID + "\"\n"
			start := strings.Index(roles, header)
			if start < 0 {
				t.Fatalf("1-org/custom-roles.tf lacks the role %s", tt.roleID)
			}
			block, _, closed := strings.Cut(roles[start:], "\n}\n")
			if !closed {
				t.Fatalf("1-org/custom-roles.tf: the role %s has no closing brace", tt.roleID)
			}
			_, list, found := strings.Cut(block, "  permissions = [\n")
			if !found {
				t.Fatalf("1-org/custom-roles.tf: the role %s has no permissions list", tt.roleID)
			}
			list, _, _ = strings.Cut(list, "\n  ]")
			// The list may carry comment lines on what a permission is for; the
			// permissions are the quoted lines.
			var got []string
			for _, line := range strings.Split(list, "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "\"") {
					got = append(got, strings.Trim(line, "\","))
				}
			}
			if strings.Join(got, " ") != strings.Join(tt.permissions, " ") {
				t.Errorf("role %s carries the permissions\n%s\nwant\n%s", tt.roleID, strings.Join(got, "\n"), strings.Join(tt.permissions, "\n"))
			}
		})
	}
}

// TestSpannerGrants reads the Spanner grants 2-spn and 2-env render: each application's
// apply identity holds the organization's creator role on its instance without condition
// and the admin roles under a condition naming its own database and backups; the restore
// right reaches production's backups alone, from every environment but production; and
// the deploy identity holds nothing on an instance.
func TestSpannerGrants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		path   string
		want   []string
		absent []string
	}{
		{
			name: "the shared instance's members carry their environment, application and restore source",
			path: "2-spn/applications.auto.tfvars",
			want: []string{
				`"serviceAccount:imp-stg-gbl-harbor-tofu@imp-stg-gbl-core-3c4d.iam.gserviceaccount.com" = { environment = "stg", application = "harbor", restore_from = "prd" }`,
				`"serviceAccount:imp-prd-gbl-harbor-tofu@imp-prd-gbl-core-5e6f.iam.gserviceaccount.com" = { environment = "prd", application = "harbor" }`,
				`"serviceAccount:imp-stg-gbl-beacon-tofu@imp-stg-gbl-core-3c4d.iam.gserviceaccount.com" = { environment = "stg", application = "beacon", restore_from = "prd" }`,
			},
			absent: []string{`restore_from = "stg"`, `environment = "tst"`},
		},
		{
			name: "the shared instance bounds the admin roles to the member's own database and backups",
			path: "2-spn/spanner.tf",
			want: []string{
				`role     = local.org.spanner_database_creator_role`,
				`own_databases = { for m, v in var.database_admins : m => "${local.instance_path}/databases/${local.prefix}-${v.environment}-gbl-${v.application}-" }`,
				`own_backups   = { for m, v in var.database_admins : m => "${local.instance_path}/backups/${local.prefix}-${v.environment}-gbl-${v.application}-" }`,
				`expression  = "resource.name.startsWith(\"${local.own_databases[each.key]}\")"`,
				`expression  = "resource.name.startsWith(\"${local.own_databases[each.key]}\") || resource.name.startsWith(\"${local.own_backups[each.key]}\")"`,
				`for_each = { for m, v in var.database_admins : m => v if v.restore_from != "" }`,
				`expression  = "resource.name.startsWith(\"${local.instance_path}/backups/${local.prefix}-${each.value.restore_from}-gbl-${each.value.application}-\")"`,
			},
		},
		{
			name: "an environment's own instance bounds the apply identity and grants the deploy identity nothing",
			path: "2-env/identities.tf",
			want: []string{
				`resource "google_spanner_instance_iam_member" "apply_database_creator" {`,
				`role     = local.org.spanner_database_creator_role`,
				`expression  = "resource.name.startsWith(\"${local.instance_path}/databases/${local.name}-gbl-${each.key}-\") || resource.name.startsWith(\"${local.instance_path}/databases/${each.key}-pr\")"`,
				`expression  = "resource.name.startsWith(\"${local.instance_path}/databases/${local.name}-gbl-${each.key}-\") || resource.name.startsWith(\"${local.instance_path}/databases/${each.key}-pr\") || resource.name.startsWith(\"${local.instance_path}/backups/${local.name}-gbl-${each.key}-\")"`,
			},
			absent: []string{`"deploy_database_admin"`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			content := renderedFile(t, tt.path)
			for _, w := range tt.want {
				if !strings.Contains(content, w) {
					t.Errorf("%s lacks %q", tt.path, w)
				}
			}
			for _, a := range tt.absent {
				if strings.Contains(content, a) {
					t.Errorf("%s still carries %q", tt.path, a)
				}
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
		{name: "restorable environments prose", placement: Placement{}, check: func(v *view) string { return v.RestorableEnvironmentsProse() }, want: "`tst` and `stg`"},
		{name: "impulse's checks: the list, as prose and backticked", placement: Placement{}, check: func(v *view) string {
			return strings.Join(v.ImpulseChecks(), ",") + " / " + v.ImpulseChecksProse() + " / " + v.ImpulseChecksProseQuoted()
		}, want: "title,go,image,secrets,migrations / title, go, image, secrets and migrations / `title`, `go`, `image`, `secrets` and `migrations`"},
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

func TestProse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		items []string
		want  string
	}{
		{name: "none", want: ""},
		{name: "one", items: []string{"a"}, want: "a"},
		{name: "two", items: []string{"a", "b"}, want: "a and b"},
		{name: "three", items: []string{"a", "b", "c"}, want: "a, b and c"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := prose(tt.items); got != tt.want {
				t.Errorf("prose(%q) = %q, want %q", tt.items, got, tt.want)
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

func TestApplicationProjects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		projects    map[string]string
		numbers     map[string]string
		wantBlock   string
		wantMissing string
	}{
		{
			name:      "both recorded for every environment: the block for tst and stg, never prd",
			projects:  map[string]string{"tst": "imp-tst-gbl-core-b241", "stg": "imp-stg-gbl-core-0fa7", "prd": "imp-prd-gbl-core-abe8"},
			numbers:   map[string]string{"tst": "1", "stg": "2", "prd": "3"},
			wantBlock: "  \"projects\": {\n    \"tst\": {\"id\": \"imp-tst-gbl-core-b241\", \"number\": \"1\"},\n    \"stg\": {\"id\": \"imp-stg-gbl-core-0fa7\", \"number\": \"2\"}\n  }",
		},
		{
			name:        "a number missing leaves its environment out and names it",
			projects:    map[string]string{"tst": "imp-tst-gbl-core-b241", "stg": "imp-stg-gbl-core-0fa7"},
			numbers:     map[string]string{"tst": "1"},
			wantBlock:   "  \"projects\": {\n    \"tst\": {\"id\": \"imp-tst-gbl-core-b241\", \"number\": \"1\"}\n  }",
			wantMissing: "stg",
		},
		{name: "nothing recorded", wantMissing: "tst,stg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := testPlacement(t)
			p.Projects, p.ProjectNumbers = tt.projects, tt.numbers
			block, missing := p.ApplicationProjects()
			if block != tt.wantBlock {
				t.Errorf("block = %q, want %q", block, tt.wantBlock)
			}
			if got := strings.Join(missing, ","); got != tt.wantMissing {
				t.Errorf("missing = %q, want %q", got, tt.wantMissing)
			}
		})
	}
}

// TestRepositoryRules reads the repository module 1-org renders: the applications it
// configures, the checks a pull request must pass (the infrastructure workflow's job, the
// pull-request build under its trigger's name and project, and the five jobs of impulse's
// CI workflow in the workflow's order), squash as the only merge, the branch up to date
// before it merges, the restorable environments, and the placement's values as the
// variables' defaults.
func TestRepositoryRules(t *testing.T) {
	t.Parallel()

	files, err := Render(testPlacement(t))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	byPath := map[string]string{}
	for _, f := range files {
		byPath[f.Path] = string(f.Content)
	}
	tests := []struct {
		name string
		path string
		want []string
	}{
		{
			name: "the applications are the repositories",
			path: "1-org/applications.auto.tfvars",
			want: []string{`applications = ["harbor", "beacon"]`},
		},
		{
			name: "the rules name the checks in order (bedrock check, the pull-request build, impulse's five jobs), squash alone and the branch up to date",
			path: "1-org/github.tf",
			want: []string{
				`      {
        context        = "bedrock check"
        integration_id = tonumber(data.github_app.actions.id)
      },
      {
        context        = "${var.prefix}-tst-${local.region_code}-${app}-pr (${module.project["tst"].project_id})"
        integration_id = tonumber(data.github_app.cloud_build.id)
      },
      {
        context        = "title"
        integration_id = tonumber(data.github_app.actions.id)
      },
      {
        context        = "go"
        integration_id = tonumber(data.github_app.actions.id)
      },
      {
        context        = "image"
        integration_id = tonumber(data.github_app.actions.id)
      },
      {
        context        = "secrets"
        integration_id = tonumber(data.github_app.actions.id)
      },
      {
        context        = "migrations"
        integration_id = tonumber(data.github_app.actions.id)
      },
    ]`,
				`strict_required_status_checks_policy = true`,
				`allowed_merge_methods           = ["squash"]`,
				`require_last_push_approval      = var.github_infrastructure_team != ""`,
				`restorable_environments = ["tst", "stg"]`,
				`include = ["refs/tags/v*", "refs/tags/*/v*"]`,
				`hotfix  = { name = "hotfix lines", include = ["refs/heads/hotfix/**"] }`,
				`prevent_destroy = true`,
			},
		},
		{
			name: "the placement's values are the variables' defaults",
			path: "1-org/variables.tf",
			want: []string{
				`default     = 5080645`,
				`default     = "master"`,
				`default     = "impulseframework"`,
				"variable \"github_infrastructure_team\" {\n  description = \"Slug of the organization's infrastructure team, whose approval a change to an application's workflow and Cloud Build files needs, given after the last push. Empty for none.\"\n  type        = string\n  default     = \"\"",
			},
		},
		{
			name: "the provider is pinned and takes the organization",
			path: "1-org/initialize.tf",
			want: []string{`source  = "integrations/github"`, `owner = var.github_organization`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			content, ok := byPath[tt.path]
			if !ok {
				t.Fatalf("%s is not rendered", tt.path)
			}
			for _, w := range tt.want {
				if !strings.Contains(content, w) {
					t.Errorf("%s lacks %q", tt.path, w)
				}
			}
		})
	}
}
