package resource

// The semantic differential's fixture: a hand-written checked resource with
// every comparison type in a NOT NULL and a nullable column, two join paths,
// a partitioned anchor for subject sets and a global anchor for subject
// values, and the hand-built collection declaring their bindings — the way
// the package's rendering tests build theirs. The schema is the SQL fixture
// under testdata/semantic/schema.

import (
	"math/big"
	"reflect"
	"testing"
	"time"

	"cloud.google.com/go/civil"
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/accesstypes/condition/conditiontest"
	"github.com/shopspring/decimal"
)

const semanticResource = accesstypes.Resource("Parcels")

// semanticParcel is the checked resource: the row as the database stores it.
type semanticParcel struct {
	ID          ccc.UUID            `spanner:"Id"`
	Depot       string              `spanner:"Depot"`
	Label       string              `spanner:"Label"`
	Note        *string             `spanner:"Note"`
	Weight      int64               `spanner:"Weight"`
	Pieces      *int64              `spanner:"Pieces"`
	Ratio       float64             `spanner:"Ratio"`
	Density     *float64            `spanner:"Density"`
	Price       decimal.Decimal     `spanner:"Price"`
	Fee         decimal.NullDecimal `spanner:"Fee"`
	Fragile     bool                `spanner:"Fragile"`
	Insured     *bool               `spanner:"Insured"`
	ShippedAt   time.Time           `spanner:"ShippedAt"`
	DeliveredAt *time.Time          `spanner:"DeliveredAt"`
	ShipDate    civil.Date          `spanner:"ShipDate"`
	DueDate     *civil.Date         `spanner:"DueDate"`
	CarrierID   *string             `spanner:"CarrierId"`
	RouteID     *string             `spanner:"RouteId"`
}

func (semanticParcel) Resource() accesstypes.Resource { return semanticResource }

func (semanticParcel) DefaultConfig() Config { return Config{} }

// semanticListRequest is the read shape: the key exempt, the tenant key
// wire-closed, every other column indexed so a filter may name it alone.
type semanticListRequest struct {
	ID          ccc.UUID            `json:"id"          perm:"-"`
	Depot       string              `json:"-"`
	Label       string              `json:"label"       index:"true"`
	Note        *string             `json:"note"        index:"true"`
	Weight      int64               `json:"weight"      index:"true"`
	Pieces      *int64              `json:"pieces"      index:"true"`
	Ratio       float64             `json:"ratio"       index:"true"`
	Density     *float64            `json:"density"     index:"true"`
	Price       decimal.Decimal     `json:"price"       index:"true"`
	Fee         decimal.NullDecimal `json:"fee"         index:"true"`
	Fragile     bool                `json:"fragile"     index:"true"`
	Insured     *bool               `json:"insured"     index:"true"`
	ShippedAt   time.Time           `json:"shippedAt"   index:"true"`
	DeliveredAt *time.Time          `json:"deliveredAt" index:"true"`
	ShipDate    civil.Date          `json:"shipDate"    index:"true"`
	DueDate     *civil.Date         `json:"dueDate"     index:"true"`
	CarrierID   *string             `json:"carrierId"   index:"true"`
	RouteID     *string             `json:"routeId"     index:"true"`
}

// semanticPatchRequest is the write shape: the tenant key wire-closed.
type semanticPatchRequest struct {
	Depot       string              `json:"-"`
	Label       string              `json:"label"`
	Note        *string             `json:"note"`
	Weight      int64               `json:"weight"`
	Pieces      *int64              `json:"pieces"`
	Ratio       float64             `json:"ratio"`
	Density     *float64            `json:"density"`
	Price       decimal.Decimal     `json:"price"`
	Fee         decimal.NullDecimal `json:"fee"`
	Fragile     bool                `json:"fragile"`
	Insured     *bool               `json:"insured"`
	ShippedAt   time.Time           `json:"shippedAt"`
	DeliveredAt *time.Time          `json:"deliveredAt"`
	ShipDate    civil.Date          `json:"shipDate"`
	DueDate     *civil.Date         `json:"dueDate"`
	CarrierID   *string             `json:"carrierId"`
	RouteID     *string             `json:"routeId"`
}

// semanticField is one projected grant-bearing field with its wire name, in
// declaration order.
type semanticField struct {
	field accesstypes.Field
	json  string
}

var semanticFields = []semanticField{
	{field: "Label", json: "label"},
	{field: "Note", json: "note"},
	{field: "Weight", json: "weight"},
	{field: "Pieces", json: "pieces"},
	{field: "Ratio", json: "ratio"},
	{field: "Density", json: "density"},
	{field: "Price", json: "price"},
	{field: "Fee", json: "fee"},
	{field: "Fragile", json: "fragile"},
	{field: "Insured", json: "insured"},
	{field: "ShippedAt", json: "shippedAt"},
	{field: "DeliveredAt", json: "deliveredAt"},
	{field: "ShipDate", json: "shipDate"},
	{field: "DueDate", json: "dueDate"},
	{field: "CarrierID", json: "carrierId"},
	{field: "RouteID", json: "routeId"},
}

