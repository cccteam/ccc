package resources

import (
	"github.com/cccteam/ccc"
	"github.com/shopspring/decimal"
)

type (
	// Pilot is the personnel registry, the crew roster the console lists, and the FIRST
	// subject-value anchor, carrying TWO bindings on one unique-indexed user column:
	// subject.clearance is the pilot's hazard clearance (hazard <= subject.clearance) and
	// subject.feeLimit the fee they may book without sign-off (new.fee <=
	// subject.feeLimit). The database enforces one row per user; a user with no Pilot row
	// fails such conditions closed.
	//
	// The roster declares a default page but no maximum, so limit=all is legal on it and
	// the client's all() has a home.
	//
	// Demonstrates: @subjectValue, @subjectValue.two-per-anchor, paging.limit-all.
	//
	// @resource
	// @order(DisplayName asc)
	// @page(default: 10)
	Pilot struct {
		ID ccc.UUID `spanner:"Id"`
		// @subjectValue(clearance, value: Clearance)
		// @subjectValue(feeLimit, value: FeeLimit)
		UserID      string          `spanner:"UserId"`
		DisplayName string          `spanner:"DisplayName"`
		Clearance   int64           `spanner:"Clearance"`
		FeeLimit    decimal.Decimal `spanner:"FeeLimit"`
	}
)
