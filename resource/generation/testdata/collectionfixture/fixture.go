// Package collectionfixture provides parsed-struct fixtures for the static permission
// collection computation tests. The structs cover the registration-relevant tag shapes:
// perm-tagged fields, untagged fields, immutable fields, and input-only/output-only
// fields. The constants cover @manualAddResource annotation shapes: doc-comment and
// line-comment placement, an explicit scope, and an unannotated (dormant) constant that
// must contribute nothing.
package collectionfixture

import (
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
)

const (
	// @manualAddResource(Execute)
	ManualThing accesstypes.Resource = "ManualThings"

	// ScopedThing has an ordinary doc comment above its annotation.
	// @manualAddResource(Read, domain)
	ScopedThing accesstypes.Resource = "ScopedThings"

	DormantThing accesstypes.Resource = "DormantThings"
)

// UploadThing carries its annotation in a line comment.
const UploadThing accesstypes.Resource = "UploadThings" // @manualAddResource(Execute)

type Widget struct {
	ID         ccc.UUID `spanner:"Id"`
	Name       string   `spanner:"Name" perm:"Read,Update"`
	ListedName string   `spanner:"ListedName" perm:"List"`
	Code       string   `spanner:"Code" conditions:"immutable"`
	Secret     string   `spanner:"Secret" conditions:"input_only"`
	Derived    string   `spanner:"Derived" conditions:"output_only"`
}

type Gadget struct {
	ID   ccc.UUID `spanner:"Id"`
	Name string   `spanner:"Name"`
}

type Sprocket struct {
	ID   ccc.UUID `spanner:"Id"`
	Name string   `spanner:"Name" perm:"Update"`
}

type Summary struct {
	ID    ccc.UUID `spanner:"Id"`
	Total int64    `spanner:"Total"`
}

type Relic struct {
	ID   ccc.UUID `spanner:"Id"`
	Name string   `spanner:"Name"`
}

type (
	// Ledger's handlers are hand-written; the permission Sets they register are
	// declared manually.
	// @suppress(allHandlers)
	// @manualAddResourceSet(listHandler, readHandler)
	Ledger struct {
		ID    ccc.UUID `spanner:"Id"`
		Total int64    `spanner:"Total" perm:"Read"`
	}

	// Vault's registrations all use the domain scope.
	// @suppress(allHandlers)
	// @manualAddResourceSet(listHandler)
	// @permissionScope(domain)
	Vault struct {
		ID   ccc.UUID `spanner:"Id"`
		Name string   `spanner:"Name"`
	}
)

type Fossil struct {
	ID   ccc.UUID `spanner:"Id"`
	Name string   `spanner:"Name"`
}

type DoSomething struct {
	Input string
}

type HiddenMethod struct {
	Input string
}
