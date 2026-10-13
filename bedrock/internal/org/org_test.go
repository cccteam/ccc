package org

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/render"
)

const fixture = "testdata/imp"

// update rewrites the fixture organization's files from the render (go test -update),
// the way the render package's goldens are rewritten.
var update = flag.Bool("update", false, "rewrite the fixture organization under testdata from the render")

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
		{name: "no release app slug", mutate: func(p *Placement) { p.GithubReleaseAppSlug = "" }, wantErr: `githubReleaseAppSlug "" is not a GitHub App's slug (lowercase letters, digits and single hyphens, as in github.com/apps/<slug>)`},
		{name: "a release app slug in capitals", mutate: func(p *Placement) { p.GithubReleaseAppSlug = "Imp-Release" }, wantErr: `githubReleaseAppSlug "Imp-Release" is not a GitHub App's slug`},
		{name: "a release app slug with its bot suffix", mutate: func(p *Placement) { p.GithubReleaseAppSlug = "imp-release[bot]" }, wantErr: `githubReleaseAppSlug "imp-release[bot]" is not a GitHub App's slug`},
		{name: "no machine account", mutate: func(p *Placement) { p.GithubMachineAccount = "" }, wantErr: "githubMachineAccount is empty: the GitHub login of the organization's machine account, an owner of the organization, which authorizes the Cloud Build GitHub connection in the browser and owns the personal access token that can stand in for it"},
		{name: "a machine account with a space", mutate: func(p *Placement) { p.GithubMachineAccount = "imp machine" }, wantErr: `githubMachineAccount "imp machine" is not a GitHub login (letters, digits and single hyphens, at most 39 characters)`},
		{name: "a machine account starting with a hyphen", mutate: func(p *Placement) { p.GithubMachineAccount = "-imp" }, wantErr: `githubMachineAccount "-imp" is not a GitHub login`},
		{name: "a machine account with two hyphens in a row", mutate: func(p *Placement) { p.GithubMachineAccount = "imp--machine" }, wantErr: `githubMachineAccount "imp--machine" is not a GitHub login`},
		{name: "a machine account longer than GitHub allows", mutate: func(p *Placement) { p.GithubMachineAccount = strings.Repeat("a", 40) }, wantErr: "is not a GitHub login"},
		{name: "a machine account of 39 characters", mutate: func(p *Placement) { p.GithubMachineAccount = strings.Repeat("a", 39) }},
		{name: "no infrastructure team", mutate: func(p *Placement) { p.GithubInfrastructureTeam = "" }},
		{name: "project numbers for the environments", mutate: func(p *Placement) { p.ProjectNumbers = map[string]string{"tst": "123456789012"} }},
		{name: "a project number in an unknown environment", mutate: func(p *Placement) { p.ProjectNumbers = map[string]string{"qa": "1"} }, wantErr: `projectNumbers names "qa", which is not one of boot, shr, net, spn, tst, stg, prd`},
		{name: "a project number that is not a number", mutate: func(p *Placement) { p.ProjectNumbers = map[string]string{"tst": "imp-tst"} }, wantErr: `projectNumbers.tst "imp-tst" is not a project number (digits)`},
		{name: "the boot and shared projects", mutate: func(p *Placement) {
			p.Projects = map[string]string{"boot": "imp-boot-gbl-core-a1b2", "shr": "imp-shr-gbl-core-7a8b"}
			p.ProjectNumbers = map[string]string{"boot": "100000000001"}
		}},
		{name: "no infrastructure app yet", mutate: func(p *Placement) { p.GithubInfrastructureAppID, p.GithubInfrastructureKeyVersion = "", "" }},
		{name: "a larger runner the organization already has", mutate: func(p *Placement) { p.CiLargeRunnerSize, p.CiLargeRunnerMaximum = "", 0 }},
		{name: "no larger runner", mutate: func(p *Placement) { p.CiLargeRunner, p.CiLargeRunnerSize, p.CiLargeRunnerMaximum = "", "", 0 }},
		{name: "a created runner with GitHub's default cap", mutate: func(p *Placement) { p.CiLargeRunnerMaximum = 0 }},
		{name: "an arm size", mutate: func(p *Placement) { p.CiLargeRunnerSize = "arm-16-core" }},
		{name: "a size without the runner's name", mutate: func(p *Placement) { p.CiLargeRunner = "" }, wantErr: "ciLargeRunnerSize 8-core names the runner 1-org creates, and ciLargeRunner, its name, is empty"},
		{name: "a size that is not GitHub's", mutate: func(p *Placement) { p.CiLargeRunnerSize = "large" }, wantErr: `ciLargeRunnerSize "large" is not a GitHub machine size`},
		{name: "a size in cores without the word", mutate: func(p *Placement) { p.CiLargeRunnerSize = "8" }, wantErr: `ciLargeRunnerSize "8" is not a GitHub machine size`},
		{name: "a name a hosted runner cannot carry", mutate: func(p *Placement) { p.CiLargeRunner = "ci large" }, wantErr: `ciLargeRunner "ci large" cannot name a GitHub-hosted runner`},
		{name: "a cap without a size", mutate: func(p *Placement) { p.CiLargeRunnerSize = "" }, wantErr: "ciLargeRunnerMaximum 10 caps the runner 1-org creates, and ciLargeRunnerSize is empty"},
		{name: "a negative cap", mutate: func(p *Placement) { p.CiLargeRunnerMaximum = -1 }, wantErr: "ciLargeRunnerMaximum -1 is negative"},
		{name: "an infrastructure app named by slug", mutate: func(p *Placement) { p.GithubInfrastructureAppID = "imp-infrastructure" }, wantErr: `githubInfrastructureAppId "imp-infrastructure" is not an App ID`},
		{name: "a key version of latest", mutate: func(p *Placement) { p.GithubInfrastructureKeyVersion = "latest" }, wantErr: `githubInfrastructureKeyVersion "latest" is not a secret version's number (digits, never latest)`},
		{name: "no deployer app yet", mutate: func(p *Placement) { p.GithubDeployerAppID, p.GithubDeployerKeyVersion = "", "" }},
		{name: "a deployer app named by slug", mutate: func(p *Placement) { p.GithubDeployerAppID = "imp-deployer" }, wantErr: `githubDeployerAppId "imp-deployer" is not an App ID (digits)`},
		{name: "a deployer key version of latest", mutate: func(p *Placement) { p.GithubDeployerKeyVersion = "latest" }, wantErr: `githubDeployerKeyVersion "latest" is not a secret version's number (digits, never latest)`},
		{name: "no Cloud Build authorization yet", mutate: func(p *Placement) { p.CloudBuildInstallationID, p.CloudBuildTokenSecretVersion = "", "" }},
		{name: "an installation id that is not one", mutate: func(p *Placement) { p.CloudBuildInstallationID = "imp" }, wantErr: `cloudBuildInstallationId "imp" is not an installation id (digits, from the installation's settings page)`},
		{name: "a token secret version named by latest", mutate: func(p *Placement) {
			p.CloudBuildTokenSecretVersion = "projects/imp-boot-gbl-core-a1b2/locations/us-central1/secrets/github-token/versions/latest"
		}, wantErr: `is not a secret version's resource name (projects/<project>[/locations/<region>]/secrets/<secret>/versions/<number>, a number and never latest)`},
		{name: "a token secret version that is a number alone", mutate: func(p *Placement) { p.CloudBuildTokenSecretVersion = "1" }, wantErr: `cloudBuildTokenSecretVersion "1" is not a secret version's resource name`},
		{name: "a global token secret version", mutate: func(p *Placement) {
			p.CloudBuildTokenSecretVersion = "projects/imp-boot-gbl-core-a1b2/secrets/github-token/versions/3"
		}},
		{name: "the installation id without the token", mutate: func(p *Placement) { p.CloudBuildTokenSecretVersion = "" }, wantErr: "cloudBuildInstallationId and cloudBuildTokenSecretVersion are recorded together: the browser authorization in the boot project's Cloud Build console gives both (0-bootstrap/README.md, \"The Cloud Build GitHub authorization\")"},
		{name: "the token without the installation id", mutate: func(p *Placement) { p.CloudBuildInstallationID = "" }, wantErr: "cloudBuildInstallationId and cloudBuildTokenSecretVersion are recorded together"},
		{name: "no bedrock pin yet", mutate: func(p *Placement) { p.BedrockVersion, p.BedrockSHA256 = "", "" }},
		{name: "a release pin without its checksum", mutate: func(p *Placement) { p.BedrockSHA256 = "" }, wantErr: "bedrockVersion v0.5.0 is a release, and bedrockSha256 \"\" is not the SHA-256 checksum of its bedrock-linux-amd64: bedrock upgrade v0.5.0 records it from the release's checksums.txt"},
		{name: "a release pin with a short checksum", mutate: func(p *Placement) { p.BedrockSHA256 = "abc123" }, wantErr: `bedrockSha256 "abc123" is not the SHA-256 checksum`},
		{name: "a commit pin", mutate: func(p *Placement) {
			p.BedrockVersion, p.BedrockSHA256 = "v0.0.0-lab.1.0.20260928222237-58b211dce544", ""
		}},
		{name: "a commit pin with a checksum", mutate: func(p *Placement) { p.BedrockVersion = "v0.0.0-lab.1.0.20260928222237-58b211dce544" }, wantErr: "bedrockVersion v0.0.0-lab.1.0.20260928222237-58b211dce544 is a commit pin, which carries no checksum (go install verifies it against Go's checksum database): drop bedrockSha256"},
		{name: "a checksum without a version", mutate: func(p *Placement) { p.BedrockVersion = "" }, wantErr: "bedrockSha256 is recorded without bedrockVersion: bedrock upgrade records both"},
		{name: "a pin that is neither", mutate: func(p *Placement) { p.BedrockVersion, p.BedrockSHA256 = "0.5.0", "" }, wantErr: `bedrockVersion "0.5.0" is neither a release (v0.5.0) nor a commit pin (the pseudo-version the Go module proxy names a commit by): bedrock upgrade records one`},
		{name: "no team groups at all", mutate: func(p *Placement) { p.TeamGroups = nil }, wantErr: "teamGroups.tst is empty: each environment names the group"},
		{name: "no team group for one environment", mutate: func(p *Placement) { delete(p.TeamGroups, "stg") }, wantErr: "teamGroups.stg is empty"},
		{name: "a team group that is a person", mutate: func(p *Placement) { p.TeamGroups["prd"] = "user:someone@imp.example" }, wantErr: `teamGroups.prd "user:someone@imp.example" is a person (user:): an environment's team is a group`},
		{name: "a team group with a member prefix", mutate: func(p *Placement) { p.TeamGroups["tst"] = "group:team-tst@imp.example" }, wantErr: `teamGroups.tst "group:team-tst@imp.example" is not a group's address (name@domain, with no member prefix`},
		{name: "a team group that is not an address", mutate: func(p *Placement) { p.TeamGroups["tst"] = "team-tst" }, wantErr: `teamGroups.tst "team-tst" is not a group's address`},
		{name: "a team group under an environment the model lacks", mutate: func(p *Placement) { p.TeamGroups["qa"] = "team-qa@imp.example" }, wantErr: `teamGroups names "qa", which is not one of tst, stg, prd`},
		{name: "production's group the first environment's", mutate: func(p *Placement) { p.TeamGroups["prd"] = p.TeamGroups["tst"] }, wantErr: "teamGroups.prd is teamGroups.tst (team-tst@imp.example): production's team group is not the first environment's"},
		{name: "one group for tst and stg", mutate: func(p *Placement) { p.TeamGroups["stg"] = p.TeamGroups["tst"] }},
		{name: "no entitlement durations", mutate: func(p *Placement) { p.EntitlementDurations = nil }},
		{name: "every entitlement's duration set", mutate: func(p *Placement) {
			p.EntitlementDurations = map[string]string{"secretOperator": "30m", "spannerAdmin": "90m", "spannerViewer": "168h", "layerAdministrator": "2h"}
		}},
		{name: "a duration for an entitlement the model lacks", mutate: func(p *Placement) { p.EntitlementDurations = map[string]string{"releaseOperator": "1h"} }, wantErr: `entitlementDurations names "releaseOperator", which is not one of secretOperator, spannerAdmin, spannerViewer, layerAdministrator`},
		{name: "a duration that is not one", mutate: func(p *Placement) { p.EntitlementDurations = map[string]string{"secretOperator": "an hour"} }, wantErr: `entitlementDurations.secretOperator "an hour" is not a duration (1h, 90m)`},
		{name: "a duration too short", mutate: func(p *Placement) { p.EntitlementDurations = map[string]string{"secretOperator": "10m"} }, wantErr: "entitlementDurations.secretOperator 10m is outside what Privileged Access Manager admits: between 30m0s and 7 days"},
		{name: "a duration too long", mutate: func(p *Placement) { p.EntitlementDurations = map[string]string{"layerAdministrator": "200h"} }, wantErr: "entitlementDurations.layerAdministrator 200h is outside"},
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
		if *update {
			if err := os.WriteFile(filepath.Join(fixture, filepath.FromSlash(f.Path)), f.Content, 0o600); err != nil {
				t.Fatalf("os.WriteFile(): %v", err)
			}
		}
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

