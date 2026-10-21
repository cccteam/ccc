package generation

import (
	"go/format"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// declarationResources extracts the named tenantfixture structs and resolves the
// package's declaration the way Generate does: the structs first, then the tenant
// record and its declaration across them.
func declarationResources(t *testing.T, names ...string) (*resourceGenerator, error) {
	t.Helper()

	structs := fixtureStructs(loadFixture(t, "tenantfixture"))
	c := &client{tableMap: tenantFixtureTables()}
	resources, err := c.structsToResources(tenantFixtureStructs(t, structs, names...))
	if err != nil {
		return nil, err
	}
	c.resources = resources
	r := &resourceGenerator{client: c, routePrefix: "api", applicationName: "App", receiverName: "a", domainRouteSegment: defaultDomainRouteSegment, domainRouteParam: defaultDomainRouteParam}
	err = r.resolveTenantRecord()

	return r, err
}

// Test_resolveTenantDeclaration pins the declaration's capture on each struct: the
// record's flag and the directories record's columns are marked, and each refused
// shape names its rule, at capture (one struct) or once every struct is known (the
// record the directories belong to).
func Test_resolveTenantDeclaration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		structs []string
		wantErr string
	}{
		{name: "the record's flag and the directories record are captured", structs: []string{"Harbor", "HarborDirectory"}},
		{name: "a directories record beside a record with no flag is a declaration too", structs: []string{"Sector", "HarborDirectory"}},
		{
			name:    "@directoryOnly on a resource that is not the tenant record is refused",
			structs: []string{"Harbor", "HarborDirectory", "Pier"},
			wantErr: "struct Pier field Closed: @directoryOnly on a resource that is not the tenant record; the directory-only flag is the tenant record's column, so annotate the @tenant record's BOOL column",
		},
		{
			name:    "@directoryOnly on a column that is not a BOOL is refused",
			structs: []string{"Quay", "HarborDirectory"},
			wantErr: "struct Quay field Mode: @directoryOnly on a string column; the directory-only flag is a BOOL, true when the tenant's people enter through its directories only",
		},
		{
			name:    "@tenantDirectory on a global resource is refused",
			structs: []string{"Harbor", "Beacon"},
			wantErr: "struct Beacon: @tenantDirectory on a global resource; the directories record is tenant-scoped (@permissionScope(domain)), since a tenant's directories are its own and its administrator declares them",
		},
		{
			name:    "@directoryMethod off the directories record is refused",
			structs: []string{"Harbor", "HarborDirectory", "Buoy"},
			wantErr: "struct Buoy field Method: @directoryMethod on a resource that is not the directories record; the sign-in method a directory is reached through is the @tenantDirectory record's column, so annotate the record's struct @tenantDirectory first",
		},
		{
			name:    "@directoryMethod on a second column is refused naming the first",
			structs: []string{"Harbor", "Jetty"},
			wantErr: "struct Jetty field Kind: @directoryMethod on a second column; field Method carries it already",
		},
		{
			name:    "a second directories record is refused naming the first",
			structs: []string{"Harbor", "HarborDirectory", "Mooring"},
			wantErr: "struct Mooring: @tenantDirectory on a second struct; struct HarborDirectory is the directories record already, and a tenant record has one",
		},
		{
			name:    "a directories record missing its connection column is refused",
			structs: []string{"Harbor", "Mooring"},
			wantErr: "struct Mooring: @tenantDirectory with no @directoryConnection column; annotate the string column holding the connection a directory's identities carry (a Google Workspace by its domain, a Microsoft Entra tenant by its tenant id)",
		},
		{
			name:    "a directories record whose tenant is a join path is refused naming the bare column",
			structs: []string{"Sector", "Berth", "Anchorage"},
			wantErr: "struct Anchorage: @tenantDirectory needs the bare @domain column, the tenant's key on the row itself, since a directory is the tenant's own declaration; a join path (@domain(via: ...)) is refused",
		},
		{
			name:    "the flag without a directories record is refused",
			structs: []string{"Harbor"},
			wantErr: "struct Harbor field DirectoryOnly: @directoryOnly with no directories record in the package; a tenant whose people enter through its directories only needs directories to enter through, so declare the directories record (@tenantDirectory) or drop the flag",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, err := declarationResources(t, tt.structs...)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("declaration error = %v, want it to contain %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("declaration error = %v", err)
			}
			record, directories := r.tenantRecord(), r.tenantDirectoryRecord()
			if record == nil || directories == nil {
				t.Fatalf("record = %v, directories = %v; want both", record, directories)
			}
			if !record.HasDeclaration() || record.declaration.Directories != directories {
				t.Errorf("%s.declaration = %v, want the %s record", record.Name(), record.declaration, directories.Name())
			}
			if directories.DirectoryMethodField == nil || directories.DirectoryMethodField.Name() != "Method" {
				t.Errorf("%s.DirectoryMethodField = %v, want Method", directories.Name(), directories.DirectoryMethodField)
			}
			if directories.DirectoryConnectionField == nil || directories.DirectoryConnectionField.Name() != "Connection" {
				t.Errorf("%s.DirectoryConnectionField = %v, want Connection", directories.Name(), directories.DirectoryConnectionField)
			}
			if !directories.SignalsTenants() || !record.SignalsTenants() {
				t.Error("both records' write paths keep the roster current")
			}
			if (record.DirectoryOnlyField != nil) != (record.Name() == "Harbor") {
				t.Errorf("%s.DirectoryOnlyField = %v", record.Name(), record.DirectoryOnlyField)
			}
		})
	}
}

