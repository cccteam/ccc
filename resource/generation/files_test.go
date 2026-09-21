package generation

import (
	"go/format"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/google/go-cmp/cmp"
)

// pointerToString is the nullable string type as the frame spells it.
const pointerToString = "*string"

// The @file declaration: a field of a keyed struct names the column holding a stored
// file's key, or a keyed computed struct names a rendered file, and the generator
// serves it under the read route. These tests pin the grammar, the extraction onto
// each kind, every refusal with its fix named, the content function check, the route,
// the collection's route-own field, the matrix case, and the templates.

func Test_parseFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		arg         genlang.Arg
		wantSegment string
		wantName    string
		wantType    string
		wantErr     string
	}{
		{name: "bare declares the default segment", arg: "", wantSegment: "content"},
		{name: "a segment alone", arg: "thumbnail", wantSegment: "thumbnail"},
		{name: "the siblings without a segment", arg: "name: FileName, type: ContentType", wantSegment: "content", wantName: "FileName", wantType: "ContentType"},
		{name: "a segment with the siblings", arg: "sheet, type: MediaType", wantSegment: "sheet", wantType: "MediaType"},
		{name: "a segment that is not a route segment", arg: "Content", wantErr: `segment "Content" is not a route segment`},
		{name: "two segments", arg: "content, thumbnail", wantErr: "expected at most 1 positional argument(s), found 2"},
		{name: "an unknown argument", arg: "size: Size", wantErr: `unknown argument "size"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			segment, nameField, typeField, err := parseFile(tt.arg)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parseFile(%q) error = %v, want containing %q", tt.arg, err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("parseFile(%q) error = %v", tt.arg, err)
			}
			if segment != tt.wantSegment || nameField != tt.wantName || typeField != tt.wantType {
				t.Errorf("parseFile(%q) = (%q, %q, %q), want (%q, %q, %q)", tt.arg, segment, nameField, typeField, tt.wantSegment, tt.wantName, tt.wantType)
			}
		})
	}
}

// fileFixtureResource builds a table-backed fixture resource, keyed by ID, and
// resolves its @file declarations.
func fileFixtureResource(t *testing.T, structs map[string]*parser.Struct, name string, mutate func(*resourceInfo)) (*resourceInfo, error) {
	t.Helper()

	pStruct, annotations := scanFixtureStruct(t, structs, name)
	res := fixtureResource(t, structs, name, mutate)

	return res, resolveResourceFiles(res, pStruct, annotations)
}

// fileFixtureComputed builds a computed fixture resource and resolves its @file
// declarations.
func fileFixtureComputed(t *testing.T, structs map[string]*parser.Struct, name string) (*computedResource, error) {
	t.Helper()

	pStruct, annotations := scanFixtureStruct(t, structs, name)
	res := fixtureComputedView(t, structs, name)

	return res, resolveComputedFiles(res, pStruct, annotations)
}

func Test_resolveResourceFiles(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "filefixture"))

	tests := []struct {
		name      string
		fixture   string
		mutate    func(*resourceInfo)
		wantFiles []*fileRoute
		// wantCreateDisabled pins the NOT NULL key rule.
		wantCreateDisabled bool
		wantErr            string
	}{
		{
			name:    "a stored file with its name and type columns, and a second under its own segment with a nullable key",
			fixture: "Document",
			wantFiles: []*fileRoute{
				{Segment: "content", Key: &fileField{Name: "StoreKey", Type: "string"}, Name: &fileField{Name: "FileName", Type: "string"}, Type: &fileField{Name: "ContentType", Type: "string"}},
				{Segment: "thumbnail", Key: &fileField{Name: "ThumbKey", Type: pointerToString, Pointer: true, Nullable: true}},
			},
			wantCreateDisabled: true,
		},
		{
			name:      "a nullable key leaves Create ordinary",
			fixture:   "Logo",
			wantFiles: []*fileRoute{{Segment: "content", Key: &fileField{Name: "StoreKey", Type: pointerToString, Pointer: true, Nullable: true}}},
		},
		{name: "an unknown sibling is refused naming it", fixture: "BadSibling", wantErr: "@file names name field Missing, which is not a field of the struct"},
		{name: "a key column that is not a string is refused", fixture: "BadType", wantErr: "the store key of a @file is a string or a nullable string (*string), not int64"},
		{name: "a type column that is not a string is refused", fixture: "BadSiblingType", wantErr: "the type field of a @file is a string or a nullable string (*string), not int64"},
		{name: "a segment that is not a route segment is refused", fixture: "BadSegment", wantErr: `segment "Content" is not a route segment`},
		{name: "the struct-scope form on a table is refused", fixture: "StructScoped", wantErr: "a struct-scope @file is a rendered file, which is a @computed struct's content"},
		{name: "two declarations on one segment are refused", fixture: "Twice", wantErr: `@file declares segment "content" twice, on field FirstKey and on field SecondKey`},
		{name: "the declaration on the primary key is refused", fixture: "OnKey", wantErr: "@file goes on the column holding the store key, not on the primary key"},
		{
			name:    "a suppressed read route has nothing to hang the file under",
			fixture: "Logo",
			mutate: func(res *resourceInfo) {
				res.SuppressedHandlers = []HandlerType{ReadHandler}
			},
			wantErr: "@file serves the file under the read route, which Logo suppresses",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res, err := fileFixtureResource(t, structs, tt.fixture, tt.mutate)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("resolveResourceFiles(%s) error = %v, want containing %q", tt.fixture, err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("resolveResourceFiles(%s) error = %v", tt.fixture, err)
			}
			if diff := cmp.Diff(tt.wantFiles, res.Files, cmp.AllowUnexported(fileRoute{}, fileField{})); diff != "" {
				t.Errorf("Files mismatch (-want +got):\n%s", diff)
			}
			if res.CreateDisabled() != tt.wantCreateDisabled || res.CreateHandlerDisabled() != tt.wantCreateDisabled {
				t.Errorf("CreateDisabled() = %v, CreateHandlerDisabled() = %v, want %v", res.CreateDisabled(), res.CreateHandlerDisabled(), tt.wantCreateDisabled)
			}
			for _, file := range res.Files {
				for _, field := range res.Fields {
					if field.Name() != file.Key.Name {
						continue
					}
					// The key column is off the wire both ways.
					if !field.IsFileKey || field.JSONTag() != `json:"-"` || field.JSONTagForPatch() != `json:"-"` || field.WireName() != "" || !field.IsOutputOnly() || !field.IsInputOnly() {
						t.Errorf("key field %s: IsFileKey %v, read tag %s, patch tag %s, wire name %q", field.Name(), field.IsFileKey, field.JSONTag(), field.JSONTagForPatch(), field.WireName())
					}
				}
			}
		})
	}
}

func Test_resolveResourceFiles_views(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "filefixture"))

	tests := []struct {
		name     string
		fixture  string
		wantKeys []string
		wantErr  string
	}{
		{name: "a keyed view carries a stored file", fixture: "KeyedView", wantKeys: []string{"StoreKey"}},
		{name: "a key-less view has no row for a file to belong to", fixture: "KeylessView", wantErr: "@file needs a row to belong to, and KeylessView declares no @primarykey"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pStruct, annotations := scanFixtureStruct(t, structs, tt.fixture)
			res := fixtureVirtualView(t, structs, tt.fixture)
			err := resolveResourceFiles(res, pStruct, annotations)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("resolveResourceFiles(%s) error = %v, want containing %q", tt.fixture, err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("resolveResourceFiles(%s) error = %v", tt.fixture, err)
			}
			var keys []string
			for _, file := range res.Files {
				keys = append(keys, file.Key.Name)
			}
			if diff := cmp.Diff(tt.wantKeys, keys); diff != "" {
				t.Errorf("key columns mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_resolveComputedFiles(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "filefixture"))

	tests := []struct {
		name      string
		fixture   string
		wantFiles []*fileRoute
		wantErr   string
	}{
		{name: "the struct-scope form renders under the default segment", fixture: "Manifest", wantFiles: []*fileRoute{{Segment: "content"}}},
		{name: "the struct-scope form under its own segment", fixture: "Statement", wantFiles: []*fileRoute{{Segment: "sheet"}}},
		{
			name:      "the field-scope form stores, with the key column off the wire",
			fixture:   "StoredComputed",
			wantFiles: []*fileRoute{{Segment: "content", Key: &fileField{Name: "StoreKey", Type: "string"}, Name: &fileField{Name: "Name", Type: "string"}}},
		},
		{name: "a key-less computed struct is refused", fixture: "KeylessComputed", wantErr: "@file needs a row to belong to, and KeylessComputed declares no @primarykey"},
		{name: "a name column on the struct-scope form is refused", fixture: "NamedRender", wantErr: "a struct-scope @file renders its file, whose name and type come from the resource.Content the content function returns"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res, err := fileFixtureComputed(t, structs, tt.fixture)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("resolveComputedFiles(%s) error = %v, want containing %q", tt.fixture, err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("resolveComputedFiles(%s) error = %v", tt.fixture, err)
			}
			if diff := cmp.Diff(tt.wantFiles, res.Files, cmp.AllowUnexported(fileRoute{}, fileField{})); diff != "" {
				t.Errorf("Files mismatch (-want +got):\n%s", diff)
			}
			for _, file := range res.Files {
				if file.Rendered() {
					continue
				}
				for _, field := range res.Fields {
					if field.Name() == file.Key.Name && (!field.IsFileKey || field.JSONTag() != `json:"-"`) {
						t.Errorf("key field %s: IsFileKey %v, tag %s; want off the wire", field.Name(), field.IsFileKey, field.JSONTag())
					}
				}
			}
		})
	}
}

func Test_validateComputedContentFunctions(t *testing.T) {
	t.Parallel()

	loaded, parsed := loadFixturePackage(t, "filefixture")
	structs := fixtureStructs(parsed)

	tests := []struct {
		name    string
		fixture string
		wantErr string
	}{
		{name: "the function with the frame's signature passes", fixture: "Manifest"},
		{name: "a compound key's parameters, under the declared segment", fixture: "Statement"},
		{name: "a missing function is refused naming it and its shape", fixture: "MissingFunction", wantErr: "declares no MissingFunctionContent; declare func MissingFunctionContent(ctx context.Context, id ccc.UUID, qSet *resource.QuerySet[MissingFunction], client resource.Client, computedClient *Client) (*resource.Content, error) in package filefixture"},
		{name: "a key parameter of another type is refused", fixture: "WrongKey", wantErr: "WrongKeyContent's parameter 2 is string, not the key field ID's ccc.UUID"},
		{name: "a result by value is refused", fixture: "WrongResult", wantErr: "WrongResultContent answers with resource.Content, not *resource.Content"},
		{name: "a function skipping the QuerySet is refused", fixture: "NoQuerySet", wantErr: "not the key, the QuerySet, and the clients"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res, err := fileFixtureComputed(t, structs, tt.fixture)
			if err != nil {
				t.Fatalf("resolveComputedFiles(%s) error = %v", tt.fixture, err)
			}
			err = validateComputedContentFunctions(loaded.Types, []*computedResource{res})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("validateComputedContentFunctions(%s) error = %v, want containing %q", tt.fixture, err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("validateComputedContentFunctions(%s) error = %v", tt.fixture, err)
			}
		})
	}
}

func Test_rejectFileAnnotations(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "filefixture"))
	pStruct, annotations := scanFixtureStruct(t, structs, "Attach")
	err := rejectFileAnnotations(pStruct, annotations, "RPC method")
	if err == nil || !strings.Contains(err.Error(), "struct Attach field StoreKey: @file serves a file under a resource's read route; a RPC method has none") {
		t.Fatalf("rejectFileAnnotations(Attach) error = %v, want the RPC refusal", err)
	}
	pStruct, annotations = scanFixtureStruct(t, structs, "Client")
	if err := rejectFileAnnotations(pStruct, annotations, "RPC method"); err != nil {
		t.Errorf("rejectFileAnnotations(Client) error = %v, want none: a struct with no declaration passes", err)
	}
}

// fileGenerator is a generator with Lodestar's route shape: /api, the sectors
// segment pair.
func fileGenerator() *resourceGenerator {
	return &resourceGenerator{client: &client{}, routePrefix: "api", domainRouteSegment: "sectors", domainRouteParam: "sectorID"}
}

func Test_fileRoutes(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "filefixture"))

	tests := []struct {
		name  string
		build func(t *testing.T) ([]*generatedRoute, error)
		want  []*generatedRoute
	}{
		{
			name: "a global resource's files hang under its read route",
			build: func(t *testing.T) ([]*generatedRoute, error) {
				t.Helper()
				res, err := fileFixtureResource(t, structs, "Document", nil)
				if err != nil {
					t.Fatal(err)
				}

				return fileGenerator().resourceFileRoutes(res, "api")
			},
			want: []*generatedRoute{
				{Method: "GET", Path: "/api/documents/{documentID}/content", HandlerFunc: "DocumentContent", HandlerType: fileHandler, TestURL: "/api/documents/testDocumentID/content", TestParams: []routeTestParam{{Key: "documentID", Value: "testDocumentID"}}},
				{Method: "GET", Path: "/api/documents/{documentID}/thumbnail", HandlerFunc: "DocumentThumbnail", HandlerType: fileHandler, TestURL: "/api/documents/testDocumentID/thumbnail", TestParams: []routeTestParam{{Key: "documentID", Value: "testDocumentID"}}},
			},
		},
		{
			name: "a domain-scoped resource's file is guarded under the segment pair",
			build: func(t *testing.T) ([]*generatedRoute, error) {
				t.Helper()
				res, err := fileFixtureResource(t, structs, "Logo", func(res *resourceInfo) {
					res.PermissionScope = accesstypes.DomainPermissionScope
				})
				if err != nil {
					t.Fatal(err)
				}

				return fileGenerator().resourceFileRoutes(res, "api")
			},
			want: []*generatedRoute{
				{Method: "GET", Path: "/api/sectors/{sectorID}/logos/{logoID}/content", HandlerFunc: "LogoContent", HandlerType: fileHandler, DomainScoped: true, TestURL: "/api/sectors/testDomain/logos/testLogoID/content", TestParams: []routeTestParam{{Key: "sectorID", Value: "testDomain"}, {Key: "logoID", Value: "testLogoID"}}},
			},
		},
		{
			name: "a computed resource's rendered file hangs under its read route",
			build: func(t *testing.T) ([]*generatedRoute, error) {
				t.Helper()
				res, err := fileFixtureComputed(t, structs, "Statement")
				if err != nil {
					t.Fatal(err)
				}

				return fileGenerator().computedFileRoutes(res, "api")
			},
			want: []*generatedRoute{
				{Method: "GET", Path: "/api/statements/{statementClientID}/{statementPeriod}/sheet", HandlerFunc: "StatementSheet", HandlerType: fileHandler, TestURL: "/api/statements/testStatementClientID/testStatementPeriod/sheet", TestParams: []routeTestParam{{Key: "statementClientID", Value: "testStatementClientID"}, {Key: "statementPeriod", Value: "testStatementPeriod"}}},
			},
		},
		{
			name: "a resource with no declaration has no file route",
			build: func(t *testing.T) ([]*generatedRoute, error) {
				t.Helper()

				return fileGenerator().resourceFileRoutes(fixtureResource(t, structs, "BadType", nil), "api")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.build(t)
			if err != nil {
				t.Fatalf("file routes error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(generatedRoute{})); diff != "" {
				t.Errorf("routes mismatch (-want +got):\n%s", diff)
			}
			for _, route := range got {
				if route.SharedHandler() {
					t.Errorf("%s answers POST; a file route is GET alone", route.Path)
				}
			}
		})
	}
}

// Test_fileCollection pins the Collection's view of a @file: the segment is a
// route-own field under Read alone, the key column is registered nowhere, and a NOT
// NULL key drops Create from the patch registration.
func Test_fileCollection(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "filefixture"))

	tests := []struct {
		name             string
		fixture          string
		wantReadTags     map[accesstypes.Tag][]accesstypes.Permission
		wantPatchPerms   []accesstypes.Permission
		wantPatchAbsent  []accesstypes.Tag
		wantPatchPermStr string
	}{
		{
			name:    "a NOT NULL key: content and thumbnail under Read, no Create",
			fixture: "Document",
			wantReadTags: map[accesstypes.Tag][]accesstypes.Permission{
				"id": {accesstypes.NullPermission}, "title": {accesstypes.Read}, "fileName": {accesstypes.Read}, "contentType": {accesstypes.Read},
				"content": {accesstypes.Read}, "thumbnail": {accesstypes.Read},
			},
			wantPatchPerms:   []accesstypes.Permission{accesstypes.Delete, accesstypes.Update},
			wantPatchAbsent:  []accesstypes.Tag{"storeKey", "thumbKey", "content", "thumbnail"},
			wantPatchPermStr: "accesstypes.Update, accesstypes.Delete",
		},
		{
			name:             "a nullable key keeps Create",
			fixture:          "Logo",
			wantReadTags:     map[accesstypes.Tag][]accesstypes.Permission{"id": {accesstypes.NullPermission}, "content": {accesstypes.Read}},
			wantPatchPerms:   []accesstypes.Permission{accesstypes.Create, accesstypes.Delete, accesstypes.Update},
			wantPatchAbsent:  []accesstypes.Tag{"storeKey", "content"},
			wantPatchPermStr: "accesstypes.Create, accesstypes.Update, accesstypes.Delete",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res, err := fileFixtureResource(t, structs, tt.fixture, nil)
			if err != nil {
				t.Fatal(err)
			}
			read, err := handlerSetData(res, ReadHandler, fileSetTags(res.Files)...)
			if err != nil {
				t.Fatalf("handlerSetData(Read) error = %v", err)
			}
			if diff := cmp.Diff(tt.wantReadTags, map[accesstypes.Tag][]accesstypes.Permission(read.TagPermissions)); diff != "" {
				t.Errorf("Read tags mismatch (-want +got):\n%s", diff)
			}
			patch, err := handlerSetData(res, PatchHandler)
			if err != nil {
				t.Fatalf("handlerSetData(Patch) error = %v", err)
			}
			if diff := cmp.Diff(tt.wantPatchPerms, patch.Permissions); diff != "" {
				t.Errorf("patch permissions mismatch (-want +got):\n%s", diff)
			}
			for _, tag := range tt.wantPatchAbsent {
				if _, ok := patch.TagPermissions[tag]; ok {
					t.Errorf("patch set registers %q, want it absent", tag)
				}
			}
			if got := res.PatchPermissionList(); got != tt.wantPatchPermStr {
				t.Errorf("PatchPermissionList() = %q, want %q", got, tt.wantPatchPermStr)
			}
		})
	}
}

// Test_fileAuthzCases pins the matrix's view: a file route is a Read query case with
// its placeholder key, and a resource whose NOT NULL key disables Create has no create
// case to pin.
func Test_fileAuthzCases(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "filefixture"))
	document, err := fileFixtureResource(t, structs, "Document", nil)
	if err != nil {
		t.Fatal(err)
	}
	logo, err := fileFixtureResource(t, structs, "Logo", nil)
	if err != nil {
		t.Fatal(err)
	}

	routes, err := fileGenerator().resourceFileRoutes(document, "api")
	if err != nil {
		t.Fatal(err)
	}
	c, ok, err := queryRouteCase(routes[0], resourcePKTypes(document), "")
	if err != nil || !ok {
		t.Fatalf("queryRouteCase() = %v, %v", ok, err)
	}
	want := authzCase{Name: "DocumentContent", Method: "http.MethodGet", URL: "/api/documents/00000000-0000-0000-0000-000000000001/content", Permission: string(accesstypes.Read)}
	if diff := cmp.Diff(want, c, cmp.AllowUnexported(authzCase{})); diff != "" {
		t.Errorf("file route case mismatch (-want +got):\n%s", diff)
	}

	tests := []struct {
		name       string
		res        *resourceInfo
		wantCreate bool
	}{
		{name: "a NOT NULL key drops the create case", res: document},
		{name: "a nullable key keeps it", res: logo, wantCreate: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cases, err := patchOpCases("Patch", "/api/things", "", tt.res, resourcePKTypes(tt.res))
			if err != nil {
				t.Fatalf("patchOpCases() error = %v", err)
			}
			hasCreate := false
			for _, c := range cases {
				if strings.HasSuffix(c.Name, " create") {
					hasCreate = true
				}
			}
			if hasCreate != tt.wantCreate {
				t.Errorf("create case present = %v, want %v: %v", hasCreate, tt.wantCreate, cases)
			}
		})
	}
}

func Test_fileHandlerTemplate(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "filefixture"))
	res, err := fileFixtureResource(t, structs, "Document", func(res *resourceInfo) {
		res.PermissionScope = accesstypes.DomainPermissionScope
	})
	if err != nil {
		t.Fatal(err)
	}

	c := &client{}
	for _, file := range res.Files {
		out, err := c.generateTemplateOutput("fileHandler", fileHandlerTemplate, fileHandlerData{
			handlerContentData: handlerContentData{ResourcePackage: "resources", Resource: res, VirtualResourcesPackage: "virtualresources", ApplicationName: "App", ReceiverName: "a"},
			File:               file,
		})
		if err != nil {
			t.Fatalf("generateTemplateOutput(%s) error = %v", file.Segment, err)
		}
		// The handler is a function body, so it is formatted inside a package.
		formatted, err := format.Source(append([]byte("package app\n\n"), out...))
		if err != nil {
			t.Fatalf("format.Source(%s) error = %v on:\n%s", file.Segment, err, out)
		}
		wants := map[string][]string{
			"content": {
				"func (a *App) DocumentContent() http.HandlerFunc {",
				"ID          ccc.UUID `json:\"id\" perm:\"-\"`",
				"StoreKey    string   `json:\"-\" perm:\"-\"`",
				"FileName    string   `json:\"-\" perm:\"-\"`",
				"ContentType string   `json:\"-\" perm:\"-\"`",
				"decoder := NewFileDecoder[resources.Document, request](a, \"content\")",
				"id := httpio.Param[ccc.UUID](r, router.DocumentID)",
				"domain := httpio.Param[accesstypes.Domain](r, router.Domain)",
				"querySet, err := decoder.Decode(r, a.UserPermissions(r), accesstypes.DomainScope(domain))",
				"row, err := resources.NewDocumentQueryFromQuerySet(querySet).SetID(id).Read(ctx, a.ResourceClient())",
				"source := &row.Data",
				"file.Key = source.StoreKey",
				"file.Name = source.FileName",
				"file.ContentType = source.ContentType",
				"if err := resource.ServeStoredFile(ctx, w, r, a.FileStore(), file, \"content\", \"Document\", id); err != nil {",
			},
			"thumbnail": {
				"func (a *App) DocumentThumbnail() http.HandlerFunc {",
				"ThumbKey *string  `json:\"-\" perm:\"-\"`",
				"decoder := NewFileDecoder[resources.Document, request](a, \"thumbnail\")",
				"if source.ThumbKey != nil {\n\t\t\tfile.Key = *source.ThumbKey\n\t\t}",
				"if err := resource.ServeStoredFile(ctx, w, r, a.FileStore(), file, \"thumbnail\", \"Document\", id); err != nil {",
			},
		}
		for _, want := range wants[file.Segment] {
			if !strings.Contains(string(formatted), want) {
				t.Errorf("%s handler missing %q:\n%s", file.Segment, want, formatted)
			}
		}
		if file.Segment == "thumbnail" && strings.Contains(string(formatted), "file.Name") {
			t.Errorf("thumbnail handler reads a name column it does not declare:\n%s", formatted)
		}
	}
}

