package generation

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// Every payload whose template renders parsed types must scope import
// resolution to what it renders; forgetting the interface degrades that
// payload's files to goimports fallback warnings.
var (
	_ typeImporter = (*resourceInterfacesData)(nil)
	_ typeImporter = (*resourceFileData)(nil)
	_ typeImporter = (*handlersFileData)(nil)
	_ typeImporter = (*consolidatedPatchData)(nil)
	_ typeImporter = (*computedHandlerData)(nil)
	_ typeImporter = (*rpcFileData)(nil)
	_ typeImporter = (*rpcHandlerData)(nil)
	_ typeImporter = (*rpcInterfacesData)(nil)
)

// scopedPayload is a minimal typeImporter for exercising payload-scoped import
// resolution in tests.
type scopedPayload struct {
	imports []fixerImport
}

func (s scopedPayload) typeImports() []fixerImport {
	return s.imports
}

// Test_typeImports_scopedToPayload pins the reason typeImports is per payload
// rather than a union over all parsed resources: two resources may use
// same-named packages from different import paths, and only a file rendering
// both should see an ambiguity.
func Test_typeImports_scopedToPayload(t *testing.T) {
	t.Parallel()

	src := `package resources

type Widget struct {
	Kind types.Kind
}
`

	tests := []struct {
		name        string
		known       []fixerImport
		wantImport  string
		wantUnknown []string
	}{
		{
			name:       "scoped to one resource the qualifier is unambiguous",
			known:      []fixerImport{{name: "types", path: "example.com/foo/types"}},
			wantImport: `"example.com/foo/types"`,
		},
		{
			name: "union of both resources turns the same file ambiguous",
			known: []fixerImport{
				{name: "types", path: "example.com/foo/types"},
				{name: "types", path: "example.com/bar/types"},
			},
			wantUnknown: []string{"types (ambiguous: example.com/foo/types, example.com/bar/types)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fixed, unknown, err := newImportFixer(tt.known, nil).fix("a.go", []byte(src))
			if err != nil {
				t.Fatalf("importFixer.fix() error = %v", err)
			}

			if diff := cmp.Diff(tt.wantUnknown, unknown); diff != "" {
				t.Fatalf("importFixer.fix() unknown mismatch (-want +got):\n%s", diff)
			}

			if tt.wantImport != "" && !strings.Contains(string(fixed), tt.wantImport) {
				t.Errorf("importFixer.fix() should import %s; got:\n%s", tt.wantImport, fixed)
			}
		})
	}
}

// Test_formatGoBytes_usesPayloadScope verifies formatGoBytes resolves imports
// from the payload's typeImports: the import path below is not resolvable any
// other way (not declared, not stdlib, not a local package), so its presence in
// the output proves the payload scope was used.
func Test_formatGoBytes_usesPayloadScope(t *testing.T) {
	t.Parallel()

	src := `package resources

type Widget struct {
	Kind fakepkg.Kind
}
`

	tests := []struct {
		name         string
		data         any
		wantImport   string
		wantNoImport string
	}{
		{
			name:       "payload scope resolves the import",
			data:       scopedPayload{imports: []fixerImport{{name: "fakepkg", path: "example.com/fake/v2"}}},
			wantImport: `import fakepkg "example.com/fake/v2"`,
		},
		{
			name:         "payload without the qualifier in scope falls back to goimports",
			data:         scopedPayload{},
			wantNoImport: "example.com/fake",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &client{}
			got, err := c.formatGoBytes("widget.go", "test", []byte(src), tt.data)
			if err != nil {
				t.Fatalf("formatGoBytes() error = %v", err)
			}

			if tt.wantImport != "" && !strings.Contains(string(got), tt.wantImport) {
				t.Errorf("formatGoBytes() should add the payload-scoped import; got:\n%s", got)
			}

			if tt.wantNoImport != "" && strings.Contains(string(got), tt.wantNoImport) {
				t.Errorf("formatGoBytes() should not resolve %s without it in scope; got:\n%s", tt.wantNoImport, got)
			}
		})
	}
}