// TestNamesFromPlacement renders the fixture organization and reads back every name a
// rendered file gives an account or an address, which must come from the placement: an
// address is at the organization's domain, at a contact domain, or a service account's;
// a machine account named anywhere is the placement's. A name a template carried as fixed
// text, the account or the domains of the deployment bedrock was first tried in, would be
// none of these.
func TestNamesFromPlacement(t *testing.T) {
	t.Parallel()

	p := testPlacement(t)
	files, err := Render(p)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	tests := []struct {
		name string
		// pattern captures the name in its first group; fromPlacement reports whether
		// the placement gives it; atLeast is how many the render carries.
		pattern       *regexp.Regexp
		fromPlacement func(name string) bool
		atLeast       int
	}{
		{
			name:    "every address is the organization's, a contact domain's or a service account's",
			pattern: regexp.MustCompile(`@([A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)+)`),
			fromPlacement: func(domain string) bool {
				return domain == p.OrganizationDomain || slices.Contains(p.ContactDomains, "@"+domain) || strings.HasSuffix(domain, ".iam.gserviceaccount.com")
			},
			atLeast: 10,
		},
		{
			name:    "every machine account named is the placement's",
			pattern: regexp.MustCompile("machine account,?\\s+`?([A-Za-z0-9][A-Za-z0-9-]*)"),
			fromPlacement: func(login string) bool {
				return login == p.GithubMachineAccount
			},
			atLeast: 4,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			found := 0
			for _, f := range files {
				for _, m := range tt.pattern.FindAllStringSubmatch(string(f.Content), -1) {
					found++
					if !tt.fromPlacement(m[1]) {
						t.Errorf("%s names %q, which the placement does not give", f.Path, m[1])
					}
				}
			}
			if found < tt.atLeast {
				t.Errorf("the render names %d, want at least %d", found, tt.atLeast)
			}
		})
	}
}

// TestMachineAccount pins the organization's machine account where the GitHub
// authorization names it: the browser step and the personal access token in 2-env's
// README, the connection's comment and the token variable's description.
func TestMachineAccount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want []string
	}{
		{
			name: "0-bootstrap's README: the browser step in the boot project, by the bootstrap administrator as the machine account, and the token",
			path: "0-bootstrap/README.md",
			want: []string{
				"### 2. The Cloud Build GitHub authorization\n",
				"The person at the browser is the bootstrap\nadministrator, who is Owner on the boot project from step 1; the GitHub\naccount signed in is the organization's machine account,\n`imp-machine` (`githubMachineAccount` in `placement.json`)",
				"belongs to the `imp-example`\norganization and is an owner of it",
				"Sign in to GitHub as\n   the machine account `imp-machine`, install or select",
				"Record both in `placement.json`, as `cloudBuildInstallationId` and\n   `cloudBuildTokenSecretVersion`",
				"fine-grained personal access\ntoken of `imp-machine` (contents, metadata, pull requests)",
			},
		},
		{
			name: "2-env's README points at it",
			path: "2-env/README.md",
			want: []string{"## The GitHub authorization\n", "before the first apply of `0-bootstrap` (`0-bootstrap/README.md`, \"2. The\nCloud Build GitHub authorization\")", "signed in to GitHub as the organization's machine account\n`imp-machine` (`githubMachineAccount` in `placement.json`)"},
		},
		{name: "the environment connection's comment", path: "2-env/cloud-build.tf", want: []string{"machine account imp-machine, before the first apply of 0-bootstrap"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := renderedFile(t, tt.path)
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("%s lacks %q", tt.path, want)
				}
			}
		})
	}
}

// TestBootstrapRolesNamed reads the READMEs that tell the bootstrap administrator what to
// hold: each names every role of org preflight's table, by title and id, and the
// preflight command, so a role added to the check cannot be missing from the steps; the
// four-role list the check replaced is gone.
func TestBootstrapRolesNamed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		path   string
		want   []string
		absent []string
	}{
		{
			name: "0-bootstrap's seed step: the role table, then preflight before the seed",
			path: "0-bootstrap/README.md",
			want: []string{
				"| Role | Granted on | Needed for |\n",
				"gcloud auth application-default login\nbedrock org preflight\n\nexport ORG_ID=123456789012\n",
			},
			absent: []string{"who holds\nOrganization Administrator, Project Creator, and Organization Policy\nAdministrator"},
		},
		{
			name: "the root README's seed step",
			path: "README.md",
			want: []string{"3. **Seed.** In a terminal, as the bootstrap administrator: `bedrock org\n   preflight` first, which checks that the person holds each of these roles:\n"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := renderedFile(t, tt.path)
			want := slices.Clone(tt.want)
			for _, role := range BootstrapRoles {
				want = append(want, role.Title+" (`"+role.Role+"`)")
			}
			for _, w := range want {
				if !strings.Contains(got, w) {
					t.Errorf("%s lacks %q", tt.path, w)
				}
			}
			for _, a := range tt.absent {
				if strings.Contains(got, a) {
					t.Errorf("%s still carries %q", tt.path, a)
				}
			}
		})
	}
}

// TestGithubAppSteps reads the steps for the organization's GitHub Apps: the root README
// gives the release and deployer apps' permissions, their installation, the release
// app's two organization secrets and the deployer key's commands; 0-bootstrap's README
// and 2-env's add and pin each key with bedrock secret, never by a gcloud command or an
// edit of the pin by hand.
func TestGithubAppSteps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		path   string
		want   []string
		absent []string
	}{
		{
			name: "the root README: the two apps' permissions, installation and secrets",
			path: "README.md",
			want: []string{
				"## The two GitHub Apps\n",
				"   - the release app: Contents, Issues and Pull requests, each read and\n     write;\n",
				"   - the deployer app: Checks, Deployments, Issues and Pull requests, each\n     read and write.\n",
				"Install App, on the organization,\n   for all repositories",
				"`RELEASE_APP_ID` holds the release app's App ID and\n   `RELEASE_APP_PRIVATE_KEY` the whole of its `.pem` file",
				"   bedrock secret add github-deployer-key --from-file <the .pem file>\n   bedrock secret pin github-deployer-key <version>\n",
				"([GitHub prerequisites](https://github.com/cccteam/ccc/blob/master/bedrock/README.md#github-prerequisites))",
			},
		},
		{
			name: "the root README: the consent screen and the role groups",
			path: "README.md",
			want: []string{
				"9. **The consent screen.** In each environment project's Google Cloud\n   console, before the first application's OAuth client is made there",
				"with the audience **Internal**",
				"`<group prefix><role>@imp.example`",
				"set \"Who can view members\" so that the group's members can",
			},
		},
		{
			name: "0-bootstrap: the infrastructure key added and pinned with bedrock secret, the budget beside the billing grants",
			path: "0-bootstrap/README.md",
			want: []string{
				"   bedrock secret add github-infrastructure-key --from-file <the .pem file>\n   bedrock secret pin github-infrastructure-key <version>\n",
				"### 5. Billing grants and the budget, by a billing administrator\n",
				"In the Billing console, as the same billing administrator, set the spend\nbudget",
				"## The deployer GitHub App's key\n",
				"   bedrock secret add github-deployer-key --from-file <the .pem file>\n   bedrock secret pin github-deployer-key <version>\n",
				"records it as `githubDeployerKeyVersion` in `placement.json`",
			},
			absent: []string{"gcloud secrets versions add", "github-deployer-key <env>"},
		},
		{
			name: "2-env: the deployer key is 0-bootstrap's, added and pinned there",
			path: "2-env/README.md",
			want: []string{
				"## The deployer GitHub App's key\n",
				"added with\n`bedrock secret add github-deployer-key` and pinned with `bedrock secret pin\ngithub-deployer-key <version>` (`0-bootstrap/README.md`, \"The deployer GitHub\nApp's key\")",
				"`imp-<env>-gbl-github-deployer-key`, held\na copy of the key pinned per environment; a `removed` block destroys it with\nits versions on the first apply of this render, and the grants move to the\nboot container.",
			},
			absent: []string{"gcloud secrets versions add", "github-deployer-key <env>", "github_deployer_key_secret_versions"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := renderedFile(t, tt.path)
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("%s lacks %q", tt.path, w)
				}
			}
			for _, a := range tt.absent {
				if strings.Contains(got, a) {
					t.Errorf("%s still carries %q", tt.path, a)
				}
			}
		})
	}
}

// TestDeployerKeyGrants reads 2-env's grants on the deployer GitHub App's key: the one
// container is 0-bootstrap's in the boot project, named by locals.tf and read by every
// application's deploy identity and in tst by its apply identity; the container an earlier
// bedrock made in the environment project is removed and destroyed; and the outputs carry
// the App ID and the pinned version's resource name from placement.json, with no retired
// tfvars value read anywhere.
// retiredAttributes is every 2-env/terraform.tfvars attribute org check reports as
// retired, whose value placement.json carries now.
func retiredAttributes() []string {
	names := make([]string, 0, len(retiredValues))
	for _, v := range retiredValues {
		names = append(names, v.attribute)
	}

	return names
}

func TestDeployerKeyGrants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		path   string
		want   []string
		absent []string
	}{
		{
			name: "the environment project's container is removed and destroyed",
			path: "2-env/github-apps.tf",
			want: []string{
				"removed {\n  from = google_secret_manager_secret.github_deployer_key\n\n  lifecycle {\n    destroy = true\n  }\n}\n",
			},
			absent: []string{"resource \"google_secret_manager_secret\" \"github_deployer_key\"", "local.project_id", "var.github_deployer"},
		},
		{
			name: "the deploy identities and tst's apply identities read the boot container",
			path: "2-env/github-apps.tf",
			want: []string{
				"resource \"google_secret_manager_secret_iam_member\" \"deploy_deployer_key\" {\n  for_each = local.apps\n\n  project   = var.boot_project_id\n  secret_id = local.github_deployer_key_secret\n  role      = \"roles/secretmanager.secretAccessor\"\n  member    = google_service_account.deploy[each.key].member\n}\n",
				"resource \"google_secret_manager_secret_iam_member\" \"apply_deployer_key\" {\n  for_each = local.is_tst ? local.apps : toset([])\n\n  project   = var.boot_project_id\n  secret_id = local.github_deployer_key_secret\n  role      = \"roles/secretmanager.secretAccessor\"\n  member    = google_service_account.apply[each.key].member\n}\n",
			},
		},
		{
			name: "locals.tf names the boot container and carries the pin from placement.json",
			path: "2-env/locals.tf",
			want: []string{
				"  github_deployer_key_secret         = \"${local.prefix}-boot-gbl-github-deployer-key\"\n",
				"  github_deployer_key_secret_version = \"projects/imp-boot-gbl-core-a1b2/secrets/imp-boot-gbl-github-deployer-key/versions/1\"\n",
			},
			absent: []string{"var.github_deployer_app_id", "var.github_deployer_key_secret_versions"},
		},
		{
			name: "the outputs carry the App ID and the pinned version's resource name",
			path: "2-env/outputs.tf",
			want: []string{
				"output \"github_deployer_app_id\" {",
				"  value       = local.github_deployer_app_id\n",
				"output \"github_deployer_key_secret_version\" {",
				"  value       = local.github_deployer_key_secret_version\n",
			},
			absent: []string{"var.github_"},
		},
		{
			name:   "no value org check reports as retired is declared",
			path:   "2-env/variables.tf",
			absent: retiredAttributes(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := renderedFile(t, tt.path)
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("%s lacks %q", tt.path, w)
				}
			}
			for _, a := range tt.absent {
				if strings.Contains(got, a) {
					t.Errorf("%s still carries %q", tt.path, a)
				}
			}
		})
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
		{name: "the triggers' file is owned", path: "0-bootstrap/cloud-build.tf", want: render.Owned},
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
// TestRecordsBucketPolicy reads the records bucket's policy, which 2-env sets whole: one
// binding per role, the deploy identities' create and read, the plan identities' read (the
// hotfix preview), the next environment's deploy identities' read (the record gate) and
// the team group's read, no binding for a role nobody holds, and no member resource left
// on the bucket beside the policy.
func TestRecordsBucketPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		path   string
		want   []string
		absent []string
	}{
		{
			name: "the bindings, from the identities and the team group, and the policy",
			path: "2-env/records.tf",
			want: []string{
				"  records_deploy_members = [for app in var.applications : google_service_account.deploy[app].member]\n  records_plan_members   = [for app in var.applications : google_service_account.plan[app].member]\n",
				"      { role = \"roles/storage.objectCreator\", members = local.records_deploy_members, condition = local.records_creator_condition },\n",
				"      { role = \"roles/storage.objectViewer\", members = concat(local.records_deploy_members, local.records_plan_members, local.records_ops_members, local.records_lower_ops_members, local.records_rehearsal_members, values(local.next_deploy_members), [local.team_group]), condition = null },\n",
				// In tst the stack jobs' outputs are the apply identities' to write, under
				// their prefix alone, which the deploy identities' creator binding excludes.
				"  records_stack_prefix = local.is_tst ? { for app in var.applications : app => \"projects/_/buckets/${google_storage_bucket.records.name}/objects/${app}/stacks/\" } : {}\n",
				"    expression  = join(\" && \", [for app, prefix in local.records_stack_prefix : \"!resource.name.startsWith(\\\"${prefix}\\\")\"])\n",
				"      role    = \"roles/storage.objectUser\"\n      members = [google_service_account.apply[app].member]\n",
				"  ], local.records_stack_bindings)\n",
				// In production, the lower environments' operations identities read the records
				// for a restore from production's backup that names no release.
				"  records_lower_ops_members = local.next_environment != \"\" ? [] : [\n    for pair in setproduct([for env, next in var.next_environment : env if next != \"\"], var.applications) :\n    \"serviceAccount:${local.prefix}-${pair[0]}-gbl-${pair[1]}-ops@${local.org.project_ids[pair[0]]}.iam.gserviceaccount.com\"\n  ]\n",
				"    ] : b if length(b.members) > 0\n",
				"data \"google_iam_policy\" \"records\" {\n  dynamic \"binding\" {\n    for_each = local.records_bindings\n    content {\n      role    = binding.value.role\n      members = binding.value.members\n\n      dynamic \"condition\" {\n        for_each = binding.value.condition == null ? [] : [binding.value.condition]\n",
				"resource \"google_storage_bucket_iam_policy\" \"records\" {\n  bucket      = google_storage_bucket.records.name\n  policy_data = data.google_iam_policy.records.policy_data\n}\n",
				"Cloud Storage's default grants to the project's basic roles (projectOwner,",
				// The member resources the policy replaced leave the state without a destroy,
				// which would take their members out of the live policy.
				"removed {\n  from = google_storage_bucket_iam_member.deploy_records\n\n  lifecycle {\n    destroy = false\n  }\n}\n",
				"removed {\n  from = google_storage_bucket_iam_member.deploy_records_viewer\n\n  lifecycle {\n    destroy = false\n  }\n}\n",
				"removed {\n  from = google_storage_bucket_iam_member.plan_records\n\n  lifecycle {\n    destroy = false\n  }\n}\n",
				"removed {\n  from = google_storage_bucket_iam_member.next_deploy_records_viewer\n\n  lifecycle {\n    destroy = false\n  }\n}\n",
				"Carried for one bedrock release, so an organization that\n# moves to the policy applies this layer once; dropped in the next release",
			},
			absent: []string{"resource \"google_storage_bucket_iam_member\"", "builder role"},
		},
		{
			name:   "no member resource on the bucket beside the policy",
			path:   "2-env/identities.tf",
			want:   []string{"bindings of the records\n# bucket's policy (records.tf)"},
			absent: []string{"\"deploy_records\"", "\"deploy_records_viewer\"", "\"plan_records\"", "\"next_deploy_records_viewer\"", "bucket = google_storage_bucket.records.name"},
		},
		{
			name: "the README says who reads a record and what a hand grant's fate is",
			path: "2-env/README.md",
			want: []string{
				"the environment's team group (a person\n  reads a record through the group)",
				"in production the lower\n  environments' operations identities too (a restore from production's\n  backup reads production's live release)",
				"A grant added on the\n  bucket by hand, for a day's debugging, is removed by this layer's next\n  apply; a grant added on the project is not",
				"applies this layer once to move to the policy",
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
			for _, a := range tt.absent {
				if strings.Contains(content, a) {
					t.Errorf("%s still carries %q", tt.path, a)
				}
			}
		})
	}
}