// Test_resourceFileTemplate_fileKeys pins the generated FileKeys method: a struct with
// stored files declares its key fields in declaration order beside an unchanged
// DefaultConfig, and a struct with none declares no such method.
func Test_resourceFileTemplate_fileKeys(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "filefixture"))

	tests := []struct {
		name    string
		fixture string
		want    string
		absent  string
	}{
		{name: "two stored files name both keys", fixture: "Document", want: "func (Document) FileKeys() []accesstypes.Field {\n\treturn []accesstypes.Field{\"StoreKey\", \"ThumbKey\"}\n}"},
		{name: "one stored file with a nullable key names it", fixture: "Logo", want: "func (Logo) FileKeys() []accesstypes.Field {\n\treturn []accesstypes.Field{\"StoreKey\"}\n}"},
		{name: "a struct with no file declares no method", fixture: "NoFile", want: "func (NoFile) DefaultConfig() resource.Config {\n\treturn defaultConfig()\n}", absent: "FileKeys"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res, err := fileFixtureResource(t, structs, tt.fixture, nil)
			if err != nil {
				t.Fatal(err)
			}
			c := &client{}
			out, err := c.generateTemplateOutput("resourceFile", resourceFileTemplate, &resourceFileData{Source: "fixture", Package: "resources", Resource: res})
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}
			formatted, err := format.Source(out)
			if err != nil {
				t.Fatalf("format.Source() error = %v on:\n%s", err, out)
			}
			if !strings.Contains(string(formatted), tt.want) {
				t.Errorf("resource file mismatch: want\n%s\nin\n%s", tt.want, formatted)
			}
			if tt.fixture != "NoFile" && !strings.Contains(string(formatted), "func ("+tt.fixture+") DefaultConfig() resource.Config {\n\treturn defaultConfig()\n}") {
				t.Errorf("DefaultConfig changed for %s:\n%s", tt.fixture, formatted)
			}
			if tt.absent != "" && strings.Contains(string(formatted), tt.absent) {
				t.Errorf("%s declares %s:\n%s", tt.fixture, tt.absent, formatted)
			}
		})
	}
}

