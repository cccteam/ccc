package generation

import (
	"errors"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cccteam/ccc/resource"
)

// clientDeclarationDir holds the two stand-ins for @cccteam/resource the compile test
// type-checks the emitted files against: older.d.ts, a client released before the
// descriptor's live and features blocks and the feature gate, and current.d.ts, the
// declaration the next client release publishes with every field today's generator emits.
const clientDeclarationDir = "clientdecl"

// compileTsconfig mirrors the strictness Lodestar's web workspace builds the generated
// files under, so what compiles here compiles there; the default libraries are not
// re-checked, the fixture declaration is.
const compileTsconfig = `{
  "compilerOptions": {
    "strict": true,
    "noEmit": true,
    "target": "ES2022",
    "module": "ES2022",
    "moduleResolution": "bundler",
    "lib": ["ES2022", "DOM"],
    "types": [],
    "skipDefaultLibCheck": true,
    "isolatedModules": true,
    "noUncheckedIndexedAccess": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "noImplicitOverride": true,
    "noPropertyAccessFromIndexSignature": true,
    "noImplicitReturns": true,
    "noFallthroughCasesInSwitch": true
  },
  "include": ["*.ts"]
}
`

// The two literals the client types, as the generator writes them and as it wrote them
// before the wrappers, which the test rewrites them back into.
const (
	descriptorHelperDoc    = "/**\n * Keeps the literal types of the descriptor"
	descriptorWrappedOpen  = "export const apiDescriptor = defineApiDescriptor({\n"
	descriptorTypedOpen    = "export const apiDescriptor: ApiDescriptor = {\n"
	descriptorWrappedClose = "\n});\n\n/** Handles for the global scope"
	descriptorTypedClose   = "\n};\n\n/** Handles for the global scope"

	resourceMetaHelperDoc    = "/**\n * Keeps the literal types of the metadata"
	resourceMetaWrappedOpen  = "const resourceMap = defineResourceMap({\n"
	resourceMetaTypedOpen    = "const resourceMap: ResourceMap = {\n"
	resourceMetaWrappedClose = "\n});\n\nexport function resourceMeta"
	resourceMetaTypedClose   = "\n};\n\nexport function resourceMeta"
)

