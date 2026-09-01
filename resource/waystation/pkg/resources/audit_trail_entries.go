package resources

import "github.com/cccteam/ccc/accesstypes"

// AuditTrailEntries is the audit page's resource: a hand-written list handler over
// the DataChangeEvents table the change-tracked resources (WorkOrders, Requisitions)
// write into. There is no generated handler — the table is library infrastructure,
// not a schema resource — so the permission registration the generator cannot derive
// is declared manually: @manualAddResource puts List into the generated permission
// collection (MigrateRoles validates grants against it, the TypeScript permission
// constants include it) and the handler in the app package checks it.
//
// @manualAddResource(List)
const AuditTrailEntries accesstypes.Resource = "AuditTrailEntries"
