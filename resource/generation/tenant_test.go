package generation

import (
	"go/format"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/generation/parser"
)

// tenantFixtureTables is the synthetic schema behind the tenantfixture's table-backed
// structs: every table keyed once on Id, except Docks, keyed on two columns.
func tenantFixtureTables() map[string]*tableMetadata {
	pk := columnMeta{IsPrimaryKey: true, IsIndex: true, IsUniqueIndex: true}
	compositeKey := columnMeta{IsPrimaryKey: true, IsIndex: true}
	plain := columnMeta{}
	nullable := columnMeta{IsNullable: true}

	return map[string]*tableMetadata{
		"Sectors":   {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "Name": plain}},
		"Berths":    {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "SectorId": plain, "Name": plain}},
		"Outposts":  {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "SectorId": plain}},
		"Docks":     {PkCount: 2, Columns: map[string]columnMeta{"SectorId": compositeKey, "Number": compositeKey}},
		"Hangars":   {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "Name": plain}},
		"Regions":   {PkCount: 1, Columns: map[string]columnMeta{"Id": pk}},
		"Stations":  {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "Name": plain, "Motto": nullable, "Opened": plain, "Derived": plain}},
		"Colonies":  {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "Name": plain}},
		"Enclaves":  {PkCount: 1, Columns: map[string]columnMeta{"Id": pk}},
		"Outlands":  {PkCount: 1, Columns: map[string]columnMeta{"Id": pk}},
		"Provinces": {PkCount: 1, Columns: map[string]columnMeta{"Id": pk}},
		"Bastions":  {PkCount: 1, Columns: map[string]columnMeta{"Id": pk}},
		// The declaration's shapes: the record with its flag, the directories record,
		// and each shape the annotations refuse.
		"Harbors":           {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "Name": plain, "DirectoryOnly": plain}},
		"HarborDirectories": {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "HarborId": plain, "Method": plain, "Connection": plain}},
		"Piers":             {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "Closed": plain}},
		"Quays":             {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "Mode": plain}},
		"Beacons":           {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "Method": plain, "Connection": plain}},
		"Buoys":             {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "HarborId": plain, "Method": plain}},
		"Moorings":          {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "HarborId": plain, "Method": plain, "Connection": plain}},
		"Jetties":           {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "HarborId": plain, "Method": plain, "Kind": plain, "Connection": plain}},
		"Anchorages":        {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "BerthId": {IsForeignKey: true, ReferencedTable: "Berths", ReferencedColumn: "Id"}, "Method": plain, "Connection": plain}},
	}
}

// tenantFixtureStructs returns the named tenantfixture structs, in order.
func tenantFixtureStructs(t *testing.T, structs map[string]*parser.Struct, names ...string) []*parser.Struct {
	t.Helper()

	pStructs := make([]*parser.Struct, 0, len(names))
	for _, name := range names {
		s := structs[name]
		if s == nil {
			t.Fatalf("struct %q not found in fixture package", name)
		}
		pStructs = append(pStructs, s)
	}

	return pStructs
}

