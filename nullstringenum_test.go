package ccc

import (
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestDecodeSpanner validates DecodeSpanner method.
func TestNullStringEnum_DecodeSpanner(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   any
		want    NullEnum[string]
		wantErr bool
	}{
		{
			name:    "Nil value",
			input:   nil,
			want:    NullEnum[string]{Value: "", Valid: false},
			wantErr: false,
		},
		{
			name:    "Valid string",
			input:   "testValue",
			want:    NullEnum[string]{Value: "testValue", Valid: true},
			wantErr: false,
		},
		{
			name:    "Invalid type",
			input:   123,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var n NullEnum[string]
			err := n.DecodeSpanner(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("DecodeSpanner() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && !reflect.DeepEqual(n, tt.want) {
				t.Errorf("DecodeSpanner() = %+v, want %+v", n, tt.want)
			}
		})
	}
}

// TestEncodeSpanner validates EncodeSpanner.
func TestNullStringEnum_EncodeSpanner(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input NullEnum[string]
		want  any
	}{
		{
			name:  "Valid Enum",
			input: NullEnum[string]{Value: "enumValue", Valid: true},
			want:  "enumValue",
		},
		{
			name:  "Invalid Enum",
			input: NullEnum[string]{Valid: false},
			want:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.input.EncodeSpanner()
			if err != nil {
				t.Errorf("EncodeSpanner() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("EncodeSpanner() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestMarshalText validates MarshalText.
func TestNullStringEnum_MarshalText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input NullEnum[string]
		want  []byte
	}{
		{
			name:  "Valid Enum",
			input: NullEnum[string]{Value: "enumValue", Valid: true},
			want:  []byte("enumValue"),
		},
		{
			name:  "Invalid Enum",
			input: NullEnum[string]{Valid: false},
			want:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.input.MarshalText()
			if err != nil {
				t.Errorf("MarshalText() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("MarshalText() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestUnmarshalText validates UnmarshalText.
func TestNullStringEnum_UnmarshalText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   []byte
		want    NullEnum[string]
		wantErr bool
	}{
		{
			name:  "Valid text",
			input: []byte("enumValue"),
			want:  NullEnum[string]{Value: "enumValue", Valid: true},
		},
		{
			name: "Nil text",
			// []byte(nil) produces a nil slice
			input: nil,
			want:  NullEnum[string]{Value: "", Valid: false},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var n NullEnum[string]
			err := n.UnmarshalText(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("UnmarshalText() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(n, tt.want) {
				t.Errorf("UnmarshalText() = %+v, want %+v", n, tt.want)
			}
		})
	}
}

// TestMarshalJSON validates MarshalJSON.
func TestNullStringEnum_MarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input NullEnum[string]
		want  []byte
	}{
		{
			name:  "Valid Enum",
			input: NullEnum[string]{Value: "enumValue", Valid: true},
			want:  []byte(`"enumValue"`),
		},
		{
			name:  "Invalid Enum",
			input: NullEnum[string]{Valid: false},
			// Assuming jsonNull is defined as "null"
			want: []byte("null"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.input.MarshalJSON()
			if err != nil {
				t.Errorf("MarshalJSON() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("MarshalJSON() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestUnmarshalJSON validates UnmarshalJSON.
func TestNullStringEnum_UnmarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   []byte
		want    NullEnum[string]
		wantErr bool
	}{
		{
			name:  "Valid Enum",
			input: []byte(`"enumValue"`),
			want:  NullEnum[string]{Value: "enumValue", Valid: true},
		},
		{
			name:  "Null JSON",
			input: []byte(`"null"`),
			want:  NullEnum[string]{Value: "", Valid: false},
		},
		{
			name:    "Invalid JSON",
			input:   []byte(`"enumValue`), // missing closing quote
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var n NullEnum[string]
			err := n.UnmarshalJSON(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("UnmarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if diff := cmp.Diff(tt.want, n); diff != "" {
					t.Errorf("UnmarshalJSON() mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}
