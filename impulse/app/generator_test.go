package app

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// programSource renders a generator program: a main package importing the generation
// package and the extra imports, each as its import spec reads (`"cloud.google.com/go/logging"`,
// with a local name when wanted), passing the options after the positional arguments.
func programSource(options string, imports ...string) string {
	var b strings.Builder
	b.WriteString("package main\n\nimport (\n\t\"context\"\n\n")
	for _, imp := range imports {
		b.WriteString("\t" + imp + "\n")
	}
	b.WriteString("\t\"github.com/cccteam/ccc/resource/generation\"\n)\n\n")
	b.WriteString("func main() {\n\t_, _ = generation.NewResourceGenerator(\n\t\tcontext.Background(),\n\t\t\"pkg/resources\",\n\t\t[]string{\"file://schema/migrations\"},\n")
	b.WriteString(options)
	b.WriteString("\n\t)\n}\n")

	return b.String()
}

// loggingImport is the logging library's import spec, which a program chaining a
// MinSeverity floor needs.
const loggingImport = `"cloud.google.com/go/logging"`

// The kinds the reader names when it refuses a request log word, a trace setting, a
// severity floor, or a body limit.
const (
	requestLogKind = "generation.<RequestLog> call (LogAlways(), LogOnEvent(), LogSampled(fraction) or LogNever(), with .MinSeverity(logging.<Severity>) chained when wanted)"
	tracesKind     = "generation.<Traces> call (TracesFollowFrontEnd(), TracesCapped(rate) or TracesOff())"
	severityKind   = "logging.<Severity> constant (Default, Debug, Info, Notice, Warning, Error, Critical, Alert or Emergency)"
	bodyLimitKind  = "whole number literal (4194304) or constant expression (2<<20)"
)

