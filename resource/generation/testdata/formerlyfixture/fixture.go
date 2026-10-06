// Package formerlyfixture holds the structs the @formerly tests resolve: a resource
// whose field moved to a new wire name, a method renamed with a request field and a
// result field renamed beside it, and the declarations each rule refuses.
package formerlyfixture

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
)

// Client stands in for the application's RPC client.
type Client struct{}

type (
	// Article moved Title to Headline.
	Article struct {
		ID ccc.UUID `spanner:"Id"`
		// @formerly(Title)
		Headline string `spanner:"Headline"`
		Body     string `spanner:"Body"`
	}

	// SameName names the field's current wire name as its former one, which is refused.
	SameName struct {
		ID ccc.UUID `spanner:"Id"`
		// @formerly(headline)
		Headline string `spanner:"Headline"`
	}

	// Clash names another field's wire name as a former one, which is refused.
	Clash struct {
		ID ccc.UUID `spanner:"Id"`
		// @formerly(Body)
		Headline string `spanner:"Headline"`
		Body     string `spanner:"Body"`
	}

	// TwoFormer names one former name on two fields, which is refused.
	TwoFormer struct {
		ID ccc.UUID `spanner:"Id"`
		// @formerly(Title)
		Headline string `spanner:"Headline"`
		// @formerly(Title)
		Body string `spanner:"Body"`
	}

	// Renamed carries the struct form on a resource, which is refused: a resource keeps
	// its name.
	// @formerly(Story)
	Renamed struct {
		ID ccc.UUID `spanner:"Id"`
	}

	// Publish was Release; its Headline was Title, and so was its result's.
	// @rpc
	// @formerly(Release)
	Publish struct {
		ID ccc.UUID
		// @formerly(Title)
		Headline string
	}

	// Published is what Publish answers with.
	Published struct {
		ID ccc.UUID
		// @formerly(Title)
		Headline string
		Body     string
	}

	// Withdraw names its own name as its former one, which is refused.
	// @rpc
	// @formerly(Withdraw)
	Withdraw struct {
		ID ccc.UUID
	}

	// Retract names another method as its former name, which is refused.
	// @rpc
	// @formerly(Publish)
	Retract struct {
		ID ccc.UUID
	}

	// Reword's request names another request field's wire name as a former one, which
	// is refused.
	// @rpc
	Reword struct {
		ID ccc.UUID
		// @formerly(Body)
		Headline string
		Body     string
	}

	// Restate answers with a result whose former name clashes with its own field.
	// @rpc
	Restate struct {
		ID ccc.UUID
	}

	// Restated is what Restate answers with: its Headline names Body, another field.
	Restated struct {
		// @formerly(Body)
		Headline string
		Body     string
	}
)

// Execute answers with the published article.
func (*Publish) Execute(context.Context, resource.ReadWriteTransaction, *Client) (*Published, error) {
	return nil, nil
}

// Execute withdraws.
func (*Withdraw) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}

// Execute retracts.
func (*Retract) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}

// Execute rewords.
func (*Reword) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}

// Execute answers with the restated article.
func (*Restate) Execute(context.Context, resource.ReadWriteTransaction, *Client) (*Restated, error) {
	return nil, nil
}
