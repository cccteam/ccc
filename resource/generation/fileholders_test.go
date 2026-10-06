package generation

import (
	"go/format"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/tools/go/packages"
)

// fileFixturePackagePath is the import path of the @file fixture package, which its
// typed structs declare their store in.
const fileFixturePackagePath = generationImportPath + "/testdata/filefixture"

// fileHoldersGenerator builds a generator over the named fixture resources: tables and
// views through fileFixtureResource, computed ones through fileFixtureComputed.
func fileHoldersGenerator(t *testing.T, structs map[string]*parser.Struct, tables, computed []string) *resourceGenerator {
	t.Helper()

	r := &resourceGenerator{client: &client{genComputedResources: len(computed) > 0}}
	r.resource = packageDir("pkg/resources")
	for _, name := range tables {
		res, err := fileFixtureResource(t, structs, name, nil)
		if err != nil {
			t.Fatal(err)
		}
		r.resources = append(r.resources, res)
	}
	for _, name := range computed {
		res, err := fileFixtureComputed(t, structs, name)
		if err != nil {
			t.Fatal(err)
		}
		r.computedResources = append(r.computedResources, res)
	}

	return r
}

// Test_fileHoldersTemplate pins the resources package's generated FileHolders: one
// FileHolderOf per table resource with a stored file, a ComputedFileHolder naming each
// computed resource's key columns and stores, views and file-less resources left out,
// and nil when nothing holds a file.
func Test_fileHoldersTemplate(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "filefixture"))

	tests := []struct {
		name         string
		tables       []string
		computed     []string
		wantContains []string
		wantAbsent   []string
	}{
		{
			name:     "tables and computed resources with stored files",
			tables:   []string{"Document", "NoFile", "TypedDocument"},
			computed: []string{"StoredComputed", "TypedComputed", "Statement"},
			wantContains: []string{
				"func FileHolders() []resource.FileHolder {\n\treturn []resource.FileHolder{\n\t\tresource.FileHolderOf[Document](),\n\t\tresource.FileHolderOf[TypedDocument](),\n\t\tresource.ComputedFileHolder(\"StoredComputeds\", resource.FileKey{Field: \"StoreKey\", Store: resource.DefaultStore}),\n\t\tresource.ComputedFileHolder(\"TypedComputeds\", resource.FileKey{Field: \"StoreKey\", Store: resource.StoreNameFor[Documents]()}),\n\t}\n}",
			},
			wantAbsent: []string{"NoFile", "Statement", "filefixture."},
		},
		{
			name:         "nothing holds a file",
			tables:       []string{"NoFile"},
			wantContains: []string{"func FileHolders() []resource.FileHolder {\n\treturn nil\n}"},
			wantAbsent:   []string{"FileHolderOf", "ComputedFileHolder"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := fileHoldersGenerator(t, structs, tt.tables, tt.computed)
			out, err := r.generateTemplateOutput("fileHoldersTemplate", fileHoldersTemplate, r.fileHoldersData())
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}
			formatted, err := format.Source(out)
			if err != nil {
				t.Fatalf("format.Source() error = %v on:\n%s", err, out)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(string(formatted), want) {
					t.Errorf("file holders missing %q:\n%s", want, formatted)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(string(formatted), absent) {
					t.Errorf("file holders carry %q:\n%s", absent, formatted)
				}
			}
		})
	}
}

// Test_storeExprFrom pins how a store's name is spelled from a package: the default by
// its constant, a local store by its bare type, a foreign one qualified with its import.
func Test_storeExprFrom(t *testing.T) {
	t.Parallel()

	documents := types.NewNamed(types.NewTypeName(token.NoPos, types.NewPackage("example.com/app/pkg/resources", "resources"), "Documents", nil), types.NewStruct(nil, nil), nil)

	tests := []struct {
		name       string
		store      *types.Named
		fromPath   string
		wantExpr   string
		wantImport string
	}{
		{name: "the default store", fromPath: "example.com/app/pkg/resources", wantExpr: "resource.DefaultStore"},
		{name: "a store of the package itself", store: documents, fromPath: "example.com/app/pkg/resources", wantExpr: "resource.StoreNameFor[Documents]()"},
		{name: "a store of another package", store: documents, fromPath: "example.com/app/pkg/computed", wantExpr: "resource.StoreNameFor[resources.Documents]()", wantImport: "example.com/app/pkg/resources"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			expr, importPath := storeExprFrom(tt.store, tt.fromPath)
			if expr != tt.wantExpr || importPath != tt.wantImport {
				t.Errorf("storeExprFrom() = (%q, %q), want (%q, %q)", expr, importPath, tt.wantExpr, tt.wantImport)
			}
		})
	}
}

