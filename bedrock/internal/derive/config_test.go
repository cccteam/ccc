package derive

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/cccteam/ccc/impulse/app"
)

// The framework settings struct the harbor fixture embeds, as the fixture writes it, and
// an alias the test imports it under.
const (
	gcpImport    = `"github.com/cccteam/ccc/cloud/gcp"`
	gcpEmbedding = "\t// The Google Cloud driver's variables: the logging project and the trace sampling.\n\tgcp.Settings\n"
	gcpAlias     = "cloud"
)

// The expanded fields' docs, as the framework declares them.
const (
	loggingProjectDoc = "LoggingProject is the Google Cloud project request logs ship to and spans are\nrecorded in. Empty logs to the console and exports no span: development."
	traceSamplingDoc  = "TraceSampling says which spans are recorded: every one (all), or the ones a request\nGoogle's edge sampled starts (edge)."
)

// expectedVariable is a variable a test expects: the text of the source line it is
// declared on, and what it says about itself (variableLine) with <line> for that line.
type expectedVariable struct {
	needle string
	line   string
}

// The harbor fixture's core variables: the two the struct declares and the two the
// framework settings struct it embeds declares, at the embedding's line.
var (
	appVersionVar  = expectedVariable{needle: "AppVersion string", line: "APP_VERSION core coreConfig.AppVersion string pkg/config/config.go:<line> required=false default=\"dev\" set=true doc=\"AppVersion is the build-time or runtime application version.\""}
	serviceNameVar = expectedVariable{needle: "ServiceName string", line: "APP_SERVICE_NAME core coreConfig.ServiceName string pkg/config/config.go:<line> required=true default=\"\" set=false doc=\"ServiceName names the process in logs.\""}
	loggingVar     = expectedVariable{needle: "gcp.Settings", line: "GOOGLE_CLOUD_LOGGING_PROJECT core coreConfig.LoggingProject string pkg/config/config.go:<line> required=false default=\"\" set=false doc=" + strconv.Quote(loggingProjectDoc)}
	samplingVar    = expectedVariable{needle: "gcp.Settings", line: "APP_TRACE_SAMPLING core coreConfig.TraceSampling string pkg/config/config.go:<line> required=false default=\"edge\" set=true doc=" + strconv.Quote(traceSamplingDoc)}
)

