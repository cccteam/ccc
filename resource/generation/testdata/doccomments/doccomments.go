// Package doccomments is a parser fixture: the same doc comment on a struct declared
// on its own line and on one declared in a type group.
package doccomments

// Standalone is declared on its own line.
//
// @rpc
type Standalone struct {
	ID string
}

type (
	// Grouped is declared in a type group.
	//
	// @rpc
	Grouped struct {
		ID string
	}
)

type (
	// Sibling shares a group with another spec; its doc is its own.
	Sibling struct{}
	// Other is the second spec of the group.
	Other struct{}
)

type Undocumented struct{}