// Test_resolveTenant pins the tenant record's capture on a table-backed struct: the
// well-formed record is marked, and each refused shape names its rule.
func Test_resolveTenant(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "tenantfixture"))

	tests := []struct {
		name       string
		structs    []string
		wantTenant string
		wantShape  tenantCreation
		wantErr    string
	}{
		{
			name:       "the global, single string-keyed record is the tenant record",
			structs:    []string{"Sector", "Berth"},
			wantTenant: "Sector",
			wantShape:  tenantCreationAdministrator,
		},
		{
			name:       "creation: self-service is the self-service shape",
			structs:    []string{"Station"},
			wantTenant: "Station",
			wantShape:  tenantCreationSelfService,
		},
		{
			name:       "creation: both is both shapes",
			structs:    []string{"Colony"},
			wantTenant: "Colony",
			wantShape:  tenantCreationBoth,
		},
		{
			name:       "creation: administrator spells the default out",
			structs:    []string{"Enclave"},
			wantTenant: "Enclave",
			wantShape:  tenantCreationAdministrator,
		},
		{
			name:    "a creation shape the generator does not know is refused naming the three",
			structs: []string{"Outland"},
			wantErr: "struct Outland: @tenant(creation: nowhere) names no creation shape; the shapes are administrator (an administrator alone, the default), self-service (the person who becomes its administrator) and both",
		},
		{
			name:    "the shape without its key is refused",
			structs: []string{"Province"},
			wantErr: `expected 0 positional argument(s), found 1 in "self-service"`,
		},
		{
			name:    "an argument @tenant does not take is refused",
			structs: []string{"Bastion"},
			wantErr: `unknown argument "shape" in "shape: both" (accepted: creation)`,
		},
		{
			name:    "@tenant on a tenant-scoped resource is refused",
			structs: []string{"Outpost"},
			wantErr: "struct Outpost: @tenant on a tenant-scoped resource; the tenant record is global, since its rows are the tenants themselves",
		},
		{
			name:    "@tenant on a resource with a compound key is refused",
			structs: []string{"Dock"},
			wantErr: "struct Dock: @tenant on a resource with a compound primary key; the tenant record has one key, the domain in every tenant-scoped URL",
		},
		{
			name:    "@tenant on a resource whose key is not a string is refused",
			structs: []string{"Hangar"},
			wantErr: "struct Hangar: @tenant on a resource whose key ID is ccc.UUID; the tenant record's key is a string, the domain in every tenant-scoped URL",
		},
		{
			name:    "@tenant on a second struct is refused naming the first",
			structs: []string{"Sector", "Region"},
			wantErr: "struct Region: @tenant on a second struct; struct Sector is the tenant record already, and an application has one",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &client{tableMap: tenantFixtureTables()}
			resources, err := c.structsToResources(tenantFixtureStructs(t, structs, tt.structs...))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("structsToResources() error = %v, want it to contain %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("structsToResources() error = %v", err)
			}
			for _, res := range resources {
				if got, want := res.IsTenant, res.Name() == tt.wantTenant; got != want {
					t.Errorf("%s.IsTenant = %v, want %v", res.Name(), got, want)
				}
				if res.IsTenant && res.TenantCreation != tt.wantShape {
					t.Errorf("%s.TenantCreation = %q, want %q", res.Name(), res.TenantCreation, tt.wantShape)
				}
			}
		})
	}
}