func TestParseGeneratorProblems(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		options string
		// imports are the program's imports beyond the generation package.
		imports      []string
		wantProblems []string
	}{
		{
			name:    "clean program",
			options: `generation.GenerateHandlers("app"), generation.WithConcealedDomains(),`,
		},
		{
			name:         "unknown option",
			options:      `generation.WithFrobnicator("x"),`,
			wantProblems: []string{"unknown option generation.WithFrobnicator"},
		},
		{
			name:         "retired option",
			options:      `generation.WithDomainRoute("tenants"),`,
			wantProblems: []string{"retired option generation.WithDomainRoute: the tenant segment derives from the tenant record, the resource struct annotated @tenant; remove the option and annotate the record"},
		},
		{
			name:         "non-literal argument",
			options:      `generation.GenerateHandlers(dir),`,
			wantProblems: []string{"GenerateHandlers argument dir is not a literal"},
		},
		{
			name:         "too few arguments",
			options:      `generation.GenerateRoutes("pkg/router"),`,
			wantProblems: []string{"GenerateRoutes takes 2 argument(s), found 1"},
		},
		{
			name:         "too many arguments",
			options:      `generation.WithRPC("pkg/rpc", "extra"),`,
			wantProblems: []string{"WithRPC takes 1 argument(s), found 2"},
		},
		{
			name:    "WithTypes may be given more than once",
			options: `generation.WithTypes("pkg/telemetry"), generation.WithTypes("pkg/cms"),`,
		},
		{
			name:         "wrong literal kind",
			options:      `generation.WithConsolidatedHandlers("resources", "yes"),`,
			wantProblems: []string{`WithConsolidatedHandlers argument "yes" should be a bool literal`},
		},
		{
			name:    "the router options with a flavor identifier",
			options: `generation.GenerateRouter(), generation.GenerateRoutes("pkg/router", "api", generation.Auth("example.com/acme/pkg/auth/staff", generation.Password), generation.WebApp("/")), generation.WithRouterOutlet("machines", "machines", generation.APIKey()),`,
		},
		{
			name:    "the oldest answered release as a string or as this release",
			options: `generation.GenerateRouter(), generation.GenerateRoutes("pkg/router", "api", generation.Auth("example.com/acme/pkg/auth/staff", generation.Password), generation.WebApp("/"), generation.OldestAnswered("1.5.0")), generation.WithRouterOutlet("portal", "portal/api", generation.Auth("example.com/acme/pkg/auth/members", generation.OIDCAzure), generation.OldestAnswered(generation.ThisRelease)),`,
		},
		{
			name:         "an oldest answered release that is another identifier",
			options:      `generation.GenerateRoutes("pkg/router", "api", generation.OldestAnswered(generation.Latest)),`,
			wantProblems: []string{`OldestAnswered argument generation.Latest should be a release string literal ("1.5.0") or generation.ThisRelease`},
		},
		{
			name:         "an oldest answered release that is not a string",
			options:      `generation.GenerateRoutes("pkg/router", "api", generation.OldestAnswered(true)),`,
			wantProblems: []string{`OldestAnswered argument true should be a release string literal ("1.5.0") or generation.ThisRelease`},
		},
		{
			name:         "an auth flavor that is not one of the generation package's",
			options:      `generation.GenerateRoutes("pkg/router", "api", generation.Auth("example.com/acme/pkg/auth/staff", generation.LDAP)),`,
			wantProblems: []string{"Auth argument generation.LDAP should be a generation.<Flavor> identifier (Password, OIDCGoogle, or OIDCAzure)"},
		},
		{
			name:         "an auth flavor written as a string",
			options:      `generation.GenerateRoutes("pkg/router", "api", generation.Auth("example.com/acme/pkg/auth/staff", "password")),`,
			wantProblems: []string{`Auth argument "password" should be a generation.<Flavor> identifier (Password, OIDCGoogle, or OIDCAzure)`},
		},
		{
			name:         "ts option at top level",
			options:      `generation.GenerateEnums(),`,
			wantProblems: []string{"GenerateEnums is a TSOption, but a ResourceOption is expected here"},
		},
		{
			name:         "resource option nested in typescript",
			options:      `generation.GenerateTypescript("gui/src", generation.WithRPC("pkg/rpc")),`,
			wantProblems: []string{"WithRPC is a ResourceOption, but a TSOption is expected here"},
		},
		{
			name:         "not a call",
			options:      `myOption,`,
			wantProblems: []string{"expected a ResourceOption call, found myOption"},
		},
		{
			name:         "call from another package",
			options:      `other.Option(),`,
			wantProblems: []string{"expected a generation.<Option>() call, found other.Option(...)"},
		},
		{
			name:    "manual registrations are composites",
			options: `generation.WithManualResources(generation.ManualRegistration{Resource: "Beacons"}),`,
		},
		{
			name:         "bool map with string value",
			options:      `generation.CaserInitialismOverrides(map[string]bool{"ID": true}), generation.WithPluralOverrides(map[string]bool{"a": true}),`,
			wantProblems: []string{"WithPluralOverrides argument map[string]bool{...} should be a map[string]string literal"},
		},
		{
			name:    "the request log words and trace settings, each written as its constructor",
			options: `generation.WithRequestLog(generation.LogAlways()), generation.WithMountedRoutes("/beacons/", generation.LogOnEvent(), generation.TracesOff()), generation.WithMountedRoutes("/pulses/", generation.LogSampled(0.1), generation.TracesCapped(0.25)), generation.WithMountedRoutes("/health", generation.LogNever(), generation.TracesFollowFrontEnd()),`,
		},
		{
			name:    "an outlet's word and setting ride its declaration",
			options: `generation.GenerateRoutes("pkg/router", "api", generation.OutletRequestLog(generation.LogOnEvent()), generation.OutletTraces(generation.TracesOff())), generation.WithRouterOutlet("machines", "machines", generation.OutletRequestLog(generation.LogNever()), generation.OutletTraces(generation.TracesCapped(0.5))),`,
		},
		{
			name:    "a word with a severity floor chained",
			options: `generation.WithRequestLog(generation.LogOnEvent().MinSeverity(logging.Warning)),`,
			imports: []string{loggingImport},
		},
		{
			name:    "a body limit as a literal or a constant expression",
			options: `generation.WithBodyLimit(4194304), generation.WithBodyLimit(2<<20), generation.WithBodyLimit((1+1)*1024*1024),`,
		},
		{
			name:         "a word that is a variable",
			options:      `generation.WithRequestLog(word),`,
			wantProblems: []string{"WithRequestLog argument word should be a " + requestLogKind},
		},
		{
			name:         "a word that is another package's call",
			options:      `generation.WithRequestLog(logger.OnEvent()),`,
			wantProblems: []string{"WithRequestLog argument logger.OnEvent(...) should be a " + requestLogKind},
		},
		{
			name:         "a word that is not one of the constructors",
			options:      `generation.WithRequestLog(generation.LogLoudly()),`,
			wantProblems: []string{"WithRequestLog argument generation.LogLoudly(...) should be a " + requestLogKind},
		},
		{
			name:         "a trace setting where a word is expected",
			options:      `generation.WithRequestLog(generation.TracesOff()),`,
			wantProblems: []string{"WithRequestLog argument generation.TracesOff(...) should be a " + requestLogKind},
		},
		{
			name:         "a word where a trace setting is expected",
			options:      `generation.WithMountedRoutes("/beacons/", generation.LogOnEvent(), generation.LogNever()),`,
			wantProblems: []string{"WithMountedRoutes argument generation.LogNever(...) should be a " + tracesKind},
		},
		{
			name:         "a floor chained onto a trace setting",
			options:      `generation.GenerateRoutes("pkg/router", "api", generation.OutletTraces(generation.TracesOff().MinSeverity(logging.Warning))),`,
			imports:      []string{loggingImport},
			wantProblems: []string{"OutletTraces argument generation.TracesOff(...).MinSeverity(...) should be a " + tracesKind},
		},
		{
			name:         "a fraction that is a variable",
			options:      `generation.WithRequestLog(generation.LogSampled(share)),`,
			wantProblems: []string{"LogSampled argument share should be a fraction literal (0.1)"},
		},
		{
			name:         "a rate written as a string",
			options:      `generation.WithMountedRoutes("/beacons/", generation.LogOnEvent(), generation.TracesCapped("0.1")),`,
			wantProblems: []string{`TracesCapped argument "0.1" should be a rate literal (0.1)`},
		},
		{
			name:         "constructors with the wrong number of arguments",
			options:      `generation.WithRequestLog(generation.LogAlways(1)), generation.WithRequestLog(generation.LogSampled()),`,
			wantProblems: []string{"LogAlways takes 0 argument(s), found 1", "LogSampled takes 1 argument(s), found 0"},
		},
		{
			name:         "a floor that is not a severity of the library",
			options:      `generation.WithRequestLog(generation.LogOnEvent().MinSeverity(logging.Loud)),`,
			imports:      []string{loggingImport},
			wantProblems: []string{"MinSeverity argument logging.Loud should be a " + severityKind},
		},
		{
			name:         "a floor naming a library the file does not import",
			options:      `generation.WithRequestLog(generation.LogOnEvent().MinSeverity(logging.Warning)),`,
			wantProblems: []string{"MinSeverity argument logging.Warning should be a " + severityKind},
		},
		{
			name:         "a floor that is a variable",
			options:      `generation.WithRequestLog(generation.LogOnEvent().MinSeverity(floor)),`,
			imports:      []string{loggingImport},
			wantProblems: []string{"MinSeverity argument floor should be a " + severityKind},
		},
		{
			name:         "a floor with the wrong number of arguments",
			options:      `generation.WithRequestLog(generation.LogOnEvent().MinSeverity()),`,
			imports:      []string{loggingImport},
			wantProblems: []string{"MinSeverity takes 1 argument(s), found 0"},
		},
		{
			name:         "a body limit that is a variable",
			options:      `generation.WithBodyLimit(limit),`,
			wantProblems: []string{"WithBodyLimit argument limit is not a literal"},
		},
		{
			name:         "a body limit written as a string",
			options:      `generation.WithBodyLimit("4MB"),`,
			wantProblems: []string{`WithBodyLimit argument "4MB" should be a ` + bodyLimitKind},
		},
		{
			name:         "a body limit dividing, or shifting by more than a word",
			options:      `generation.WithBodyLimit(8/2), generation.WithBodyLimit(1<<64),`,
			wantProblems: []string{"WithBodyLimit argument 8/2 is not a literal", "WithBodyLimit argument 1<<64 is not a literal"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g, err := parseGenerator("main.go", []byte(programSource(tt.options, tt.imports...)))
			if err != nil {
				t.Fatalf("parseGenerator() error = %v", err)
			}
			if g == nil {
				t.Fatal("parseGenerator() = nil, want a generator")
			}
			if len(g.Problems) != len(tt.wantProblems) {
				t.Fatalf("got %d problems, want %d:\n%s", len(g.Problems), len(tt.wantProblems), problems(g))
			}
			for i, want := range tt.wantProblems {
				if !strings.Contains(g.Problems[i].Message, want) {
					t.Errorf("problem %d = %q, want containing %q", i, g.Problems[i].Message, want)
				}
				if !strings.HasPrefix(g.Problems[i].Pos, "main.go:") {
					t.Errorf("problem %d position = %q, want main.go:<line>", i, g.Problems[i].Pos)
				}
			}
		})
	}
}

