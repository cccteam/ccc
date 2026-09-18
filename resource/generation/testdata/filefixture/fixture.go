// Package filefixture holds the structs the @file tests resolve: stored files declared
// on table, view, and computed structs in every accepted shape, the declarations each
// rule refuses, and the content functions a struct-scope @file names, right and wrong.
package filefixture

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
)

// Client is the computed package's client, as the content functions take it.
type Client struct{}

type (
	// Document stores one file under the default segment, with its name and type
	// columns named, and a second file under its own segment with a nullable key.
	Document struct {
		ID          ccc.UUID `spanner:"Id"`
		Title       string   `spanner:"Title"`
		FileName    string   `spanner:"FileName"`
		ContentType string   `spanner:"ContentType"`
		// @file(name: FileName, type: ContentType)
		StoreKey string `spanner:"StoreKey"`
		// @file(thumbnail)
		ThumbKey *string `spanner:"ThumbKey"`
	}

	// Logo's one file has a nullable key and no name or type column.
	Logo struct {
		ID ccc.UUID `spanner:"Id"`
		// @file
		StoreKey *string `spanner:"StoreKey"`
	}

	// BadSibling names a name column the struct does not declare.
	BadSibling struct {
		ID ccc.UUID `spanner:"Id"`
		// @file(name: Missing)
		StoreKey string `spanner:"StoreKey"`
	}

	// BadType declares the file on a column that is not a string.
	BadType struct {
		ID ccc.UUID `spanner:"Id"`
		// @file
		Size int64 `spanner:"Size"`
	}

	// BadSiblingType names a type column that is not a string.
	BadSiblingType struct {
		ID ccc.UUID `spanner:"Id"`
		// @file(type: Size)
		StoreKey string `spanner:"StoreKey"`
		Size     int64  `spanner:"Size"`
	}

	// BadSegment spells its segment as no route segment is spelled.
	BadSegment struct {
		ID ccc.UUID `spanner:"Id"`
		// @file(Content)
		StoreKey string `spanner:"StoreKey"`
	}

	// StructScoped puts the struct-scope form on a table.
	//
	// @file
	StructScoped struct {
		ID       ccc.UUID `spanner:"Id"`
		StoreKey string   `spanner:"StoreKey"`
	}

	// Twice declares the default segment on two columns.
	Twice struct {
		ID ccc.UUID `spanner:"Id"`
		// @file
		FirstKey string `spanner:"FirstKey"`
		// @file
		SecondKey string `spanner:"SecondKey"`
	}

	// OnKey puts the declaration on the primary key.
	OnKey struct {
		// @file
		ID   ccc.UUID `spanner:"Id"`
		Name string   `spanner:"Name"`
	}

	// KeylessView is a view with no @primarykey carrying a file.
	//
	// @virtual
	KeylessView struct {
		Name string `spanner:"Name"`
		// @file
		StoreKey string `spanner:"StoreKey"`
	}

	// KeyedView is a view with a key carrying a file.
	//
	// @virtual
	KeyedView struct {
		// @primarykey
		ID ccc.UUID `spanner:"Id" uniqueindex:"true"`
		// @file
		StoreKey string `spanner:"StoreKey"`
	}

	// Manifest renders its content: the struct-scope form on a keyed computed struct,
	// with ManifestContent declared below.
	//
	// @computed
	// @file
	Manifest struct {
		// @primarykey
		MissionID ccc.UUID
		Lines     int64
	}

	// Statement renders under its own segment, whose function is StatementSheet.
	//
	// @computed
	// @file(sheet)
	Statement struct {
		// @primarykey
		ClientID ccc.UUID
		// @primarykey
		Period string
		Total  int64
	}

	// StoredComputed's row names a stored file: the field-scope form on a computed
	// struct.
	//
	// @computed
	StoredComputed struct {
		// @primarykey
		ID   ccc.UUID
		Name string
		// @file(name: Name)
		StoreKey string
	}

	// KeylessComputed renders a file with no key to address it by.
	//
	// @computed
	// @file
	KeylessComputed struct {
		Lines int64
	}

	// NamedRender names a column on the struct-scope form.
	//
	// @computed
	// @file(name: Name)
	NamedRender struct {
		// @primarykey
		ID   ccc.UUID
		Name string
	}

	// MissingFunction declares a rendered file the package declares no function for.
	//
	// @computed
	// @file
	MissingFunction struct {
		// @primarykey
		ID ccc.UUID
	}

	// WrongKey's function takes the key as a string.
	//
	// @computed
	// @file
	WrongKey struct {
		// @primarykey
		ID ccc.UUID
	}

	// WrongResult's function answers with the content by value.
	//
	// @computed
	// @file
	WrongResult struct {
		// @primarykey
		ID ccc.UUID
	}

	// NoQuerySet's function skips the QuerySet.
	//
	// @computed
	// @file
	NoQuerySet struct {
		// @primarykey
		ID ccc.UUID
	}

	// Attach is an RPC method carrying the annotation it may not.
	//
	// @rpc
	Attach struct {
		// @file
		StoreKey string
	}
)

// The computed structs a QuerySet is instantiated over declare their resource names, as
// every computed resource does.
func (Manifest) Resource() accesstypes.Resource    { return "Manifests" }
func (Statement) Resource() accesstypes.Resource   { return "Statements" }
func (WrongKey) Resource() accesstypes.Resource    { return "WrongKeys" }
func (WrongResult) Resource() accesstypes.Resource { return "WrongResults" }

// ManifestContent is the content function Manifest's @file names, in the shape the
// generated handler calls.
func ManifestContent(_ context.Context, _ ccc.UUID, _ *resource.QuerySet[Manifest], _ resource.Client, _ *Client) (*resource.Content, error) {
	return nil, nil
}

// StatementSheet is Statement's content function under the sheet segment, with the
// compound key.
func StatementSheet(_ context.Context, _ ccc.UUID, _ string, _ *resource.QuerySet[Statement], _ resource.Client, _ *Client) (*resource.Content, error) {
	return nil, nil
}

// WrongKeyContent takes the key as a string where the struct's key is a UUID.
func WrongKeyContent(_ context.Context, _ string, _ *resource.QuerySet[WrongKey], _ resource.Client, _ *Client) (*resource.Content, error) {
	return nil, nil
}

// WrongResultContent answers with the content by value.
func WrongResultContent(_ context.Context, _ ccc.UUID, _ *resource.QuerySet[WrongResult], _ resource.Client, _ *Client) (resource.Content, error) {
	return resource.Content{}, nil
}

// NoQuerySetContent skips the QuerySet the frame hands over.
func NoQuerySetContent(_ context.Context, _ ccc.UUID, _ resource.Client, _ *Client) (*resource.Content, error) {
	return nil, nil
}
