package resources

import (
	"github.com/cccteam/ccc"
)

type (
	// CrewMember is structurally enforced (see Ship). ClearanceLevel and MedicalNotes
	// exist to model naturally restricted fields; MedicalNotes is additionally PII.
	//
	// @resource
	CrewMember struct {
		ID             ccc.UUID `spanner:"Id"`
		ShipID         ccc.UUID `spanner:"ShipId"`
		Name           string   `spanner:"Name"`
		Rank           string   `spanner:"Rank"`
		ClearanceLevel int64    `spanner:"ClearanceLevel"`
		MedicalNotes   *string  `spanner:"MedicalNotes"   conditions:"pii"`
	}
)
