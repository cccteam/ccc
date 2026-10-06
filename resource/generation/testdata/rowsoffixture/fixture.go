// Package rowsoffixture holds the structs the struct-scope @rowsOf tests resolve: a
// table with a compound key, the views that carry its rows under that key, the views
// whose keys miss it in each way the check names, and the two kinds that may not
// declare a table at all.
package rowsoffixture

import "github.com/cccteam/ccc"

type (
	// Berth is the table the views carry, keyed by dock and slot.
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

	// Mooring carries Berths' rows under Berths' key, with a column of its own.
	//
	// @virtual
	// @rowsOf(Berths)
	Mooring struct {
		// @primarykey
		DockID ccc.UUID `spanner:"DockId" index:"true"`
		// @primarykey
		Slot     int64  `spanner:"Slot"`
		Name     string `spanner:"Name"`
		DockName string `spanner:"DockName"`
	}

	// Occupancy is a computed view carrying Berths' rows.
	//
	// @computed
	// @rowsOf(Berths)
	Occupancy struct {
		// @primarykey
		DockID ccc.UUID
		// @primarykey
		Slot   int64
		Visits int64
	}

	// Renamed reads the key from columns spelled differently from Berths'.
	//
	// @virtual
	// @rowsOf(Berths)
	Renamed struct {
		// @primarykey
		DockID ccc.UUID `spanner:"Dock"`
		// @primarykey
		Slot int64 `spanner:"Position"`
	}

	// Retyped carries the dock as a string.
	//
	// @virtual
	// @rowsOf(Berths)
	Retyped struct {
		// @primarykey
		DockID string `spanner:"DockId"`
		// @primarykey
		Slot int64 `spanner:"Slot"`
	}

	// Reordered declares the key in the other order.
	//
	// @virtual
	// @rowsOf(Berths)
	Reordered struct {
		// @primarykey
		Slot int64 `spanner:"Slot"`
		// @primarykey
		DockID ccc.UUID `spanner:"DockId"`
	}

	// Misnamed carries the key under other Go field names.
	//
	// @virtual
	// @rowsOf(Berths)
	Misnamed struct {
		// @primarykey
		Dock ccc.UUID `spanner:"DockId"`
		// @primarykey
		Slot int64 `spanner:"Slot"`
	}

	// TableWithRowsOf is a table-backed struct declaring a table, which only a view may.
	//
	// @resource
	// @rowsOf(Berths)
	TableWithRowsOf struct {
		ID ccc.UUID `spanner:"Id"`
	}

	// MethodWithRowsOf is a method declaring a table, which only a view may.
	//
	// @rpc
	// @rowsOf(Berths)
	MethodWithRowsOf struct {
		Input string
	}
)
