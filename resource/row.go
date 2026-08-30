package resource

import (
	"encoding/json"
	"slices"

	"github.com/go-playground/errors/v5"
)

// Row is the generic per-row envelope yielded by List and returned by Read. It carries
// the row data together with per-row metadata; cell masking is the envelope's first
// metadata tenant. No machinery populates the masking metadata yet, so Masked reports
// false for every cell until mask rendering lands.
type Row[Resource Resourcer] struct {
	// Data is the row itself, exactly as scanned from the database.
	Data Resource

	// masked holds the JSON names of this row's masked cells. It stays empty until
	// mask rendering lands.
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
