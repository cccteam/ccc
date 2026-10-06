package check

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestScanBuildSecrets(t *testing.T) {
	t.Parallel()

	const declaredInTst = "build_secrets = {\n  tst = {\n    BUILD_PROOF = \"1\"\n  }\n  stg = {}\n  prd = {}\n}\n"
	const declaredEverywhere = "build_secrets = {\n  tst = {\n    BUILD_PROOF = \"1\"\n  }\n  stg = {\n    BUILD_PROOF = \"1\"\n  }\n  prd = {\n    BUILD_PROOF = \"3\"\n  }\n}\n"
	tests := []struct {
		name       string
		dockerfile string
		noFile     bool
		tfvars     string
		want       []BuildSecretFinding
		wantErr    bool
	}{
		{
			name:       "a required mount declared in one environment names the other two",
			dockerfile: "FROM scratch\nRUN --mount=type=secret,id=BUILD_PROOF,required=true cat /run/secrets/BUILD_PROOF\n",
			tfvars:     declaredInTst,
			want:       []BuildSecretFinding{{ID: "BUILD_PROOF", Missing: []string{"stg", "prd"}}},
		},
		{
			name:       "a required mount declared everywhere passes",
			dockerfile: "FROM scratch\nRUN --mount=type=secret,id=BUILD_PROOF,required=true cat /run/secrets/BUILD_PROOF\n",
			tfvars:     declaredEverywhere,
		},
		{
			name:       "an optional mount passes with nothing said",
			dockerfile: "FROM scratch\nRUN --mount=type=secret,id=BUILD_PROOF cat /run/secrets/BUILD_PROOF || true\nRUN --mount=type=secret,id=OTHER,required=false true\n",
			tfvars:     "build_secrets = {\n  tst = {}\n  stg = {}\n  prd = {}\n}\n",
		},
		{
			name:       "a bare required is required",
			dockerfile: "FROM scratch\nRUN --mount=type=secret,id=BUILD_PROOF,required cat /run/secrets/BUILD_PROOF\n",
			tfvars:     "secret_versions = {\n  tst = {}\n}\n",
			want:       []BuildSecretFinding{{ID: "BUILD_PROOF", Missing: []string{"tst", "stg", "prd"}}},
		},
		{
			name:       "the seeded Dockerfile's comment is not an instruction",
			dockerfile: "# RUN --mount=type=secret,id=NAME,required=true NAME=\"$(cat /run/secrets/NAME)\" bun run build\nFROM scratch\n",
			tfvars:     "secret_versions = {\n  tst = {}\n}\n",
		},
		{
			name:       "two mounts on one instruction, a bind mount ignored, reported sorted",
			dockerfile: "FROM scratch\nRUN --mount=type=bind,source=.,target=/src --mount=type=secret,id=ZED,required=true --mount=type=secret,id=ALPHA,required=true true\n",
			tfvars:     "build_secrets = {\n  tst = {\n    ALPHA = \"1\"\n  }\n}\n",
			want:       []BuildSecretFinding{{ID: "ALPHA", Missing: []string{"stg", "prd"}}, {ID: "ZED", Missing: []string{"tst", "stg", "prd"}}},
		},
		{
			name:   "no Dockerfile checks nothing",
			noFile: true,
			tfvars: declaredInTst,
		},
		{
			name:       "a placement whose map is not written out is refused",
			dockerfile: "FROM scratch\nRUN --mount=type=secret,id=BUILD_PROOF,required=true true\n",
			tfvars:     "build_secrets = var.x\n",
			wantErr:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			appDir := t.TempDir()
			dir := t.TempDir()
			if !tt.noFile {
				if err := os.WriteFile(filepath.Join(appDir, "Dockerfile"), []byte(tt.dockerfile), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(dir, "terraform.tfvars"), []byte(tt.tfvars), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := scanBuildSecrets(appDir, dir, []string{"tst", "stg", "prd"})
			if (err != nil) != tt.wantErr {
				t.Fatalf("scanBuildSecrets() error = %v, wantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("scanBuildSecrets() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestReport_Write_buildSecrets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		finding BuildSecretFinding
		want    string
	}{
		{name: "two environments", finding: BuildSecretFinding{ID: "BUILD_PROOF", Missing: []string{"stg", "prd"}}, want: "  refused  Dockerfile mounts build secret BUILD_PROOF as required; stg and prd declare no such secret (terraform.tfvars build_secrets)\n"},
		{name: "one environment", finding: BuildSecretFinding{ID: "UI_LICENSE", Missing: []string{"prd"}}, want: "  refused  Dockerfile mounts build secret UI_LICENSE as required; prd declares no such secret (terraform.tfvars build_secrets)\n"},
		{name: "three environments", finding: BuildSecretFinding{ID: "X", Missing: []string{"tst", "stg", "prd"}}, want: "  refused  Dockerfile mounts build secret X as required; tst, stg and prd declare no such secret (terraform.tfvars build_secrets)\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &Report{Dir: "infrastructure", AppDir: ".", BuildSecrets: []BuildSecretFinding{tt.finding}}
			if r.Clean() {
				t.Error("Clean() = true with a build-secret finding")
			}
			var b bytes.Buffer
			r.Write(&b)
			if got := b.String(); !bytes.HasSuffix([]byte(got), []byte(tt.want)) {
				t.Errorf("Write() = %q, want a last line %q", got, tt.want)
			}
		})
	}
}
