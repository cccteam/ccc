package generation

import (
	"testing"

	"github.com/cccteam/ccc/cache"
	"github.com/google/go-cmp/cmp"
)

// cascadeFixtureTables is the synthetic schema behind the cascadefixture structs: the
// parent, and one child table per outcome, each carrying the interleave facts and the
// delete rule of its foreign keys as the schema read records them.
func cascadeFixtureTables() map[string]*tableMetadata {
	pk := columnMeta{IsPrimaryKey: true}
	key := func(ordinal int64) columnMeta {
		return columnMeta{IsPrimaryKey: true, OrdinalPosition: ordinal, KeyOrdinalPosition: ordinal}
	}
	nullable := func(ordinal int64) columnMeta {
		return columnMeta{IsNullable: true, OrdinalPosition: ordinal}
	}
	foreignKey := func(ordinal int64, rule string) columnMeta {
		return columnMeta{IsForeignKey: true, ReferencedTable: "Parents", ReferencedColumn: "Id", DeleteRule: rule, OrdinalPosition: ordinal}
	}

	return map[string]*tableMetadata{
		"Parents": {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "Name": {OrdinalPosition: 1}}},
		"Interleaveds": {PkCount: 2, IsInterleaved: true, ParentTable: "Parents", OnDeleteCascade: true, Columns: map[string]columnMeta{
			"Id": key(0), "Sequence": key(1), "PhotoKey": nullable(2),
		}},
		"Referencings": {PkCount: 1, Columns: map[string]columnMeta{
			"Id": pk, "ParentId": foreignKey(1, "CASCADE"), "StoreKey": nullable(2),
		}},
		"Boths": {PkCount: 2, IsInterleaved: true, ParentTable: "Parents", OnDeleteCascade: true, Columns: map[string]columnMeta{
			"Id": key(0), "Sequence": key(1), "OwnerId": foreignKey(2, "CASCADE"), "StoreKey": nullable(3),
		}},
		"Retaineds": {PkCount: 2, IsInterleaved: true, ParentTable: "Parents", Columns: map[string]columnMeta{
			"Id": key(0), "Sequence": key(1), "StoreKey": nullable(2),
		}},
		"Plains": {PkCount: 1, Columns: map[string]columnMeta{
			"Id": pk, "ParentId": foreignKey(1, "NO ACTION"), "StoreKey": nullable(2),
		}},
		"Filelesses": {PkCount: 2, IsInterleaved: true, ParentTable: "Parents", OnDeleteCascade: true, Columns: map[string]columnMeta{
			"Id": key(0), "Sequence": key(1), "Note": nullable(2),
		}},
	}
}

// TestAuditFindings pins the audit pass over the cascadefixture resources: which shapes
// raise the cascade finding, which stay silent, and the exact values a finding carries.
func TestAuditFindings(t *testing.T) {
	t.Parallel()

	c := &client{tableMap: cascadeFixtureTables()}
	pkg := loadFixture(t, "cascadefixture")

	resources, err := c.structsToResources(pkg.Structs)
	if err != nil {
		t.Fatalf("structsToResources() error = %v", err)
	}
	got := c.auditFindings(resources)

	byResource := make(map[string][]Finding, len(got))
	for _, f := range got {
		finding, ok := f.(CascadeReleaseFinding)
		if !ok {
			t.Fatalf("unexpected finding kind %T", f)
		}
		byResource[finding.Resource] = append(byResource[finding.Resource], f)
	}

	tests := []struct {
		name     string
		resource string
		want     []Finding
	}{
		{name: "the parent stores no file and is silent", resource: "Parent"},
		{
			name:     "an interleaved child on cascade names the parent",
			resource: "Interleaved",
			want:     []Finding{CascadeReleaseFinding{Resource: "Interleaved", Table: "Interleaveds", Parent: "Parents"}},
		},
		{
			name:     "a cascading foreign key names the column",
			resource: "Referencing",
			want:     []Finding{CascadeReleaseFinding{Resource: "Referencing", Table: "Referencings", Column: "ParentId"}},
		},
		{
			name:     "both causes raise one finding each, the interleave first",
			resource: "Both",
			want: []Finding{
				CascadeReleaseFinding{Resource: "Both", Table: "Boths", Parent: "Parents"},
				CascadeReleaseFinding{Resource: "Both", Table: "Boths", Column: "OwnerId"},
			},
		},
		{name: "an interleaved child on no action is silent", resource: "Retained"},
		{name: "a plain foreign key is silent", resource: "Plain"},
		{name: "a cascade table without a file is silent", resource: "Fileless"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tt.want, byResource[tt.resource]); diff != "" {
				t.Errorf("auditFindings() for %s mismatch (-want +got):\n%s", tt.resource, diff)
			}
		})
	}

	if len(got) != 4 {
		t.Errorf("auditFindings() raised %d findings, want 4: %v", len(got), got)
	}
}

