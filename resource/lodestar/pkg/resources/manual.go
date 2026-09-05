package resources

import "github.com/cccteam/ccc/accesstypes"

// ShipsLogEntries is the ship's log page's resource: a hand-written, SECTOR-scoped
// list handler over the DataChangeEvents table the change-tracked resources
// (Missions, Refits, Ships) write into. There is no generated handler — the table is
// library infrastructure, not a schema resource — so the permission registration the
// generator cannot derive is declared manually, with the scope argument:
// @manualAddResource(List, domain) puts List into the generated permission collection
// in the domain scope (MigrateRoles validates grants against it, the TypeScript
// permission constants include it) and the handler in the app package checks it.
//
// @manualAddResource(List, domain)
const ShipsLogEntries accesstypes.Resource = "ShipsLogEntries"

// ViewAsUser gates the impersonation mint route's "view as" moment: minting a session
// that operates as another user, read-only. It is the first manual EXECUTE
// registration — the constant reaches the generated TypeScript Methods constants, so
// the crew roster checks Execute on it by the generated name, never a hand-typed
// string (c9a7469).
//
// @manualAddResource(Execute)
const ViewAsUser accesstypes.Resource = "ViewAsUser"

// AssumeRole gates the impersonation mint route's "act as a role" moment: minting a
// session that operates as a role, with subject still bound to the actor.
//
// @manualAddResource(Execute)
const AssumeRole accesstypes.Resource = "AssumeRole"
