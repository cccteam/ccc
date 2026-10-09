package check

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/app"
)

// The flat fixture's config as the test edits it to embed the cloud driver's settings
// struct: the import line it replaces and the import block replacing it, the field the
// embedding follows and the embedding, and the struct's name as a tag's origin.
const (
	flatTimeImport  = "import \"time\"\n"
	flatCloudImport = "import (\n\t\"time\"\n\n\t\"github.com/cccteam/ccc/cloud/gcp\"\n)\n"
	flatDataField   = "\tData      Data   `env:\",prefix=LIGHTHOUSE_\"`\n"
	flatEmbedding   = "\t// The Google Cloud driver's variables: the logging project and the trace sampling.\n\tgcp.Settings\n"
	gcpSettings     = "github.com/cccteam/ccc/cloud/gcp.Settings"
)

// TestEnvTemplateEmbeddedSettings runs the check on a configuration embedding the cloud
// driver's settings struct: the variable the struct declares without a default is
// checked as the application's own are, failing with the struct named when the template
// lacks it, covered when the template documents it, and added by --fix the same way.
func TestEnvTemplateEmbeddedSettings(t *testing.T) {
	t.Parallel()

	const (
		loggingMissing = "pkg/config/config.go:<embedding>: GOOGLE_CLOUD_LOGGING_PROJECT (declared by the embedded " + gcpSettings + ") is not in .envrc.template"
		beaconMissing  = "pkg/config/config.go:<beacon>: LIGHTHOUSE_BEACON_API_KEY (required) is not in .envrc.template"
	)
	tests := []struct {
		name string
		// template is appended to the fixture's template.
		template string
		fix      bool
		// want is the result, with <embedding> and <beacon> for the lines of the embedding
		// and of the application's own missing variable.
		want Result
		// wantTemplate is the template after --fix.
		wantTemplate string
	}{
		{
			name: "a framework variable the template lacks fails, naming the struct",
			want: Result{Name: envTemplate{}.Name(), Status: Fail, Summary: "2 variable(s) missing from .envrc.template (--fix adds them)", Details: []string{loggingMissing, beaconMissing}},
		},
		{
			name:     "the template documenting the variable covers it",
			template: "# export GOOGLE_CLOUD_LOGGING_PROJECT=\nexport LIGHTHOUSE_BEACON_API_KEY=\n",
			want:     pass(envTemplate{}.Name(), "9 env tag(s) covered by .envrc.template"),
		},
		{
			name:         "--fix adds it as it adds the application's",
			fix:          true,
			want:         Result{Name: envTemplate{}.Name(), Status: Pass, Summary: "2 variable(s) added to .envrc.template", Details: []string{loggingMissing, beaconMissing}},
			wantTemplate: "# Lighthouse local development\nexport LIGHTHOUSE_PROJECT_ID=\nexport LIGHTHOUSE_SPANNER_DATABASE=\n# export LIGHTHOUSE_COOKIE_KEY=\n# Added by impulse check --fix: set these for local development.\nexport GOOGLE_CLOUD_LOGGING_PROJECT=\nexport LIGHTHOUSE_BEACON_API_KEY=\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a, src := embeddingFixture(t, tt.template)
			want := tt.want
			want.Details = atLines(t, src, want.Details)
			got := envTemplate{}.Run(context.Background(), &Env{App: a, Fix: tt.fix})
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Run() mismatch (-want +got):\n%s", diff)
			}
			if tt.wantTemplate == "" {
				return
			}
			data, err := os.ReadFile(a.Abs(a.EnvTemplate))
			if err != nil {
				t.Fatalf("os.ReadFile() error = %v", err)
			}
			if diff := cmp.Diff(tt.wantTemplate, string(data)); diff != "" {
				t.Errorf("template after --fix mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// embeddingFixture copies the flat fixture, embeds the cloud driver's settings struct in
// its core configuration, appends the lines to its template, and discovers it; it returns
// the application and the edited config source.
func embeddingFixture(t *testing.T, template string) (fixture *app.App, source string) {
	t.Helper()

	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("..", "..", "app", "testdata", "flat"))); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
	src := editFile(t, filepath.Join(dir, "pkg", "config", "config.go"), func(src string) string {
		for _, edit := range []struct{ old, new string }{{flatTimeImport, flatCloudImport}, {flatDataField, flatDataField + flatEmbedding}} {
			if !strings.Contains(src, edit.old) {
				t.Fatalf("the fixture's config does not contain %q", edit.old)
			}
			src = strings.Replace(src, edit.old, edit.new, 1)
		}

		return src
	})
	if template != "" {
		editFile(t, filepath.Join(dir, ".envrc.template"), func(src string) string {
			return src + template
		})
	}
	a, err := app.Discover(dir)
	if err != nil {
		t.Fatalf("app.Discover() error = %v", err)
	}

	return a, src
}

// editFile rewrites a file of the fixture copy and returns the new content.
func editFile(t *testing.T, path string, edit func(src string) string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	src := edit(string(data))
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	return src
}

// atLines fills <embedding> and <beacon> in the details with the lines the embedding and
// the fixture's own missing variable are declared on in the edited source; a result
// without details stays without.
func atLines(t *testing.T, src string, details []string) []string {
	t.Helper()

	if details == nil {
		return nil
	}
	filled := make([]string, 0, len(details))
	for _, d := range details {
		d = strings.ReplaceAll(d, "<embedding>", strconv.Itoa(sourceLine(t, src, "gcp.Settings")))
		d = strings.ReplaceAll(d, "<beacon>", strconv.Itoa(sourceLine(t, src, "BeaconAPIKey")))
		filled = append(filled, d)
	}

	return filled
}

// sourceLine is the line the needle's first occurrence in the source ends on.
func sourceLine(t *testing.T, src, needle string) int {
	t.Helper()

	at := strings.Index(src, needle)
	if at < 0 {
		t.Fatalf("the source does not contain %q", needle)
	}

	return strings.Count(src[:at+len(needle)], "\n") + 1
}
