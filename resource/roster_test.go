package resource

import (
	"errors"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/google/go-cmp/cmp"
)

// TestTenantRosterStatements pins the roster's statements per database type: the key
// column as text under the name the scan reads with the directory-only flag beside it
// (FALSE where the record declares none), the directories record's rows when the
// declaration names them, the one-tenant form the sign-in reader runs, the identifiers
// quoted the way the package quotes them for each dialect, and a refusal for a type the
// package does not know.
func TestTenantRosterStatements(t *testing.T) {
	t.Parallel()

	declared := &TenantDeclarationColumns{DirectoryOnly: "DirectoryOnly", DirectoriesTable: "SectorDirectories", TenantColumn: "SectorId", MethodColumn: "Method", ConnectionColumn: "Connection"}

	tests := []struct {
		name     string
		dbType   DBType
		table    string
		columns  *TenantDeclarationColumns
		filtered bool
		want     rosterStatements
		wantErr  bool
	}{
		{
			name:   "Spanner quotes with backticks and casts to STRING; no declaration reads FALSE and no directories",
			dbType: SpannerDBType,
			table:  "Sectors",
			want:   rosterStatements{Keys: "SELECT CAST(`Id` AS STRING) AS Domain, FALSE AS DirectoryOnly FROM `Sectors`"},
		},
		{
			name:    "Spanner with the declaration reads the flag and the directories, ordered",
			dbType:  SpannerDBType,
			table:   "Sectors",
			columns: declared,
			want: rosterStatements{
				Keys:        "SELECT CAST(`Id` AS STRING) AS Domain, `DirectoryOnly` AS DirectoryOnly FROM `Sectors`",
				Directories: "SELECT CAST(`SectorId` AS STRING) AS Domain, `Method` AS Method, `Connection` AS Connection FROM `SectorDirectories` ORDER BY `Method`, `Connection`",
			},
		},
		{
			name:     "the sign-in reader's form reads one tenant by parameter",
			dbType:   SpannerDBType,
			table:    "Sectors",
			columns:  declared,
			filtered: true,
			want: rosterStatements{
				Keys:        "SELECT CAST(`Id` AS STRING) AS Domain, `DirectoryOnly` AS DirectoryOnly FROM `Sectors` WHERE CAST(`Id` AS STRING) = @domain",
				Directories: "SELECT CAST(`SectorId` AS STRING) AS Domain, `Method` AS Method, `Connection` AS Connection FROM `SectorDirectories` WHERE CAST(`SectorId` AS STRING) = @domain ORDER BY `Method`, `Connection`",
			},
		},
		{
			name:    "directories without a directory-only column read FALSE for the flag",
			dbType:  SpannerDBType,
			table:   "Sectors",
			columns: &TenantDeclarationColumns{DirectoriesTable: "SectorDirectories", TenantColumn: "SectorId", MethodColumn: "Method", ConnectionColumn: "Connection"},
			want: rosterStatements{
				Keys:        "SELECT CAST(`Id` AS STRING) AS Domain, FALSE AS DirectoryOnly FROM `Sectors`",
				Directories: "SELECT CAST(`SectorId` AS STRING) AS Domain, `Method` AS Method, `Connection` AS Connection FROM `SectorDirectories` ORDER BY `Method`, `Connection`",
			},
		},
		{
			name:   "Postgres quotes with double quotes and casts to TEXT",
			dbType: PostgresDBType,
			table:  "Sectors",
			want:   rosterStatements{Keys: `SELECT CAST("Id" AS TEXT) AS "Domain", FALSE AS "DirectoryOnly" FROM "Sectors"`},
		},
		{
			name:     "Postgres with the declaration, filtered, takes the tenant as $1",
			dbType:   PostgresDBType,
			table:    "Sectors",
			columns:  declared,
			filtered: true,
			want: rosterStatements{
				Keys:        `SELECT CAST("Id" AS TEXT) AS "Domain", "DirectoryOnly" AS "DirectoryOnly" FROM "Sectors" WHERE CAST("Id" AS TEXT) = $1`,
				Directories: `SELECT CAST("SectorId" AS TEXT) AS "Domain", "Method" AS "Method", "Connection" AS "Connection" FROM "SectorDirectories" WHERE CAST("SectorId" AS TEXT) = $1 ORDER BY "Method", "Connection"`,
			},
		},
		{
			name:   "a quote in an identifier is doubled",
			dbType: PostgresDBType,
			table:  `Odd"Name`,
			want:   rosterStatements{Keys: `SELECT CAST("Id" AS TEXT) AS "Domain", FALSE AS "DirectoryOnly" FROM "Odd""Name"`},
		},
		{
			name:    "an unknown database type is refused",
			dbType:  DBType("oracle"),
			table:   "Sectors",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tenantRosterStatements(tt.dbType, tt.table, "Id", tt.columns, tt.filtered)
			if (err != nil) != tt.wantErr {
				t.Fatalf("tenantRosterStatements() error = %v, wantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("tenantRosterStatements() mismatch (-want +got):\n%s", diff)
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
			// A tenant Add put in declares nothing, and so does one the set does not hold.
			for _, domain := range append(tt.wantHas, tt.wantNot...) {
				declaration, err := r.Declarations(t.Context(), domain)
				if err != nil {
					t.Fatalf("Declarations(%q) error = %v", domain, err)
				}
				if declaration != nil {
					t.Errorf("Declarations(%q) = %v, want nil", domain, declaration)
				}
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
	if err := r.Reload(t.Context()); err == nil {
		t.Error("Reload() on a nil roster = nil, want an error")
	}
	declaration, err := r.Declarations(t.Context(), "alpha")
	if err != nil || declaration != nil {
		t.Errorf("Declarations() on a nil roster = %v, %v; want nil, nil", declaration, err)
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
		name    string
		dbType  DBType
		wantErr error
	}{
		{name: "Postgres renders the statement and refuses the read", dbType: PostgresDBType, wantErr: errTenantRosterUnsupportedDatabase},
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
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("Start() error = %v, want %v", err, tt.wantErr)
			}
			if r.Has("alpha") {
				t.Error("a roster whose start failed holds alpha")
			}
		})
	}
}

// The roster's Domains is a DomainRoster and its Declarations a TenantDeclarations: what
// a tenanted application hands SessionPermissions, pinned at compile time.
var _ TenantDeclarations = (*TenantRoster)(nil).Declarations

// The roster's Domains is a DomainRoster: what a tenanted application hands
// SessionPermissions, pinned at compile time.
var _ DomainRoster = (*TenantRoster)(nil).Domains
