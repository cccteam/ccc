package resources

import (
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource/generation/conversion"
)

type (
	Stores struct {
		// @primarykey
		ID conversion.IntTo[ccc.UUID] `db:"store_id"`
		// @foreignkey (StoreTypes(Id))
		Type               string                  `db:"store_type"`
		CharityParticipant *conversion.IntTo[bool] `db:"charity_participant"`

		// @check (@self = 'S')
		// @default ('S')
		EconomyType conversion.Hidden[string] `db:"-"`

		// @foreignkey (ParentCompanies(Id))
		// @uniqueindex
		ParentCompanyID conversion.IntTo[ccc.UUID] `db:"parent_id"`
	} /*
		@foreignkey (Id, EconomyType) (Economies(Id, Type))
		@uniqueindex (Id, Type)
	*/
)

type (
	Customers struct {
		// @primarykey
		ID  conversion.IntTo[ccc.UUID] `db:"store_id"`
		Ssn string                     `db"ssn"`
	}
)

type (
	// @view
	StoreByPerson struct {
		StoreID  conversion.View[Stores] // @using (ID)
		Type     conversion.View[Stores]
		PersonID conversion.View[Customers] // @using (ID)
		Ssn      conversion.View[Customers]
	} /*
		@query(
			FROM Stores
			LEFT JOIN StorePurchasers ON StorePurchasers.Id = Stores.Id
			LEFT JOIN Customers ON Customers.Id = StorePurchasers.CustomerId
		)
	*/
)