// TestEmbeddedFrameworkSettings reads a config package whose core struct embeds the
// framework's settings struct: the embedding expands into the fields the struct
// declares, each a field of the embedding struct at the embedding's line, placed among
// the variables where the embedding stands; the tags impulse's scan declares at the
// embedding from the same declaration are placed once; an import alias is honored; and
// an embedded type of another package the framework does not declare is refused, as is
// a settings struct embedded outside every level.
func TestEmbeddedFrameworkSettings(t *testing.T) {
	t.Parallel()

	aliased := func(v expectedVariable) expectedVariable {
		v.needle = gcpAlias + ".Settings"

		return v
	}
	tests := []struct {
		name string
		// edit rewrites the fixture's config file; nil keeps it as committed.
		edit func(src string) string
		// scanned adds the tags impulse's scan reads for the embedding: the variables the
		// struct declares, at the embedding's line, naming the struct.
		scanned bool
		// want are the core level's variables in order.
		want []expectedVariable
		// wantErr is the refusal, with <line> for the line of wantErrAt.
		wantErr   string
		wantErrAt string
	}{
		{
			name: "the embedded settings expand into fields of the struct, after the declared ones",
			want: []expectedVariable{appVersionVar, serviceNameVar, loggingVar, samplingVar},
		},
		{
			name:    "the tags impulse's scan declares at the embedding are placed once",
			scanned: true,
			want:    []expectedVariable{appVersionVar, serviceNameVar, loggingVar, samplingVar},
		},
		{
			name: "an embedding before the declared fields places its variables first",
			edit: func(src string) string {
				src = strings.Replace(src, gcpEmbedding, "", 1)

				return strings.Replace(src, "type coreConfig struct {\n", "type coreConfig struct {\n\tgcp.Settings\n", 1)
			},
			want: []expectedVariable{loggingVar, samplingVar, appVersionVar, serviceNameVar},
		},
		{
			name: "an import alias names the settings struct",
			edit: func(src string) string {
				src = strings.Replace(src, gcpImport, gcpAlias+" "+gcpImport, 1)

				return strings.Replace(src, "gcp.Settings", gcpAlias+".Settings", 1)
			},
			want: []expectedVariable{appVersionVar, serviceNameVar, aliased(loggingVar), aliased(samplingVar)},
		},
		{
			name: "an embedded type of another package is refused",
			edit: func(src string) string {
				return strings.Replace(src, gcpImport, `"example.com/other/gcp"`, 1)
			},
			wantErr:   "pkg/config/config.go:<line>: coreConfig embeds gcp.Settings (example.com/other/gcp), which is not a settings struct the framework declares: declare its variables on the struct, or export the struct's declaration from the module that owns it",
			wantErrAt: "gcp.Settings",
		},
		{
			name: "settings embedded outside every level are refused",
			edit: func(src string) string {
				return src + "\n// extraConfig is read by no level.\ntype extraConfig struct {\n\tgcp.Settings\n}\n"
			},
			wantErr:   "pkg/config/config.go:<line>: GOOGLE_CLOUD_LOGGING_PROJECT is declared outside every configuration level",
			wantErrAt: "type extraConfig struct {\n\tgcp.Settings",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(t.TempDir(), "app")
			if err := os.CopyFS(dir, os.DirFS(filepath.Join("testdata", "harbor"))); err != nil {
				t.Fatalf("os.CopyFS() error = %v", err)
			}
			file := filepath.Join(dir, "pkg", "config", "config.go")
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			src := string(data)
			if tt.edit != nil {
				src = tt.edit(src)
				if err := os.WriteFile(file, []byte(src), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			a, err := app.Discover(dir)
			if err != nil {
				t.Fatalf("app.Discover() error = %v", err)
			}
			if tt.scanned {
				a.EnvTags = append(a.EnvTags, scannedTags(t, src)...)
			}
			cfg, err := readConfig(a)
			if tt.wantErr != "" {
				wantErr := atLine(t, src, tt.wantErrAt, tt.wantErr)
				if err == nil || !strings.Contains(err.Error(), wantErr) {
					t.Fatalf("readConfig() error = %v, wantErr %q", err, wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("readConfig() error = %v", err)
			}
			var got []string
			for i := range cfg.variables {
				v := &cfg.variables[i]
				if v.Level == LevelCore {
					got = append(got, variableLine(v))
				}
			}
			want := make([]string, 0, len(tt.want))
			for _, e := range tt.want {
				want = append(want, atLine(t, src, e.needle, e.line))
			}
			if !slices.Equal(got, want) {
				t.Errorf("core variables =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
		})
	}
}

// scannedTags are the tags impulse's scan reads for the fixture's embedding of the
// framework's settings struct: each of its variables at the embedding's line, naming the
// struct as its origin.
func scannedTags(t *testing.T, src string) []app.EnvTag {
	t.Helper()

	line := lineOf(t, src, "gcp.Settings")

	return []app.EnvTag{
		{File: "pkg/config/config.go", Line: line, Name: "GOOGLE_CLOUD_LOGGING_PROJECT"},
		{File: "pkg/config/config.go", Line: line, Name: "APP_TRACE_SAMPLING", HasDefault: true},
	}
}

// atLine fills <line> in text with the line the needle's first occurrence in the source
// ends on.
func atLine(t *testing.T, src, needle, text string) string {
	t.Helper()

	return strings.ReplaceAll(text, "<line>", strconv.Itoa(lineOf(t, src, needle)))
}

// lineOf is the line the needle's first occurrence in the source ends on.
func lineOf(t *testing.T, src, needle string) int {
	t.Helper()

	at := strings.Index(src, needle)
	if at < 0 {
		t.Fatalf("the config file does not contain %q", needle)
	}

	return strings.Count(src[:at+len(needle)], "\n") + 1
}

// variableLine is what a variable says about itself: its name, level, declaration, type,
// place, tag options and doc.
func variableLine(v *Variable) string {
	return fmt.Sprintf("%s %s %s %s %s:%d required=%v default=%q set=%v doc=%q", v.Name, v.Level, v.Declaration(), v.Type, v.File, v.Line, v.Required, v.Default, v.HasDefault, v.Doc)
}
