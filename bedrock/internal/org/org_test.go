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
		{name: "a project number in an unknown environment", mutate: func(p *Placement) { p.ProjectNumbers = map[string]string{"qa": "1"} }, wantErr: `projectNumbers names "qa", which is not one of boot, shr, net, spn, tst, stg, prd`},
		{name: "a project number that is not a number", mutate: func(p *Placement) { p.ProjectNumbers = map[string]string{"tst": "imp-tst"} }, wantErr: `projectNumbers.tst "imp-tst" is not a project number (digits)`},
		{name: "the boot and shared projects", mutate: func(p *Placement) {
			p.Projects = map[string]string{"boot": "imp-boot-gbl-core-a1b2", "shr": "imp-shr-gbl-core-7a8b"}
			p.ProjectNumbers = map[string]string{"boot": "100000000001"}
		}},
		{name: "no infrastructure app yet", mutate: func(p *Placement) { p.GithubInfrastructureAppID, p.GithubInfrastructureKeyVersion = "", "" }},
		{name: "an infrastructure app named by slug", mutate: func(p *Placement) { p.GithubInfrastructureAppID = "imp-infrastructure" }, wantErr: `githubInfrastructureAppId "imp-infrastructure" is not an App ID`},
		{name: "a key version of latest", mutate: func(p *Placement) { p.GithubInfrastructureKeyVersion = "latest" }, wantErr: `githubInfrastructureKeyVersion "latest" is not a secret version's number (digits, never latest)`},
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
		{name: "the public-invoker grants are owned", path: "1-org/public-invokers.auto.tfvars", want: render.Owned},
		{name: "the layers workflow is owned", path: WorkflowFile, want: render.Owned},
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
				"datastore.databases.getMetadata", "datastore.indexes.get",
				"firebaserules.releases.get", "firebaserules.rulesets.get",
				"logging.buckets.get", "logging.sinks.get",
				"run.jobs.get", "run.services.get", "run.services.listTagBindings",
				"secretmanager.secrets.get", "secretmanager.versions.get",
				"apikeys.keys.get", "apikeys.keys.getKeyString", "storage.buckets.get",
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
		{name: "the layer order", placement: Placement{}, check: func(v *view) string { return v.LayerOrderProse() }, want: "0-bootstrap, 1-org, 2-shr, 2-spn and 2-net, then 2-env for tst, stg and prd"},
		{name: "the provider before the boot project's number is recorded", placement: Placement{Prefix: "acme"}, check: func(v *view) string { return v.WorkflowProvider() + "|" + v.WorkflowPool() }, want: "|acme-boot-github"},
		{
			name:      "the provider, the boot project, the repository and the key container",
			placement: Placement{Prefix: "acme", GithubOrganization: "acme", SourceRepo: "acme-infrastructure", Projects: map[string]string{"boot": "acme-boot-gbl-core-1a2b"}, ProjectNumbers: map[string]string{"boot": "42"}},
			check: func(v *view) string {
				return v.WorkflowProvider() + " " + v.BootProject() + " " + v.InfrastructureRepository() + " " + v.InfrastructureKeySecret()
			},
			want: "projects/42/locations/global/workloadIdentityPools/acme-boot-github/providers/github acme-boot-gbl-core-1a2b acme/acme-infrastructure acme-boot-gbl-github-infrastructure-key",
		},
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
		{
			name: "a hand edit to the layers workflow is drift",
			mutate: func(t *testing.T, dir string) {
				t.Helper()
				path := filepath.Join(dir, filepath.FromSlash(WorkflowFile))
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				edited := strings.Replace(string(data), "      max-parallel: 1\n", "      max-parallel: 2\n", 1)
				if edited == string(data) {
					t.Fatal("the workflow has no max-parallel line to edit")
				}
				if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantFinding: WorkflowFile,
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
		{name: "an environment the model lacks is refused", projects: map[string]string{"qa": "imp-qa-gbl-core-1a2b"}, wantErr: `projects names "qa", which is not one of boot, shr, net, spn, tst, stg, prd`},
		{name: "an empty project is refused", projects: map[string]string{"tst": " "}, wantErr: "projects.tst is empty"},
		{name: "the boot and shared projects are not environments", projects: map[string]string{"boot": "imp-boot-gbl-core-1a2b", "shr": "imp-shr-gbl-core-7a8b"}, wantMissing: "tst,stg,prd"},
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

// TestWorkflowUnwired names what the layers workflow still lacks in a placement: the boot
// project's number, which names the identity provider, and every project the layer
// identities are named under.
func TestWorkflowUnwired(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		projects map[string]string
		numbers  map[string]string
		want     string
	}{
		{name: "nothing recorded", want: "projectNumbers.boot,projects.boot,projects.shr,projects.net,projects.spn,projects.tst,projects.stg,projects.prd"},
		{name: "the seed's values alone", projects: map[string]string{"boot": "imp-boot-gbl-core-a1b2"}, numbers: map[string]string{"boot": "1"}, want: "projects.shr,projects.net,projects.spn,projects.tst,projects.stg,projects.prd"},
		{name: "the environments alone", projects: map[string]string{"tst": "a", "stg": "b", "prd": "c"}, want: "projectNumbers.boot,projects.boot,projects.shr,projects.net,projects.spn"},
		{
			name:     "everything",
			projects: map[string]string{"boot": "a", "shr": "b", "net": "c", "spn": "d", "tst": "e", "stg": "f", "prd": "g"},
			numbers:  map[string]string{"boot": "1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := testPlacement(t)
			p.Projects, p.ProjectNumbers = tt.projects, tt.numbers
			if got := strings.Join(p.WorkflowUnwired(), ","); got != tt.want {
				t.Errorf("WorkflowUnwired() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestLayerRuns reads the runs the workflow is rendered with: the five single-run layers
// then 2-env per environment, each with its identities under the recorded projects, and
// REPLACEME under a project the placement does not record.
func TestLayerRuns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		projects map[string]string
		want     []string
	}{
		{
			name:     "every project recorded",
			projects: map[string]string{"boot": "acme-boot-gbl-core-1a2b", "shr": "acme-shr-gbl-core-3c4d", "net": "acme-net-gbl-core-5e6f", "spn": "acme-spn-gbl-core-7a8b", "tst": "acme-tst-gbl-core-9c0d", "stg": "acme-stg-gbl-core-1e2f", "prd": "acme-prd-gbl-core-3a4b"},
			want: []string{
				"0-bootstrap||acme-boot-gbl-tofu@acme-boot-gbl-core-1a2b.iam.gserviceaccount.com|acme-boot-gbl-plan@acme-boot-gbl-core-1a2b.iam.gserviceaccount.com",
				"1-org||acme-org-gbl-tofu@acme-boot-gbl-core-1a2b.iam.gserviceaccount.com|acme-org-gbl-plan@acme-boot-gbl-core-1a2b.iam.gserviceaccount.com",
				"2-shr||acme-shr-gbl-tofu@acme-shr-gbl-core-3c4d.iam.gserviceaccount.com|acme-shr-gbl-plan@acme-shr-gbl-core-3c4d.iam.gserviceaccount.com",
				"2-spn||acme-spn-gbl-tofu@acme-spn-gbl-core-7a8b.iam.gserviceaccount.com|acme-spn-gbl-plan@acme-spn-gbl-core-7a8b.iam.gserviceaccount.com",
				"2-net||acme-net-gbl-tofu@acme-net-gbl-core-5e6f.iam.gserviceaccount.com|acme-net-gbl-plan@acme-net-gbl-core-5e6f.iam.gserviceaccount.com",
				"2-env tst|2-env/tst|acme-tst-gbl-tofu@acme-tst-gbl-core-9c0d.iam.gserviceaccount.com|acme-tst-gbl-plan@acme-tst-gbl-core-9c0d.iam.gserviceaccount.com",
				"2-env stg|2-env/stg|acme-stg-gbl-tofu@acme-stg-gbl-core-1e2f.iam.gserviceaccount.com|acme-stg-gbl-plan@acme-stg-gbl-core-1e2f.iam.gserviceaccount.com",
				"2-env prd|2-env/prd|acme-prd-gbl-tofu@acme-prd-gbl-core-3a4b.iam.gserviceaccount.com|acme-prd-gbl-plan@acme-prd-gbl-core-3a4b.iam.gserviceaccount.com",
			},
		},
		{
			name:     "a project not recorded carries REPLACEME",
			projects: map[string]string{"boot": "acme-boot-gbl-core-1a2b"},
			want: []string{
				"0-bootstrap||acme-boot-gbl-tofu@acme-boot-gbl-core-1a2b.iam.gserviceaccount.com|acme-boot-gbl-plan@acme-boot-gbl-core-1a2b.iam.gserviceaccount.com",
				"1-org||acme-org-gbl-tofu@acme-boot-gbl-core-1a2b.iam.gserviceaccount.com|acme-org-gbl-plan@acme-boot-gbl-core-1a2b.iam.gserviceaccount.com",
				"2-shr||acme-shr-gbl-tofu@acme-shr-gbl-core-REPLACEME.iam.gserviceaccount.com|acme-shr-gbl-plan@acme-shr-gbl-core-REPLACEME.iam.gserviceaccount.com",
				"2-spn||acme-spn-gbl-tofu@acme-spn-gbl-core-REPLACEME.iam.gserviceaccount.com|acme-spn-gbl-plan@acme-spn-gbl-core-REPLACEME.iam.gserviceaccount.com",
				"2-net||acme-net-gbl-tofu@acme-net-gbl-core-REPLACEME.iam.gserviceaccount.com|acme-net-gbl-plan@acme-net-gbl-core-REPLACEME.iam.gserviceaccount.com",
				"2-env tst|2-env/tst|acme-tst-gbl-tofu@acme-tst-gbl-core-REPLACEME.iam.gserviceaccount.com|acme-tst-gbl-plan@acme-tst-gbl-core-REPLACEME.iam.gserviceaccount.com",
				"2-env stg|2-env/stg|acme-stg-gbl-tofu@acme-stg-gbl-core-REPLACEME.iam.gserviceaccount.com|acme-stg-gbl-plan@acme-stg-gbl-core-REPLACEME.iam.gserviceaccount.com",
				"2-env prd|2-env/prd|acme-prd-gbl-tofu@acme-prd-gbl-core-REPLACEME.iam.gserviceaccount.com|acme-prd-gbl-plan@acme-prd-gbl-core-REPLACEME.iam.gserviceaccount.com",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			v := &view{Placement: &Placement{Prefix: "acme", Projects: tt.projects}}
			var got []string
			for _, r := range v.LayerRuns() {
				got = append(got, r.Name+"|"+r.StatePrefix+"|"+r.Apply+"|"+r.Plan)
			}
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("LayerRuns() =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
			}
		})
	}
}

// TestWorkflow reads the rendered layers workflow: a plan job on pull requests into the
// default branch alone, holding the pull-request write and nothing more, every layer's
// plan in parallel; an apply job on pushes to the default branch and on a run started from
// it, one layer at a time in layer order, canceling the rest at the first failure; the
// identity rows in layer order with 2-env once per environment; the boot project's provider
// from the placement; the OpenTofu version from the repository's pin; and the
// infrastructure app's values where the placement records them.
func TestWorkflow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(p *Placement)
		want   []string
		absent []string
	}{
		{
			name:   "the triggers",
			mutate: func(*Placement) {},
			want: []string{
				"on:\n  pull_request:\n    types: [opened, synchronize, reopened]\n    branches: [master]\n  push:\n    branches: [master]\n  workflow_dispatch:\n",
				"        options:\n          - 0-bootstrap\n          - 1-org\n          - 2-shr\n          - 2-spn\n          - 2-net\n          - 2-env\n",
				"\npermissions: {}\n",
			},
		},
		{
			name:   "the plan job: pull requests, parallel, the pull-request write and nothing more",
			mutate: func(*Placement) {},
			want: []string{
				"  plan:\n    name: plan ${{ matrix.name }}\n    needs: layers\n    if: github.event_name == 'pull_request' && needs.layers.outputs.count != '0'\n    runs-on: ubuntu-latest\n    permissions:\n      contents: read\n      id-token: write\n      pull-requests: write\n    strategy:\n      fail-fast: false\n",
				"          service_account: ${{ env.IDENTITY }}\n",
				"      IDENTITY: ${{ matrix.plan }}\n",
				"tofu plan -input=false -lock=false -no-color ${ENVIRONMENT:+-var environment=$ENVIRONMENT}",
				"gh api --method PATCH \"repos/$GITHUB_REPOSITORY/issues/comments/$id\"",
			},
		},
		{
			name:   "the apply job: the default branch, one layer at a time, stopping at the first failure",
			mutate: func(*Placement) {},
			want: []string{
				"  apply:\n    name: apply ${{ matrix.name }}\n    needs: layers\n    if: github.event_name != 'pull_request' && needs.layers.outputs.count != '0'\n    runs-on: ubuntu-latest\n    permissions:\n      contents: read\n      id-token: write\n",
				"    strategy:\n      fail-fast: true\n      max-parallel: 1\n      matrix:\n        include: ${{ fromJSON(needs.layers.outputs.matrix) }}\n",
				"      IDENTITY: ${{ matrix.apply }}\n",
				"tofu apply -input=false -no-color \"$RUNNER_TEMP/plan.tfplan\"",
				"concurrency:\n  group: ${{ github.event_name == 'pull_request' && format('layers-plan-{0}', github.ref) || 'layers-apply' }}\n  cancel-in-progress: ${{ github.event_name == 'pull_request' }}\n",
			},
		},
		{
			name:   "the identity per layer, in layer order, 2-env once per environment",
			mutate: func(*Placement) {},
			want: []string{
				"          0-bootstrap - - imp-boot-gbl-tofu@imp-boot-gbl-core-a1b2.iam.gserviceaccount.com imp-boot-gbl-plan@imp-boot-gbl-core-a1b2.iam.gserviceaccount.com\n" +
					"          1-org - - imp-org-gbl-tofu@imp-boot-gbl-core-a1b2.iam.gserviceaccount.com imp-org-gbl-plan@imp-boot-gbl-core-a1b2.iam.gserviceaccount.com\n" +
					"          2-shr - - imp-shr-gbl-tofu@imp-shr-gbl-core-7a8b.iam.gserviceaccount.com imp-shr-gbl-plan@imp-shr-gbl-core-7a8b.iam.gserviceaccount.com\n" +
					"          2-spn - - imp-spn-gbl-tofu@imp-spn-gbl-core-2e3f.iam.gserviceaccount.com imp-spn-gbl-plan@imp-spn-gbl-core-2e3f.iam.gserviceaccount.com\n" +
					"          2-net - - imp-net-gbl-tofu@imp-net-gbl-core-9c0d.iam.gserviceaccount.com imp-net-gbl-plan@imp-net-gbl-core-9c0d.iam.gserviceaccount.com\n" +
					"          2-env tst 2-env/tst imp-tst-gbl-tofu@imp-tst-gbl-core-1a2b.iam.gserviceaccount.com imp-tst-gbl-plan@imp-tst-gbl-core-1a2b.iam.gserviceaccount.com\n" +
					"          2-env stg 2-env/stg imp-stg-gbl-tofu@imp-stg-gbl-core-3c4d.iam.gserviceaccount.com imp-stg-gbl-plan@imp-stg-gbl-core-3c4d.iam.gserviceaccount.com\n" +
					"          2-env prd 2-env/prd imp-prd-gbl-tofu@imp-prd-gbl-core-5e6f.iam.gserviceaccount.com imp-prd-gbl-plan@imp-prd-gbl-core-5e6f.iam.gserviceaccount.com\n",
				"  PROVIDER: projects/100000000001/locations/global/workloadIdentityPools/imp-boot-github/providers/github\n",
				"          tofu_version: ${{ needs.layers.outputs.tofu }}\n",
				"echo \"tofu=$(tr -d '[:space:]' < .opentofu-version)\"",
				"          APP_ID: 5080700\n          KEY_VERSION: 1\n          KEY_SECRET: imp-boot-gbl-github-infrastructure-key\n          BOOT_PROJECT: imp-boot-gbl-core-a1b2\n",
			},
			absent: []string{"-core-REPLACEME", "GOOGLE_IMPERSONATE_SERVICE_ACCOUNT"},
		},
		{
			name: "before the seed's values and the infrastructure app are recorded",
			mutate: func(p *Placement) {
				p.Projects, p.ProjectNumbers = nil, nil
				p.GithubInfrastructureAppID, p.GithubInfrastructureKeyVersion = "", ""
			},
			want: []string{
				"  PROVIDER: \"\"\n",
				"          2-shr - - imp-shr-gbl-tofu@imp-shr-gbl-core-REPLACEME.iam.gserviceaccount.com imp-shr-gbl-plan@imp-shr-gbl-core-REPLACEME.iam.gserviceaccount.com\n",
				"          APP_ID: \"\"\n          KEY_VERSION: \"\"\n",
				"          app-id: \"\"\n",
				"if printf '%s' \"$selected\" | grep -q REPLACEME; then",
				"record githubInfrastructureAppId and githubInfrastructureKeyVersion in placement.json and run bedrock org render",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := testPlacement(t)
			tt.mutate(p)
			files, err := Render(p)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			var workflow string
			for _, f := range files {
				if f.Path == WorkflowFile {
					workflow = string(f.Content)
				}
			}
			if workflow == "" {
				t.Fatalf("%s is not rendered", WorkflowFile)
			}
			for _, w := range tt.want {
				if !strings.Contains(workflow, w) {
					t.Errorf("%s lacks:\n%s", WorkflowFile, w)
				}
			}
			for _, a := range tt.absent {
				if strings.Contains(workflow, a) {
					t.Errorf("%s still carries %q", WorkflowFile, a)
				}
			}
		})
	}
}

// TestFederation reads the sign-in the layers render: 0-bootstrap's pool and provider, which
// trust the infrastructure repository and the workflow file alone and map a token's event
// and ref to plan or apply; the bindings on the two identities 0-bootstrap owns, which admit
// a run that applies; and 1-org's bindings, which admit a run that applies for every apply
// identity and a run that plans for every plan identity, the two boot layers' included.
func TestFederation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		path   string
		want   []string
		absent []string
	}{
		{
			name: "the pool and provider trust the repository and the workflow file, and map the purpose",
			path: "0-bootstrap/github.tf",
			want: []string{
				`workload_identity_pool_id = "${var.prefix}-boot-github"`,
				`workload_identity_pool_provider_id = "github"`,
				`"attribute.purpose"    = "assertion.event_name == \"pull_request\" && assertion.base_ref == \"${var.github_default_branch}\" ? \"plan\" : ((assertion.event_name == \"push\" || assertion.event_name == \"workflow_dispatch\") && assertion.ref == \"refs/heads/${var.github_default_branch}\" ? \"apply\" : \"none\")"`,
				`attribute_condition = "assertion.repository == \"${var.github_organization}/${var.infrastructure_repository}\" && assertion.job_workflow_ref.startsWith(\"${var.github_organization}/${var.infrastructure_repository}/.github/workflows/layers.yml@\")"`,
				`issuer_uri = "https://token.actions.githubusercontent.com"`,
				`workflow_plan  = "principalSet://iam.googleapis.com/${google_iam_workload_identity_pool.github.name}/attribute.purpose/plan"`,
				`workflow_apply = "principalSet://iam.googleapis.com/${google_iam_workload_identity_pool.github.name}/attribute.purpose/apply"`,
			},
		},
		{
			name: "the two identities 0-bootstrap owns admit a run that applies, and no plan",
			path: "0-bootstrap/github.tf",
			want: []string{
				"resource \"google_service_account_iam_member\" \"boot_tofu_workflow\" {\n  service_account_id = google_service_account.boot_tofu.name\n  role               = \"roles/iam.workloadIdentityUser\"\n  member             = local.workflow_apply\n}\n",
				"resource \"google_service_account_iam_member\" \"org_tofu_workflow\" {\n  service_account_id = google_service_account.org_tofu.name\n  role               = \"roles/iam.workloadIdentityUser\"\n  member             = local.workflow_apply\n}\n",
			},
			absent: []string{"member             = local.workflow_plan"},
		},
		{
			name: "the infrastructure app's key container, read and administered by the org identity",
			path: "0-bootstrap/github.tf",
			want: []string{
				`secret_id = "${var.prefix}-boot-gbl-github-infrastructure-key"`,
				"resource \"google_secret_manager_secret_iam_member\" \"org_tofu_infrastructure_key\" {\n  project   = module.boot_project.project_id\n  secret_id = google_secret_manager_secret.github_infrastructure_key.secret_id\n  role      = \"roles/secretmanager.admin\"\n  member    = google_service_account.org_tofu.member\n}\n",
			},
		},
		{
			name: "1-org's apply identities admit a run that applies, its plan identities a run that plans",
			path: "1-org/workflow.tf",
			want: []string{
				`workflow_plan  = "principalSet://iam.googleapis.com/${local.boot.github_pool_name}/attribute.purpose/plan"`,
				`workflow_apply = "principalSet://iam.googleapis.com/${local.boot.github_pool_name}/attribute.purpose/apply"`,
				"resource \"google_service_account_iam_member\" \"tofu_workflow\" {\n  for_each = local.layers\n\n  service_account_id = google_service_account.tofu[each.key].name\n  role               = \"roles/iam.workloadIdentityUser\"\n  member             = local.workflow_apply\n}\n",
				"resource \"google_service_account_iam_member\" \"plan_workflow\" {\n  for_each = local.layers\n\n  service_account_id = google_service_account.plan[each.key].name\n  role               = \"roles/iam.workloadIdentityUser\"\n  member             = local.workflow_plan\n}\n",
				"resource \"google_service_account_iam_member\" \"boot_plan_workflow\" {\n  for_each = local.boot_plans\n\n  service_account_id = google_service_account.boot_plan[each.key].name\n  role               = \"roles/iam.workloadIdentityUser\"\n  member             = local.workflow_plan\n}\n",
			},
		},
		{
			name: "the boot layers' plan identities, their organization-level reads and the key's read",
			path: "1-org/workflow.tf",
			want: []string{
				"resource \"google_service_account\" \"boot_plan\" {\n  for_each = local.boot_plans\n\n  project      = var.boot_project_id\n  account_id   = \"${var.prefix}-${each.key}-gbl-plan\"\n",
				"    boot = { layer = \"0-bootstrap\", reads = [\"0-bootstrap\"] }\n    org  = { layer = \"1-org\", reads = [\"0-bootstrap\", \"1-org\"] }\n",
				"    for pair in setproduct(keys(local.boot_plans), var.boot_plan_roles) :",
				"resource \"google_secret_manager_secret_iam_member\" \"org_plan_infrastructure_key\" {\n  project   = var.boot_project_id\n  secret_id = local.boot.github_infrastructure_key_secret\n  role      = \"roles/secretmanager.secretAccessor\"\n  member    = google_service_account.boot_plan[\"org\"].member\n}\n",
			},
		},
		{
			name: "1-org reads 0-bootstrap's state for the pool, and the boot identity holds the pool role",
			path: "1-org/initialize.tf",
			want: []string{"data \"terraform_remote_state\" \"boot\" {\n  backend = \"gcs\"\n  config = {\n    bucket = var.state_bucket\n    prefix = \"0-bootstrap\"\n  }\n}\n"},
		},
		{
			name: "the boot identity's organization roles carry the pool role; the org identity's the tag role",
			path: "0-bootstrap/variables.tf",
			want: []string{`"roles/iam.workloadIdentityPoolAdmin",     # the boot project's identity pool and provider`, `"roles/resourcemanager.tagAdmin",        # the public-invoker tag and the grants on its value`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			content := renderedFile(t, tt.path)
			for _, w := range tt.want {
				if !strings.Contains(content, w) {
					t.Errorf("%s lacks:\n%s", tt.path, w)
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

// TestStateBucketGrants reads the state bucket's grants: 0-bootstrap grants for the two
// identities it owns (the list, each one's own prefix, the org identity's read of
// 0-bootstrap's state) and gives both the bucket's policy authority through its custom
// role; 1-org grants for every identity it creates (the list for all, the own prefix for
// the apply identities, the upstream reads for both kinds) and gives the environment layer
// identities the same authority for the application identities 2-env creates.
func TestStateBucketGrants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want []string
	}{
		{
			name: "the policy role sets a bucket's policy and nothing else",
			path: "0-bootstrap/state-bucket.tf",
			want: []string{
				"resource \"google_organization_iam_custom_role\" \"state_bucket_policy_admin\" {\n  org_id      = local.org_id\n  role_id     = \"${var.prefix}GblStateBucketPolicyAdmin\"\n",
				"  permissions = [\n    \"storage.buckets.get\",\n    \"storage.buckets.getIamPolicy\",\n    \"storage.buckets.setIamPolicy\",\n  ]\n",
			},
		},
		{
			name: "0-bootstrap grants for its two identities",
			path: "0-bootstrap/state-bucket.tf",
			want: []string{
				"    boot = { member = google_service_account.boot_tofu.member, prefix = \"0-bootstrap\" }\n    org  = { member = google_service_account.org_tofu.member, prefix = \"1-org\" }\n",
				"resource \"google_storage_bucket_iam_member\" \"policy_admin\" {\n  for_each = local.own_identities\n\n  bucket = var.state_bucket\n  role   = google_organization_iam_custom_role.state_bucket_policy_admin.id\n",
				"resource \"google_storage_bucket_iam_member\" \"state_list\" {\n  for_each = local.own_identities\n\n  bucket = var.state_bucket\n  role   = \"roles/storage.legacyBucketReader\"\n",
				"resource \"google_storage_bucket_iam_member\" \"state_own\" {\n  for_each = local.own_identities\n\n  bucket = var.state_bucket\n  role   = \"roles/storage.objectUser\"\n",
				`expression  = "resource.name.startsWith(\"${local.state_bucket_objects}/${each.value.prefix}/\")"`,
				"resource \"google_storage_bucket_iam_member\" \"org_tofu_state_upstream\" {\n  bucket = var.state_bucket\n  role   = \"roles/storage.objectViewer\"\n  member = google_service_account.org_tofu.member\n",
				`expression  = "resource.name.startsWith(\"${local.state_bucket_objects}/0-bootstrap/\")"`,
			},
		},
		{
			name: "1-org grants for the identities it creates",
			path: "1-org/workflow.tf",
			want: []string{
				`layer_state_prefixes    = { for k, v in local.layers : k => v.folder == "shared" ? "2-${k}" : "2-env/${k}" }`,
				`layer_upstream_prefixes = { for k, v in local.layers : k => v.folder == "shared" ? ["1-org"] : ["1-org", "2-shr", "2-spn", "2-net", "2-env"] }`,
				"resource \"google_storage_bucket_iam_member\" \"tofu_state_list\" {\n  for_each = local.layers\n\n  bucket = var.state_bucket\n  role   = \"roles/storage.legacyBucketReader\"\n  member = google_service_account.tofu[each.key].member\n}\n",
				"resource \"google_storage_bucket_iam_member\" \"plan_state_list\" {\n  for_each = local.layers\n\n  bucket = var.state_bucket\n  role   = \"roles/storage.legacyBucketReader\"\n  member = google_service_account.plan[each.key].member\n}\n",
				"resource \"google_storage_bucket_iam_member\" \"boot_plan_state_list\" {\n  for_each = local.boot_plans\n",
				"resource \"google_storage_bucket_iam_member\" \"tofu_state_own\" {\n  for_each = local.layers\n\n  bucket = var.state_bucket\n  role   = \"roles/storage.objectUser\"\n  member = google_service_account.tofu[each.key].member\n",
				`expression  = "resource.name.startsWith(\"${local.state_bucket_objects}/${local.layer_state_prefixes[each.key]}/\")"`,
				"resource \"google_storage_bucket_iam_member\" \"tofu_state_upstream\" {\n  for_each = local.layers\n\n  bucket = var.state_bucket\n  role   = \"roles/storage.objectViewer\"\n",
				`expression  = join(" || ", [for p in local.layer_upstream_prefixes[each.key] : "resource.name.startsWith(\"${local.state_bucket_objects}/${p}/\")"])`,
				"resource \"google_storage_bucket_iam_member\" \"plan_state_read\" {\n  for_each = local.layers\n\n  bucket = var.state_bucket\n  role   = \"roles/storage.objectViewer\"\n  member = google_service_account.plan[each.key].member\n",
				`expression  = join(" || ", [for p in concat([local.layer_state_prefixes[each.key]], local.layer_upstream_prefixes[each.key]) : "resource.name.startsWith(\"${local.state_bucket_objects}/${p}/\")"])`,
				"resource \"google_storage_bucket_iam_member\" \"boot_plan_state_read\" {\n  for_each = local.boot_plans\n\n  bucket = var.state_bucket\n  role   = \"roles/storage.objectViewer\"\n",
				`expression  = join(" || ", [for p in each.value.reads : "resource.name.startsWith(\"${local.state_bucket_objects}/${p}/\")"])`,
				"resource \"google_storage_bucket_iam_member\" \"environment_tofu_policy_admin\" {\n  for_each = local.environment_layers\n\n  bucket = var.state_bucket\n  role   = local.boot.state_bucket_policy_admin_role\n  member = google_service_account.tofu[each.key].member\n}\n",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			content := renderedFile(t, tt.path)
			for _, w := range tt.want {
				if !strings.Contains(content, w) {
					t.Errorf("%s lacks:\n%s", tt.path, w)
				}
			}
		})
	}
}

// TestPublicInvoker reads where the tagUser grant on the public-invoker tag lives: 1-org,
// which owns the tag, from the members its public-invokers.auto.tfvars renders per
// application and environment; 2-env, which created the identities, no longer grants it.
func TestPublicInvoker(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		path   string
		want   []string
		absent []string
	}{
		{
			name: "1-org grants tagUser from the public invokers",
			path: "1-org/tags.tf",
			want: []string{"resource \"google_tags_tag_value_iam_member\" \"public_invoker\" {\n  for_each = toset(flatten(values(var.public_invokers)))\n\n  tag_value = google_tags_tag_value.public_invoker.id\n  role      = \"roles/resourcemanager.tagUser\"\n  member    = each.value\n}\n"},
		},
		{
			name: "the public invokers are each application's apply identities in every environment",
			path: "1-org/public-invokers.auto.tfvars",
			want: []string{
				"public_invokers = {\n  harbor = [\n    \"serviceAccount:imp-tst-gbl-harbor-tofu@imp-tst-gbl-core-1a2b.iam.gserviceaccount.com\",\n    \"serviceAccount:imp-stg-gbl-harbor-tofu@imp-stg-gbl-core-3c4d.iam.gserviceaccount.com\",\n    \"serviceAccount:imp-prd-gbl-harbor-tofu@imp-prd-gbl-core-5e6f.iam.gserviceaccount.com\",\n  ]\n  beacon = [\n",
			},
		},
		{
			name: "the variable",
			path: "1-org/variables.tf",
			want: []string{"variable \"public_invokers\" {", "  type        = map(list(string))\n  default     = {}\n"},
		},
		{
			name:   "2-env no longer grants it",
			path:   "2-env/identities.tf",
			absent: []string{"apply_public_invoker", "roles/resourcemanager.tagUser", "public_invoker_tag_value"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			content := renderedFile(t, tt.path)
			for _, w := range tt.want {
				if !strings.Contains(content, w) {
					t.Errorf("%s lacks:\n%s", tt.path, w)
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
