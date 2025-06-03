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

type StoreTypes struct {
	// @primarykey
	ID          string `db:"store_type_code"`
	Description string `db:"store_type_desc"`
} // @uniqueindex(Id, Description)

type ParentCompanies struct {
	// @primarykey
	ID IntTo[ccc.UUID] `db:"store_id"`
}

type Economies struct {
	// @primarykey
	ID   IntTo[ccc.UUID] `db:"econ_type"`
	Type string          `db:"-"`
}

type Stores struct {
	// @primarykey
	ID IntTo[ccc.UUID] `db:"store_id"`
	// @substring(@self)
	Name string `db:"store_name"`
	// @substring(@self)
	Address string `db:"store_addy"`
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

func (s *Stores) EconomyTypeConversion() string {
	switch s.EconomyType {
	case "A", "B", "C":
		return "Thriving"
	case "D":
		return "Elementary"
	default:
		return "Recession"
	}
}

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
