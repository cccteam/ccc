package resource

import (
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/google/go-cmp/cmp"
)

// TestTenantRosterStatement pins the roster's one statement per database type: the key
// column as text under the name the scan reads, the identifiers quoted the way the
// package quotes them for each dialect, and a refusal for a type the package does not
// know.
func TestTenantRosterStatement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		dbType    DBType
		table     string
		keyColumn string
		want      string
		wantErr   bool
	}{
		{
			name:      "Spanner quotes with backticks and casts to STRING",
			dbType:    SpannerDBType,
			table:     "Sectors",
			keyColumn: "Id",
			want:      "SELECT CAST(`Id` AS STRING) AS Domain FROM `Sectors`",
		},
		{
			name:      "Postgres quotes with double quotes and casts to TEXT",
			dbType:    PostgresDBType,
			table:     "Sectors",
			keyColumn: "Id",
			want:      `SELECT CAST("Id" AS TEXT) AS "Domain" FROM "Sectors"`,
		},
		{
			name:      "a quote in an identifier is doubled",
			dbType:    PostgresDBType,
			table:     `Odd"Name`,
			keyColumn: "Id",
			want:      `SELECT CAST("Id" AS TEXT) AS "Domain" FROM "Odd""Name"`,
		},
		{
			name:      "an unknown database type is refused",
			dbType:    DBType("oracle"),
			table:     "Sectors",
			keyColumn: "Id",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tenantRosterStatement(tt.dbType, tt.table, tt.keyColumn)
			if (err != nil) != tt.wantErr {
				t.Fatalf("tenantRosterStatement() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("tenantRosterStatement() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestTenantRoster_set pins the in-memory half of the roster, which touches no
// database: Add and Remove change the set at once, Has answers from it, and Domains
// lists it sorted and never nil.
func TestTenantRoster_set(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		steps       func(r *TenantRoster)
		wantHas     []accesstypes.Domain
		wantNot     []accesstypes.Domain
		wantDomains []accesstypes.Domain
	}{
		{
			name:        "an unstarted roster holds nothing",
			steps:       func(*TenantRoster) {},
			wantNot:     []accesstypes.Domain{"alpha"},
			wantDomains: []accesstypes.Domain{},
		},
		{
			name: "Add puts the domain in the set",
			steps: func(r *TenantRoster) {
				r.Add("beta")
				r.Add("alpha")
			},
			wantHas:     []accesstypes.Domain{"alpha", "beta"},
			wantNot:     []accesstypes.Domain{"gamma"},
			wantDomains: []accesstypes.Domain{"alpha", "beta"},
		},
		{
			name: "Add twice holds the domain once",
			steps: func(r *TenantRoster) {
				r.Add("alpha")
				r.Add("alpha")
			},
			wantHas:     []accesstypes.Domain{"alpha"},
			wantDomains: []accesstypes.Domain{"alpha"},
		},
		{
			name: "Remove drops the domain and leaves the others",
			steps: func(r *TenantRoster) {
				r.Add("alpha")
				r.Add("beta")
				r.Remove("alpha")
				r.Remove("never-added")
			},
			wantHas:     []accesstypes.Domain{"beta"},
			wantNot:     []accesstypes.Domain{"alpha", "never-added"},
			wantDomains: []accesstypes.Domain{"beta"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := NewTenantRoster(nil, "Sectors", "Id")
			tt.steps(r)
			for _, domain := range tt.wantHas {
				if !r.Has(domain) {
					t.Errorf("Has(%q) = false, want true", domain)
				}
			}
			for _, domain := range tt.wantNot {
				if r.Has(domain) {
					t.Errorf("Has(%q) = true, want false", domain)
				}
			}
			domains, err := r.Domains(t.Context())
			if err != nil {
				t.Fatalf("Domains() error = %v", err)
			}
			if diff := cmp.Diff(tt.wantDomains, domains); diff != "" {
				t.Errorf("Domains() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestTenantRoster_nil pins the nil roster: every domain unknown, no domains, Add and
// Remove no-ops, and a Start that says what is missing, so an application that wires
// none fails closed rather than panicking.
func TestTenantRoster_nil(t *testing.T) {
	t.Parallel()

	var r *TenantRoster
	r.Add("alpha")
	r.Remove("alpha")
	if r.Has("alpha") {
		t.Error("a nil roster holds alpha")
	}
	domains, err := r.Domains(t.Context())
	if err != nil {
		t.Fatalf("Domains() error = %v", err)
	}
	if diff := cmp.Diff([]accesstypes.Domain{}, domains); diff != "" {
		t.Errorf("Domains() mismatch (-want +got):\n%s", diff)
	}
	if err := r.Start(t.Context()); err == nil {
		t.Error("Start() on a nil roster = nil, want an error")
	}
}

// dbTypeClient is a Client that answers its database type and nothing else: what a
// roster over a database the read is not implemented for sees before it reads.
type dbTypeClient struct {
	Client
	dbType DBType
}

func (c dbTypeClient) DBType() DBType {
	return c.dbType
}

// TestTenantRoster_Start_unsupportedDatabase pins Start on a database type the roster's
// read is not implemented for: the start fails naming it, before any transaction opens,
// and the set stays empty.
func TestTenantRoster_Start_unsupportedDatabase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		dbType DBType
	}{
		{name: "an unknown type is refused at the statement", dbType: DBType("oracle")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := NewTenantRoster(dbTypeClient{dbType: tt.dbType}, "Sectors", "Id")
			err := r.Start(t.Context())
			if err == nil {
				t.Fatal("Start() = nil, want an error")
			}
			if r.Has("alpha") {
				t.Error("a roster whose start failed holds alpha")
			}
		})
	}
}

// The roster's Domains is a DomainRoster: what a tenanted application hands
// SessionPermissions, pinned at compile time.
var _ DomainRoster = (*TenantRoster)(nil).Domains
