// Package rpc defines Lodestar's RPC methods, wiring them up to implementation code. The
// generator uses this package to generate handlers for RPC endpoints. The methods are the
// only writers of workflow state: the two StatusId columns are structurally unwritable
// from the wire (@state), so every mission and refit transition is an Execute-gated method
// whose declared @transition owns edge legality, and only edge legality. What each role
// may do in each state, and which rows a role may move at all, is conditional grants
// (schema/roles/crew.json), never code here.
//
// A method is a struct with an Execute whose signature says how it runs: the transaction
// form (resource.ReadWriteTransaction second) runs inside the handler's transaction, the
// client form (resource.Client second) outside one. Execute returns error, or (Result,
// error) to answer; the generator mirrors the request, the result, and every struct they
// reach into the handler with generated wire names.
//
// Three conventions the generator cannot see hold here (resource/README.md §7): a method
// answers with identifiers and outcomes, never rows; a method that only answers a question
// is a computed resource, not an RPC (the hazard board and the pilot card live in
// pkg/computedresources); and a transaction-form body keeps its effects inside the
// transaction, so a dry run (X-Dry-Run) of it tells the truth.
//
// Bodies are trusted by default: FailMission writes its note as the application. A body
// that should defer to the caller's own grants arms the write (HoldMission writes its
// reason with Enforce(caller)) or the read (ReleaseConsignment and CompileBriefing read
// armed, so the droid and the archivist see only what their grants allow), reads a
// decision as data (CompleteMission, before leaving a note), or borrows a role's checker
// through caller.As (CompleteMission posts the settlement as the Paymaster).
package rpc

import (
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
)

// PaymasterRole is the service role a body borrows to post a settlement: nobody signs in
// as it, MigrateRoles provisions it from schema/roles/crew.json, and the ship's log names
// it beside the actor.
//
// Demonstrates: rpc.as-role.
const PaymasterRole accesstypes.Role = "Paymaster"

// RoleCheckerFunc answers a role's permission checker: the engine's ForRole, the same
// checker an act-as-role session is built on.
type RoleCheckerFunc func(role accesstypes.Role) resource.RolePermissions

// Client carries application dependencies into RPC method implementations: the role
// checker a body composes through caller.As.
type Client struct {
	forRole RoleCheckerFunc
}

// NewClient constructs a Client over the engine's role checker.
func NewClient(forRole RoleCheckerFunc) *Client {
	return &Client{forRole: forRole}
}

// ForRole returns the checker for a role, for a body acting through caller.As.
func (c *Client) ForRole(role accesstypes.Role) resource.RolePermissions {
	return c.forRole(role)
}
