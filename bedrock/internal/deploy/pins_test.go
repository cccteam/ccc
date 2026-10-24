package deploy

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/cccteam/ccc/bedrock/internal/derive"
)

// declaring is a model whose code declares the secrets named, in that order.
func declaring(variables ...string) *derive.Model {
	m := &derive.Model{}
	for _, v := range variables {
		m.Secrets = append(m.Secrets, derive.Secret{Variable: &derive.Variable{Name: v}})
	}

	return m
}

func TestUnpinnedSecrets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// tfvars is the stack's placement values; none in the checkout when missing.
		tfvars  string
		missing bool
		model   *derive.Model
		env     string
		want    []string
		wantErr string
	}{
		{name: "every declared secret pinned leaves none", tfvars: pinnedTfvars("", allPinned, ""), model: declaring("APP_COOKIE_KEY", "APP_STAFF_DIRECTORY_CLIENT_SECRET"), env: stgEnvironment},
		{name: "the declared secrets the environment pins no version for, in declaration order", tfvars: pinnedTfvars("", `APP_STAFF_DIRECTORY_CLIENT_SECRET = "1"`, ""), model: declaring("APP_MAIL_KEY", "APP_STAFF_DIRECTORY_CLIENT_SECRET", "APP_COOKIE_KEY"), env: stgEnvironment, want: []string{"APP_MAIL_KEY", "APP_COOKIE_KEY"}},
		{name: "a bare number and latest pin", tfvars: pinnedTfvars(`APP_COOKIE_KEY = 3, APP_STAFF_DIRECTORY_CLIENT_SECRET = "latest"`, "", ""), model: declaring("APP_COOKIE_KEY", "APP_STAFF_DIRECTORY_CLIENT_SECRET"), env: tstEnvironment},
		{name: "a pin for a variable the code does not declare counts for nothing", tfvars: pinnedTfvars("", `APP_OLD_KEY = "1"`, ""), model: declaring("APP_COOKIE_KEY"), env: stgEnvironment, want: []string{"APP_COOKIE_KEY"}},
		{name: "another environment's pins do not count", tfvars: pinnedTfvars(allPinned, "", allPinned), model: declaring("APP_COOKIE_KEY"), env: stgEnvironment, want: []string{"APP_COOKIE_KEY"}},
		{name: "a code that declares no secret has none unpinned", tfvars: pinnedTfvars("", "", ""), model: declaring(), env: stgEnvironment},
		{name: "no placement values in the checkout pin nothing", missing: true, model: declaring("APP_COOKIE_KEY"), env: stgEnvironment, want: []string{"APP_COOKIE_KEY"}},
		{name: "a map that is not written out is refused", tfvars: "secret_versions = var.pins\n", model: declaring("APP_COOKIE_KEY"), env: stgEnvironment, wantErr: "secret_versions in"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			files := map[string]string{}
			if !tt.missing {
				files[tfvarsPath] = tt.tfvars
			}
			w := workspaceFiles(t, files)
			got, err := unpinnedSecrets(w, tt.model, tt.env)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("unpinnedSecrets() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("unpinnedSecrets() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("unpinnedSecrets() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestPinMessages: the refusal a tag build gives and the line a pull request's summary
// carries, bending with one unpinned secret or several.
func TestPinMessages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		unpinned    []string
		wantRefusal string
		wantNotice  string
		wantPara    string
	}{
		{
			name:        "one secret",
			unpinned:    []string{"APP_MAIL_KEY"},
			wantRefusal: `the code declares a secret stg has no pinned version for (secret_versions.stg in infrastructure/terraform.tfvars): APP_MAIL_KEY. stg's processes would start without it, so the release is refused here; pin it after an operator adds its value (bedrock secret add stg APP_MAIL_KEY): bedrock secret pin stg APP_MAIL_KEY <version>. The pin promotes as a release; tst, the first environment, alone runs without one ("not yet").`,
			wantNotice:  "APP_MAIL_KEY has no pinned version in stg (secret_versions.stg in infrastructure/terraform.tfvars); the release is refused there until it is pinned (bedrock secret pin stg APP_MAIL_KEY <version>, after bedrock secret add stg APP_MAIL_KEY has put the value in place).",
			wantPara:    "**Refused until pinned.** `APP_MAIL_KEY` has no pinned version in stg (`secret_versions.stg` in `infrastructure/terraform.tfvars`). After `bedrock secret add stg APP_MAIL_KEY` has put a value in place, pin it: `bedrock secret pin stg APP_MAIL_KEY <version>`.",
		},
		{
			name:        "several secrets, each with its pin",
			unpinned:    []string{"APP_MAIL_KEY", "APP_COOKIE_KEY"},
			wantRefusal: `the code declares secrets stg has no pinned version for (secret_versions.stg in infrastructure/terraform.tfvars): APP_MAIL_KEY, APP_COOKIE_KEY. stg's processes would start without them, so the release is refused here; pin each after an operator adds its value (bedrock secret add stg <VARIABLE>): bedrock secret pin stg APP_MAIL_KEY <version>, bedrock secret pin stg APP_COOKIE_KEY <version>. The pin promotes as a release; tst, the first environment, alone runs without one ("not yet").`,
			wantNotice:  "APP_MAIL_KEY, APP_COOKIE_KEY have no pinned version in stg (secret_versions.stg in infrastructure/terraform.tfvars); the release is refused there until each is pinned (bedrock secret pin stg APP_MAIL_KEY <version>, bedrock secret pin stg APP_COOKIE_KEY <version>, after bedrock secret add stg <VARIABLE> has put the value in place).",
			wantPara:    "**Refused until pinned.** `APP_MAIL_KEY` and `APP_COOKIE_KEY` have no pinned version in stg (`secret_versions.stg` in `infrastructure/terraform.tfvars`). After `bedrock secret add stg <VARIABLE>` has put a value in place, pin each: `bedrock secret pin stg APP_MAIL_KEY <version>`, `bedrock secret pin stg APP_COOKIE_KEY <version>`.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := pinRefusal(stgEnvironment, tstEnvironment, tt.unpinned); got != tt.wantRefusal {
				t.Errorf("pinRefusal() = %q, want %q", got, tt.wantRefusal)
			}
			if got := pinNotice(stgEnvironment, tt.unpinned); got != tt.wantNotice {
				t.Errorf("pinNotice() = %q, want %q", got, tt.wantNotice)
			}
			if got := pinParagraph(stgEnvironment, tt.unpinned); got != tt.wantPara {
				t.Errorf("pinParagraph() =\n%s\nwant\n%s", got, tt.wantPara)
			}
		})
	}
}