// Test_validateHolderStores pins the one shape the holders file cannot spell: a computed
// resource's store declared in a package that imports the resources package.
func Test_validateHolderStores(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "filefixture"))
	resourcesPkg := types.NewPackage(fileFixturePackagePath, "filefixture")
	importer := types.NewPackage("example.com/app/pkg/computed", "computed")
	importer.SetImports([]*types.Package{resourcesPkg})
	aside := types.NewPackage("example.com/app/pkg/stores", "stores")
	storeIn := func(pkg *types.Package) *types.Named {
		return types.NewNamed(types.NewTypeName(token.NoPos, pkg, "Scans", nil), types.NewStruct(nil, nil), nil)
	}

	tests := []struct {
		name    string
		store   *types.Named
		wantErr string
	}{
		{name: "the default store"},
		{name: "a store of the resources package", store: storeIn(resourcesPkg)},
		{name: "a store of a package the resources package may import", store: storeIn(aside)},
		{name: "a store of a package importing the resources package is refused", store: storeIn(importer), wantErr: "computed resource StoredComputed: the @file column StoreKey names the store computed.Scans, declared in example.com/app/pkg/computed, which imports the resources package; the resources package's FileHolders names the store, so declare it in the resources package"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := fileHoldersGenerator(t, structs, []string{"Document"}, []string{"StoredComputed"})
			key := r.computedResources[0].Files[0].Key
			key.Store = tt.store
			if tt.store != nil {
				key.storeQualified = typeStringer(tt.store)
			}
			err := r.validateHolderStores()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateHolderStores() error = %v", err)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateHolderStores() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

// Test_validateUploadStores pins that an upload's store is held by some @file column:
// the default by any string key, a named one by a key typed for it.
func Test_validateUploadStores(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "filefixture"))
	rpcStructs := fixtureStructs(loadFixture(t, "rpcform"))
	// The typed fixture's store: the upload streams to it when typed is set.
	typedDocument, err := fileFixtureResource(t, structs, "TypedDocument", nil)
	if err != nil {
		t.Fatal(err)
	}
	documents := typedDocument.Files[0].Key.Store

	tests := []struct {
		name    string
		tables  []string
		typed   bool
		wantErr string
	}{
		{name: "the default store held by a string key", tables: []string{"Document"}},
		{name: "the default store held by no column is refused", tables: []string{"TypedDocument"}, wantErr: "struct UploadForm: @upload streams to the default store, and no @file column typed string holds its keys"},
		{name: "a named store held by a typed key", tables: []string{"TypedDocument"}, typed: true},
		{name: "a named store held by no column is refused", tables: []string{"Document"}, typed: true, wantErr: "struct UploadForm: @upload(store: filefixture.Documents) names a store no @file column holds; type the column the body records its keys in resource.Key[filefixture.Documents], or name that column's store"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := fileHoldersGenerator(t, structs, tt.tables, nil)
			r.genRPCMethods = true
			upload := &rpcUpload{MaxBytes: 1}
			if tt.typed {
				upload.Store = documents
			}
			r.rpcMethods = []*rpcMethodInfo{{Struct: rpcStructs["UploadForm"], Form: rpcFormTxn, takesFiles: true, filesStore: upload.Store, Upload: upload}}
			err := r.validateUploadStores()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateUploadStores() error = %v", err)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateUploadStores() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

// Test_validateStoreTypes pins that a named store's type comes from a package the run
// loaded, since the generated code spells it by that package's name.
func Test_validateStoreTypes(t *testing.T) {
	t.Parallel()

	loaded, parsed := loadFixturePackage(t, "filefixture")
	structs := fixtureStructs(parsed)

	tests := []struct {
		name    string
		loaded  map[string]*packages.Package
		wantErr string
	}{
		{name: "a store of a loaded package", loaded: map[string]*packages.Package{"filefixture": loaded}},
		{name: "a store of a package the run did not load is refused", loaded: map[string]*packages.Package{}, wantErr: "the @file column TypedDocument.StoreKey names the store filefixture.Documents, declared in " + fileFixturePackagePath + ", which this generator run does not read"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := fileHoldersGenerator(t, structs, []string{"TypedDocument"}, nil)
			r.loadedPackages = tt.loaded
			err := r.validateStoreTypes()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateStoreTypes() error = %v", err)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateStoreTypes() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

// Test_servedRouter_fileStores pins the router's start-up requirement: the Handlers
// interface asks for the resource client, New requires every store the generated code
// uses on it, and the router test's stub wires a stub store under each.
func Test_servedRouter_fileStores(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "filefixture"))
	outlets := []routerOutlet{{name: "default", prefix: "api", servesSessions: true, auth: &outletAuth{importPath: "example.com/acme/beacon/pkg/config", flavor: OIDCAzure}}}

	tests := []struct {
		name           string
		tables         []string
		wantRouter     []string
		wantRouterTest []string
		wantAbsent     []string
	}{
		{
			name:   "the default and a named store",
			tables: []string{"Document", "TypedDocument"},
			wantRouter: []string{
				"\t\"" + fileFixturePackagePath + "\"\n",
				"\tResourceClient() resource.Client\n",
				"\tif err := resource.RequireFileStores(h.ResourceClient(), resource.DefaultStore, resource.StoreNameFor[filefixture.Documents]()); err != nil {\n\t\tpanic(fmt.Sprintf(\"router.New: %v\", err))\n\t}\n",
			},
			wantRouterTest: []string{
				"\t\"context\"\n",
				"\t\"" + fileFixturePackagePath + "\"\n",
				"func (s *routerHandlersStub) ResourceClient() resource.Client {\n\treturn resource.NewMockClient(nil, nil, nil, resource.WithFileStore(stubFileStore{}), resource.WithNamedFileStore[filefixture.Documents](stubFileStore{}))\n}",
				"type stubFileStore struct{}",
			},
		},
		{
			name:       "no stored file asks for no client",
			tables:     []string{"NoFile"},
			wantAbsent: []string{"ResourceClient", "RequireFileStores", "stubFileStore", fileFixturePackagePath},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := fileHoldersGenerator(t, structs, tt.tables, nil)
			r.router = packageDir("pkg/router")
			data := r.servedRouterData(outlets, nil, nil)
			router := render(t, r, "servedRouterTemplate", servedRouterTemplate, data)
			routerTest := render(t, r, "servedRouterTestTemplate", servedRouterTestTemplate, data)
			for _, want := range tt.wantRouter {
				if !strings.Contains(router, want) {
					t.Errorf("router missing %q:\n%s", want, router)
				}
			}
			for _, want := range tt.wantRouterTest {
				if !strings.Contains(routerTest, want) {
					t.Errorf("router test missing %q:\n%s", want, routerTest)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(router, absent) || strings.Contains(routerTest, absent) {
					t.Errorf("router or its test carries %q:\n%s\n%s", absent, router, routerTest)
				}
			}
		})
	}
}

// Test_storesUsed pins the order and the dedupe of the stores a package uses: the
// default first, each named store once, uploads counted beside columns.
func Test_storesUsed(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "filefixture"))
	r := fileHoldersGenerator(t, structs, []string{"TypedDocument", "Document", "Logo"}, []string{"TypedComputed"})

	uses := r.storesUsed()
	got := make([]string, 0, len(uses))
	for _, use := range uses {
		got = append(got, use.NameExpr()+" from "+use.Where)
	}
	want := []string{
		"resource.DefaultStore from the @file column Document.StoreKey",
		"resource.StoreNameFor[filefixture.Documents]() from the @file column TypedDocument.StoreKey",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("storesUsed() mismatch (-want +got):\n%s", diff)
	}
}

// Test_typedKeys_crissCrossDoesNotTypeCheck type-checks the crisscross fixture, which
// hands a key minted for one store to a setter typed for another, and pins that the Go
// type checker refuses it: the typed keys hold at compile time, so a body cannot record
// a file's key in a column of another store.
func Test_typedKeys_crissCrossDoesNotTypeCheck(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() error = %v", err)
	}
	dir, err := filepath.Rel(cwd, filepath.Join(filepath.Dir(thisFile), "testdata", "crisscross"))
	if err != nil {
		t.Fatalf("filepath.Rel() error = %v", err)
	}
	pkgs, err := packages.Load(&packages.Config{Mode: packages.NeedName | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps}, "./"+dir)
	if err != nil {
		t.Fatalf("packages.Load() error = %v", err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("packages.Load() loaded %d packages, want 1", len(pkgs))
	}
	messages := make([]string, 0, len(pkgs[0].Errors))
	for _, e := range pkgs[0].Errors {
		messages = append(messages, e.Msg)
	}
	if len(messages) != 1 {
		t.Fatalf("type errors = %q, want exactly the criss-crossed setter call", messages)
	}
	for _, want := range []string{"cannot use photo.Key", "Key[Photos]", "Key[Documents]", "SetStoreKey"} {
		if !strings.Contains(messages[0], want) {
			t.Errorf("type error %q does not mention %q", messages[0], want)
		}
	}
}
