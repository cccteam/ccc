package resources

import (
	"github.com/cccteam/ccc"
)

type (
	// CrewMember is a fully tagged resource (see Ship). ClearanceLevel and MedicalNotes
	// exist to model naturally restricted fields; MedicalNotes is additionally PII.
	//
	// @resource
	CrewMember struct {
		ID             ccc.UUID `spanner:"Id"`
		ShipID         ccc.UUID `spanner:"ShipId"         perm:"Read,List,Create,Update"`
		Name           string   `spanner:"Name"           perm:"Read,List,Create,Update"`
		Rank           string   `spanner:"Rank"           perm:"Read,List,Create,Update"`
		ClearanceLevel int64    `spanner:"ClearanceLevel" perm:"Read,List,Create,Update"`
		MedicalNotes   *string  `spanner:"MedicalNotes"   conditions:"pii"               perm:"Read,List,Create,Update"`
	}
)