// semanticAttribute is one column attribute: its binding name, the Go field
// it reads, and its comparison type.
type semanticAttribute struct {
	name  string
	field accesstypes.Field
	typ   AttributeType
}

// semanticColumnAttributes are the checked resource's column attributes; the
// two join-path attributes (carrierCode, hubRegion) resolve through the
// world's related rows.
var semanticColumnAttributes = []semanticAttribute{
	{name: "label", field: "Label", typ: AttributeTypeString},
	{name: "note", field: "Note", typ: AttributeTypeString},
	{name: "weight", field: "Weight", typ: AttributeTypeNumber},
	{name: "pieces", field: "Pieces", typ: AttributeTypeNumber},
	{name: "ratio", field: "Ratio", typ: AttributeTypeNumber},
	{name: "density", field: "Density", typ: AttributeTypeNumber},
	{name: "price", field: "Price", typ: AttributeTypeNumber},
	{name: "fee", field: "Fee", typ: AttributeTypeNumber},
	{name: "fragile", field: "Fragile", typ: AttributeTypeBool},
	{name: "insured", field: "Insured", typ: AttributeTypeBool},
	{name: "shippedAt", field: "ShippedAt", typ: AttributeTypeTimestamp},
	{name: "deliveredAt", field: "DeliveredAt", typ: AttributeTypeTimestamp},
	{name: "shipDate", field: "ShipDate", typ: AttributeTypeDate},
	{name: "dueDate", field: "DueDate", typ: AttributeTypeDate},
}

// semanticCollection declares the fixture's vocabulary: every column attribute
// typed, the two join paths, the domain binding, the partitioned anchor's
// subject sets (one dotted), and the global anchor's typed subject values (one
// dotted).
func semanticCollection(t *testing.T) *GeneratedCollection {
	t.Helper()

	attributes := make([]AttributeData, 0, len(semanticColumnAttributes)+2)
	for _, attr := range semanticColumnAttributes {
		attributes = append(attributes, AttributeData{Name: attr.name, Column: string(attr.field), Type: attr.typ})
	}
	attributes = append(attributes,
		AttributeData{Name: "carrierCode", Column: "CarrierId", Type: AttributeTypeString, Path: []BindingHop{{Table: "Carriers", JoinColumn: "Id", Column: "Code"}}},
		AttributeData{Name: "hubRegion", Column: "RouteId", Type: AttributeTypeString, Path: []BindingHop{
			{Table: "Routes", JoinColumn: "Id", Column: "HubId"},
			{Table: "Hubs", JoinColumn: "Id", Column: "Region"},
		}},
	)

	g, err := NewGeneratedCollection(CollectionData{Resources: []CollectionResource{
		{
			Name:        semanticResource,
			Scope:       accesstypes.DomainPermissionScope,
			Permissions: []accesstypes.Permission{accesstypes.List, accesstypes.Read, accesstypes.Create, accesstypes.Update, accesstypes.Delete},
			Attributes:  attributes,
			Domain:      &DomainBindingData{Column: "Depot"},
		},
		{
			Name:  "Memberships",
			Scope: accesstypes.DomainPermissionScope,
			SubjectSets: []SubjectBindingData{
				{Name: "teams", UserColumn: "UserId", Column: "Team", Type: AttributeTypeString},
				{Name: "tiers", UserColumn: "UserId", Column: "Tier", Type: AttributeTypeNumber},
				{Name: "regions", UserColumn: "UserId", Column: "HubId", Type: AttributeTypeString, Path: []BindingHop{{Table: "Hubs", JoinColumn: "Id", Column: "Region"}}},
			},
			Domain: &DomainBindingData{Column: "Depot"},
		},
		{
			Name:  "Profiles",
			Scope: accesstypes.GlobalPermissionScope,
			SubjectValues: []SubjectBindingData{
				{Name: "nickname", UserColumn: "UserId", Column: "Nickname", Type: AttributeTypeString},
				{Name: "quota", UserColumn: "UserId", Column: "Quota", Type: AttributeTypeNumber},
				{Name: "rate", UserColumn: "UserId", Column: "Rate", Type: AttributeTypeNumber},
				{Name: "allowance", UserColumn: "UserId", Column: "Allowance", Type: AttributeTypeNumber},
				{Name: "active", UserColumn: "UserId", Column: "Active", Type: AttributeTypeBool},
				{Name: "clearedUntil", UserColumn: "UserId", Column: "ClearedUntil", Type: AttributeTypeTimestamp},
				{Name: "startDate", UserColumn: "UserId", Column: "StartDate", Type: AttributeTypeDate},
				{Name: "homeRegion", UserColumn: "UserId", Column: "HomeHubId", Type: AttributeTypeString, Path: []BindingHop{{Table: "Hubs", JoinColumn: "Id", Column: "Region"}}},
			},
		},
	}})
	if err != nil {
		t.Fatalf("NewGeneratedCollection() error = %v", err)
	}

	return g
}

