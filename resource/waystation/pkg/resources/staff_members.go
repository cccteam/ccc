package resources

import (
	"github.com/cccteam/ccc"
	"github.com/shopspring/decimal"
)

type (
	// StaffMember is the personnel registry and the subject-value anchor: UserId is
	// unique-indexed (the database enforces one row per user), and the
	// @subjectValue binding lets grant conditions compare row attributes against
	// the requesting user's approval limit (`totalCost <= subject.approvalLimit`).
	// A user with no StaffMember row fails such conditions closed.
	//
	// @resource
	StaffMember struct {
		ID ccc.UUID `spanner:"Id"`
		// @subjectValue(approvalLimit, value: ApprovalLimit)
		UserID           string          `spanner:"UserId"`
		DisplayName      string          `spanner:"DisplayName"`
		ApprovalLimit    decimal.Decimal `spanner:"ApprovalLimit"`
		HomeWaystationID *string         `spanner:"HomeWaystationId"`
	}
)
