// Package wirefixture holds one struct per shape the wire walker accepts or
// refuses: the flat request that converts whole, the nested report that needs
// mirrors and pinned views at three levels, and each departure from the rules.
package wirefixture

import (
	"context"
	"fmt"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/testdata/wirefixture/other"
)

// Execute answers with a pointer to the nested report.
func (*Inspect) Execute(context.Context, resource.ReadWriteTransaction, *Client) (*Report, error) {
	return nil, nil
}

// Execute runs outside a transaction.
func (*Notify) Execute(context.Context, resource.Client, *Client) error { return nil }

// Client stands in for the application's RPC client.
type Client struct{}

// Code is a named basic type: a leaf typed by its underlying string.
type Code string

// UUIDs is a named slice type, which the walker refuses.
type UUIDs []ccc.UUID

// Time is a struct whose mirror name would shadow the time package.
type Time struct {
	X int
}

// Request is a struct whose mirror name is the handler's request mirror.
type Request struct {
	X int
}

type (
	// Flat has only leaves: it converts whole and needs no mirror behind it.
	Flat struct {
		ID    ccc.UUID
		Name  string
		Count int
		Tags  []string
		When  *time.Time
		Code  Code
	}

	// Inspect is a method answering with the nested Report; its request is flat.
	Inspect struct {
		ID ccc.UUID
	}

	// Notify is a client-form method: it runs outside a transaction.
	Notify struct {
		ID ccc.UUID
	}

	// Report nests three levels deep with every pointer and slice marker.
	Report struct {
		ID      ccc.UUID
		Ship    Ship
		Primary *Reading
		Notes   []string
	}
	Ship struct {
		Name    string
		Systems []System
	}
	System struct {
		Name     string
		Readings []*Reading
		Latest   *Reading
	}
	Reading struct {
		Value float64
		At    time.Time
	}

	// Shared reaches Reading from two fields; the mirror is declared once.
	Shared struct {
		First  Reading
		Second []Reading
	}

	Recursive struct {
		Child *Recursive
	}
	Mutual struct {
		Other Other
	}
	Other struct {
		Back *Mutual
	}
	WithMap struct {
		M map[string]string
	}
	WithAny struct {
		A any
	}
	WithInterface struct {
		I fmt.Stringer
	}
	SliceOfSlices struct {
		S [][]string
	}
	PointerToSlice struct {
		P *[]string
	}
	Embedded struct {
		Reading
	}
	Unexported struct {
		hidden string
	}
	AnonStruct struct {
		A struct{ X int }
	}
	ArrayField struct {
		A [3]int
	}
	NamedSlice struct {
		IDs UUIDs
	}
	Collision struct {
		A Reading
		B other.Reading
	}
	ShadowsPackage struct {
		T  Time
		At time.Time
	}
	ShadowsHandler struct {
		R Request
	}
	LocalCollision struct {
		View []Reading
	}
	BoolPointer struct {
		Flag *bool
	}

	// Board is a computed row with an opaque nested field beside its flat ones.
	Board struct {
		ID     ccc.UUID `spanner:"Id"`
		Name   string   `spanner:"Name" allow_filter:"true"`
		Recent []Reading
	}
	// BoardFiltered puts a filter tag on the nested field.
	BoardFiltered struct {
		ID     ccc.UUID
		Recent []Reading `allow_filter:"true"`
	}
	// BoardInnerTag nests a struct whose field carries a PII tag.
	BoardInnerTag struct {
		ID     ccc.UUID
		Recent []Tagged
	}
	Tagged struct {
		Secret string `conditions:"pii"`
	}
)
