package resources

import "github.com/cccteam/ccc"

type AddressType struct {
	ID          string `spanner:"Id"`
	Description string `spanner:"description"`
}

func (a AddressType) method() {}

func doer() error {
	return nil
}

type Status struct {
	ID          ccc.UUID `spanner:"Id"`
	Description string   `spanner:"description"`
}

type enum int

const (
	e1 enum = iota
	e2
)

type alias = struct{}