// TestStorageAdminBounds pins each apply identity's Cloud Storage grants: an application's
// storage admin from 2-env under a condition naming its own buckets (its file stores and
// its pull-request stacks') and storageBucketCreator without condition, with no
// unconditioned storage admin left in its role set; and the environment layer identity's
// storage admin from 1-org under a condition naming the records bucket, the app role set
// carrying storageBucketCreator in its place while the other sets keep storage admin.
func TestStorageAdminBounds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		// section narrows the file to the text between its two markers, when set.
		section [2]string
		want    []string
		absent  []string
	}{
		{
			name: "an application's apply identity: its own buckets by name, the creator role beside",
			path: "2-env/identities.tf",
			want: []string{
				"resource \"google_project_iam_member\" \"apply_bucket_creator\" {\n  for_each = local.apps\n\n  project = local.project_id\n  role    = local.org.storage_bucket_creator_role\n  member  = google_service_account.apply[each.key].member\n}\n",
				"resource \"google_project_iam_member\" \"apply_storage_admin\" {\n  for_each = local.apps\n\n  project = local.project_id\n  role    = \"roles/storage.admin\"\n  member  = google_service_account.apply[each.key].member\n\n  condition {\n    title       = \"${each.key} ${var.environment} buckets\"\n",
				`expression  = join(" || ", [for code in local.files_bucket_codes : "resource.name.startsWith(\"projects/_/buckets/${local.name}-${code}-${each.key}-\")"])`,
				"resource \"google_project_iam_member\" \"apply_security_policy_admin\" {\n  for_each = local.apps\n\n  project = local.project_id\n  role    = \"roles/compute.securityAdmin\"\n  member  = google_service_account.apply[each.key].member\n\n  condition {\n    title       = \"${each.key} ${var.environment} security policies\"\n",
				`expression  = "resource.name.startsWith(\"projects/${local.project_id}/global/securityPolicies/${local.name}-gbl-${each.key}-\")"`,
			},
		},
		{
			name: "the location names the bucket grant admits",
			path: "2-env/locals.tf",
			want: []string{
				`files_location_code = local.instance_config == null ? null : (startswith(local.instance_config, "regional-") ? lookup({ (local.region) = local.region_code, (local.secondary_region) = local.secondary_region_code }, trimprefix(local.instance_config, "regional-"), null) : local.instance_config)`,
				`files_bucket_codes  = distinct(compact(["gbl", local.files_location_code]))`,
			},
		},
		{
			name:    "no unconditioned storage admin in the application apply identity's role set",
			path:    "2-env/locals.tf",
			section: [2]string{"  apply_project_roles = [\n", "  ]\n"},
			want:    []string{"\"roles/run.admin\",\n"},
			absent:  []string{"roles/storage.admin"},
		},
		{
			name: "the environment layer identity: the records bucket by name",
			path: "1-org/service-accounts.tf",
			want: []string{
				"resource \"google_project_iam_member\" \"tofu_storage_admin\" {\n  for_each = local.environment_layers\n\n  project = module.project[each.key].project_id\n  role    = \"roles/storage.admin\"\n  member  = google_service_account.tofu[each.key].member\n\n  condition {\n    title       = \"${each.key} records bucket\"\n",
				`records_bucket_conditions = { for k, v in local.environment_layers : k => "resource.name.startsWith(\"projects/_/buckets/${local.layer_names[k]}-records-\")" }`,
				"    expression  = local.records_bucket_conditions[each.key]\n",
			},
		},
		{
			name:    "the app role set carries the creator role and no storage admin",
			path:    "1-org/variables.tf",
			section: [2]string{"    app = [\n", "    ]\n"},
			want:    []string{"      \"secretContainerAdmin\",\n", "      \"storageBucketCreator\",\n"},
			absent:  []string{"roles/storage.admin"},
		},
		{
			name:    "the shared sets keep storage admin",
			path:    "1-org/variables.tf",
			section: [2]string{"    shr = [\n", "    ]\n"},
			want:    []string{"      \"roles/storage.admin\",\n"},
		},
		{
			name: "1-org resolves the creator role by bare id and publishes it",
			path: "1-org/locals.tf",
			want: []string{"    storageBucketCreator       = google_organization_iam_custom_role.storage_bucket_creator.id\n"},
		},
		{
			name: "the output",
			path: "1-org/outputs.tf",
			want: []string{"output \"storage_bucket_creator_role\" {", "  value       = google_organization_iam_custom_role.storage_bucket_creator.id\n"},
		},
		{
			name: "the READMEs name what a bucket's name cannot bound",
			path: "1-org/README.md",
			want: []string{"`storage.buckets.create`,\n  `storage.buckets.list`), the two of `roles/storage.admin`'s permissions that\n  Cloud Storage checks on the project"},
		},
		{
			name: "the 2-env README says which buckets the condition admits",
			path: "2-env/README.md",
			want: []string{"`imp-<env>-<location>-<app>-`, where the location is where the\ndatabase is", "which is the application's file stores", "Without the condition, any application's\napply identity, and so its pipeline and its pull-request builds, would read\nand delete every other application's files and every record."},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			content := renderedFile(t, tt.path)
			if tt.section[0] != "" {
				_, rest, found := strings.Cut(content, tt.section[0])
				if !found {
					t.Fatalf("%s lacks %q", tt.path, tt.section[0])
				}
				content, _, _ = strings.Cut(rest, tt.section[1])
			}
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
			name: "the roles a build needs, and no trigger runner: the sweep is the apply identity's",
			text: "  deploy_project_roles = [\n" +
				"    \"roles/serviceusage.serviceUsageConsumer\",\n" +
				"    \"roles/run.developer\",\n" +
				"    \"roles/logging.logWriter\",\n" +
				"    \"roles/monitoring.viewer\",\n" +
				"    local.org.run_job_policy_admin_role,\n" +
				"    local.org.cloud_build_build_reader_role,\n" +
				"  ]\n",
		},
		{name: "no trigger runner for the deploy identity", text: "local.org.cloud_build_trigger_runner_role] : []", absent: true},
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
				"cloudbuild.builds.create", "cloudbuild.builds.get", "cloudbuild.builds.list", "cloudbuild.builds.update",
				"cloudbuild.triggers.get", "cloudbuild.triggers.list",
			},
		},
		{
			name:     "adds a secret: creating and adding, the project's read and the use of its services, never a payload",
			resource: "secret_operator",
			roleID:   "secretOperator",
			permissions: []string{
				"resourcemanager.projects.get",
				"secretmanager.secrets.create", "secretmanager.secrets.get", "secretmanager.secrets.list",
				"secretmanager.versions.add", "secretmanager.versions.get", "secretmanager.versions.list",
				"serviceusage.services.use",
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
			name:        "creates a bucket in a project: what Cloud Storage checks on the project, and nothing of a bucket",
			resource:    "storage_bucket_creator",
			roleID:      "storageBucketCreator",
			permissions: []string{"storage.buckets.create", "storage.buckets.list"},
		},
		{
			name:     "plans an application stack",
			resource: "application_plan_reader",
			roleID:   "applicationPlanReader",
			permissions: []string{
				"cloudbuild.builds.get", "cloudscheduler.jobs.get", "cloudtasks.queues.get",
				"compute.backendServices.get", "compute.regionNetworkEndpointGroups.get", "compute.securityPolicies.get",
				"datastore.databases.getMetadata", "datastore.indexes.get",
				"firebaserules.releases.get", "firebaserules.rulesets.get",
				"logging.buckets.get", "logging.sinks.get",
				"run.jobs.get", "run.services.get", "run.services.listTagBindings",
				"secretmanager.secrets.get", "secretmanager.versions.get",
				"apikeys.keys.get", "apikeys.keys.getKeyString", "storage.buckets.get",
			},
		},
		{
			name:     "plans the environment layer",
			resource: "environment_layer_plan_reader",
			roleID:   "environmentLayerPlanReader",
			permissions: []string{
				"cloudbuild.connections.get", "cloudbuild.repositories.get",
				"compute.backendServices.get", "compute.regionNetworkEndpointGroups.get",
				"firebaseauth.configs.get",
				"iam.workloadIdentityPoolProviders.get",
				"iam.workloadIdentityPools.get", "iam.workloadIdentityPools.getAttestationRules",
				"privilegedaccessmanager.entitlements.get",
				"secretmanager.secrets.get", "spanner.instances.get", "storage.buckets.get",
			},
		},
		{
			name:        "plans the shared services layer",
			resource:    "services_layer_plan_reader",
			roleID:      "servicesLayerPlanReader",
			permissions: []string{"artifactregistry.repositories.get"},
		},
		{
			name:     "plans the shared network layer",
			resource: "network_layer_plan_reader",
			roleID:   "networkLayerPlanReader",
			permissions: []string{
				"certificatemanager.certmapentries.get", "certificatemanager.certmaps.get",
				"certificatemanager.certs.get", "certificatemanager.dnsauthorizations.get",
				"compute.backendServices.get", "compute.globalAddresses.get",
				"compute.globalForwardingRules.get", "compute.sslPolicies.get",
				"compute.targetHttpProxies.get", "compute.targetHttpsProxies.get",
				"compute.urlMaps.get", "dns.managedZones.get",
			},
		},
		{
			name:        "plans the shared Spanner layer",
			resource:    "spanner_layer_plan_reader",
			roleID:      "spannerLayerPlanReader",
			permissions: []string{"privilegedaccessmanager.entitlements.get", "spanner.instances.get"},
		},
		{
			name:        "reads a bucket's policy",
			resource:    "bucket_policy_reader",
			roleID:      "bucketPolicyReader",
			permissions: []string{"storage.buckets.getIamPolicy"},
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

// TestLayerPlanRoles pins each layer plan identity's roles on its project, by role set: the
// custom role of the project's kind and securityReviewer, granted by the same flatten over
// the role sets as the layer identities' roles with the bare ID resolved through
// local.custom_roles, and no roles/viewer anywhere in 1-org (the bundle reads the rows of
// every database in the project, every container image, and the objects of any bucket
// that still carries Cloud Storage's default grants to the project's basic roles).
func TestLayerPlanRoles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		path   string
		want   []string
		absent []string
	}{
		{
			name: "the environment layer's plan roles",
			path: "1-org/variables.tf",
			want: []string{"    app = [\n      \"environmentLayerPlanReader\",\n      \"roles/iam.securityReviewer\",\n    ]\n"},
		},
		{
			name: "the shared network layer's plan roles",
			path: "1-org/variables.tf",
			want: []string{"    net = [\n      \"networkLayerPlanReader\",\n      \"roles/iam.securityReviewer\",\n", "      \"roles/domains.viewer\",\n    ]\n"},
		},
		{
			name: "the shared services layer's plan roles",
			path: "1-org/variables.tf",
			want: []string{"    shr = [\n      \"servicesLayerPlanReader\",\n      \"roles/iam.securityReviewer\",\n    ]\n"},
		},
		{
			name: "the shared Spanner layer's plan roles",
			path: "1-org/variables.tf",
			want: []string{"    spn = [\n      \"spannerLayerPlanReader\",\n      \"roles/iam.securityReviewer\",\n    ]\n"},
		},
		{
			name: "the grant runs over the plan role sets and resolves the custom roles",
			path: "1-org/service-accounts.tf",
			want: []string{
				"resource \"google_project_iam_member\" \"plan\" {\n  for_each = {\n    for pair in flatten([\n      for key, cfg in local.layers : [\n        for role in var.plan_roles[cfg.role_set] : {\n",
				"  role    = lookup(local.custom_roles, each.value.role, each.value.role)\n  member  = google_service_account.plan[each.value.proj].member\n",
			},
			absent: []string{"\"roles/viewer\"", "\"roles/browser\""},
		},
		{
			name: "the custom roles resolve by bare ID",
			path: "1-org/locals.tf",
			want: []string{
				"    environmentLayerPlanReader = google_organization_iam_custom_role.environment_layer_plan_reader.id\n",
				"    servicesLayerPlanReader    = google_organization_iam_custom_role.services_layer_plan_reader.id\n",
				"    networkLayerPlanReader     = google_organization_iam_custom_role.network_layer_plan_reader.id\n",
				"    spannerLayerPlanReader     = google_organization_iam_custom_role.spanner_layer_plan_reader.id\n",
			},
		},
		{
			name:   "no plan role set carries the viewer bundle",
			path:   "1-org/variables.tf",
			absent: []string{"\"roles/viewer\""},
		},
		{
			name:   "the README says what the plan identities hold and why not viewer",
			path:   "1-org/README.md",
			want:   []string{"`roles/viewer` is not\n  among them", "| `app` | `environmentLayerPlanReader`, iam.securityReviewer |"},
			absent: []string{"holding `roles/viewer`"},
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

// TestSpannerGrants reads the Spanner grants 2-spn and 2-env render: each application's
// apply identity holds the organization's creator role on its instance without condition
// and the admin roles under a condition naming its own database and backups; the restore
// right reaches production's backups alone, from every environment but production; on the
// shared instance's project the apply identity may grant the metric writer role, for its
// stack's Spanner clients, and no other, and the plan identity reads the project's policy;
// and the deploy identity holds nothing on an instance.
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
				"resource \"google_spanner_instance_iam_member\" \"restore_admin\" {\n  for_each = var.database_admins\n",
				`title       = "${each.value.application} ${each.value.environment} restores"`,
				`expression  = each.value.restore_from != "" ? "resource.name.startsWith(\"${local.own_backups[each.key]}\") || resource.name.startsWith(\"${local.instance_path}/backups/${local.prefix}-${each.value.restore_from}-gbl-${each.value.application}-\")" : "resource.name.startsWith(\"${local.own_backups[each.key]}\")"`,
			},
			absent: []string{`m => v if v.restore_from != ""`},
		},
		{
			name: "the environment's own instance lets each apply identity restore its own backups, for a rollback",
			path: "2-env/identities.tf",
			want: []string{
				"resource \"google_spanner_instance_iam_member\" \"apply_restore_admin\" {\n  for_each = { for app in var.applications : app => app if local.own_instance }\n",
				`role     = "roles/spanner.restoreAdmin"`,
				`expression  = "resource.name.startsWith(\"${local.instance_path}/backups/${local.name}-gbl-${each.key}-\")"`,
			},
		},
		{
			name: "the operations identities read the deployment records, for a rollback's defaults",
			path: "2-env/records.tf",
			want: []string{
				`records_ops_members = [for app in var.applications : google_service_account.operations[app].member]`,
				`concat(local.records_deploy_members, local.records_plan_members, local.records_ops_members, local.records_lower_ops_members, local.records_rehearsal_members, values(local.next_deploy_members), [local.team_group])`,
			},
		},
		{
			name: "the trigger runner cancels a build left waiting",
			path: "1-org/custom-roles.tf",
			want: []string{"    \"cloudbuild.builds.list\",\n    \"cloudbuild.builds.update\",\n"},
		},
		{
			name: "the shared instance's apply identities may grant the metric writer role on its project and no other, and its plan identities read the project's policy",
			path: "2-spn/spanner.tf",
			want: []string{
				"resource \"google_project_iam_member\" \"metric_writer_granter\" {\n" +
					"  for_each = var.database_admins\n\n" +
					"  project = local.project_id\n" +
					"  role    = \"roles/resourcemanager.projectIamAdmin\"\n" +
					"  member  = each.key\n\n" +
					"  condition {\n",
				`expression  = "api.getAttribute('iam.googleapis.com/modifiedGrantsByRole', []).hasOnly(['roles/monitoring.metricWriter'])"`,
				"resource \"google_project_iam_member\" \"plan_policy_reader\" {\n" +
					"  for_each = toset(var.database_planners)\n\n" +
					"  project = local.project_id\n" +
					"  role    = \"roles/iam.securityReviewer\"\n",
			},
		},
		{
			name: "the 2-spn README says what the metric writer grants are for and how they are bounded",
			path: "2-spn/README.md",
			want: []string{
				"`roles/resourcemanager.projectIamAdmin` on the `spn`\n  project under a condition that admits a change to the grants of\n  `roles/monitoring.metricWriter` and of no other role",
				"and `roles/iam.securityReviewer` on the project",
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

// TestTeamGroup reads what the team group gets: the release approval in the approval
// environments and not the first, the entitlements with the group eligible, one approval
// by the group with the approver's justification in the approval environments and none in
// the first, the longest grants from the placement with the defaults where it sets none,
// the Spanner entitlements on the project holding the environment's instance (2-env for an
// own instance, 2-spn bounded to the environment's databases for the shared one), the
// layer administrator's bindings from what 1-org publishes of the layer identity's grants
// (TestLayerAdministratorBindings evaluates them), and no standing grant to a person
// anywhere.
func TestTeamGroup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		path   string
		want   []string
		absent []string
	}{
		{
			name: "the release approval, in the approval environments alone",
			path: "2-env/team-group.tf",
			want: []string{
				"  approval_environments = [\"stg\", \"prd\"]\n  approval_required     = contains(local.approval_environments, var.environment)\n",
				"resource \"google_project_iam_member\" \"release_approver\" {\n  count = local.approval_required ? 1 : 0\n\n  project = local.project_id\n  role    = \"roles/cloudbuild.builds.approver\"\n  member  = local.team_group\n}\n",
				"  team_group_address = var.team_groups[var.environment]\n  team_group         = \"group:${local.team_group_address}\"\n",
			},
		},
		{
			name: "the entitlements: the group eligible, the approval step where a release waits for one, the justifications",
			path: "2-env/team-group.tf",
			want: []string{
				"resource \"google_privileged_access_manager_entitlement\" \"team\" {\n  for_each = local.entitlements\n\n  entitlement_id       = \"${local.name}-${each.key}\"\n  location             = \"global\"\n  parent               = \"projects/${local.project_id}\"\n  max_request_duration = each.value.duration\n\n  eligible_users {\n    principals = [local.team_group]\n  }\n",
				"  requester_justification_config {\n    unstructured {}\n  }\n",
				"  dynamic \"approval_workflow\" {\n    for_each = local.approval_required ? [1] : []\n    content {\n      manual_approvals {\n        require_approver_justification = true\n\n        steps {\n          approvals_needed          = 1\n          approver_email_recipients = [local.team_group_address]\n\n          approvers {\n            principals = [local.team_group]\n          }\n        }\n      }\n    }\n  }\n",
				"          condition_expression = role_bindings.value.condition == \"\" ? null : role_bindings.value.condition\n",
			},
		},
		{
			name: "the service set up on the project before its first entitlement: the organization's service agent holds its role, the entitlements after it",
			path: "2-env/team-group.tf",
			want: []string{
				"resource \"google_project_iam_member\" \"pam_service_agent\" {\n  project = local.project_id\n  role    = \"roles/privilegedaccessmanager.serviceAgent\"\n  member  = \"serviceAccount:service-org-${local.org.org_id}@gcp-sa-pam.iam.gserviceaccount.com\"\n}\n",
				"  depends_on = [google_project_iam_member.pam_service_agent]\n}\n",
			},
			absent: []string{"google_project_service_identity\" \"pam\""},
		},
		{
			name: "the shared instance's project set up the same way before its entitlements",
			path: "2-spn/entitlements.tf",
			want: []string{
				"  member  = \"serviceAccount:service-org-${local.org.org_id}@gcp-sa-pam.iam.gserviceaccount.com\"\n",
				"  role    = \"roles/privilegedaccessmanager.serviceAgent\"\n",
				"  depends_on = [google_project_iam_member.pam_service_agent]\n}\n",
			},
			absent: []string{"google_project_service_identity\" \"pam\""},
		},
		{
			name: "the longest grants: the placement's eight hours for the viewer, the defaults for the rest",
			path: "2-env/team-group.tf",
			want: []string{
				"  entitlement_durations = {\n    secret_operator     = \"3600s\"\n    spanner_admin       = \"7200s\"\n    spanner_viewer      = \"28800s\"\n    layer_administrator = \"14400s\"\n  }\n",
			},
		},
		{
			name: "what each entitlement grants, the Spanner ones for an own instance alone, the layer administrator the layer identity's roles",
			path: "2-env/team-group.tf",
			want: []string{
				"      secret-operator = {\n        declared = true\n        duration = local.entitlement_durations.secret_operator\n        bindings = [{ role = local.org.secret_operator_role, condition = \"\" }]\n      }\n",
				"      spanner-admin = {\n        declared = local.own_instance\n",
				"          { role = \"roles/spanner.databaseAdmin\", condition = \"\" },\n          { role = \"roles/spanner.backupAdmin\", condition = \"\" },\n",
				"      spanner-viewer = {\n        declared = local.own_instance\n",
				"          { role = \"roles/spanner.databaseReader\", condition = \"\" },\n          { role = local.org.spanner_plan_reader_role, condition = \"\" },\n",
				"        bindings = local.layer_administrator_bindings\n",
				"    } : name => e if e.declared\n",
			},
			absent: []string{"user:", "roles/cloudbuild.builds.editor", "roles/owner", "roles/iam.serviceAccountTokenCreator"},
		},
		{
			name:   "the groups are the variable's default, from the placement, and no person is seeded",
			path:   "2-env/variables.tf",
			want:   []string{"variable \"team_groups\" {", "  default = {\n    tst = \"team-tst@imp.example\"\n    stg = \"team-stg@imp.example\"\n    prd = \"team-prd@imp.example\"\n  }\n"},
			absent: []string{"secret_operators", "user:"},
		},
		{
			name:   "no standing secret operator grant and no seeded person",
			path:   "2-env/identities.tf",
			want:   []string{"asks for the secret operator entitlement"},
			absent: []string{"google_project_iam_member\" \"secret_operator\"", "var.secret_operators"},
		},
		{
			name:   "the seeded values name nobody",
			path:   "2-env/terraform.tfvars",
			absent: []string{"secret_operators", "user:"},
		},
		{
			name: "the shared instance's entitlements, per environment on it, bounded to its databases and backups",
			path: "2-spn/entitlements.tf",
			want: []string{
				"  entitled_environments = {\n    stg = { group = \"team-stg@imp.example\", approval = true }\n    prd = { group = \"team-prd@imp.example\", approval = true }\n  }\n",
				"  entitlement_durations = {\n    spanner_admin  = \"7200s\"\n    spanner_viewer = \"28800s\"\n  }\n",
				"  environment_databases = { for env in keys(local.entitled_environments) : env => \"${local.instance_path}/databases/${local.prefix}-${env}-gbl-\" }\n  environment_backups   = { for env in keys(local.entitled_environments) : env => \"${local.instance_path}/backups/${local.prefix}-${env}-gbl-\" }\n",
				"    for env, e in local.entitled_environments : \"${env}-spanner-admin\" => {\n",
				"        { role = \"roles/spanner.databaseAdmin\", condition = \"resource.name.startsWith(\\\"${local.environment_databases[env]}\\\")\" },\n        { role = \"roles/spanner.backupAdmin\", condition = \"resource.name.startsWith(\\\"${local.environment_databases[env]}\\\") || resource.name.startsWith(\\\"${local.environment_backups[env]}\\\")\" },\n        { role = local.org.spanner_plan_reader_role, condition = \"\" },\n",
				"    for env, e in local.entitled_environments : \"${env}-spanner-viewer\" => {\n",
				"        { role = \"roles/spanner.databaseReader\", condition = \"resource.name.startsWith(\\\"${local.environment_databases[env]}\\\")\" },\n",
				"  entitlement_id       = \"${local.prefix}-${each.key}\"\n",
				"  eligible_users {\n    principals = [\"group:${each.value.group}\"]\n  }\n",
				"    for_each = each.value.approval ? [1] : []\n",
				"          approver_email_recipients = [each.value.group]\n",
			},
			absent: []string{"tst = { group", "user:"},
		},
		{
			name:   "1-org publishes the layer identity's grants for the layer administrator, and no unique id for a condition",
			path:   "1-org/outputs.tf",
			want:   []string{"output \"environment_layer_grants\" {"},
			absent: []string{"layer_service_account_unique_ids", "sa.unique_id"},
		},
		{
			name: "the READMEs say what the group holds, where the Spanner entitlements are and whose the other layers' recovery is",
			path: "2-env/README.md",
			want: []string{
				"### The team group",
				"Release approval is the one standing grant: `roles/cloudbuild.builds.approver`\non the environment project in `stg` and `prd`",
				"unset, the secret operator an hour, the Spanner admin two hours, the Spanner viewer four hours and the layer administrator four hours.",
				"so `2-spn`\ndeclares them on that project (`entitlements.tf` there)",
				"recovery is the bootstrap administrator's",
			},
			absent: []string{"### Secret operators", "`var.secret_operators`"},
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

// TestEntitlementPrerequisites pins what creating the entitlements needs from 1-org: the
// admin role in the app and spn role sets and the API in the app and spn API sets.
func TestEntitlementPrerequisites(t *testing.T) {
	t.Parallel()

	variables := renderedFile(t, "1-org/variables.tf")
	tests := []struct {
		name string
		want string
	}{
		{name: "the app role set", want: "      \"roles/monitoring.admin\",\n      # 2-env declares the team group's entitlements on the environment project\n      # (Privileged Access Manager); a starting set, completed by refusal.\n      \"roles/privilegedaccessmanager.admin\",\n      \"roles/resourcemanager.projectIamAdmin\",\n      \"roles/run.admin\",\n"},
		{name: "the spn role set", want: "      \"roles/monitoring.admin\",\n      # 2-spn declares the environments' Spanner entitlements on the spn project\n      # (Privileged Access Manager); a starting set, completed by refusal.\n      \"roles/privilegedaccessmanager.admin\",\n      \"roles/resourcemanager.projectIamAdmin\",\n      \"roles/serviceusage.serviceUsageAdmin\",\n      \"roles/spanner.admin\",\n"},
		{name: "the app API set", want: "      \"privilegedaccessmanager.googleapis.com\",\n      \"run.googleapis.com\",\n"},
		{name: "the spn API set", want: "      \"privilegedaccessmanager.googleapis.com\",\n      \"spanner.googleapis.com\",\n    ]\n  }\n}\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if !strings.Contains(variables, tt.want) {
				t.Errorf("1-org/variables.tf lacks:\n%s", tt.want)
			}
		})
	}
}

// TestEntitlementDuration reads the longest grants as the layers declare them: the
// placement's, in seconds, and the defaults where it sets none.
func TestEntitlementDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		durations map[string]string
		want      map[string]string
	}{
		{
			name: "the defaults",
			want: map[string]string{"secretOperator": "3600s", "spannerAdmin": "7200s", "spannerViewer": "14400s", "layerAdministrator": "14400s"},
		},
		{
			name:      "the placement's where set, the defaults for the rest",
			durations: map[string]string{"secretOperator": "30m", "layerAdministrator": "168h"},
			want:      map[string]string{"secretOperator": "1800s", "spannerAdmin": "7200s", "spannerViewer": "14400s", "layerAdministrator": "604800s"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := Placement{EntitlementDurations: tt.durations}
			for name, want := range tt.want {
				if got := p.EntitlementDuration(name); got != want {
					t.Errorf("EntitlementDuration(%s) = %q, want %q", name, got, want)
				}
			}
		})
	}
}