// Test_rejectTenantAnnotation pins the refusal of @tenant on the kinds that have no
// table: a view, a computed resource and a method, each through its own extractor.
func Test_rejectTenantAnnotation(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "tenantfixture"))

	tests := []struct {
		name       string
		structName string
		extract    func(c *client, pStruct *parser.Struct) error
		wantErr    string
	}{
		{
			name:       "a view",
			structName: "Ledger",
			extract: func(c *client, pStruct *parser.Struct) error {
				_, err := c.structsToVirtualResources([]*parser.Struct{pStruct})

				return err
			},
			wantErr: "struct Ledger: @tenant on a @virtual struct; the tenant record is a table-backed @resource, since its rows are the tenants",
		},
		{
			name:       "a computed resource",
			structName: "Tally",
			extract: func(c *client, pStruct *parser.Struct) error {
				_, err := c.structsToCompResources([]*parser.Struct{pStruct})

				return err
			},
			wantErr: "struct Tally: @tenant on a @computed struct; the tenant record is a table-backed @resource, since its rows are the tenants",
		},
		{
			name:       "a method",
			structName: "Reindex",
			extract: func(c *client, pStruct *parser.Struct) error {
				_, err := c.structsToRPCMethods([]*parser.Struct{pStruct})

				return err
			},
			wantErr: "struct Reindex: @tenant on a @rpc struct; the tenant record is a table-backed @resource, since its rows are the tenants",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pStruct := structs[tt.structName]
			if pStruct == nil {
				t.Fatalf("struct %q not found in fixture package", tt.structName)
			}
			err := tt.extract(&client{tableMap: tenantFixtureTables()}, pStruct)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("extraction error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// Test_resolveTenantRecord pins what the package-level resolution does once every kind
// is extracted: the record names the segment pair, nothing tenant-scoped keeps the
// defaults, and a tenant-scoped resource, computed resource or method without a record
// is refused with the one sentence.
func Test_resolveTenantRecord(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadCollectionFixture(t))
	tenantStructs := fixtureStructs(loadFixture(t, "tenantfixture"))

	domainScoped := func(res *resourceInfo) { res.PermissionScope = accesstypes.DomainPermissionScope }

	tests := []struct {
		name        string
		resources   []*resourceInfo
		computed    []*computedResource
		methods     []*rpcMethodInfo
		wantSegment string
		wantParam   string
		wantErr     string
	}{
		{
			name: "the record names the segment pair",
			resources: []*resourceInfo{
				fixtureResource(t, tenantStructs, "Sector", func(res *resourceInfo) { res.IsTenant = true }),
				fixtureResource(t, structs, "Vault", domainScoped),
			},
			wantSegment: "sectors",
			wantParam:   "sectorID",
		},
		{
			name:        "nothing tenant-scoped and no record keeps the defaults",
			resources:   []*resourceInfo{fixtureResource(t, structs, "Fossil", nil)},
			wantSegment: "domains",
			wantParam:   "domain",
		},
		{
			name:      "a tenant-scoped resource without a record is refused",
			resources: []*resourceInfo{fixtureResource(t, structs, "Vault", domainScoped)},
			wantErr:   "struct Vault: a tenant-scoped resource needs a tenant record; annotate it @tenant",
		},
		{
			name: "a tenant-scoped computed resource without a record is refused",
			computed: []*computedResource{{
				Struct:          structs["Summary"],
				PermissionScope: accesstypes.DomainPermissionScope,
			}},
			wantErr: "struct Summary: a tenant-scoped resource needs a tenant record; annotate it @tenant",
		},
		{
			name: "a tenant-scoped method without a record is refused",
			methods: []*rpcMethodInfo{{
				Struct:          structs["DoSomething"],
				PermissionScope: accesstypes.DomainPermissionScope,
			}},
			wantErr: "struct DoSomething: a tenant-scoped resource needs a tenant record; annotate it @tenant",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &resourceGenerator{
				client:             &client{},
				domainRouteSegment: defaultDomainRouteSegment,
				domainRouteParam:   defaultDomainRouteParam,
			}
			r.resources = tt.resources
			r.computedResources = tt.computed
			r.rpcMethods = tt.methods

			// Generate's order: the record resolves before the methods are extracted,
			// and the methods are checked against it afterwards.
			err := r.resolveTenantRecord()
			if err == nil {
				err = r.requireTenantRecordForMethods()
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("resolveTenantRecord() error = %v, want it to contain %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("resolveTenantRecord() error = %v", err)
			}
			if r.domainRouteSegment != tt.wantSegment || r.domainRouteParam != tt.wantParam {
				t.Errorf("segment pair = %q/%q, want %q/%q", r.domainRouteSegment, r.domainRouteParam, tt.wantSegment, tt.wantParam)
			}
		})
	}
}

// Test_tenantsTemplate pins the generated roster constructor: named after the record,
// carrying its table and key column into resource.NewTenantRoster.
func Test_tenantsTemplate(t *testing.T) {
	t.Parallel()

	c := &client{}
	out, err := c.generateTemplateOutput("tenantsTemplate", tenantsTemplate, &tenantsData{
		Source:    "resources",
		Package:   "app",
		Name:      "Sector",
		Table:     "Sectors",
		KeyColumn: "Id",
	})
	if err != nil {
		t.Fatalf("generateTemplateOutput() error = %v", err)
	}
	if _, err := format.Source(out); err != nil {
		t.Fatalf("the rendered file does not parse: %v\n%s", err, out)
	}

	for _, want := range []string{
		"package app",
		"func NewSectorRoster(client resource.Client, opts ...resource.TenantRosterOption) *resource.TenantRoster {",
		`return resource.NewTenantRoster(client, "Sectors", "Id", opts...)`,
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("tenantsTemplate output missing %q:\n%s", want, out)
		}
	}
}

// Test_patchTemplate_tenant pins the standalone patch handler of the tenant record and
// of the directories record: a write of either marks the transaction, the roster is
// reloaded after the commit, and the tenants kind is signaled after the roster and
// before the answer; a resource that is neither carries none of it.
func Test_patchTemplate_tenant(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "tenantfixture"))

	tests := []struct {
		name            string
		tenant          bool
		directory       bool
		creation        tenantCreation
		wantContains    []string
		wantNotContains []string
	}{
		{
			name:   "the record reloads the roster after the commit and signals",
			tenant: true,
			wantContains: []string{
				"tenantsWritten := false",
				"tenantsWritten = false",
				"tenantsWritten = true",
				"if tenantsWritten {",
				"if err := a.TenantRoster().Reload(ctx); err != nil {",
				"if err := a.LiveService().Signal(ctx, resource.KindTenants); err != nil {",
				"logger.FromCtx(ctx).Errorf(",
			},
		},
		{
			name:     "a self-service record refuses the create path naming the route",
			tenant:   true,
			creation: tenantCreationSelfService,
			wantContains: []string{
				`return httpio.NewBadRequestMessage("a Sector is created by the person who becomes its administrator, through the create-sector route (@tenant(creation: self-service)); no administrator creates one")`,
				"if err := a.TenantRoster().Reload(ctx); err != nil {",
			},
		},
		{
			name:      "the directories record reloads the roster after the commit and signals, as the record does",
			directory: true,
			wantContains: []string{
				"tenantsWritten := false",
				"if err := a.TenantRoster().Reload(ctx); err != nil {",
				"if err := a.LiveService().Signal(ctx, resource.KindTenants); err != nil {",
			},
		},
		{
			name:            "another resource carries none of it",
			wantNotContains: []string{"tenantsWritten", "TenantRoster()", "KindTenants"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := fixtureResource(t, structs, "Sector", func(res *resourceInfo) {
				res.IsTenant, res.IsTenantDirectory, res.TenantCreation = tt.tenant, tt.directory, tt.creation
			})
			r := &resourceGenerator{client: &client{resource: "testdata/tenantfixture"}, applicationName: "App", receiverName: "a"}
			out, err := r.handlerContent(PatchHandler, res)
			if err != nil {
				t.Fatalf("handlerContent() error = %v", err)
			}
			if _, err := format.Source(append([]byte("package app\n\n"), out...)); err != nil {
				t.Fatalf("the rendered handler does not parse: %v\n%s", err, out)
			}

			for _, want := range tt.wantContains {
				if !strings.Contains(string(out), want) {
					t.Errorf("patch handler missing %q:\n%s", want, out)
				}
			}
			for _, notWant := range tt.wantNotContains {
				if strings.Contains(string(out), notWant) {
					t.Errorf("patch handler must not contain %q:\n%s", notWant, out)
				}
			}
			if (!tt.tenant && !tt.directory) || tt.creation.SelfService() {
				return
			}

			// After the commit means after ExecuteFunc returns; the roster before the
			// signal, the signal before the answer.
			s := string(out)
			commit := strings.Index(s, "}); err != nil {")
			reload := strings.Index(s, "a.TenantRoster().Reload(ctx)")
			signal := strings.Index(s, "Signal(ctx, resource.KindTenants)")
			answer := strings.Index(s, "return httpio.NewEncoder(w).Ok(")
			if commit >= reload || reload >= signal || signal >= answer {
				t.Errorf("order: commit %d, roster %d, signal %d, answer %d; want ascending:\n%s", commit, reload, signal, answer, out)
			}
		})
	}
}

// Test_authzTestTemplate_tenantContract pins the generated matrix's harness contract:
// newTestHandler adds the suite's domain value to the test application's roster, and
// the retired seam is not asked for.
func Test_authzTestTemplate_tenantContract(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		`newTestHandler adds the suite's domain value "testDomain" to the test application's`,
		"tenant roster with Add (resource.TenantRoster.Add)",
	} {
		if !strings.Contains(authzTestTemplate, want) {
			t.Errorf("authzTestTemplate missing %q", want)
		}
	}
	for _, notWant := range []string{"DomainExists", "DomainVisible"} {
		if strings.Contains(authzTestTemplate, notWant) {
			t.Errorf("authzTestTemplate still asks the harness for %s", notWant)
		}
	}
}