// Test_typescriptClientFiles_compile type-checks the four emitted client files with tsc
// against the two client declarations. The descriptor and the resource metadata map
// reach the client through defineApiDescriptor and defineResourceMap, so TypeScript
// applies no excess-property check to either literal and the files compile against a
// client that predates a field they carry; the check lives here instead: the two
// literals taken out of their wrappers and typed directly must compile against the
// current declaration, and must not once a field the declaration lacks is in them, so a
// field the generator learns is a field the fixture learns in the same change. The
// unwrapped literals against the older declaration pin that the older fixture really
// predates the gate and the blocks, which is what the first case proves anything by.
// Skipped when no tsc is on this machine.
func Test_typescriptClientFiles_compile(t *testing.T) {
	t.Parallel()

	tsc := findTsc(t)
	files := emitFeatureFixtureClient(t)

	tests := []struct {
		name        string
		declaration string
		// unwrap rewrites the descriptor and the resource map into the form the
		// generator wrote before the wrappers, the literals typed ApiDescriptor and
		// ResourceMap, which puts the excess-property check back on them.
		unwrap bool
		// injectDescriptor is a field added at the top of the unwrapped descriptor,
		// one the declaration does not know.
		injectDescriptor string
		// injectResourceMeta is a field added to the first entry of the unwrapped
		// resource map, one the declaration does not know.
		injectResourceMeta string
		// wantDiagnostics are the diagnostics tsc must report; none means the
		// program compiles.
		wantDiagnostics []string
	}{
		{
			name:        "the wrapped files compile against the older client, which predates live, features and the gate",
			declaration: "older.d.ts",
		},
		{
			name:        "the wrapped files compile against the current client",
			declaration: "current.d.ts",
		},
		{
			name:        "the unwrapped literals compile against the current client: every field the generator emits is declared",
			declaration: "current.d.ts",
			unwrap:      true,
		},
		{
			name:             "the unwrapped descriptor refuses a field the current client does not declare",
			declaration:      "current.d.ts",
			unwrap:           true,
			injectDescriptor: "unknownToTheClient: { route: 'unknown' },",
			wantDiagnostics:  []string{"error TS2353: Object literal may only specify known properties, and 'unknownToTheClient' does not exist in type 'ApiDescriptor'."},
		},
		{
			name:               "the unwrapped resource map refuses a field the current client does not declare",
			declaration:        "current.d.ts",
			unwrap:             true,
			injectResourceMeta: "unknownToTheClient: true,",
			wantDiagnostics:    []string{"Object literal may only specify known properties, and 'unknownToTheClient' does not exist in type 'ResourceMeta'."},
		},
		{
			// TypeScript reports one excess property per literal and elaborates
			// through the first property that fails: the gate on a resource's entry,
			// ahead of the features block at the descriptor's top level.
			name:        "the unwrapped literals refuse the older client: the fixture predates the gate and the blocks",
			declaration: "older.d.ts",
			unwrap:      true,
			wantDiagnostics: []string{
				"Object literal may only specify known properties, and 'feature' does not exist in type 'ResourceDescriptor'.",
				"Object literal may only specify known properties, and 'feature' does not exist in type 'ResourceMeta'.",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			apiFile, resourcesFile := generatedTypescriptFileName("api"), generatedTypescriptFileName("resources")
			emitted := maps.Clone(files)
			if tt.unwrap {
				emitted[apiFile] = unwrapLiteral(t, emitted[apiFile], descriptorHelperDoc, descriptorWrappedOpen, descriptorTypedOpen, descriptorWrappedClose, descriptorTypedClose)
				emitted[resourcesFile] = unwrapLiteral(t, emitted[resourcesFile], resourceMetaHelperDoc, resourceMetaWrappedOpen, resourceMetaTypedOpen, resourceMetaWrappedClose, resourceMetaTypedClose)
			}
			if tt.injectDescriptor != "" {
				emitted[apiFile] = replaceOnce(t, emitted[apiFile], descriptorTypedOpen, descriptorTypedOpen+"  "+tt.injectDescriptor+"\n")
			}
			if tt.injectResourceMeta != "" {
				firstEntry := resourceMetaTypedOpen + "  [Resources.Debriefs]: {\n"
				emitted[resourcesFile] = replaceOnce(t, emitted[resourcesFile], firstEntry, firstEntry+"    "+tt.injectResourceMeta+"\n")
			}

			dir := t.TempDir()
			for name, content := range emitted {
				writeCompileFile(t, filepath.Join(dir, name), content)
			}
			writeCompileFile(t, filepath.Join(dir, "tsconfig.json"), compileTsconfig)
			packageDir := filepath.Join(dir, "node_modules", "@cccteam", "resource")
			writeCompileFile(t, filepath.Join(packageDir, "package.json"), `{"name":"@cccteam/resource","version":"0.0.0","types":"index.d.ts"}`)
			writeCompileFile(t, filepath.Join(packageDir, "index.d.ts"), readClientDeclaration(t, tt.declaration))

			cmd := exec.CommandContext(t.Context(), tsc, "-p", dir, "--pretty", "false")
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			var exitErr *exec.ExitError
			if err != nil && !errors.As(err, &exitErr) {
				t.Fatalf("tsc did not run: %v\n%s", err, out)
			}
			if len(tt.wantDiagnostics) == 0 {
				if err != nil {
					t.Fatalf("tsc reported errors against %s, want a clean compile:\n%s\n--- %s ---\n%s\n--- %s ---\n%s", tt.declaration, out, apiFile, emitted[apiFile], resourcesFile, emitted[resourcesFile])
				}

				return
			}
			if err == nil {
				t.Fatalf("tsc compiled against %s, want %q:\n--- %s ---\n%s\n--- %s ---\n%s", tt.declaration, tt.wantDiagnostics, apiFile, emitted[apiFile], resourcesFile, emitted[resourcesFile])
			}
			for _, want := range tt.wantDiagnostics {
				if !strings.Contains(string(out), want) {
					t.Errorf("tsc output against %s missing %q:\n%s", tt.declaration, want, out)
				}
			}
		})
	}
}

