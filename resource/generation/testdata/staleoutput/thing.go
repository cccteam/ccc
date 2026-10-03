// Package staleoutput is a parser test fixture: a valid hand-written struct
// beside generated output that no longer type-checks (see zz_gen_stale.go),
// simulating the state after the generator's dependencies changed shape.
package staleoutput

type Thing struct {
	ID   string `spanner:"Id"`
	Name string `spanner:"Name"`
}