// TestApprovalAgreement holds the organization's approval environments, where the team
// group holds the release approval, to the rule an application's placement derives its
// own from when it names none: every environment but the first.
func TestApprovalAgreement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want string
	}{
		{name: "the organization's rule", want: "stg,prd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			org := strings.Join((&Placement{}).ApprovalEnvironments(), ",")
			application := strings.Join((&derive.Placement{Environments: Environments}).ApprovalEnvironments(), ",")
			if org != tt.want || application != tt.want {
				t.Errorf("the organization's approval environments are %q and an application's %q, want both %q", org, application, tt.want)
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
		{name: "no contact domains", placement: Placement{OrganizationDomain: "acme.com"}, check: func(v *view) string { return v.ContactDomainsList() + " " + v.ContactDomainsProse() }, want: "[\"@acme.com\"] contact domain `@acme.com`"},
		{name: "one contact domain", placement: Placement{OrganizationDomain: "acme.com", ContactDomains: []string{"@acme.org"}}, check: func(v *view) string { return v.ContactDomainsList() + " " + v.ContactDomainsProse() }, want: "[\"@acme.org\"] contact domain `@acme.org`"},
		{name: "two contact domains", placement: Placement{ContactDomains: []string{"@acme.com", "@partner.example"}}, check: func(v *view) string { return v.ContactDomainsList() + " " + v.ContactDomainsProse() }, want: "[\"@acme.com\", \"@partner.example\"] contact domains `@acme.com` and `@partner.example`"},
		{name: "seed labels", placement: Placement{SourceRepo: "acme-infrastructure", Labels: map[string]string{"team": "core"}}, check: func(v *view) string { return v.SeedLabels() }, want: "terraform=true,terraform_source_path=0-bootstrap,source_repo=acme-infrastructure,environment=boot,team=core"},
		{name: "label prose", placement: Placement{Labels: map[string]string{"team": "core", "cost": "a"}}, check: func(v *view) string { return v.ExtraLabelsProse() + " / " + v.ExtraLabelKeys() }, want: "`cost = \"a\"`, `team = \"core\"` / `cost`, `team`"},
		{name: "the environments, as a list and as prose", placement: Placement{}, check: func(v *view) string { return v.EnvironmentsList() + " " + v.EnvironmentsProse() }, want: "[\"tst\", \"stg\", \"prd\"] `tst`, `stg` and `prd`"},
		{name: "the approval environments, as a list and as prose", placement: Placement{}, check: func(v *view) string { return v.ApprovalEnvironmentsList() + " " + v.ApprovalEnvironmentsProse() }, want: "[\"stg\", \"prd\"] `stg` and `prd`"},
		{name: "the team groups as a variable's default", placement: Placement{TeamGroups: map[string]string{"tst": "a@x.com", "stg": "b@x.com", "prd": "c@x.com"}}, check: func(v *view) string { return v.TeamGroupLines() }, want: "    tst = \"a@x.com\"\n    stg = \"b@x.com\"\n    prd = \"c@x.com\""},
		{name: "the shared instance's environments with their groups and approvals", placement: Placement{TeamGroups: map[string]string{"tst": "a@x.com", "stg": "b@x.com", "prd": "c@x.com"}}, check: func(v *view) string { return v.SharedInstanceEntitlementLines() }, want: "    stg = { group = \"b@x.com\", approval = true }\n    prd = { group = \"c@x.com\", approval = true }"},
		{name: "the default longest grants as prose", placement: Placement{}, check: func(v *view) string { return v.EntitlementDefaultsProse() }, want: "the secret operator an hour, the Spanner admin two hours, the Spanner viewer four hours and the layer administrator four hours"},
		{name: "durations in words", placement: Placement{}, check: func(*view) string {
			return durationWords(time.Hour) + "|" + durationWords(90*time.Minute) + "|" + durationWords(12*time.Hour) + "|" + durationWords(3*time.Hour)
		}, want: "an hour|90 minutes|12 hours|three hours"},
		{name: "impulse's checks: the list, as prose and backticked", placement: Placement{}, check: func(v *view) string {
			return strings.Join(v.ImpulseChecks(), ",") + " / " + v.ImpulseChecksProse() + " / " + v.ImpulseChecksProseQuoted()
		}, want: "title,go,web,image,secrets,migrations / title, go, web, image, secrets and migrations / `title`, `go`, `web`, `image`, `secrets` and `migrations`"},
		{name: "the bucket before the seed", placement: Placement{Prefix: "acme"}, check: func(v *view) string { return v.Bucket() }, want: "acme-boot-gbl-state-REPLACEME"},
		{name: "the bucket after the seed", placement: Placement{Prefix: "acme", StateBucket: "acme-boot-gbl-state-1a2b"}, check: func(v *view) string { return v.Bucket() }, want: "acme-boot-gbl-state-1a2b"},
		{name: "the layer order", placement: Placement{}, check: func(v *view) string { return v.LayerOrderProse() }, want: "0-bootstrap, 1-org, 2-shr, 2-spn and 2-net, then 2-env for tst, stg and prd"},
		{
			name:      "the connection, the repository link and the triggers, named in the primary region",
			placement: Placement{Prefix: "acme", Regions: []Region{{Name: "us-central1", Code: "usc"}, {Name: "us-east1", Code: "use"}}},
			check: func(v *view) string {
				return v.ConnectionName() + " " + v.RepositoryLinkName() + " " + v.PlanTriggerName() + " " + v.ApplyTriggerName()
			},
			want: "acme-boot-usc-github acme-boot-usc-infrastructure-repo acme-boot-usc-layers-plan acme-boot-usc-layers-apply",
		},
		{
			name:      "the boot project, the repository and the two key containers",
			placement: Placement{Prefix: "acme", GithubOrganization: "acme", SourceRepo: "acme-infrastructure", Projects: map[string]string{"boot": "acme-boot-gbl-core-1a2b"}},
			check: func(v *view) string {
				return v.BootProject() + " " + v.InfrastructureRepository() + " " + v.InfrastructureKeySecret() + " " + v.DeployerKeySecret()
			},
			want: "acme-boot-gbl-core-1a2b acme/acme-infrastructure acme-boot-gbl-github-infrastructure-key acme-boot-gbl-github-deployer-key",
		},
		{name: "the boot project before the seed", placement: Placement{Prefix: "acme"}, check: func(v *view) string { return v.BootProject() }, want: "acme-boot-gbl-core-REPLACEME"},
		{
			name:      "the Cloud Build values as HCL before the authorization",
			placement: Placement{Prefix: "acme"},
			check: func(v *view) string {
				return v.CloudBuildInstallationIDHCL() + " " + v.CloudBuildTokenSecretVersionHCL() + " " + v.GithubDeployerAppIDHCL() + " " + v.DeployerKeyVersionNameHCL() + " " + v.EnvironmentProjectNumbersHCL()
			},
			want: "null null null null {}",
		},
		{
			name: "the Cloud Build values as HCL once recorded",
			placement: Placement{
				Prefix: "acme", Projects: map[string]string{"boot": "acme-boot-gbl-core-1a2b"}, ProjectNumbers: map[string]string{"boot": "42", "tst": "43", "prd": "45"},
				CloudBuildInstallationID: "12345678", CloudBuildTokenSecretVersion: "projects/acme-boot-gbl-core-1a2b/locations/us-central1/secrets/github-token/versions/1",
				GithubDeployerAppID: "5080800", GithubDeployerKeyVersion: "3",
			},
			check: func(v *view) string {
				return v.CloudBuildInstallationIDHCL() + " " + v.CloudBuildTokenSecretVersionHCL() + " " + v.GithubDeployerAppIDHCL() + " " + v.DeployerKeyVersionNameHCL() + " " + v.EnvironmentProjectNumbersHCL()
			},
			want: "12345678 \"projects/acme-boot-gbl-core-1a2b/locations/us-central1/secrets/github-token/versions/1\" 5080800 \"projects/acme-boot-gbl-core-1a2b/secrets/acme-boot-gbl-github-deployer-key/versions/3\" {\n    tst = \"43\"\n    prd = \"45\"\n  }",
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
		wantReason   string
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
			name: "a .tf file bedrock does not render is refused",
			mutate: func(t *testing.T, dir string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, "1-org", "extra.tf"), []byte("resource \"google_project_iam_member\" \"extra\" {}\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantFinding: "1-org/extra.tf",
			wantReason:  "not rendered by bedrock: every .tf file of a layer comes from the placement, and a file added by hand would run as the layer's identity; move what it declares into the placement, or remove it",
		},
		{
			name: "a JSON configuration file bedrock does not render is refused too",
			mutate: func(t *testing.T, dir string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, "2-net", "extra.tf.json"), []byte("{}\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantFinding: "2-net/extra.tf.json",
			wantReason:  "not rendered by bedrock",
		},
		{
			name: "an override file is refused",
			mutate: func(t *testing.T, dir string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, "2-env", "identities_override.tf"), []byte("resource \"google_service_account\" \"tofu\" {}\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantFinding: "2-env/identities_override.tf",
			wantReason:  "an override file: OpenTofu merges it into the layer, so it changes what the layer declares without a rendered file changing; the layers hold no override files",
		},
		{
			name: "the plain override file is refused",
			mutate: func(t *testing.T, dir string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, "0-bootstrap", "override.tf"), []byte("terraform {}\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantFinding: "0-bootstrap/override.tf",
			wantReason:  "an override file",
		},
		{
			name: "a person's inputs are allowed",
			mutate: func(t *testing.T, dir string) {
				t.Helper()
				for _, f := range []string{"2-env/.terraform.lock.hcl", "2-env/extra.auto.tfvars", "2-env/notes.md"} {
					if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(f)), []byte("# a person's\n"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			},
			wantClean: true,
		},
		{
			name: "the retired layers workflow is reported, and render removes it",
			mutate: func(t *testing.T, dir string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(dir, ".github", "workflows"), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(RetiredWorkflowFile)), []byte("name: layers\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantFinding: RetiredWorkflowFile,
			wantReason:  "the layers workflow is retired: the Cloud Build triggers 0-bootstrap renders plan and apply the layers (0-bootstrap/README.md, \"The Cloud Build triggers\"); bedrock org render removes it",
		},
		{
			name: "1-org's retired workflow.tf is reported",
			mutate: func(t *testing.T, dir string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, "1-org", "workflow.tf"), []byte("# the old file\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantFinding: "1-org/workflow.tf",
			wantReason:  "retired: 1-org/runners.tf carries its resources now, and a .tf file left beside it declares them twice; bedrock org render removes it",
		},
		{
			name: "a value 2-env's tfvars still sets that the placement carries now",
			mutate: func(t *testing.T, dir string) {
				t.Helper()
				path := filepath.Join(dir, "2-env", "terraform.tfvars")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, append(data, []byte("github_app_installation_id = 12345678\n")...), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantFinding: "2-env/terraform.tfvars",
			wantReason:  "sets github_app_installation_id, which placement.json carries now as cloudBuildInstallationId: record it there (bedrock org render reads it) and remove it here",
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
			if tt.wantReason != "" && (len(r.Findings) != 1 || !strings.Contains(r.Findings[0].Reason, tt.wantReason)) {
				t.Errorf("Findings = %+v, want the reason %q", r.Findings, tt.wantReason)
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
	}{
		{name: "none recorded yet"},
		{name: "all three recorded", projects: map[string]string{"tst": "imp-tst-gbl-core-1a2b", "stg": "imp-stg-gbl-core-3c4d", "prd": "imp-prd-gbl-core-5e6f"}},
		{name: "one recorded", projects: map[string]string{"tst": "imp-tst-gbl-core-1a2b"}},
		{name: "an environment the model lacks is refused", projects: map[string]string{"qa": "imp-qa-gbl-core-1a2b"}, wantErr: `projects names "qa", which is not one of boot, shr, net, spn, tst, stg, prd`},
		{name: "an empty project is refused", projects: map[string]string{"tst": " "}, wantErr: "projects.tst is empty"},
		{name: "the boot and shared projects", projects: map[string]string{"boot": "imp-boot-gbl-core-1a2b", "shr": "imp-shr-gbl-core-7a8b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := testPlacement(t)
			p.Projects = tt.projects
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

func TestBuildCacheCleanup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		path   string
		want   []string
		absent []string
	}{
		{
			name: "the rule deletes the tags starting with cache- after thirty days and selects nothing by another name",
			path: "2-shr/registry.tf",
			want: []string{
				"  cleanup_policies {\n    id     = \"delete-build-caches\"\n    action = \"DELETE\"\n    condition {\n      tag_state    = \"TAGGED\"\n      tag_prefixes = [\"cache-\"]\n      older_than   = \"${30 * 24 * 60 * 60}s\"\n    }\n  }\n",
				"never with cache-, so this selects no release",
			},
			absent: []string{"tag_prefixes = [\"v\"]"},
		},
		{
			name: "the README says so",
			path: "2-shr/README.md",
			want: []string{"the tags starting with\n  `cache-`, which are deleted after 30 days; release tags are untouched."},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			text := renderedFile(t, tt.path)
			for _, want := range tt.want {
				if !strings.Contains(text, want) {
					t.Errorf("%s lacks %q", tt.path, want)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(text, absent) {
					t.Errorf("%s carries %q", tt.path, absent)
				}
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
			name:      "both recorded for every environment: the block for all three, production's for the rerun",
			projects:  map[string]string{"tst": "imp-tst-gbl-core-b241", "stg": "imp-stg-gbl-core-0fa7", "prd": "imp-prd-gbl-core-abe8"},
			numbers:   map[string]string{"tst": "1", "stg": "2", "prd": "3"},
			wantBlock: "  \"projects\": {\n    \"tst\": {\"id\": \"imp-tst-gbl-core-b241\", \"number\": \"1\"},\n    \"stg\": {\"id\": \"imp-stg-gbl-core-0fa7\", \"number\": \"2\"},\n    \"prd\": {\"id\": \"imp-prd-gbl-core-abe8\", \"number\": \"3\"}\n  }",
		},
		{
			name:        "a number missing leaves its environment out and names it",
			projects:    map[string]string{"tst": "imp-tst-gbl-core-b241", "stg": "imp-stg-gbl-core-0fa7"},
			numbers:     map[string]string{"tst": "1"},
			wantBlock:   "  \"projects\": {\n    \"tst\": {\"id\": \"imp-tst-gbl-core-b241\", \"number\": \"1\"}\n  }",
			wantMissing: "stg,prd",
		},
		{name: "nothing recorded", wantMissing: "tst,stg,prd"},
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

// TestApplicationPlacement: an application's first placement takes every field from the
// organization's placement and the pin given, and is refused while the organization's
// placement lacks what it names, or for a pin derive would refuse.
func TestApplicationPlacement(t *testing.T) {
	t.Parallel()

	const (
		commitPin = "v0.0.0-20260928182105-6f6f7795969d"
		sum       = "0000000000000000000000000000000000000000000000000000000000000000"
	)
	three := 3
	tests := []struct {
		name    string
		mutate  func(p *Placement)
		version string
		sha256  string
		wantErr string
	}{
		{name: "a commit pin", mutate: func(*Placement) {}, version: commitPin},
		{name: "a release pin with its checksum", mutate: func(*Placement) {}, version: "v0.4.0", sha256: sum},
		{name: "no labels of the organization's", mutate: func(p *Placement) { p.Labels = nil }, version: commitPin},
		{name: "the organization's outlier detection thresholds", mutate: func(p *Placement) { p.OutlierDetection = &derive.OutlierDetection{ConsecutiveErrors: &three} }, version: commitPin},
		{name: "the organization's Cloud Armor policy", mutate: func(p *Placement) {
			p.CloudArmor = &derive.CloudArmor{RuleSets: []derive.RuleSet{{Name: "sqli-v33-stable", Sensitivity: 2}}, Bypasses: []derive.Bypass{{Path: "/hooks/*", Reason: "the senders sign their bodies"}}}
		}, version: commitPin},
		{name: "a release pin without its checksum", mutate: func(*Placement) {}, version: "v0.4.0", wantErr: "bedrockVersion v0.4.0 is a release, and a release pin carries its bedrockSha256"},
		{name: "no state bucket", mutate: func(p *Placement) { p.StateBucket = "" }, version: commitPin, wantErr: "placement.json records no stateBucket yet: the seed's state bucket goes there"},
		{name: "no environment project numbers", mutate: func(p *Placement) { p.ProjectNumbers = map[string]string{"boot": "1"} }, version: commitPin, wantErr: "placement.json records no project id and number for tst, stg and prd yet"},
		{name: "no production project", mutate: func(p *Placement) { delete(p.Projects, "prd") }, version: commitPin, wantErr: "placement.json records no project id and number for prd yet"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := testPlacement(t)
			tt.mutate(p)
			got, err := p.ApplicationPlacement("quill", tt.version, tt.sha256)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ApplicationPlacement() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("ApplicationPlacement() error = %v", err)
			}
			var labels map[string]string
			if len(p.Labels) > 0 {
				labels = p.Labels
			}
			want := &derive.Placement{
				Prefix: "imp", Environments: []string{"tst", "stg", "prd"},
				Regions:    []derive.Region{{Name: "us-central1", Code: "uc1"}, {Name: "us-west3", Code: "uw3"}},
				AppsDomain: "apps.imp.example", HostedDomain: "imp.example", StateBucket: "imp-boot-gbl-state-a1b2",
				PlaceholderImage: derive.PlaceholderImage, DefaultBranch: "master", Repository: "quill", ReleaseApp: "imp-release",
				BedrockVersion: tt.version, BedrockSHA256: tt.sha256, Labels: labels, Seed: []string{"tst"},
				Projects: map[string]derive.Project{
					"tst": {ID: "imp-tst-gbl-core-1a2b", Number: "100000000002"},
					"stg": {ID: "imp-stg-gbl-core-3c4d", Number: "100000000003"},
					"prd": {ID: "imp-prd-gbl-core-5e6f", Number: "100000000004"},
				},
				OutlierDetection: p.OutlierDetection,
				CloudArmor:       p.CloudArmor,
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("ApplicationPlacement() =\n%+v\nwant\n%+v", got, want)
			}
			if p.OutlierDetection != nil && got.OutlierDetection == p.OutlierDetection {
				t.Error("the application's thresholds share the organization's block; they are a copy")
			}
			if p.CloudArmor != nil && (got.CloudArmor == p.CloudArmor || &got.CloudArmor.Bypasses[0] == &p.CloudArmor.Bypasses[0]) {
				t.Error("the application's Cloud Armor policy shares the organization's block; it is a copy")
			}
		})
	}
}

// TestWildcardOutlierThresholds renders 2-env's pull-request backend with the
// organization's outlier detection thresholds: today's when the placement sets none, the
// placement's when it does; a threshold out of range refuses the placement.
func TestWildcardOutlierThresholds(t *testing.T) {
	t.Parallel()

	n := func(v int) *int {
		return &v
	}
	block := func(consecutive, enforcing, ejection, interval, base int) string {
		return fmt.Sprintf("  outlier_detection {\n    consecutive_errors           = %d\n    enforcing_consecutive_errors = %d\n    max_ejection_percent         = %d\n\n    interval {\n      seconds = %d\n    }\n\n    base_ejection_time {\n      seconds = %d\n    }\n  }\n\n  log_config {\n", consecutive, enforcing, ejection, interval, base)
	}
	tests := []struct {
		name    string
		set     *derive.OutlierDetection
		want    string
		wantErr string
	}{
		{name: "none set: today's thresholds", want: block(5, 100, 50, 1, 30)},
		{name: "every threshold set", set: &derive.OutlierDetection{ConsecutiveErrors: n(3), EnforcingConsecutiveErrors: n(80), MaxEjectionPercent: n(100), IntervalSeconds: n(2), BaseEjectionSeconds: n(60)}, want: block(3, 80, 100, 2, 60)},
		{name: "one threshold set, the rest today's", set: &derive.OutlierDetection{IntervalSeconds: n(5)}, want: block(5, 100, 50, 5, 30)},
		{name: "a threshold out of range", set: &derive.OutlierDetection{EnforcingConsecutiveErrors: n(0)}, wantErr: "outlierDetection.enforcingConsecutiveErrors 0 is outside what keeps outlier detection on"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := testPlacement(t)
			p.OutlierDetection = tt.set
			files, err := Render(p)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Render() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			for _, f := range files {
				if f.Path == envLayer+"/pull-requests.tf" && !strings.Contains(string(f.Content), tt.want) {
					t.Errorf("2-env/pull-requests.tf lacks:\n%s\nin:\n%s", tt.want, f.Content)
				}
			}
		})
	}
}

// TestUnrecordedProjects names what the triggers' runs still lack in a placement: every
// project the layer identities are named under.
func TestUnrecordedProjects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		projects map[string]string
		want     string
	}{
		{name: "nothing recorded", want: "projects.boot,projects.shr,projects.net,projects.spn,projects.tst,projects.stg,projects.prd"},
		{name: "the seed's values alone", projects: map[string]string{"boot": "imp-boot-gbl-core-a1b2"}, want: "projects.shr,projects.net,projects.spn,projects.tst,projects.stg,projects.prd"},
		{name: "the environments alone", projects: map[string]string{"tst": "a", "stg": "b", "prd": "c"}, want: "projects.boot,projects.shr,projects.net,projects.spn"},
		{name: "everything", projects: map[string]string{"boot": "a", "shr": "b", "net": "c", "spn": "d", "tst": "e", "stg": "f", "prd": "g"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := testPlacement(t)
			p.Projects = tt.projects
			if got := strings.Join(p.UnrecordedProjects(), ","); got != tt.want {
				t.Errorf("UnrecordedProjects() = %q, want %q", got, tt.want)
			}
		})
	}
}

// authorized is a placement recording the Cloud Build authorization's two values.
func authorized(installation, version string) Placement {
	p := Placement{}
	p.CloudBuildInstallationID = installation
	p.CloudBuildTokenSecretVersion = version

	return p
}

// TestConnectionUnset names the Cloud Build values the placement does not record yet.
func TestConnectionUnset(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		placement Placement
		want      string
		connected bool
	}{
		{name: "neither", want: "cloudBuildInstallationId,cloudBuildTokenSecretVersion"},
		{name: "the installation alone", placement: authorized("1", ""), want: "cloudBuildTokenSecretVersion"},
		{name: "the token alone", placement: authorized("", "projects/p/secrets/s/versions/1"), want: "cloudBuildInstallationId"},
		{name: "both", placement: authorized("1", "projects/p/secrets/s/versions/1"), connected: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := tt.placement
			if got := strings.Join(p.ConnectionUnset(), ","); got != tt.want {
				t.Errorf("ConnectionUnset() = %q, want %q", got, tt.want)
			}
			if p.Connected() != tt.connected {
				t.Errorf("Connected() = %t, want %t", p.Connected(), tt.connected)
			}
		})
	}
}

// TestLayerRuns reads the runs the triggers' commands drive: the five single-run layers
// then 2-env per environment, each with its state prefix, its marker object and its
// identities under the recorded projects, and REPLACEME under a project the placement
// does not record.
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
				"0-bootstrap|0-bootstrap|acme-boot-gbl-tofu@acme-boot-gbl-core-1a2b.iam.gserviceaccount.com|acme-boot-gbl-plan@acme-boot-gbl-core-1a2b.iam.gserviceaccount.com",
				"1-org|1-org|acme-org-gbl-tofu@acme-boot-gbl-core-1a2b.iam.gserviceaccount.com|acme-org-gbl-plan@acme-boot-gbl-core-1a2b.iam.gserviceaccount.com",
				"2-shr|2-shr|acme-shr-gbl-tofu@acme-shr-gbl-core-3c4d.iam.gserviceaccount.com|acme-shr-gbl-plan@acme-shr-gbl-core-3c4d.iam.gserviceaccount.com",
				"2-spn|2-spn|acme-spn-gbl-tofu@acme-spn-gbl-core-7a8b.iam.gserviceaccount.com|acme-spn-gbl-plan@acme-spn-gbl-core-7a8b.iam.gserviceaccount.com",
				"2-net|2-net|acme-net-gbl-tofu@acme-net-gbl-core-5e6f.iam.gserviceaccount.com|acme-net-gbl-plan@acme-net-gbl-core-5e6f.iam.gserviceaccount.com",
				"2-env tst|2-env/tst|acme-tst-gbl-tofu@acme-tst-gbl-core-9c0d.iam.gserviceaccount.com|acme-tst-gbl-plan@acme-tst-gbl-core-9c0d.iam.gserviceaccount.com",
				"2-env stg|2-env/stg|acme-stg-gbl-tofu@acme-stg-gbl-core-1e2f.iam.gserviceaccount.com|acme-stg-gbl-plan@acme-stg-gbl-core-1e2f.iam.gserviceaccount.com",
				"2-env prd|2-env/prd|acme-prd-gbl-tofu@acme-prd-gbl-core-3a4b.iam.gserviceaccount.com|acme-prd-gbl-plan@acme-prd-gbl-core-3a4b.iam.gserviceaccount.com",
			},
		},
		{
			name:     "a project not recorded carries REPLACEME",
			projects: map[string]string{"boot": "acme-boot-gbl-core-1a2b"},
			want: []string{
				"0-bootstrap|0-bootstrap|acme-boot-gbl-tofu@acme-boot-gbl-core-1a2b.iam.gserviceaccount.com|acme-boot-gbl-plan@acme-boot-gbl-core-1a2b.iam.gserviceaccount.com",
				"1-org|1-org|acme-org-gbl-tofu@acme-boot-gbl-core-1a2b.iam.gserviceaccount.com|acme-org-gbl-plan@acme-boot-gbl-core-1a2b.iam.gserviceaccount.com",
				"2-shr|2-shr|acme-shr-gbl-tofu@acme-shr-gbl-core-REPLACEME.iam.gserviceaccount.com|acme-shr-gbl-plan@acme-shr-gbl-core-REPLACEME.iam.gserviceaccount.com",
				"2-spn|2-spn|acme-spn-gbl-tofu@acme-spn-gbl-core-REPLACEME.iam.gserviceaccount.com|acme-spn-gbl-plan@acme-spn-gbl-core-REPLACEME.iam.gserviceaccount.com",
				"2-net|2-net|acme-net-gbl-tofu@acme-net-gbl-core-REPLACEME.iam.gserviceaccount.com|acme-net-gbl-plan@acme-net-gbl-core-REPLACEME.iam.gserviceaccount.com",
				"2-env tst|2-env/tst|acme-tst-gbl-tofu@acme-tst-gbl-core-REPLACEME.iam.gserviceaccount.com|acme-tst-gbl-plan@acme-tst-gbl-core-REPLACEME.iam.gserviceaccount.com",
				"2-env stg|2-env/stg|acme-stg-gbl-tofu@acme-stg-gbl-core-REPLACEME.iam.gserviceaccount.com|acme-stg-gbl-plan@acme-stg-gbl-core-REPLACEME.iam.gserviceaccount.com",
				"2-env prd|2-env/prd|acme-prd-gbl-tofu@acme-prd-gbl-core-REPLACEME.iam.gserviceaccount.com|acme-prd-gbl-plan@acme-prd-gbl-core-REPLACEME.iam.gserviceaccount.com",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := &Placement{Prefix: "acme", Projects: tt.projects}
			var got []string
			for _, r := range p.LayerRuns() {
				got = append(got, r.Name+"|"+r.StatePrefix+"|"+r.Apply+"|"+r.Plan)
				if want := strings.Contains(r.Apply, "REPLACEME"); r.Unrecorded() != want {
					t.Errorf("%s: Unrecorded() = %t, want %t", r.Name, r.Unrecorded(), want)
				}
				wantMarker := "applied/" + r.Layer
				if r.Environment != "" {
					wantMarker += "/" + r.Environment
				}
				if r.Marker() != wantMarker {
					t.Errorf("%s: Marker() = %q, want %q", r.Name, r.Marker(), wantMarker)
				}
			}
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("LayerRuns() =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
			}
		})
	}
}

// TestTriggers reads the triggers 0-bootstrap renders: the connection and the repository
// link from the placement's authorization, the two triggers on the infrastructure
// repository (the plan trigger on pull requests into the default branch with comment
// control off, the apply trigger on pushes to it), each running as its runner identity,
// the shared steps (the fetch that reads the pin off placement.json at run time, the
// build of a commit pin, the check) followed by the one command, the substitutions by
// which a person applies one layer again and by which the commands know the steps'
// shape, logging to Cloud Logging alone, and nothing of the retired workflow.
func TestTriggers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(p *Placement)
		want   []string
		absent []string
	}{
		{
			name:   "the connection and the repository link, from the placement's authorization",
			mutate: func(*Placement) {},
			want: []string{
				"  cloud_build_installation_id      = 12345678\n  cloud_build_token_secret_version = \"projects/imp-boot-gbl-core-a1b2/locations/us-central1/secrets/github-github-oauthtoken-abcdef/versions/1\"\n  connected                        = local.cloud_build_installation_id != null && local.cloud_build_token_secret_version != null\n",
				"resource \"google_cloudbuildv2_connection\" \"github\" {\n  count = local.connected ? 1 : 0\n\n  project  = module.boot_project.project_id\n  location = local.region\n  name     = \"${var.prefix}-boot-${local.region_code}-github\"\n\n  github_config {\n    app_installation_id = local.cloud_build_installation_id\n\n    authorizer_credential {\n      oauth_token_secret_version = local.cloud_build_token_secret_version\n    }\n  }\n",
				"resource \"google_cloudbuildv2_repository\" \"infrastructure\" {\n  count = local.connected ? 1 : 0\n\n  project           = module.boot_project.project_id\n  location          = local.region\n  name              = \"${var.prefix}-boot-${local.region_code}-infrastructure-repo\"\n  parent_connection = google_cloudbuildv2_connection.github[0].id\n  remote_uri        = \"https://github.com/${var.github_organization}/${var.infrastructure_repository}.git\"\n}\n",
				"  environment_project_numbers = {\n    tst = \"100000000002\"\n    stg = \"100000000003\"\n    prd = \"100000000004\"\n  }\n  token_readers = local.connected ? merge(\n    { boot = module.boot_project.project_number },\n    local.environment_project_numbers,\n  ) : {}\n",
				"resource \"google_secret_manager_regional_secret_iam_member\" \"token_accessor\" {\n  for_each = local.token_regional ? local.token_readers : {}\n\n  project   = local.token_project\n  location  = local.token_location\n  secret_id = local.token_secret\n  role      = \"roles/secretmanager.secretAccessor\"\n  member    = \"serviceAccount:service-${each.value}@gcp-sa-cloudbuild.iam.gserviceaccount.com\"\n}\n",
			},
		},
		{
			name:   "the plan trigger: pull requests into the default branch, no comment control, as the planner",
			mutate: func(*Placement) {},
			want: []string{
				"resource \"google_cloudbuild_trigger\" \"plan\" {\n  count = local.connected ? 1 : 0\n\n  project  = module.boot_project.project_id\n  location = local.region\n  name     = \"${var.prefix}-boot-${local.region_code}-layers-plan\"\n\n  service_account    = google_service_account.planner.id\n  include_build_logs = \"INCLUDE_BUILD_LOGS_WITH_STATUS\"\n  substitutions      = local.substitutions\n\n  repository_event_config {\n    repository = google_cloudbuildv2_repository.infrastructure[0].id\n\n    pull_request {\n      branch          = \"^${var.github_default_branch}$\"\n      comment_control = \"COMMENTS_DISABLED\"\n    }\n  }\n",
				"    step {\n      id         = \"Plan\"\n      name       = \"ghcr.io/opentofu/opentofu:1.12.6\"\n      entrypoint = local.bedrock\n      args       = [\"org\", \"plan\"]\n      env        = local.run_env\n      timeout    = \"3600s\"\n    }\n\n    timeout = \"5100s\"\n\n    options {\n      logging = \"CLOUD_LOGGING_ONLY\"\n    }\n  }\n\n  depends_on = [google_service_account_iam_member.runner_agent_user]\n}\n",
			},
		},
		{
			name:   "the apply trigger: pushes to the default branch, as the applier",
			mutate: func(*Placement) {},
			want: []string{
				"resource \"google_cloudbuild_trigger\" \"apply\" {\n  count = local.connected ? 1 : 0\n\n  project  = module.boot_project.project_id\n  location = local.region\n  name     = \"${var.prefix}-boot-${local.region_code}-layers-apply\"\n\n  service_account    = google_service_account.applier.id\n  include_build_logs = \"INCLUDE_BUILD_LOGS_WITH_STATUS\"\n  substitutions      = local.substitutions\n\n  repository_event_config {\n    repository = google_cloudbuildv2_repository.infrastructure[0].id\n\n    push {\n      branch = \"^${var.github_default_branch}$\"\n    }\n  }\n",
				"    step {\n      id         = \"Apply\"\n      name       = \"ghcr.io/opentofu/opentofu:1.12.6\"\n      entrypoint = local.bedrock\n      args       = [\"org\", \"apply\"]\n      env        = local.run_env\n      timeout    = \"7200s\"\n    }\n\n    timeout = \"8700s\"\n",
				"gcloud builds triggers run imp-boot-uc1-layers-apply --region=us-central1 --project=imp-boot-gbl-core-a1b2 --branch=master --substitutions=_LAYER=2-env,_ENVIRONMENT=tst\n",
			},
		},
		{
			name:   "the shared steps: the pin read off placement.json at run time, the commit pin built, the check; then the substitutions",
			mutate: func(*Placement) {},
			want: []string{
				"  bedrock = \"/builder/home/bedrock\"\n  run_env = [\"BUILD_ID=$BUILD_ID\", \"PROJECT_ID=$PROJECT_ID\", \"LOCATION=$LOCATION\"]\n",
				"    _LAYER         = \"\"  # a layer to apply again by hand, with no change check; empty for the push's changes\n    _ENVIRONMENT   = \"\"  # with _LAYER=2-env, one environment; empty for the three in order\n    _BEDROCK_STEPS = \"" + StepsVersion + "\" # the shape of these steps, which bedrock org plan and apply compare with their own\n",
				"    with open(\"placement.json\", encoding=\"utf-8\") as f:\n        placement = json.load(f)\n    version = placement.get(\"bedrockVersion\", \"\")\n    digest = placement.get(\"bedrockSha256\", \"\")\n",
				"    url = host + \"/cccteam/ccc/releases/download/bedrock/\" + version + \"/bedrock-linux-amd64\"\n",
				"    if hashlib.sha256(data).hexdigest() != digest:\n",
				"    go install -ldflags=\"-s -w\" github.com/cccteam/ccc/bedrock@\"$$(cat /builder/home/bedrock-commit)\"\n",
				"    {\n      id         = \"FetchBedrock\"\n      name       = \"gcr.io/cloud-builders/gcloud\"\n      entrypoint = \"python3\"\n      args       = [\"-c\", local.fetch_bedrock, \"/builder/home\", \"https://github.com\", \"https://github.com/cccteam/ccc.git\"]\n      env        = []\n      timeout    = \"300s\"\n    },\n",
				"    {\n      id         = \"BuildBedrock\"\n      name       = \"cgr.dev/chainguard/go@sha256:2c20827746d752c1f18989847956637a0dc979c42507f4059e1d9559ee7733af\"\n      entrypoint = \"sh\"\n      args       = [\"-c\", local.build_bedrock]\n      env        = [\"GOBIN=/builder/home\", \"CGO_ENABLED=0\", \"GOTOOLCHAIN=local\", \"GOFLAGS=-trimpath\", \"GOPATH=/tmp/go\", \"GOCACHE=/tmp/go-build\", \"GOMODCACHE=/tmp/go-mod\", \"GOPROXY=https://proxy.golang.org\", \"GOSUMDB=sum.golang.org\", \"GONOSUMDB=\", \"GOPRIVATE=\", \"GOINSECURE=\"]\n      timeout    = \"600s\"\n    },\n",
				"    {\n      id         = \"Check\"\n      name       = \"ghcr.io/opentofu/opentofu:1.12.6\"\n      entrypoint = local.bedrock\n      args       = [\"org\", \"check\"]\n      env        = []\n      timeout    = \"600s\"\n    },\n  ]\n",
				"    dynamic \"step\" {\n      for_each = local.steps\n      content {\n        id         = step.value.id\n        name       = step.value.name\n        entrypoint = step.value.entrypoint\n        args       = step.value.args\n        env        = step.value.env\n        timeout    = step.value.timeout\n      }\n    }\n",
			},
			absent: []string{"v0.5.0", "0123456789abcdef", "workloadIdentityPool", "layers.yml", "GOOGLE_IMPERSONATE_SERVICE_ACCOUNT", "-core-REPLACEME"},
		},
		{
			name: "before the authorization and the project numbers are recorded",
			mutate: func(p *Placement) {
				p.CloudBuildInstallationID, p.CloudBuildTokenSecretVersion = "", ""
				p.ProjectNumbers = nil
			},
			want: []string{
				"  cloud_build_installation_id      = null\n  cloud_build_token_secret_version = null\n",
				"  environment_project_numbers = {}\n",
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
			const path = "0-bootstrap/cloud-build.tf"
			var content string
			for _, f := range files {
				if f.Path == path {
					content = string(f.Content)
				}
			}
			if content == "" {
				t.Fatalf("%s is not rendered", path)
			}
			for _, w := range tt.want {
				if !strings.Contains(content, w) {
					t.Errorf("%s lacks:\n%s", path, w)
				}
			}
			for _, a := range tt.absent {
				if strings.Contains(content, a) {
					t.Errorf("%s still carries %q", path, a)
				}
			}
		})
	}
}

// TestRunnerIdentities reads who may become each layer identity: the two runner
// identities 0-bootstrap creates with what each holds itself (the logs, the connection's
// read token, the apps' keys, the markers for the applier), the applier's impersonation
// of the two identities 0-bootstrap owns, 1-org's grants letting the planner impersonate
// every plan identity and the applier every apply identity, the retired workload
// identity pool, provider and bindings taken out of the state and destroyed, and the
// boot identity's organization roles carrying the connection administration in place of
// the pool administration.
func TestRunnerIdentities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		path   string
		want   []string
		absent []string
	}{
		{
			name: "the two runner identities, and what each holds itself",
			path: "0-bootstrap/cloud-build.tf",
			want: []string{
				"resource \"google_service_account\" \"planner\" {\n  project      = module.boot_project.project_id\n  account_id   = \"${var.prefix}-org-gbl-planner\"\n",
				"resource \"google_service_account\" \"applier\" {\n  project      = module.boot_project.project_id\n  account_id   = \"${var.prefix}-org-gbl-applier\"\n",
				"resource \"google_project_iam_member\" \"runner_logs\" {\n  for_each = local.runners\n\n  project = module.boot_project.project_id\n  role    = \"roles/logging.logWriter\"\n  member  = each.value.member\n}\n",
				"resource \"google_service_account_iam_member\" \"runner_agent_user\" {\n  for_each = local.runners\n\n  service_account_id = each.value.name\n  role               = \"roles/iam.serviceAccountUser\"\n  member             = \"serviceAccount:service-${module.boot_project.project_number}@gcp-sa-cloudbuild.iam.gserviceaccount.com\"\n",
				"resource \"google_cloudbuildv2_connection_iam_member\" \"runner_read_token\" {\n  for_each = local.connected ? local.runners : {}\n\n  project  = module.boot_project.project_id\n  location = google_cloudbuildv2_connection.github[0].location\n  name     = google_cloudbuildv2_connection.github[0].name\n  role     = \"roles/cloudbuild.readTokenAccessor\"\n  member   = each.value.member\n}\n",
				"resource \"google_secret_manager_secret_iam_member\" \"runner_keys\" {\n  for_each = local.runner_keys\n\n  project   = module.boot_project.project_id\n  secret_id = local.key_containers[each.value.key]\n  role      = \"roles/secretmanager.secretAccessor\"\n  member    = local.runners[each.value.runner].member\n}\n",
				"resource \"google_storage_bucket_iam_member\" \"applier_markers\" {\n  bucket = var.state_bucket\n  role   = \"roles/storage.objectUser\"\n  member = google_service_account.applier.member\n\n  condition {\n    title       = \"${var.prefix}-org-gbl-applied-markers\"\n    description = \"The markers of the layers' last successful applies.\"\n    expression  = \"resource.name.startsWith(\\\"${local.state_bucket_objects}/applied/\\\")\"\n  }\n}\n",
			},
		},
		{
			name: "the applier becomes the two identities 0-bootstrap owns; the planner none of them",
			path: "0-bootstrap/cloud-build.tf",
			want: []string{
				"resource \"google_service_account_iam_member\" \"applier_boot_tofu\" {\n  service_account_id = google_service_account.boot_tofu.name\n  role               = \"roles/iam.serviceAccountTokenCreator\"\n  member             = google_service_account.applier.member\n}\n",
				"resource \"google_service_account_iam_member\" \"applier_org_tofu\" {\n  service_account_id = google_service_account.org_tofu.name\n  role               = \"roles/iam.serviceAccountTokenCreator\"\n  member             = google_service_account.applier.member\n}\n",
			},
			absent: []string{"member             = google_service_account.planner.member"},
		},
		{
			name: "the two key containers, administered by the org identity; the pool, the provider and the bindings retired",
			path: "0-bootstrap/github.tf",
			want: []string{
				`secret_id = "${var.prefix}-boot-gbl-github-infrastructure-key"`,
				`secret_id = "${var.prefix}-boot-gbl-github-deployer-key"`,
				"resource \"google_secret_manager_secret_iam_member\" \"org_tofu_infrastructure_key\" {\n  project   = module.boot_project.project_id\n  secret_id = google_secret_manager_secret.github_infrastructure_key.secret_id\n  role      = \"roles/secretmanager.admin\"\n  member    = google_service_account.org_tofu.member\n}\n",
				"resource \"google_secret_manager_secret_iam_member\" \"org_tofu_deployer_key\" {\n  project   = module.boot_project.project_id\n  secret_id = google_secret_manager_secret.github_deployer_key.secret_id\n  role      = \"roles/secretmanager.admin\"\n  member    = google_service_account.org_tofu.member\n}\n",
				"removed {\n  from = google_iam_workload_identity_pool.github\n\n  lifecycle {\n    destroy = true\n  }\n}\n",
				"removed {\n  from = google_iam_workload_identity_pool_provider.github\n\n  lifecycle {\n    destroy = true\n  }\n}\n",
				"removed {\n  from = google_service_account_iam_member.boot_tofu_workflow\n\n  lifecycle {\n    destroy = true\n  }\n}\n",
				"removed {\n  from = google_service_account_iam_member.org_tofu_workflow\n\n  lifecycle {\n    destroy = true\n  }\n}\n",
			},
			absent: []string{"resource \"google_iam_workload_identity_pool\"", "workloadIdentityUser", "attribute.purpose", "token.actions.githubusercontent.com"},
		},
		{
			name: "1-org: the applier becomes every apply identity, the planner every plan identity, the boot layers' included",
			path: "1-org/runners.tf",
			want: []string{
				"resource \"google_service_account_iam_member\" \"tofu_applier\" {\n  for_each = local.layers\n\n  service_account_id = google_service_account.tofu[each.key].name\n  role               = \"roles/iam.serviceAccountTokenCreator\"\n  member             = local.boot.applier_member\n}\n",
				"resource \"google_service_account_iam_member\" \"plan_planner\" {\n  for_each = local.layers\n\n  service_account_id = google_service_account.plan[each.key].name\n  role               = \"roles/iam.serviceAccountTokenCreator\"\n  member             = local.boot.planner_member\n}\n",
				"resource \"google_service_account_iam_member\" \"boot_plan_planner\" {\n  for_each = local.boot_plans\n\n  service_account_id = google_service_account.boot_plan[each.key].name\n  role               = \"roles/iam.serviceAccountTokenCreator\"\n  member             = local.boot.planner_member\n}\n",
				"removed {\n  from = google_service_account_iam_member.tofu_workflow\n\n  lifecycle {\n    destroy = true\n  }\n}\n",
				"removed {\n  from = google_service_account_iam_member.plan_workflow\n\n  lifecycle {\n    destroy = true\n  }\n}\n",
				"removed {\n  from = google_service_account_iam_member.boot_plan_workflow\n\n  lifecycle {\n    destroy = true\n  }\n}\n",
				"resource \"google_secret_manager_secret_iam_member\" \"environment_tofu_deployer_key\" {\n  for_each = local.environment_layers\n\n  project   = var.boot_project_id\n  secret_id = local.boot.github_deployer_key_secret\n  role      = google_organization_iam_custom_role.secret_container_admin.id\n  member    = google_service_account.tofu[each.key].member\n}\n",
				"resource \"google_secret_manager_secret_iam_member\" \"environment_plan_deployer_key\" {\n  for_each = local.environment_layers\n\n  project   = var.boot_project_id\n  secret_id = local.boot.github_deployer_key_secret\n  role      = \"roles/secretmanager.viewer\"\n  member    = google_service_account.plan[each.key].member\n}\n",
			},
			absent: []string{"workflow_plan", "workflow_apply", "workloadIdentityUser", "github_pool_name"},
		},
		{
			name: "the boot layers' plan identities, their organization-level reads and the key's read",
			path: "1-org/runners.tf",
			want: []string{
				"resource \"google_service_account\" \"boot_plan\" {\n  for_each = local.boot_plans\n\n  project      = var.boot_project_id\n  account_id   = \"${var.prefix}-${each.key}-gbl-plan\"\n",
				"    boot = { layer = \"0-bootstrap\", reads = [\"0-bootstrap\"] }\n    org  = { layer = \"1-org\", reads = [\"0-bootstrap\", \"1-org\"] }\n",
				"    for pair in setproduct(keys(local.boot_plans), var.boot_plan_roles) :",
				"resource \"google_secret_manager_secret_iam_member\" \"org_plan_infrastructure_key\" {\n  project   = var.boot_project_id\n  secret_id = local.boot.github_infrastructure_key_secret\n  role      = \"roles/secretmanager.secretAccessor\"\n  member    = google_service_account.boot_plan[\"org\"].member\n}\n",
			},
		},
		{
			name: "1-org reads 0-bootstrap's state for the runners and the containers",
			path: "1-org/initialize.tf",
			want: []string{"data \"terraform_remote_state\" \"boot\" {\n  backend = \"gcs\"\n  config = {\n    bucket = var.state_bucket\n    prefix = \"0-bootstrap\"\n  }\n}\n"},
		},
		{
			name: "0-bootstrap publishes the runners and the containers for 1-org",
			path: "0-bootstrap/outputs.tf",
			want: []string{
				"output \"planner_member\" {\n", "  value       = google_service_account.planner.member\n",
				"output \"applier_member\" {\n", "  value       = google_service_account.applier.member\n",
				"output \"github_deployer_key_secret\" {\n", "  value       = google_secret_manager_secret.github_deployer_key.secret_id\n",
				"output \"connection_name\" {\n", "  value       = one(google_cloudbuildv2_connection.github[*].name)\n",
				"output \"plan_trigger_name\" {\n", "  value       = one(google_cloudbuild_trigger.plan[*].name)\n",
				"output \"apply_trigger_name\" {\n", "  value       = one(google_cloudbuild_trigger.apply[*].name)\n",
			},
			absent: []string{"github_pool_name", "github_provider_name"},
		},
		{
			name:   "the boot identity's organization roles carry the connection administration, not the pool's; the org identity's the tag role",
			path:   "0-bootstrap/variables.tf",
			want:   []string{`"roles/cloudbuild.builds.editor",          # the two triggers that plan and apply the layers (cloud-build.tf)`, `"roles/cloudbuild.connectionAdmin",        # the connection and the repository link the triggers watch (cloud-build.tf)`, `"roles/resourcemanager.tagAdmin",        # the public-invoker tag and the grants on its value`},
			absent: []string{"workloadIdentityPoolAdmin"},
		},
		{
			name:   "the boot plan identities' roles no longer read the pool",
			path:   "1-org/variables.tf",
			absent: []string{"workloadIdentityPoolViewer"},
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
			path: "1-org/runners.tf",
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
		{
			name: "the environment layers' plan identities read the bucket's policy, which the slots refresh through",
			path: "1-org/runners.tf",
			want: []string{
				"resource \"google_storage_bucket_iam_member\" \"environment_plan_policy_reader\" {\n  for_each = local.environment_layers\n\n  bucket = var.state_bucket\n  role   = google_organization_iam_custom_role.bucket_policy_reader.id\n  member = google_service_account.plan[each.key].member\n}\n",
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
// configures, the checks a pull request must pass (the application stack's check trigger
// in the first environment, reported by Google's Cloud Build GitHub App under the
// trigger's name and the project id, and the six fixed jobs of impulse's CI workflow in
// the workflow's order, web among them so a failed browser build blocks the merge; never
// the pull-request build, which is the preview on /gcbrun), squash as the only merge, the branch up to date
// before it merges, the environments the operations workflow runs in (every one), and the
// placement's values as the variables' defaults.
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
		name   string
		path   string
		want   []string
		absent []string
	}{
		{
			name: "the applications are the repositories",
			path: "1-org/applications.auto.tfvars",
			want: []string{`applications = ["harbor", "beacon"]`},
		},
		{
			name: "the rules name the checks in order (the stack's check trigger, impulse's six fixed jobs), squash alone and the branch up to date",
			path: "1-org/github.tf",
			want: []string{
				`data "github_app" "cloud_build" {
  slug = "google-cloud-build"
}`,
				`  cloud_build_check = {
    for app in var.applications : app => "${var.prefix}-tst-uc1-${app}-check (${module.project["tst"].project_id})"
  }`,
				`      {
        context        = local.cloud_build_check[app]
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
        context        = "web"
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
				`do_not_enforce_on_create = true`,
				`allowed_merge_methods           = ["squash"]`,
				`require_last_push_approval      = var.github_infrastructure_team != ""`,
				`operations_environments = ["tst", "stg", "prd"]`,
				`moved {
  from = github_repository_environment.restorable
  to   = github_repository_environment.operations
}`,
				`include = ["refs/tags/v*", "refs/tags/*/v*"]`,
				`hotfix  = { name = "hotfix lines", include = ["refs/heads/hotfix/**"] }`,
				`prevent_destroy = true`,
			},
		},
		{
			name: "the pull-request build is not a required check, and no Actions job of the name bedrock check is",
			path: "1-org/github.tf",
			absent: []string{
				`-pr (${module.project["tst"].project_id})`,
				`context        = "bedrock check"`,
			},
		},
		{
			name: "the README says the pull-request build is the preview and tst's tag build is a release's first build",
			path: "1-org/README.md",
			want: []string{
				"is not a required check. It is the developer's",
				"tst's tag build is the release's first build",
			},
		},
		{
			name: "the placement's values are the variables' defaults",
			path: "1-org/variables.tf",
			want: []string{
				`default     = 5080645`,
				`default     = "master"`,
				`default     = "imp-example"`,
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
			for _, a := range tt.absent {
				if strings.Contains(content, a) {
					t.Errorf("%s still carries %q", tt.path, a)
				}
			}
		})
	}
}

// TestProjectLien pins the lien on every project 1-org creates, the role its apply
// identity holds to place one, and the READMEs saying a project's removal starts with
// removing it.
func TestProjectLien(t *testing.T) {
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
			name: "the project module places the lien, unconditionally",
			path: "1-org/projects.tf",
			want: []string{"  deletion_policy            = \"PREVENT\"\n  lien                       = true\n"},
		},
		{
			name: "the org layer identity holds the role that places and removes a lien",
			path: "0-bootstrap/variables.tf",
			want: []string{`"roles/resourcemanager.lienModifier",    # the lien on every project (1-org projects.tf)`},
		},
		{
			name: "1-org's README starts a project's removal, and a clean-slate wipe, with the lien",
			path: "1-org/README.md",
			want: []string{
				"## Removing a project\n\nA project's removal starts with removing its lien.",
				"gcloud alpha resource-manager liens list --project=<project id>",
				"project by project in `project_ids`: the lien first, as\nabove, then `gcloud projects delete <project id>`.",
			},
		},
		{
			name: "the root README names the lien among what resists deletion",
			path: "README.md",
			want: []string{"a project's removal starts with removing its lien (`1-org/README.md`,\n\"Removing a project\")."},
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

// TestAppsZone holds the zone domain check reads to the one 2-net's dns.tf names, in the
// network project the placement records.
func TestAppsZone(t *testing.T) {
	t.Parallel()

	unrecorded := testPlacement(t)
	unrecorded.Projects = nil
	tests := []struct {
		name        string
		placement   *Placement
		wantProject string
		wantZone    string
	}{
		{name: "the network project recorded", placement: testPlacement(t), wantProject: "imp-net-gbl-core-9c0d", wantZone: "imp-net-gbl-dns-apps"},
		{name: "before 1-org's project_ids are recorded", placement: unrecorded, wantProject: "", wantZone: "imp-net-gbl-dns-apps"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			project, zone := tt.placement.AppsZone()
			if project != tt.wantProject || zone != tt.wantZone {
				t.Errorf("AppsZone() = %q, %q, want %q, %q", project, zone, tt.wantProject, tt.wantZone)
			}
		})
	}
	names := []struct {
		name string
		path string
		want string
	}{
		{name: "the zone is named after the layer's prefix", path: "2-net/dns.tf", want: `name        = "${local.name_prefix}-dns-apps"`},
		{name: "the layer's prefix is the prefix, its environment and gbl", path: "2-net/locals.tf", want: `name_prefix = "${local.prefix}-${local.environment}-gbl"`},
		{name: "the layer's environment is net", path: "2-net/locals.tf", want: `environment    = "net"`},
	}
	for _, tt := range names {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if !strings.Contains(renderedFile(t, tt.path), tt.want) {
				t.Errorf("%s lacks %q: the zone domain check reads is not the one 2-net names", tt.path, tt.want)
			}
		})
	}
}