// TestFinding_String pins the one-line texts a runner prints under -audit.
func TestFinding_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		finding Finding
		want    string
	}{
		{
			name:    "an interleaved child names its parent",
			finding: CascadeReleaseFinding{Resource: "RefitTask", Table: "RefitTasks", Parent: "Refits"},
			want:    "RefitTask stores files on RefitTasks, whose rows the database deletes by cascade, interleaved in Refits, so a cascade releases none of their objects and the sweep removes them; an application that cares deletes the rows by patch first (README section 13)",
		},
		{
			name:    "a cascading foreign key names its column",
			finding: CascadeReleaseFinding{Resource: "Attachment", Table: "Attachments", Column: "OrderId"},
			want:    "Attachment stores files on Attachments, whose rows the database deletes by cascade, through the foreign key on OrderId, so a cascade releases none of their objects and the sweep removes them; an application that cares deletes the rows by patch first (README section 13)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.finding.String(); got != tt.want {
				t.Errorf("String() =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

// TestAudit_nilBeforeGenerate pins the interface's answer before a run.
func TestAudit_nilBeforeGenerate(t *testing.T) {
	t.Parallel()

	var r resourceGenerator
	if got := r.Audit(); got != nil {
		t.Errorf("Audit() before Generate = %v, want nil", got)
	}
	if got := r.Warnings(); got != nil {
		t.Errorf("Warnings() before Generate = %v, want nil", got)
	}
}

// Test_tableMap_cacheRoundTrip pins that the facts the audit pass reads ride the table
// map through the generation cache: a table map stored and loaded again raises the same
// findings, so a cached schema audits like a freshly read one.
func Test_tableMap_cacheRoundTrip(t *testing.T) {
	t.Parallel()

	genCache, err := cache.New(t.TempDir())
	if err != nil {
		t.Fatalf("cache.New() error = %v", err)
	}
	if err := genCache.Store("spanner/test", tableMapCache, cascadeFixtureTables()); err != nil {
		t.Fatalf("cache.Store() error = %v", err)
	}
	loaded := make(map[string]*tableMetadata)
	if ok, err := genCache.Load("spanner/test", tableMapCache, &loaded); err != nil || !ok {
		t.Fatalf("cache.Load() = %v, %v; want loaded", ok, err)
	}

	tests := []struct {
		name     string
		resource string
		table    string
		want     []Finding
	}{
		{name: "the interleave survives the round trip", resource: "Interleaved", table: "Interleaveds", want: []Finding{CascadeReleaseFinding{Resource: "Interleaved", Table: "Interleaveds", Parent: "Parents"}}},
		{name: "the delete rule survives the round trip", resource: "Referencing", table: "Referencings", want: []Finding{CascadeReleaseFinding{Resource: "Referencing", Table: "Referencings", Column: "ParentId"}}},
		{name: "a no-action child stays silent", resource: "Retained", table: "Retaineds"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tt.want, cascadeReleaseFindings(tt.resource, tt.table, loaded[tt.table])); diff != "" {
				t.Errorf("cascadeReleaseFindings() after the round trip mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
