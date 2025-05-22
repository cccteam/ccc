package resources

import (
	"cloud.google.com/go/civil"
	"github.com/cccteam/ccc"
)

// Used to generate Go functions converting types between databases
type (
	IntTo[T any]    = int
	StringTo[T any] = string
	Hidden[T any]   = T
	View[T any]     = struct{}
)

type Stores struct {
	// @primarykey
	ID IntTo[ccc.UUID] `db:"store_id"`
	// @foreignkey(StoreTypes(Id))
	Type               string       `db:"store_type"`
	GrandOpeningDate   civil.Date   `db:"opening_date"`
	ClosingDate        *civil.Date  `db:"closed_date"`
	CharityParticipant *IntTo[bool] `db:"charity_participant"`
	// @check(@self = 'S')
	// @default('S')
	// @hidden
	EconomyType string `db:"-"`
	// @foreignkey(ParentCompanies(Id))
	// @uniqueindex
	ParentCompanyID IntTo[ccc.UUID] `db:"parent_id"`
} /*
	@foreignkey(Id, EconomyType) (Economies(Id, Type))
	@uniqueindex(Id, Type)
*/

type Customers struct {
	// @primarykey
	ID  IntTo[ccc.UUID] `db:"store_id"`
	Ssn string          `db"ssn"`
}

type (
	// @view
	StoreByPerson struct {
		StoreID  View[Stores] // @using(ID)
		Type     View[Stores]
		PersonID View[Customers] // @using(ID)
		Ssn      View[Customers]
	} /* @query(
	FROM Stores
	LEFT JOIN StorePurchasers ON StorePurchasers.Id = Stores.Id
	LEFT JOIN Customers ON Customers.Id = StorePurchasers.CustomerId
	) */
)
