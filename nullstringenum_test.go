package ccc

import (
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestDecodeSpanner validates DecodeSpanner method.
func TestNullEnum_DecodeSpanner_string(t *testing.T) {
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

// TestDecodeSpanner validates DecodeSpanner method.
func TestNullEnum_DecodeSpanner_int(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   any
		want    NullEnum[int]
		wantErr bool
	}{
		{
			name:    "Nil value",
			input:   nil,
			want:    NullEnum[int]{Value: 0, Valid: false},
			wantErr: false,
		},
		{
			name:    "Valid string",
			input:   44,
			want:    NullEnum[int]{Value: 44, Valid: true},
			wantErr: false,
		},
		{
			name:    "Invalid type",
			input:   "123",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var n NullEnum[int]
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

// TestDecodeSpanner validates DecodeSpanner method.
func TestNullEnum_DecodeSpanner_int64(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   any
		want    NullEnum[int64]
		wantErr bool
	}{
		{
			name:    "Nil value",
			input:   nil,
			want:    NullEnum[int64]{Value: 0, Valid: false},
			wantErr: false,
		},
		{
			name:    "Valid int64",
			input:   int64(44),
			want:    NullEnum[int64]{Value: 44, Valid: true},
			wantErr: false,
		},
		{
			name:    "Invalid type",
			input:   "123",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var n NullEnum[int64]
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

// TestDecodeSpanner validates DecodeSpanner method.
func TestNullEnum_DecodeSpanner_float64(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   any
		want    NullEnum[float64]
		wantErr bool
	}{
		{
			name:    "Nil value",
			input:   nil,
			want:    NullEnum[float64]{Value: 0, Valid: false},
			wantErr: false,
		},
		{
			name:    "Valid float64",
			input:   float64(44.2),
			want:    NullEnum[float64]{Value: 44.2, Valid: true},
			wantErr: false,
		},
		{
			name:    "Invalid type",
			input:   "123",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var n NullEnum[float64]
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
func TestNullEnum_EncodeSpanner_string(t *testing.T) {
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

// TestEncodeSpanner validates EncodeSpanner.
func TestNullEnum_EncodeSpanner_int(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input NullEnum[int]
		want  any
	}{
		{
			name:  "Valid Enum",
			input: NullEnum[int]{Value: 42, Valid: true},
			want:  42,
		},
		{
			name:  "Invalid Enum",
			input: NullEnum[int]{Valid: false},
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

// TestEncodeSpanner validates EncodeSpanner.
func TestNullEnum_EncodeSpanner_int64(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input NullEnum[int64]
		want  any
	}{
		{
			name:  "Valid Enum",
			input: NullEnum[int64]{Value: 42, Valid: true},
			want:  int64(42),
		},
		{
			name:  "Invalid Enum",
			input: NullEnum[int64]{Valid: false},
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

// TestEncodeSpanner validates EncodeSpanner.
func TestNullEnum_EncodeSpanner_float64(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input NullEnum[float64]
		want  any
	}{
		{
			name:  "Valid Enum",
			input: NullEnum[float64]{Value: 42.1, Valid: true},
			want:  float64(42.1),
		},
		{
			name:  "Invalid Enum",
			input: NullEnum[float64]{Valid: false},
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
func TestNullEnum_MarshalText_string(t *testing.T) {
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

// TestMarshalText validates MarshalText.
func TestNullEnum_MarshalText_int(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input NullEnum[int]
		want  []byte
	}{
		{
			name:  "Valid Enum",
			input: NullEnum[int]{Value: 42, Valid: true},
			want:  []byte("42"),
		},
		{
			name:  "Invalid Enum",
			input: NullEnum[int]{Valid: false},
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

// TestMarshalText validates MarshalText.
func TestNullEnum_MarshalText_int64(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input NullEnum[int64]
		want  []byte
	}{
		{
			name:  "Valid Enum",
			input: NullEnum[int64]{Value: 42, Valid: true},
			want:  []byte("42"),
		},
		{
			name:  "Invalid Enum",
			input: NullEnum[int64]{Valid: false},
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

// TestMarshalText validates MarshalText.
func TestNullEnum_MarshalText_float64(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input NullEnum[float64]
		want  []byte
	}{
		{
			name:  "Valid Enum",
			input: NullEnum[float64]{Value: 42.1, Valid: true},
			want:  []byte("42.1"),
		},
		{
			name:  "Invalid Enum",
			input: NullEnum[float64]{Valid: false},
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
func TestNullEnum_UnmarshalText_string(t *testing.T) {
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

// TestUnmarshalText validates UnmarshalText.
func TestNullEnum_UnmarshalText_int(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   []byte
		want    NullEnum[int]
		wantErr bool
	}{
		{
			name:  "Valid text",
			input: []byte("42"),
			want:  NullEnum[int]{Value: 42, Valid: true},
		},
		{
			name: "Nil text",
			// []byte(nil) produces a nil slice
			input: nil,
			want:  NullEnum[int]{Value: 0, Valid: false},
		},
		{
			name:    "Invalid text",
			input:   []byte("abc"),
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var n NullEnum[int]
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

// TestUnmarshalText validates UnmarshalText.
func TestNullEnum_UnmarshalText_int64(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   []byte
		want    NullEnum[int64]
		wantErr bool
	}{
		{
			name:  "Valid text",
			input: []byte("42"),
			want:  NullEnum[int64]{Value: 42, Valid: true},
		},
		{
			name: "Nil text",
			// []byte(nil) produces a nil slice
			input: nil,
			want:  NullEnum[int64]{Value: 0, Valid: false},
		},
		{
			name:    "Invalid text",
			input:   []byte("abc"),
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var n NullEnum[int64]
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

// TestUnmarshalText validates UnmarshalText.
func TestNullEnum_UnmarshalText_float64(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   []byte
		want    NullEnum[float64]
		wantErr bool
	}{
		{
			name:  "Valid text",
			input: []byte("42.1"),
			want:  NullEnum[float64]{Value: 42.1, Valid: true},
		},
		{
			name: "Nil text",
			// []byte(nil) produces a nil slice
			input: nil,
			want:  NullEnum[float64]{Value: 0, Valid: false},
		},
		{
			name:    "Invalid text",
			input:   []byte("abc"),
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var n NullEnum[float64]
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
func TestNullEnum_MarshalJSON_string(t *testing.T) {
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
			want:  []byte("null"),
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

// TestMarshalJSON validates MarshalJSON.
func TestNullEnum_MarshalJSON_int(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input NullEnum[int]
		want  []byte
	}{
		{
			name:  "Valid Enum",
			input: NullEnum[int]{Value: 42, Valid: true},
			want:  []byte(`42`),
		},
		{
			name:  "Invalid Enum",
			input: NullEnum[int]{Valid: false},
			want:  []byte("null"),
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

// TestMarshalJSON validates MarshalJSON.
func TestNullEnum_MarshalJSON_int64(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input NullEnum[int64]
		want  []byte
	}{
		{
			name:  "Valid Enum",
			input: NullEnum[int64]{Value: 42, Valid: true},
			want:  []byte(`42`),
		},
		{
			name:  "Invalid Enum",
			input: NullEnum[int64]{Valid: false},
			want:  []byte("null"),
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

// TestMarshalJSON validates MarshalJSON.
func TestNullEnum_MarshalJSON_float64(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input NullEnum[float64]
		want  []byte
	}{
		{
			name:  "Valid Enum",
			input: NullEnum[float64]{Value: 42.1, Valid: true},
			want:  []byte(`42.1`),
		},
		{
			name:  "Invalid Enum",
			input: NullEnum[float64]{Valid: false},
			want:  []byte("null"),
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
func TestNullEnum_UnmarshalJSON_string(t *testing.T) {
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
			input: []byte(`null`),
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

// TestUnmarshalJSON validates UnmarshalJSON.
func TestNullEnum_UnmarshalJSON_int(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   []byte
		want    NullEnum[int]
		wantErr bool
	}{
		{
			name:  "Valid Enum",
			input: []byte(`42`),
			want:  NullEnum[int]{Value: 42, Valid: true},
		},
		{
			name:  "Null JSON",
			input: []byte(`null`),
			want:  NullEnum[int]{Value: 0, Valid: false},
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

			var n NullEnum[int]
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

// TestUnmarshalJSON validates UnmarshalJSON.
func TestNullEnum_UnmarshalJSON_int64(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   []byte
		want    NullEnum[int64]
		wantErr bool
	}{
		{
			name:  "Valid Enum",
			input: []byte(`42`),
			want:  NullEnum[int64]{Value: 42, Valid: true},
		},
		{
			name:  "Null JSON",
			input: []byte(`null`),
			want:  NullEnum[int64]{Value: 0, Valid: false},
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

			var n NullEnum[int64]
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

// TestUnmarshalJSON validates UnmarshalJSON.
func TestNullEnum_UnmarshalJSON_float64(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   []byte
		want    NullEnum[float64]
		wantErr bool
	}{
		{
			name:  "Valid Enum",
			input: []byte(`42.1`),
			want:  NullEnum[float64]{Value: 42.1, Valid: true},
		},
		{
			name:  "Null JSON",
			input: []byte(`null`),
			want:  NullEnum[float64]{Value: 0, Valid: false},
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

			var n NullEnum[float64]
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
