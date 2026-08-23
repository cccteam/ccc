package resources

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/httpio"
)

type (
	// SupplyCrate exercises the query- and field-shaping features that the other
	// resources leave untouched: Label is filterable because the schema indexes it,
	// Quantity and InspectorBadge are filterable via allow_filter, Status and Barcode
	// are server-populated on create, Notes is accepted on mutations but never
	// returned, Barcode is returned but never accepted, and InspectorBadge is PII.
	// It is also the only resource with change tracking enabled (see Config below).
	//
	// @resource
	// @validateCreateType(SupplyCrateCreateValidator)
	SupplyCrate struct {
		ID             ccc.UUID     `spanner:"Id"`
		Label          string       `spanner:"Label"`
		Quantity       int64        `spanner:"Quantity"       allow_filter:"true"`
		Priority       int64        `spanner:"Priority"`
		Status         string       `spanner:"Status"         default_create_fn:"defaultStatus"`
		Barcode        string       `spanner:"Barcode"        conditions:"output_only"          default_create_fn:"defaultBarcode"`
		Notes          *string      `spanner:"Notes"          conditions:"input_only"`
		InspectorBadge *string      `spanner:"InspectorBadge" allow_filter:"true"               conditions:"pii"`
		AssignedShipID ccc.NullUUID `spanner:"AssignedShipId"`
	}
)

// Config overrides the generated DefaultConfig: SupplyCrates is the one resource with
// change tracking enabled, so mutations write DataChangeEvents rows in the same
// transaction. The other resources keep tracking off via defaultConfig.
func (SupplyCrate) Config() resource.Config {
	return defaultConfig().SetTrackChanges(true)
}

// Server-populated create defaults, referenced from the default_create_fn tags above.
// They run only when the client did not supply the field; a client-supplied Status
// wins, while Barcode is output_only so its default always runs.
var (
	defaultStatus  = resource.DefaultString("provisioned")
	defaultBarcode = resource.DefaultString("BC-UNASSIGNED")
)

// SupplyCrateCreateValidator is wired in by the @validateCreateType annotation; the
// generated create patch calls Validate inside the mutation transaction.
type SupplyCrateCreateValidator struct{}

// Validate rejects creates with a non-positive quantity.
func (v *SupplyCrateCreateValidator) Validate(_ context.Context, _ resource.ReadWriteTransaction, p *SupplyCrateCreatePatch) error {
	if p.QuantityIsSet() && p.Quantity() <= 0 {
		return httpio.NewBadRequestMessagef("quantity must be positive, got %d", p.Quantity())
	}

	return nil
}