// semanticVocabulary is the fixture's vocabulary as the condition generator
// sees it, read from the collection so the generator pairs exactly what
// deploy validation admits; postImage marks a write context.
func semanticVocabulary(t *testing.T, collection *GeneratedCollection, postImage bool) conditiontest.Vocabulary {
	t.Helper()

	bindings, ok := collection.Bindings(accesstypes.DomainPermissionScope, semanticResource)
	if !ok {
		t.Fatal("fixture bindings missing")
	}
	vocab := conditiontest.Vocabulary{PostImage: postImage}
	for _, attr := range bindings.Attributes {
		vocab.Attributes = append(vocab.Attributes, conditiontest.Attribute{Name: attr.Name, Type: attr.Type, JoinPath: len(attr.Path) > 0})
	}
	data := collection.Data()
	for i := range data.Resources {
		for _, set := range data.Resources[i].SubjectSets {
			vocab.SubjectSets = append(vocab.SubjectSets, conditiontest.SubjectBinding{Name: set.Name, Type: set.Type})
		}
		for _, value := range data.Resources[i].SubjectValues {
			vocab.SubjectValues = append(vocab.SubjectValues, conditiontest.SubjectBinding{Name: value.Name, Type: value.Type})
		}
	}

	return vocab
}

// The related tables' rows, as the harness writes them.
type semanticHub struct {
	ID     string  `spanner:"Id"`
	Region *string `spanner:"Region"`
}

type semanticCarrier struct {
	ID   string  `spanner:"Id"`
	Code *string `spanner:"Code"`
}

type semanticRoute struct {
	ID    string  `spanner:"Id"`
	HubID *string `spanner:"HubId"`
}

type semanticMembership struct {
	ID     string  `spanner:"Id"`
	UserID string  `spanner:"UserId"`
	Depot  string  `spanner:"Depot"`
	Team   *string `spanner:"Team"`
	Tier   *int64  `spanner:"Tier"`
	HubID  *string `spanner:"HubId"`
}

type semanticProfile struct {
	UserID       string              `spanner:"UserId"`
	Nickname     *string             `spanner:"Nickname"`
	Quota        *int64              `spanner:"Quota"`
	Rate         *float64            `spanner:"Rate"`
	Allowance    decimal.NullDecimal `spanner:"Allowance"`
	Active       *bool               `spanner:"Active"`
	ClearedUntil *time.Time          `spanner:"ClearedUntil"`
	StartDate    *civil.Date         `spanner:"StartDate"`
	HomeHubID    *string             `spanner:"HomeHubId"`
}

// semanticValue converts a stored Go value to the evaluator's storage
// vocabulary: pointers dereferenced (nil is no value), NUMERIC as *big.Rat,
// a date as conditiontest.Date, a UUID as its text, timestamps in UTC.
func semanticValue(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case *string:
		if x == nil {
			return nil
		}

		return *x
	case *int64:
		if x == nil {
			return nil
		}

		return *x
	case *float64:
		if x == nil {
			return nil
		}

		return *x
	case *bool:
		if x == nil {
			return nil
		}

		return *x
	case *time.Time:
		if x == nil {
			return nil
		}

		return x.UTC()
	case time.Time:
		return x.UTC()
	case *civil.Date:
		if x == nil {
			return nil
		}

		return semanticDate(*x)
	case civil.Date:
		return semanticDate(x)
	case decimal.Decimal:
		return x.Rat()
	case decimal.NullDecimal:
		if !x.Valid {
			return nil
		}

		return x.Decimal.Rat()
	case *big.Rat:
		return x
	case ccc.UUID:
		return x.String()
	case string, int64, float64, bool:
		return x
	default:
		panic("semanticValue: unsupported stored type " + reflect.TypeOf(v).String())
	}
}

func semanticDate(d civil.Date) conditiontest.Date {
	return conditiontest.Date{Year: d.Year, Month: d.Month, Day: d.Day}
}

// semanticFieldValue reads one field of a parcel by name.
func semanticFieldValue(p *semanticParcel, field accesstypes.Field) any {
	return reflect.ValueOf(*p).FieldByName(string(field)).Interface()
}

// compareSemantic orders two evaluator values of one storage type, nil first
// so a caller can place the NULL region itself.
func compareSemantic(a, b any) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return -1
	case b == nil:
		return 1
	}
	switch x := a.(type) {
	case string:
		y, _ := b.(string)

		return cmpOrdered(x, y)
	case int64:
		y, _ := b.(int64)

		return cmpOrdered(x, y)
	case float64:
		y, _ := b.(float64)

		return cmpOrdered(x, y)
	case *big.Rat:
		y, _ := b.(*big.Rat)

		return x.Cmp(y)
	case bool:
		y, _ := b.(bool)

		return cmpOrdered(boolOrder(x), boolOrder(y))
	case time.Time:
		y, _ := b.(time.Time)

		return x.Compare(y)
	case conditiontest.Date:
		y, _ := b.(conditiontest.Date)

		return cmpOrdered(x.String(), y.String())
	default:
		panic("compareSemantic: unsupported value type " + reflect.TypeOf(a).String())
	}
}

func cmpOrdered[T string | int64 | float64 | int](a, b T) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func boolOrder(b bool) int {
	if b {
		return 1
	}

	return 0
}
