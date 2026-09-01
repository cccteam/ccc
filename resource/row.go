package resource

import (
	"encoding/json"
	"slices"

	"github.com/go-playground/errors/v5"
)

// Row is the generic per-row envelope yielded by List and returned by Read. It carries
// the row data together with per-row metadata; cell masking is the envelope's first
// metadata tenant, populated from the read statement's reserved masked-names column
// when conditional grants render into the query.
type Row[Resource Resourcer] struct {
	// Data is the row itself, exactly as scanned from the database. A masked cell
	// holds its type's zero value; Masked is what distinguishes it from a genuine
	// zero.
	Data Resource

	// masked holds the JSON names of this row's masked cells.
	masked []string
}

// Masked reports whether the cell with the given JSON name is masked on this row.
func (r Row[Resource]) Masked(jsonName string) bool {
	return slices.Contains(r.masked, jsonName)
}

// MarshalJSON delegates to the row data: marshaling a Row produces exactly the bytes
// that marshaling the Resource itself would.
func (r Row[Resource]) MarshalJSON() ([]byte, error) {
	b, err := json.Marshal(r.Data)
	if err != nil {
		return nil, errors.Wrap(err, "json.Marshal()")
	}

	return b, nil
}
