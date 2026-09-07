// Package rpc defines Lodestar's RPC methods, wiring them up to implementation code.
// The generator uses this package to generate handlers for RPC endpoints. The methods
// are the only writers of workflow state: the two StatusId columns are structurally
// unwritable from the wire (@state), so every mission and refit transition is an
// Execute-gated method whose declared @transition owns edge legality — and only edge
// legality. What each role may do in each state, and which rows a role may move at
// all, is conditional grants (cmd/bootstrap/demo_access.json), never code here.
//
// A method is a struct with an Execute whose signature says how it runs: the
// transaction form (resource.ReadWriteTransaction second) runs inside the handler's
// transaction, the client form (resource.Client second) outside one. Execute returns
// error, or (Result, error) to answer; the generator mirrors the request, the result,
// and every struct they reach into the handler with generated wire names.
//
// Three conventions the generator cannot see hold here (resource/README.md §7): a method
// answers with identifiers and outcomes, never rows — InspectShip returns a RefitReport
// of ids and readings, and the refit itself is read through its route; a method that
// only answers a question is a computed resource, not an RPC — the hazard board and the
// pilot card live in pkg/computedresources; and a transaction-form body keeps its
// effects inside the transaction, so a dry run (X-Dry-Run) of it tells the truth.
//
// Bodies are trusted by default: FailMission writes its note as the application. A
// body that should defer to the caller's own grants arms the write — HoldMission
// writes its reason with Enforce(caller), so the Marshal, the Dispatcher, and the
// Flight Lead get three different answers from one method — or reads a decision as
// data, as CompleteMission does before leaving a note. IngestDroidReports takes a
// nested batch, the retrofitted request path.
package rpc

// Client carries application dependencies into RPC method implementations.
type Client struct{}

// NewClient constructs a Client.
func NewClient() *Client {
	return &Client{}
}