// Test_declarationColumns pins the columns the generated constructors carry: the
// spanner columns behind the annotated fields, the flag empty where the record
// declares none, nil where the package declares no directories.
func Test_declarationColumns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		structs []string
		want    *tenantDeclarationColumns
	}{
		{
			name:    "the record's flag and the directories record's columns",
			structs: []string{"Harbor", "HarborDirectory"},
			want:    &tenantDeclarationColumns{DirectoryOnly: "DirectoryOnly", DirectoriesTable: "HarborDirectories", TenantColumn: "HarborId", MethodColumn: "Method", ConnectionColumn: "Connection"},
		},
		{
			name:    "a record without the flag names no directory-only column",
			structs: []string{"Sector", "HarborDirectory"},
			want:    &tenantDeclarationColumns{DirectoriesTable: "HarborDirectories", TenantColumn: "HarborId", MethodColumn: "Method", ConnectionColumn: "Connection"},
		},
		{name: "no directories record is no declaration", structs: []string{"Sector", "Berth"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, err := declarationResources(t, tt.structs...)
			if err != nil {
				t.Fatalf("declaration error = %v", err)
			}
			if diff := cmp.Diff(tt.want, r.declarationColumns(r.tenantRecord()), cmp.AllowUnexported(tenantDeclarationColumns{})); diff != "" {
				t.Errorf("declarationColumns() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Test_tenantsTemplate_declaration pins the roster constructor of a record that
// declares who may enter a tenant: the declaration's columns as a package variable,
// handed to resource.WithTenantDeclarations ahead of the caller's options, and the
// sign-in policy's reader constructor beside it; a record declaring none carries
// neither.
func Test_tenantsTemplate_declaration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		data            *tenantsData
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "the declaration's columns, the option and the reader constructor",
			data: &tenantsData{
				Source: "resources", Package: "app", Name: "Harbor", Table: "Harbors", KeyColumn: "Id", RecordLower: "harbor", Directories: "HarborDirectory",
				Declaration: &tenantDeclarationColumns{DirectoryOnly: "DirectoryOnly", DirectoriesTable: "HarborDirectories", TenantColumn: "HarborId", MethodColumn: "Method", ConnectionColumn: "Connection"},
			},
			wantContains: []string{
				`"cloud.google.com/go/spanner"`,
				`"github.com/cccteam/session/signin"`,
				"var harborDeclaration = resource.TenantDeclarationColumns{",
				`DirectoryOnly:    "DirectoryOnly",`,
				`DirectoriesTable: "HarborDirectories",`,
				`TenantColumn:     "HarborId",`,
				`MethodColumn:     "Method",`,
				`ConnectionColumn: "Connection",`,
				"opts = append([]resource.TenantRosterOption{resource.WithTenantDeclarations(&harborDeclaration)}, opts...)",
				`return resource.NewTenantRoster(client, "Harbors", "Id", opts...)`,
				"func NewHarborDeclarationReader() signin.DeclarationReader[*spanner.ReadWriteTransaction] {",
				`return resource.NewTenantDeclarationReader("Harbors", "Id", &harborDeclaration)`,
			},
		},
		{
			name:            "a record declaring none carries neither",
			data:            &tenantsData{Source: "resources", Package: "app", Name: "Sector", Table: "Sectors", KeyColumn: "Id", RecordLower: "sector"},
			wantContains:    []string{`return resource.NewTenantRoster(client, "Sectors", "Id", opts...)`},
			wantNotContains: []string{"WithTenantDeclarations", "DeclarationReader", "signin", "spanner"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &client{}
			out, err := c.generateTemplateOutput("tenantsTemplate", tenantsTemplate, tt.data)
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}
			if _, err := format.Source(out); err != nil {
				t.Fatalf("the rendered file does not parse: %v\n%s", err, out)
			}
			assertContains(t, "tenantsTemplate", string(out), tt.wantContains, tt.wantNotContains)
		})
	}
}

// Test_tenantCreationTemplate_declaration pins the create-your-tenant route of a record
// that declares who may enter a tenant: the request leaves the directory-only flag out,
// and the new tenant's first directory is the one its creator came through, written in
// the row's transaction when the session was established through a directory.
func Test_tenantCreationTemplate_declaration(t *testing.T) {
	t.Parallel()

	r, err := declarationResources(t, "Harbor", "HarborDirectory")
	if err != nil {
		t.Fatalf("declaration error = %v", err)
	}
	record := r.tenantRecord()
	key, fields, setters := tenantCreationFields(record)
	if diff := cmp.Diff([]string{"Name"}, setters); diff != "" {
		t.Errorf("setters mismatch (-want +got):\n%s", diff)
	}
	if len(fields) != 2 || key.Name != "ID" {
		t.Errorf("fields = %v, key = %v; want the key and the name", fields, key)
	}
	directory := tenantCreationDirectoryData(record)
	if diff := cmp.Diff(&tenantCreationDirectory{Record: "HarborDirectory", TenantField: "HarborID", MethodField: "Method", ConnectionField: "Connection"}, directory); diff != "" {
		t.Errorf("Directory mismatch (-want +got):\n%s", diff)
	}
	out, err := r.generateTemplateOutput("tenantCreationTemplate", tenantCreationTemplate, &tenantCreationData{
		Source:              "resources",
		Package:             "app",
		LocalPackageImports: `"example.com/app/pkg/resources"` + "\n\t" + `"example.com/app/pkg/router"`,
		ApplicationName:     "App",
		ReceiverName:        "a",
		ResourcePackage:     "resources",
		RouterPackage:       "router",
		Record:              "Harbor",
		RecordLower:         "harbor",
		Shape:               "both",
		HandlerFunc:         "CreateHarbor",
		RoutePath:           "/api/create-harbor",
		Auth:                "Staff",
		AuthType:            "StaffAuth",
		SurfaceAccessor:     "StaffUserSurface",
		RolesAccessor:       "HarborAdministratorRoles",
		Key:                 key,
		Fields:              fields,
		Setters:             setters,
		Refusal:             tenantCreationRefusal("Harbor"),
		Directory:           directory,
		record:              record,
	})
	if err != nil {
		t.Fatalf("generateTemplateOutput() error = %v", err)
	}
	if _, err := format.Source(out); err != nil {
		t.Fatalf("the rendered file does not parse: %v\n%s", err, out)
	}
	assertContains(t, "tenantCreationTemplate", string(out), []string{
		`"github.com/cccteam/session/sessioninfo"`,
		"if established, ok := sessioninfo.DataFromCtx(ctx).EstablishedBy(); ok && established.Method.ThroughDirectory() {",
		"directory, err := resources.NewHarborDirectoryCreatePatch()",
		"directory.SetHarborID(req.ID).SetMethod(string(established.Method)).SetConnection(established.Connection)",
		`return errors.Wrap(err, "resources.HarborDirectoryCreatePatch.Buffer()")`,
		"if err := a.TenantRoster().Reload(ctx); err != nil {",
	}, []string{"DirectoryOnly", "SetDirectoryOnly"})

	// The directory is written in the row's transaction: after the row, before the
	// commit.
	s := string(out)
	row := strings.Index(s, "patch.Buffer(ctx, txn, eventSource)")
	written := strings.Index(s, "directory.Buffer(ctx, txn, eventSource)")
	commit := strings.Index(s, "}); err != nil {")
	if row >= written || written >= commit {
		t.Errorf("order: row %d, directory %d, commit %d; want ascending:\n%s", row, written, commit, out)
	}
}

// Test_apiClientData_tenantCreation_declaration pins the generated client's create
// body: the directory-only flag is not among its fields.
func Test_apiClientData_tenantCreation_declaration(t *testing.T) {
	t.Parallel()

	r, err := declarationResources(t, "Harbor", "HarborDirectory")
	if err != nil {
		t.Fatalf("declaration error = %v", err)
	}
	tsg := &typescriptGenerator{client: r.client}
	got := tsg.tenantCreationClientData()
	if got == nil {
		t.Fatal("tenantCreationClientData() = nil, want the route")
	}
	names := make([]string, 0, len(got.Fields))
	for _, f := range got.Fields {
		names = append(names, f.Name)
	}
	if diff := cmp.Diff([]string{"id", "name"}, names); diff != "" {
		t.Errorf("fields mismatch (-want +got):\n%s", diff)
	}
}
