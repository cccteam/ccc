package resources

import (
	"cloud.google.com/go/civil"
	"github.com/cccteam/ccc/resource"
)

type (
	// Sector is the tenant record (@tenant): the table whose rows are the sectors, the
	// permission domains every sector-scoped URL names. The route segment and the route
	// parameter derive from it (/console/api/sectors lists the sectors while
	// /console/api/sectors/{sectorID}/... serves the sector-scoped routes), and every
	// instance holds a roster of its keys (resource.TenantRoster, built by the generated
	// NewSectorRoster) that the generated DomainGuard asks before a sector-scoped request
	// runs. The application derives its domain universe from this table rather than a
	// fixed in-code list, and the roster keeps up without a restart: the generated create
	// and delete paths add and remove the sector in the writing instance's roster after
	// the commit and signal the tenants kind, so every other instance reloads at once, and
	// a role held in every sector reaches the new sector with nothing written for it.
	// Charting a sector is a data change, not a release, a migration or a new login.
	//
	// The primary key is a human-readable slug, not a UUID: sector identifiers appear in
	// every sector-scoped URL and in role provisioning, and the schema enforces the slug
	// shape with a CHECK constraint. Creating a sector therefore supplies its key, and the
	// key cannot change on update.
	//
	// Sector itself is a GLOBAL resource: administering the sector list is a global
	// concern. The star chart's dark sectors are the ones a login holds no grant in.
	//
	// Who may chart a sector is declared here, apart from who may enter one
	// (@tenant(creation: ...)), and the record declares both ways: the surveyor's, Create
	// on Sectors under a global role, through the create path above; and the pioneer's,
	// self-service, one generated route (POST /console/api/create-sector) for a person
	// who signed in holding no sector, through the crew auth's sign-up registration (any
	// Google account, open sign-up), and names their own. The route writes the row, seats
	// the person with the roles the App names (SectorAdministratorRoles: SectorMarshal)
	// and hands the sector to the roster, so their next request stands in it; a person
	// who already holds a sector is refused, the governor included, since an
	// administrator of one sector never creates another there. @tenant alone would keep
	// charting to the surveyor; @tenant(creation: self-service) to the pioneer alone.
	//
	// Who may enter a sector is the record's declaration, data of the application rather
	// than deploy configuration: the directories the sector admits its people through
	// (SectorDirectory, one row each) and whether its people enter through them only
	// (DirectoryOnly). The crew and the members auths' sign-in rule reads it in the
	// sign-in's own transaction (NewSectorDeclarationReader) and every instance's roster
	// keeps a copy beside the keys (NewSectorRoster, Declarations), which this record's
	// and the directories' generated write paths reload after their commit, so a sector
	// is offered to a session only when the session may enter it: the navigator, through
	// the fleet's directory, reaches the sectors that declare it (the seed declares it on
	// all three); the rigger, by password, reaches Anvil and not Bastion, which admits
	// its people through its directory only; the bosun, whose only sector is Bastion, is
	// refused at sign-in. The pioneer's Ember declares the directory she came through,
	// written by the create-sector route in the row's transaction, so it admits her at
	// once. The record's change events (DataChangeEvents, Config) are the audit of every
	// edit.
	//
	// Demonstrates: tenancy.tenant-record, tenancy.run-time-tenant, tenancy.concealed, tenancy.self-service, tenancy.declaration, tenancy.directory-only, tenancy.creator-directory, @order, @page.
	//
	// @resource
	// @tenant(creation: both)
	// @order(Name asc)
	// @page(default: 25, max: 200)
	Sector struct {
		ID          string     `spanner:"Id"`
		Name        string     `spanner:"Name"`
		Region      string     `spanner:"Region"`
		Established civil.Date `spanner:"Established"`
		// DirectoryOnly says the sector's people enter through its directories only: a
		// password does not reach the sector. Set by whoever holds the record's Update
		// grant on the column (Headquarters), never at charting: a new sector declares
		// nothing beyond its creator's directory.
		// @directoryOnly
		DirectoryOnly bool `spanner:"DirectoryOnly"`
	}
)

// Config enables change tracking: Sector mutations write DataChangeEvents rows in the
// same transaction, the audit of every change to what a sector declares.
func (Sector) Config() resource.Config {
	return defaultConfig().SetTrackChanges(true)
}