// TestParseGeneratorRecordsDeclarations pins how the reader records a request log word, a
// trace setting and a body limit: the constructor without the package qualifier, its
// number spelled the shortest way that reads back exactly, the floor as logging.<Severity>
// whatever name the file imports the library under, and the body limit as its value. The
// handoff guard compares option sets by these texts.
func TestParseGeneratorRecordsDeclarations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		option  string
		imports []string
		want    []Arg
	}{
		{
			name:   "a body limit as a constant expression records its value",
			option: `generation.WithBodyLimit(2<<20),`,
			want:   []Arg{{Kind: ArgInt, Int: 2097152}},
		},
		{
			name:   "a body limit with parentheses and products",
			option: `generation.WithBodyLimit((1+1)*1024*1024),`,
			want:   []Arg{{Kind: ArgInt, Int: 2097152}},
		},
		{
			name:    "a sampled word with a floor",
			option:  `generation.WithRequestLog(generation.LogSampled(0.10).MinSeverity(logging.Warning)),`,
			imports: []string{loggingImport},
			want:    []Arg{{Kind: ArgConstructor, Str: "LogSampled(0.1).MinSeverity(logging.Warning)"}},
		},
		{
			name:    "a floor through the library's local name",
			option:  `generation.WithRequestLog(generation.LogOnEvent().MinSeverity(gcl.Error)),`,
			imports: []string{"gcl " + loggingImport},
			want:    []Arg{{Kind: ArgConstructor, Str: "LogOnEvent().MinSeverity(logging.Error)"}},
		},
		{
			name:   "a mounted prefix with a whole-number rate and an exponent fraction",
			option: `generation.WithMountedRoutes("/beacons/", generation.LogSampled(1e-2), generation.TracesCapped(1)),`,
			want:   []Arg{{Kind: ArgString, Str: "/beacons/"}, {Kind: ArgConstructor, Str: "LogSampled(0.01)"}, {Kind: ArgConstructor, Str: "TracesCapped(1)"}},
		},
		{
			name:   "an outlet's word and setting nest in the outlet options",
			option: `generation.WithRouterOutlet("machines", "machines", generation.OutletRequestLog(generation.LogNever()), generation.OutletTraces(generation.TracesOff())),`,
			want: []Arg{
				{Kind: ArgString, Str: "machines"},
				{Kind: ArgString, Str: "machines"},
				{Kind: ArgCall, Call: &Call{Name: "OutletRequestLog", Pos: "main.go:14", Args: []Arg{{Kind: ArgConstructor, Str: "LogNever()"}}}},
				{Kind: ArgCall, Call: &Call{Name: "OutletTraces", Pos: "main.go:14", Args: []Arg{{Kind: ArgConstructor, Str: "TracesOff()"}}}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g, err := parseGenerator("main.go", []byte(programSource(tt.option, tt.imports...)))
			if err != nil {
				t.Fatalf("parseGenerator() error = %v", err)
			}
			if g == nil || len(g.Options) != 1 {
				t.Fatalf("parseGenerator() read %+v, want one option", g)
			}
			if len(g.Problems) != 0 {
				t.Fatalf("parseGenerator() problems:\n%s", problems(g))
			}
			if diff := cmp.Diff(tt.want, g.Options[0].Args); diff != "" {
				t.Errorf("Args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// lodestarProgram is the generator program of Lodestar, the framework's demonstration
// application in this repository, relative to Lodestar's root.
const lodestarProgram = "cmd/generate/generator.go"

// TestParseGeneratorLodestar reads Lodestar's generator program from the repository: it
// declares a prefix mounted by hand with its request log word and trace setting, and a
// body limit as a constant expression, and the reader reads it completely.
func TestParseGeneratorLodestar(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile(filepath.Join("..", "..", "resource", "lodestar", lodestarProgram))
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	g, err := parseGenerator(lodestarProgram, src)
	if err != nil {
		t.Fatalf("parseGenerator() error = %v", err)
	}
	if g == nil {
		t.Fatal("parseGenerator() = nil, want Lodestar's generator")
	}
	if len(g.Problems) != 0 {
		t.Fatalf("parseGenerator() problems:\n%s", problems(g))
	}

	tests := []struct {
		name string
		got  any
		want any
	}{
		{name: "mounted prefixes", got: g.MountedRoutes(), want: []MountedRoutes{{Prefix: "/beacons/", RequestLog: "LogOnEvent()", Traces: "TracesOff()"}}},
		{name: "application request log", got: g.RequestLog(), want: ""},
		{name: "body limit", got: mustOption(t, g, "WithBodyLimit").Args, want: []Arg{{Kind: ArgInt, Int: 2 << 20}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tt.want, tt.got, cmpopts.IgnoreFields(MountedRoutes{}, "Pos")); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestParseGeneratorNotAProgram(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
	}{
		{
			name: "no generation import",
			src:  "package main\n\nfunc main() { NewResourceGenerator() }\nfunc NewResourceGenerator() {}\n",
		},
		{
			name: "import without the call",
			src:  "package main\n\nimport _ \"github.com/cccteam/ccc/resource/generation\"\n\nfunc main() {}\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g, err := parseGenerator("main.go", []byte(tt.src))
			if err != nil {
				t.Fatalf("parseGenerator() error = %v", err)
			}
			if g != nil {
				t.Errorf("parseGenerator() = %+v, want nil", g)
			}
		})
	}
}

func TestParseEnvTagsSecret(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		src     string
		want    []EnvTag
		wantErr string
	}{
		{
			name: "the secret tag marks the variable",
			src:  "package config\n\ntype dataConfig struct {\n\tCookieKey string `env:\"APP_COOKIE_KEY\" secret:\"true\"`\n\tClientID string `env:\"APP_CLIENT_ID\"`\n\tLicenseKey string `env:\"APP_LICENSE_KEY\" secret:\"false\"`\n}\n",
			want: []EnvTag{
				{File: "pkg/config/data.go", Line: 4, Name: "APP_COOKIE_KEY", Secret: true},
				{File: "pkg/config/data.go", Line: 5, Name: "APP_CLIENT_ID"},
				{File: "pkg/config/data.go", Line: 6, Name: "APP_LICENSE_KEY"},
			},
		},
		{
			name:    "another value is refused",
			src:     "package config\n\ntype dataConfig struct {\n\tCookieKey string `env:\"APP_COOKIE_KEY\" secret:\"yes\"`\n}\n",
			wantErr: `pkg/config/data.go:4: secret:"yes" on APP_COOKIE_KEY: the secret tag takes "true" or "false"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseEnvTags("pkg/config/data.go", []byte(tt.src))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parseEnvTags() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("parseEnvTags() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseEnvTags() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// gcpSettingsOrigin is what a tag the cloud driver's settings struct declares names as its
// origin: the struct's import path and type name.
const gcpSettingsOrigin = "github.com/cccteam/ccc/cloud/gcp.Settings"

// TestParseEnvTagsEmbedded reads a configuration embedding the cloud driver's settings
// struct: the struct's variables are declared at the embedding, in its place among the
// struct's own, naming the struct they came from; an import alias and a pointer embedding
// resolve the same; an embedded type of another package, or of the application's own,
// is not expanded.
func TestParseEnvTagsEmbedded(t *testing.T) {
	t.Parallel()

	const (
		gcpImport   = "import \"github.com/cccteam/ccc/cloud/gcp\"\n"
		serviceName = "\tServiceName string `env:\"APP_SERVICE_NAME,required\"`\n"
	)
	serviceNameTag := func(line int) EnvTag {
		return EnvTag{File: "pkg/config/config.go", Line: line, Name: "APP_SERVICE_NAME", Required: true}
	}
	declared := func(line int) []EnvTag {
		return []EnvTag{
			{File: "pkg/config/config.go", Line: line, Name: "GOOGLE_CLOUD_LOGGING_PROJECT", Origin: gcpSettingsOrigin},
			{File: "pkg/config/config.go", Line: line, Name: "APP_TRACE_SAMPLING", HasDefault: true, Origin: gcpSettingsOrigin},
		}
	}
	tests := []struct {
		name string
		src  string
		want []EnvTag
	}{
		{
			name: "the embedded settings declare their variables at the embedding, after the declared field",
			src:  "package config\n\n" + gcpImport + "\ntype coreConfig struct {\n" + serviceName + "\tgcp.Settings\n}\n",
			want: append([]EnvTag{serviceNameTag(6)}, declared(7)...),
		},
		{
			name: "an embedding before the declared field places its variables first",
			src:  "package config\n\n" + gcpImport + "\ntype coreConfig struct {\n\tgcp.Settings\n" + serviceName + "}\n",
			want: append(declared(6), serviceNameTag(7)),
		},
		{
			name: "an import alias names the struct",
			src:  "package config\n\nimport cloud \"github.com/cccteam/ccc/cloud/gcp\"\n\ntype coreConfig struct {\n" + serviceName + "\tcloud.Settings\n}\n",
			want: append([]EnvTag{serviceNameTag(6)}, declared(7)...),
		},
		{
			name: "a pointer embeds the struct too",
			src:  "package config\n\n" + gcpImport + "\ntype coreConfig struct {\n" + serviceName + "\t*gcp.Settings\n}\n",
			want: append([]EnvTag{serviceNameTag(6)}, declared(7)...),
		},
		{
			name: "an embedded type of another package is not expanded",
			src:  "package config\n\nimport \"example.com/other/gcp\"\n\ntype coreConfig struct {\n" + serviceName + "\tgcp.Settings\n}\n",
			want: []EnvTag{serviceNameTag(6)},
		},
		{
			name: "an embedded struct of the application is not expanded; its own tags are read where it is declared",
			src:  "package config\n\ntype coreConfig struct {\n\tSettings\n" + serviceName + "}\n\ntype Settings struct {\n\tProject string `env:\"APP_PROJECT\"`\n}\n",
			want: []EnvTag{serviceNameTag(5), {File: "pkg/config/config.go", Line: 9, Name: "APP_PROJECT"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseEnvTags("pkg/config/config.go", []byte(tt.src))
			if err != nil {
				t.Fatalf("parseEnvTags() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("parseEnvTags() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestParseEnvTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		value  string
		want   EnvTag
		wantOK bool
	}{
		{name: "plain", value: "APP_HOST", want: EnvTag{Name: "APP_HOST"}, wantOK: true},
		{name: "required", value: "APP_KEY,required", want: EnvTag{Name: "APP_KEY", Required: true}, wantOK: true},
		{name: "default", value: "APP_PORT,default=8080", want: EnvTag{Name: "APP_PORT", HasDefault: true}, wantOK: true},
		{name: "default with expansion", value: "APP_X, default=$APP_Y", want: EnvTag{Name: "APP_X", HasDefault: true}, wantOK: true},
		{name: "prefix only", value: ",prefix=APP_", wantOK: false},
		{name: "empty", value: "", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := parseEnvTag(tt.value)
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("parseEnvTag(%q) = (%+v, %v), want (%+v, %v)", tt.value, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
