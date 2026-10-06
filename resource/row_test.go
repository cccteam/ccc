package resource

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

type rowTestResource struct {
	ID     string  `json:"id"`
	Name   string  `json:"name,omitempty"`
	Amount *int64  `json:"amount"`
	Hidden string  `json:"-"`
	Note   *string `json:"note,omitempty"`
}

func (rowTestResource) Resource() accesstypes.Resource {
	return "RowTestResources"
}

func (rowTestResource) DefaultConfig() Config {
	return Config{}
}

func TestRow_MarshalJSON(t *testing.T) {
	t.Parallel()

	amount := int64(42)

	tests := []struct {
		name string
		data rowTestResource
	}{
		{
			name: "zero value",
			data: rowTestResource{},
		},
		{
			name: "populated fields",
			data: rowTestResource{ID: "1", Name: "first", Amount: &amount, Hidden: "never"},
		},
		{
			name: "nil pointer field",
			data: rowTestResource{ID: "2", Amount: nil},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			want, err := json.Marshal(tt.data)
			if err != nil {
				t.Fatalf("json.Marshal(data) error = %v", err)
			}

			got, err := json.Marshal(Row[rowTestResource]{Data: tt.data})
			if err != nil {
				t.Fatalf("json.Marshal(Row) error = %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("json.Marshal(Row) = %s, want %s", got, want)
			}

			gotPtr, err := json.Marshal(&Row[rowTestResource]{Data: tt.data})
			if err != nil {
				t.Fatalf("json.Marshal(*Row) error = %v", err)
			}
			if !bytes.Equal(gotPtr, want) {
				t.Errorf("json.Marshal(*Row) = %s, want %s", gotPtr, want)
			}
		})
	}
}

func TestRow_Masked(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		row      *Row[rowTestResource]
		jsonName string
		want     bool
	}{
		{
			name:     "empty envelope reports false",
			row:      &Row[rowTestResource]{Data: rowTestResource{ID: "1"}},
			jsonName: "id",
			want:     false,
		},
		{
			name:     "empty envelope reports false for unknown name",
			row:      &Row[rowTestResource]{},
			jsonName: "nonexistent",
			want:     false,
		},
		{
			name:     "empty envelope reports false for empty name",
			row:      &Row[rowTestResource]{},
			jsonName: "",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.row.Masked(tt.jsonName); got != tt.want {
				t.Errorf("Row.Masked(%q) = %v, want %v", tt.jsonName, got, tt.want)
			}
		})
	}
}
