// Package changedbyfixture holds the structs the query derivation and @changedBy tests
// resolve: virtual resources whose queries are an embedded file, a literal, a
// concatenation, and a run-time string, with and without a declaration, computed
// resources with and without one, and a table-backed struct that may not declare.
package changedbyfixture

import (
	_ "embed"
	"fmt"

	"github.com/cccteam/ccc"
)

//go:embed rosters.sql
var rostersSubquery string

// literalSubquery is a query the compiler knows whole.
const literalSubquery = "SELECT d.Id, d.Name FROM Docks d"

// visitsSubquery is a concatenation of constants, which the compiler also knows whole.
const visitsSubquery = "SELECT v.Id, v.BerthDockId FROM Visits v" + " JOIN Berths b ON b.DockId = v.BerthDockId"

type (
	// Berth is the table the roster carries, keyed by dock and slot.
	Berth struct {
		DockID ccc.UUID `spanner:"DockId"`
		Slot   int64    `spanner:"Slot"`
		Name   string   `spanner:"Name"`
	}

	// Dock is a table keyed by one id.
	Dock struct {
		ID   ccc.UUID `spanner:"Id"`
		Name string   `spanner:"Name"`
	}

	// Visit is a table the computed resources read.
	Visit struct {
		ID          ccc.UUID `spanner:"Id"`
		BerthDockID ccc.UUID `spanner:"BerthDockId"`
	}

	// Plain is a table-backed resource, which may not declare what changes it.
	//
	// @changedBy(Docks)
	Plain struct {
		ID ccc.UUID `spanner:"Id"`
	}

	// Roster reads an embedded file over Berths and Docks and carries Berths' rows.
	//
	// @virtual
	// @rowsOf(Berths)
	Roster struct {
		// @primarykey
		DockID ccc.UUID `spanner:"DockId" index:"true"`
		// @primarykey
		Slot     int64  `spanner:"Slot"`
		Name     string `spanner:"Name"`
		DockName string `spanner:"DockName"`
	}

	// Literal reads a string literal over Docks and declares nothing.
	//
	// @virtual
	Literal struct {
		// @primarykey
		ID   ccc.UUID `spanner:"Id" index:"true"`
		Name string   `spanner:"Name"`
	}

	// Joined reads a concatenation of constants over Visits and Berths.
	//
	// @virtual
	Joined struct {
		// @primarykey
		ID          ccc.UUID `spanner:"Id" index:"true"`
		BerthDockID ccc.UUID `spanner:"BerthDockId"`
	}

	// RunTime builds its query when asked and declares nothing: refused.
	//
	// @virtual
	RunTime struct {
		// @primarykey
		ID ccc.UUID `spanner:"Id" index:"true"`
	}

	// Declared builds its query when asked and declares what it reads.
	//
	// @virtual
	// @changedBy(Visits, Berths)
	Declared struct {
		// @primarykey
		ID ccc.UUID `spanner:"Id" index:"true"`
	}

	// Rootless carries Berths' rows by declaration but its query names Docks alone:
	// refused.
	//
	// @virtual
	// @rowsOf(Berths)
	Rootless struct {
		// @primarykey
		DockID ccc.UUID `spanner:"DockId" index:"true"`
		// @primarykey
		Slot int64 `spanner:"Slot"`
	}

	// Unknown declares a table the schema does not have: refused.
	//
	// @virtual
	// @changedBy(Nowhere)
	Unknown struct {
		// @primarykey
		ID ccc.UUID `spanner:"Id" index:"true"`
	}

	// Extra reads the embedded file and declares a table beyond it.
	//
	// @virtual
	// @rowsOf(Berths)
	// @changedBy(Visits)
	Extra struct {
		// @primarykey
		DockID ccc.UUID `spanner:"DockId" index:"true"`
		// @primarykey
		Slot int64 `spanner:"Slot"`
	}

	// Tally is a computed resource that declares the tables its body reads.
	//
	// @computed
	// @changedBy(Visits, Docks)
	Tally struct {
		// @primarykey
		DockID ccc.UUID
		Visits int64
	}

	// Silent is a computed resource that declares nothing: not published.
	//
	// @computed
	Silent struct {
		// @primarykey
		DockID ccc.UUID
		Visits int64
	}

	// Partial is a computed resource carrying Berths' rows whose declaration leaves
	// Berths out: refused.
	//
	// @computed
	// @rowsOf(Berths)
	// @changedBy(Visits)
	Partial struct {
		// @primarykey
		DockID ccc.UUID
		// @primarykey
		Slot   int64
		Visits int64
	}
)

// Subquery provides the embedded SQL projection backing the roster.
func (Roster) Subquery() (query string, params map[string]any) {
	return rostersSubquery, map[string]any{"minSlot": 0}
}

// Subquery provides the literal projection.
func (Literal) Subquery() (query string, params map[string]any) {
	return literalSubquery, nil
}

// Subquery provides the concatenated projection.
func (Joined) Subquery() (query string, params map[string]any) {
	return (visitsSubquery), nil
}

// Subquery builds its projection when asked.
func (RunTime) Subquery() (query string, params map[string]any) {
	return fmt.Sprintf("SELECT Id FROM %s", "Docks"), nil
}

// Subquery builds its projection when asked.
func (Declared) Subquery() (query string, params map[string]any) {
	return fmt.Sprintf("SELECT Id FROM %s", "Visits"), nil
}

// Subquery provides a projection over Docks alone.
func (Rootless) Subquery() (query string, params map[string]any) {
	return literalSubquery, nil
}

// Subquery provides a projection over Docks.
func (Unknown) Subquery() (query string, params map[string]any) {
	return literalSubquery, nil
}

// Subquery provides the embedded roster projection.
func (Extra) Subquery() (query string, params map[string]any) {
	return rostersSubquery, nil
}