func Test_computedResourceHandlerTemplate_files(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "filefixture"))

	tests := []struct {
		name    string
		fixture string
		wants   []string
	}{
		{
			name:    "a rendered file calls the content function and serves what it returns",
			fixture: "Statement",
			wants: []string{
				"func (a *App) StatementSheet() http.HandlerFunc {",
				"ClientID ccc.UUID `json:\"clientId\" perm:\"-\"`",
				"Period   string   `json:\"period\" perm:\"-\"`",
				"decoder := NewComputedFileDecoder[computed.Statement, request](a, \"sheet\")",
				"clientID := httpio.Param[ccc.UUID](r, router.StatementClientID)",
				"period := httpio.Param[string](r, router.StatementPeriod)",
				"content, err := computed.StatementSheet(ctx, clientID, period, querySet, a.ResourceClient(), a.ComputedClient())",
				"if err := resource.ServeRenderedFile(w, r, content, \"sheet\", \"Statement\", clientID, period); err != nil {",
			},
		},
		{
			name:    "a stored file on a computed struct reads the row through Read<Name> and opens the store",
			fixture: "StoredComputed",
			wants: []string{
				"func (a *App) StoredComputedContent() http.HandlerFunc {",
				"StoreKey string   `json:\"-\" perm:\"-\"`",
				"Name     string   `json:\"-\" perm:\"-\"`",
				"source, err := computed.ReadStoredComputed(ctx, id, querySet, a.ResourceClient(), a.ComputedClient())",
				"httpio.NewNotFoundMessagef(\"StoredComputed %v does not exist\", id)",
				"file.Key = source.StoreKey",
				"file.Name = source.Name",
				"if err := resource.ServeStoredFile(ctx, w, r, a.FileStore(), file, \"content\", \"StoredComputed\", id); err != nil {",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res, err := fileFixtureComputed(t, structs, tt.fixture)
			if err != nil {
				t.Fatal(err)
			}
			shape, err := walkFixture(t, structs, tt.fixture)
			if err != nil {
				t.Fatalf("walk(%s) error = %v", tt.fixture, err)
			}
			res.Shape = shape
			for i, field := range res.Fields {
				field.wire = shape.Fields[i]
				field.namespace = tt.fixture + "s"
			}

			c := &client{}
			out, err := c.generateTemplateOutput("computedResourceHandlerTemplate", computedResourceHandlerTemplate, &computedHandlerData{
				Source:          "pkg/computedresources",
				Resource:        res,
				Package:         "app",
				ComputedPackage: "computed",
				ApplicationName: "App",
				ReceiverName:    "a",
			})
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}
			formatted, err := format.Source(out)
			if err != nil {
				t.Fatalf("format.Source() error = %v on:\n%s", err, out)
			}
			for _, want := range tt.wants {
				if !strings.Contains(string(formatted), want) {
					t.Errorf("handler missing %q:\n%s", want, formatted)
				}
			}
			// The key column stays off the read handler's wire too.
			if strings.Contains(string(formatted), "StoreKey string `json:\"storeKey\"") {
				t.Errorf("the read handler carries the store key on the wire:\n%s", formatted)
			}
		})
	}
}

