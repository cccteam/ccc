// Package pagingfixture provides parsed-struct fixtures for the computed-resource
// query rules: the tags a computed field may carry, and the ones refused with the
// field named.
package pagingfixture

import (
	"context"
	"iter"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
)

type (
	// Board carries the well-formed shapes: filterable leaf fields of each comparable
	// type, a nested field with no query tags, and a declared order.
	//
	// @computed
	// @order(Worst desc)
	Board struct {
		ID        ccc.UUID  // @primarykey
		ShipName  string    `allow_filter:"true"`
		Worst     float64   `allow_filter:"true"`
		Recorded  time.Time `allow_filter:"true"`
		Note      *string   `allow_filter:"true"`
		Readings  []Reading
		Subsystem string
	}

	Reading struct {
		Value float64
	}

	// IndexedBoard names a database index, which a computed resource has none of.
	//
	// @computed
	IndexedBoard struct {
		ID   ccc.UUID `index:"true"` // @primarykey
		Name string
	}

	// ListFilterBoard makes a list field filterable; the evaluator compares single values.
	//
	// @computed
	ListFilterBoard struct {
		ID   ccc.UUID // @primarykey
		Tags []string `allow_filter:"true"`
	}

	// NestedOrderBoard orders by a nested field, which is opaque.
	//
	// @computed
	// @order(Readings asc)
	NestedOrderBoard struct {
		ID       ccc.UUID // @primarykey
		Readings []Reading
	}

	// EnumeratedBoard declares a picker on a leaf field.
	//
	// @computed
	EnumeratedBoard struct {
		ID      ccc.UUID // @primarykey
		BoardID ccc.UUID // @enumerate(Boards)
	}

	// EnumeratedNestedBoard declares a picker on a nested field, which is opaque.
	//
	// @computed
	EnumeratedNestedBoard struct {
		ID       ccc.UUID  // @primarykey
		Readings []Reading // @enumerate(Boards)
	}
)

func (Board) Resource() accesstypes.Resource            { return "Boards" }
func (IndexedBoard) Resource() accesstypes.Resource     { return "IndexedBoards" }
func (ListFilterBoard) Resource() accesstypes.Resource  { return "ListFilterBoards" }
func (NestedOrderBoard) Resource() accesstypes.Resource { return "NestedOrderBoards" }
func (EnumeratedBoard) Resource() accesstypes.Resource  { return "EnumeratedBoards" }
func (EnumeratedNestedBoard) Resource() accesstypes.Resource {
	return "EnumeratedNestedBoards"
}

type Client struct{}

func ListBoard(context.Context, *resource.QuerySet[Board], resource.Client, *Client) iter.Seq2[*Board, error] {
	return nil
}

func ReadBoard(context.Context, ccc.UUID, *resource.QuerySet[Board], resource.Client, *Client) (*Board, error) {
	return nil, nil
}

func ListIndexedBoard(context.Context, *resource.QuerySet[IndexedBoard], resource.Client, *Client) iter.Seq2[*IndexedBoard, error] {
	return nil
}

func ReadIndexedBoard(context.Context, ccc.UUID, *resource.QuerySet[IndexedBoard], resource.Client, *Client) (*IndexedBoard, error) {
	return nil, nil
}

func ListListFilterBoard(context.Context, *resource.QuerySet[ListFilterBoard], resource.Client, *Client) iter.Seq2[*ListFilterBoard, error] {
	return nil
}

func ReadListFilterBoard(context.Context, ccc.UUID, *resource.QuerySet[ListFilterBoard], resource.Client, *Client) (*ListFilterBoard, error) {
	return nil, nil
}

func ListNestedOrderBoard(context.Context, *resource.QuerySet[NestedOrderBoard], resource.Client, *Client) iter.Seq2[*NestedOrderBoard, error] {
	return nil
}

func ReadNestedOrderBoard(context.Context, ccc.UUID, *resource.QuerySet[NestedOrderBoard], resource.Client, *Client) (*NestedOrderBoard, error) {
	return nil, nil
}

func ListEnumeratedBoard(context.Context, *resource.QuerySet[EnumeratedBoard], resource.Client, *Client) iter.Seq2[*EnumeratedBoard, error] {
	return nil
}

func ReadEnumeratedBoard(context.Context, ccc.UUID, *resource.QuerySet[EnumeratedBoard], resource.Client, *Client) (*EnumeratedBoard, error) {
	return nil, nil
}

func ListEnumeratedNestedBoard(context.Context, *resource.QuerySet[EnumeratedNestedBoard], resource.Client, *Client) iter.Seq2[*EnumeratedNestedBoard, error] {
	return nil
}

func ReadEnumeratedNestedBoard(context.Context, ccc.UUID, *resource.QuerySet[EnumeratedNestedBoard], resource.Client, *Client) (*EnumeratedNestedBoard, error) {
	return nil, nil
}
