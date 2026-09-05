package resources

import (
	"github.com/cccteam/ccc"
	"github.com/shopspring/decimal"
)

type (
	// Pilot is the personnel registry and the FIRST subject-value anchor, carrying
	// TWO bindings on one unique-indexed user column: `subject.clearance` is the
	// pilot's hazard clearance (`hazard <= subject.clearance`) and `subject.feeLimit`
	// the fee they may book without sign-off (`new.fee <= subject.feeLimit`). The
	// database enforces one row per user; a user with no Pilot row fails such
	// conditions closed.
	//
	// @resource
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
