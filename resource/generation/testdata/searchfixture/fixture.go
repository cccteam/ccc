// Package searchfixture provides parsed-struct fixtures for the search facts the
// generator derives from a schema (searchFixtureTables in the tests): a searched table
// with every token shape and its @searchAlso children, the prefix fact, a view naming
// the table columns it projects, and one struct per refusal.
package searchfixture

import (
	"time"

	"cloud.google.com/go/civil"
	"github.com/cccteam/ccc"
)

// Struct-level annotations only parse from a TypeSpec doc comment, so the structs sit
// in grouped type blocks like real project sources do.
type (
	// Account carries every token shape: Name anchored both ways, Phone as digits, BornOn
	// a date, OpenedAt a timestamp in a named zone, Email anchored by prefix with short
	// tokens for anchors alone and diacritics removed, Notes under a full-text tokenizer
	// (no fact), Code lowercased second in the tenant's index (the prefix fact), and
	// Initials over a plain generated column (output-only by derivation). Its box also
	// searches the contact's display name, the note's body (whose index does not store
	// the foreign key: a warning), and the pairing's label through the named foreign key.
	//
	// @resource
	// @permissionScope(domain)
	// @searchAlso(AccountContact.DisplayName)
	// @searchAlso(AccountNote.Body)
	// @searchAlso(Pairing.Label, via: OtherAccountId)
	Account struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		TenantID string     `spanner:"TenantId"`
		Name     string     `spanner:"Name"`
		Phone    string     `spanner:"Phone"`
		BornOn   civil.Date `spanner:"BornOn"`
		OpenedAt time.Time  `spanner:"OpenedAt"`
		Email    string     `spanner:"Email" conditions:"pii"`
		Notes    string     `spanner:"Notes"`
		Code     string     `spanner:"Code"`
		Initials string     `spanner:"Initials"`
	}

	// AccountContact is the child whose display name an account's box also searches;
	// its search index stores the foreign key, so the branch raises no warning.
	//
	// @resource
	// @permissionScope(domain)
	AccountContact struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		TenantID    string   `spanner:"TenantId"`
		AccountID   ccc.UUID `spanner:"AccountId"`
		DisplayName string   `spanner:"DisplayName"`
		UserID      string   `spanner:"UserId"`
	}

	// AccountNote is the child whose search index does not store the foreign key.
	//
	// @resource
	// @permissionScope(domain)
	AccountNote struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		TenantID  string   `spanner:"TenantId"`
		AccountID ccc.UUID `spanner:"AccountId"`
		Body      string   `spanner:"Body"`
	}

	// Pairing references Accounts twice, so a branch through it names its foreign key.
	//
	// @resource
	// @permissionScope(domain)
	Pairing struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		TenantID       string   `spanner:"TenantId"`
		AccountID      ccc.UUID `spanner:"AccountId"`
		OtherAccountID ccc.UUID `spanner:"OtherAccountId"`
		Label          string   `spanner:"Label"`
	}

	// Unpartitioned is tenant-scoped with a search index partitioned by nothing: warns.
	//
	// @resource
	// @permissionScope(domain)
	Unpartitioned struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		TenantID string `spanner:"TenantId"`
		Name     string `spanner:"Name"`
	}

	// Global is a global resource whose box also searches a tenant-scoped child: the
	// tenancies differ, refused.
	//
	// @resource
	// @searchAlso(GlobalContact.DisplayName)
	Global struct {
		ID   ccc.UUID `spanner:"Id"`
		Name string   `spanner:"Name"`
	}

	// GlobalContact is Global's tenant-scoped child.
	//
	// @resource
	// @permissionScope(domain)
	GlobalContact struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		TenantID    string   `spanner:"TenantId"`
		GlobalID    ccc.UUID `spanner:"GlobalId"`
		DisplayName string   `spanner:"DisplayName"`
	}

	// Twin names a link's label where the link references it twice, without saying
	// which foreign key: refused.
	//
	// @resource
	// @permissionScope(domain)
	// @searchAlso(TwinLink.Label)
	Twin struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		TenantID string `spanner:"TenantId"`
	}

	// TwinLink references Twins twice.
	//
	// @resource
	// @permissionScope(domain)
	TwinLink struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		TenantID    string   `spanner:"TenantId"`
		TwinID      ccc.UUID `spanner:"TwinId"`
		OtherTwinID ccc.UUID `spanner:"OtherTwinId"`
		Label       string   `spanner:"Label"`
	}

	// Misvia names a foreign key the child does not have: refused.
	//
	// @resource
	// @permissionScope(domain)
	// @searchAlso(AccountContact.DisplayName, via: Nope)
	Misvia struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		TenantID string `spanner:"TenantId"`
	}

	// Keyed declares two key columns and a branch: refused.
	//
	// @resource
	// @permissionScope(domain)
	// @searchAlso(AccountContact.DisplayName)
	Keyed struct {
		// @domain
		TenantID string `spanner:"TenantId"`
		Sequence int64  `spanner:"Sequence"`
	}

	// Unfacted names a child field with no search fact: refused.
	//
	// @resource
	// @permissionScope(domain)
	// @searchAlso(AccountContact.UserID)
	Unfacted struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		TenantID string `spanner:"TenantId"`
	}

	// Malformed writes the child field without the dot: refused.
	//
	// @resource
	// @searchAlso(AccountContact)
	Malformed struct {
		ID ccc.UUID `spanner:"Id"`
	}

	// Doubled's table carries two search indexes: refused.
	//
	// @resource
	Doubled struct {
		ID   ccc.UUID `spanner:"Id"`
		Name string   `spanner:"Name"`
	}

	// Orphaned's token column is in no search index: refused.
	//
	// @resource
	Orphaned struct {
		ID   ccc.UUID `spanner:"Id"`
		Name string   `spanner:"Name"`
	}

	// Twinned has two token columns over one source column: refused.
	//
	// @resource
	Twinned struct {
		ID   ccc.UUID `spanner:"Id"`
		Name string   `spanner:"Name"`
	}

	// Misparted's search index is partitioned by a column that is not the tenant's: refused.
	//
	// @resource
	// @permissionScope(domain)
	Misparted struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		TenantID string `spanner:"TenantId"`
		Region   string `spanner:"Region"`
		Name     string `spanner:"Name"`
	}

	// Mistyped reads a date token over a string field: refused.
	//
	// @resource
	Mistyped struct {
		ID     ccc.UUID `spanner:"Id"`
		BornOn string   `spanner:"BornOn"`
	}

	// Numbered reads a text token over an integer field: refused.
	//
	// @resource
	Numbered struct {
		ID    ccc.UUID `spanner:"Id"`
		Count int64    `spanner:"Count"`
	}

	// Unzoned formats a timestamp without naming its zone: refused.
	//
	// @resource
	Unzoned struct {
		ID       ccc.UUID  `spanner:"Id"`
		OpenedAt time.Time `spanner:"OpenedAt"`
	}

	// Tokened carries the TOKENLIST column as a field: refused.
	//
	// @resource
	Tokened struct {
		ID         ccc.UUID `spanner:"Id"`
		Name       string   `spanner:"Name"`
		NameSearch string   `spanner:"Name_Search"`
	}

	// Restated restates output_only on a field the database generates: refused.
	//
	// @resource
	Restated struct {
		ID       ccc.UUID `spanner:"Id"`
		Name     string   `spanner:"Name"`
		Initials string   `spanner:"Initials" conditions:"output_only"`
	}

	// Tagged writes a search tag on a table field, which the schema decides: refused.
	//
	// @resource
	Tagged struct {
		ID   ccc.UUID `spanner:"Id"`
		Name string   `spanner:"Name" search:"token=Name_Search"`
	}

	// Roster is the view over Accounts: Name names the table column whose token column
	// the SQL projects, Code the lowercased column (second in an index: the prefix fact).
	//
	// @virtual
	// @permissionScope(domain)
	Roster struct {
		// @primarykey
		ID ccc.UUID `spanner:"Id" uniqueindex:"true"`
		// @domain
		TenantID string `spanner:"TenantId"`
		Name     string `spanner:"Name" index:"true" search:"Accounts.Name"`
		Code     string `spanner:"Code" search:"Accounts.Code"`
	}

	// Lost names a table the schema does not have: refused.
	//
	// @virtual
	Lost struct {
		// @primarykey
		ID   ccc.UUID `spanner:"Id" uniqueindex:"true"`
		Name string   `spanner:"Name" search:"Nowhere.Name"`
	}

	// Unfound names a column with no fact: refused.
	//
	// @virtual
	Unfound struct {
		// @primarykey
		ID    ccc.UUID `spanner:"Id" uniqueindex:"true"`
		Notes string   `spanner:"Notes" search:"Accounts.Notes"`
	}

	// Dotless writes the tag without the dot: refused.
	//
	// @virtual
	Dotless struct {
		// @primarykey
		ID   ccc.UUID `spanner:"Id" uniqueindex:"true"`
		Name string   `spanner:"Name" search:"Accounts"`
	}

	// Branched declares @searchAlso on a view: refused.
	//
	// @virtual
	// @searchAlso(AccountContact.DisplayName)
	Branched struct {
		// @primarykey
		ID ccc.UUID `spanner:"Id" uniqueindex:"true"`
	}

	// Ledger is a view over AccountContacts whose box also reaches Accounts' index: its
	// root carries the (TenantId, AccountId) index the key set needs.
	//
	// @virtual
	// @rowsOf(AccountContacts)
	// @permissionScope(domain)
	Ledger struct {
		// @primarykey
		ID ccc.UUID `spanner:"Id" uniqueindex:"true"`
		// @domain
		TenantID    string `spanner:"TenantId"`
		DisplayName string `spanner:"DisplayName" index:"true" search:"AccountContacts.DisplayName"`
		AccountName string `spanner:"AccountName" index:"true" search:"Accounts.Name"`
	}

	// Journal is a view over AccountNotes whose box reaches Accounts' index, and
	// AccountNotes has no (TenantId, AccountId) index: refused naming it.
	//
	// @virtual
	// @rowsOf(AccountNotes)
	// @permissionScope(domain)
	Journal struct {
		// @primarykey
		ID ccc.UUID `spanner:"Id" uniqueindex:"true"`
		// @domain
		TenantID    string `spanner:"TenantId"`
		Body        string `spanner:"Body"        index:"true" search:"AccountNotes.Body"`
		AccountName string `spanner:"AccountName" index:"true" search:"Accounts.Name"`
	}

	// Sprawl spans two indexes and declares no root: refused.
	//
	// @virtual
	Sprawl struct {
		// @primarykey
		ID          ccc.UUID `spanner:"Id" uniqueindex:"true"`
		Name        string   `spanner:"Name"        index:"true" search:"Accounts.Name"`
		DisplayName string   `spanner:"DisplayName" index:"true" search:"AccountContacts.DisplayName"`
	}

	// Detached's root has no foreign key to the other table its box reaches: refused.
	//
	// @virtual
	// @rowsOf(Globals)
	Detached struct {
		// @primarykey
		ID          ccc.UUID `spanner:"Id" uniqueindex:"true"`
		Name        string   `spanner:"Name"        index:"true" search:"Globals.Name"`
		AccountName string   `spanner:"AccountName" index:"true" search:"Accounts.Name"`
	}
)
