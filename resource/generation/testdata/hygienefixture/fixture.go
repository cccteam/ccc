// Package hygienefixture holds structs that break the generator's source-hygiene rules
// on purpose: a struct claiming two kinds, and conditions tags carrying values the
// generator does not recognize. Tests load it to exercise the refusals one struct at a
// time; no test extracts the package as a whole.
package hygienefixture

type (
	// TwoKinds claims to be both a table-backed and a computed resource.
	//
	// @resource
	// @computed
	TwoKinds struct {
		ID string
	}

	// OneKind is an ordinary computed resource.
	//
	// @computed
	OneKind struct {
		// @primarykey
		ID string
	}

	// Misspelled carries a conditions value one letter off a recognized one.
	Misspelled struct {
		ID   string
		Name string `conditions:"immutble"`
	}

	// Spaced carries a value with a leading space, which exact matching would miss.
	Spaced struct {
		ID   string
		Name string `conditions:"immutable, pii"`
	}

	// Trailing carries an empty value left by a trailing comma.
	Trailing struct {
		ID   string
		Name string `conditions:"pii,"`
	}

	// Clean carries only recognized values.
	Clean struct {
		ID   string
		Name string `conditions:"immutable,pii"`
	}
)