// Test_typescriptTemplates_files pins the client's view: the descriptor lists the
// segments, and the key column is absent from the interface and the metadata.
func Test_typescriptTemplates_files(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "filefixture"))
	document, err := fileFixtureResource(t, structs, "Document", func(res *resourceInfo) {
		for _, f := range res.Fields {
			f.typescriptType = "string"
		}
	})
	if err != nil {
		t.Fatal(err)
	}

	c := &client{resources: []*resourceInfo{document}}
	generator := &typescriptGenerator{client: c}

	api, err := c.generateTemplateOutput("typescriptAPITemplate", typescriptAPITemplate, generator.apiClientData())
	if err != nil {
		t.Fatalf("api template error = %v", err)
	}
	for _, want := range []string{
		"files: ['content', 'thumbnail'],",
		// A NOT NULL key: no create operation, no Create interface.
		"operations: ['list', 'read', 'patch', 'remove'],",
	} {
		if !strings.Contains(string(api), want) {
			t.Errorf("api descriptor missing %q:\n%s", want, api)
		}
	}
	for _, notWant := range []string{"DocumentsCreate", "storeKey", "thumbKey"} {
		if strings.Contains(string(api), notWant) {
			t.Errorf("api descriptor carries %q:\n%s", notWant, api)
		}
	}

	resources, err := c.generateTemplateOutput("typescriptResourcesTemplate", typescriptResourcesTemplate, tsResourcesData{File: generator, Resources: []*resourceInfo{document}, GenPrefix: "zz_gen"})
	if err != nil {
		t.Fatalf("resources template error = %v", err)
	}
	for _, want := range []string{"  title?: string;", "createDisabled: true,", "{ fieldName: 'fileName',"} {
		if !strings.Contains(string(resources), want) {
			t.Errorf("resources metadata missing %q:\n%s", want, resources)
		}
	}
	for _, notWant := range []string{"storeKey", "thumbKey"} {
		if strings.Contains(string(resources), notWant) {
			t.Errorf("resources metadata carries the key column %q:\n%s", notWant, resources)
		}
	}
}

// Test_fileSetTags pins the route-own field the Read set registers per segment.
func Test_fileSetTags(t *testing.T) {
	t.Parallel()

	got := fileSetTags([]*fileRoute{{Segment: "content"}, {Segment: "cover-art"}})
	want := []resource.FieldTags{{Field: "Content", JSON: "content"}, {Field: "CoverArt", JSON: "cover-art"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("fileSetTags() mismatch (-want +got):\n%s", diff)
	}
}