// findTsc is the TypeScript compiler the test runs: the one Lodestar's web workspace
// installs, the version its build uses, else one on PATH; the test skips without either.
func findTsc(t *testing.T) string {
	t.Helper()

	workspace := filepath.Join(generationDir(t), "..", "lodestar", "web", "node_modules", ".bin", "tsc")
	if _, err := os.Stat(workspace); err == nil {
		return workspace
	}
	if path, err := exec.LookPath("tsc"); err == nil {
		return path
	}
	t.Skip("tsc is not installed: the compile test needs the TypeScript compiler, from a bun install in lodestar/web or on PATH")

	return ""
}

// generationDir is this package's source directory, resolved from this file because
// other tests in the package change the working directory.
func generationDir(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}

	return filepath.Dir(thisFile)
}

// readClientDeclaration reads one of the client declaration fixtures.
func readClientDeclaration(t *testing.T, name string) string {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(generationDir(t), "testdata", clientDeclarationDir, name))
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}

	return string(content)
}

// writeCompileFile writes one file of the program under test, creating its directory.
func writeCompileFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
}

// emitFeatureFixtureClient writes the feature fixture's client files (constants,
// resources, methods, api) through the generator's own writers, so the compile test
// reads exactly what a run emits: the live routes, the features block, the gate on a
// resource's and a method's descriptor entry and on the metadata, the library's
// FeatureFlags resource and SetFeature method, and the consolidated route. It returns
// the files by name.
func emitFeatureFixtureClient(t *testing.T) map[string]string {
	t.Helper()

	r := featureFixtureGenerator(t)
	// The collection registers computed resources only when the run generates them,
	// and the constants file names what the collection carries.
	r.genComputedResources = true
	for _, res := range r.resources {
		for _, f := range res.Fields {
			f.typescriptType = "string"
		}
	}
	for _, f := range r.computedResources[0].Fields {
		f.typescriptType = "string"
	}
	collection, err := r.computeCollectionData()
	if err != nil {
		t.Fatalf("computeCollectionData() error = %v", err)
	}

	dir := t.TempDir()
	gen := &typescriptGenerator{
		client:                r.client,
		genMetadata:           true,
		genPermission:         true,
		typescriptDestination: dir,
		rc:                    resource.MustNewGeneratedCollection(collection),
		domainRouteSegment:    r.domainRouteSegment,
		domainRouteParam:      r.domainRouteParam,
	}
	gen.ConsolidatedRoute = "resources"
	if err := gen.generateTypescriptMetadata(); err != nil {
		t.Fatalf("generateTypescriptMetadata() error = %v", err)
	}
	if err := gen.runTypescriptPermissionGeneration(); err != nil {
		t.Fatalf("runTypescriptPermissionGeneration() error = %v", err)
	}

	files := make(map[string]string, 4)
	for _, name := range []string{"constants", "resources", "methods", "api"} {
		content, err := os.ReadFile(filepath.Join(dir, generatedTypescriptFileName(name)))
		if err != nil {
			t.Fatalf("os.ReadFile() error = %v", err)
		}
		files[generatedTypescriptFileName(name)] = string(content)
	}

	return files
}

// unwrapLiteral rewrites one emitted file into the form the generator wrote before the
// wrapper: the helper, from its doc comment to the literal, gone, and the literal typed
// directly.
func unwrapLiteral(t *testing.T, text, helperDoc, wrappedOpen, typedOpen, wrappedClose, typedClose string) string {
	t.Helper()

	helperStart := strings.Index(text, helperDoc)
	literalStart := strings.Index(text, wrappedOpen)
	if helperStart < 0 || literalStart < helperStart {
		t.Fatalf("emitted file lacks the helper ahead of %q:\n%s", wrappedOpen, text)
	}
	text = text[:helperStart] + text[literalStart:]
	text = replaceOnce(t, text, wrappedOpen, typedOpen)

	return replaceOnce(t, text, wrappedClose, typedClose)
}

// replaceOnce replaces old, which the text must carry exactly once, with repl.
func replaceOnce(t *testing.T, text, old, repl string) string {
	t.Helper()

	if n := strings.Count(text, old); n != 1 {
		t.Fatalf("emitted file carries %q %d times, want once:\n%s", old, n, text)
	}

	return strings.Replace(text, old, repl, 1)
}
