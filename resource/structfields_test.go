package resource

import (
	"reflect"
	"testing"
)

type registryInner struct {
	Promoted string
	Shadowed int
	Twice    bool
}

type registryOther struct {
	Twice bool
}

type registryOuter struct {
	registryInner
	registryOther
	Own      string
	Shadowed string
	hidden   int
}

func TestStructField(t *testing.T) {
	t.Parallel()

	outer := reflect.TypeOf(registryOuter{})
	tests := []struct {
		name      string
		field     string
		wantFound bool
		wantType  reflect.Type
		wantIndex []int
	}{
		{name: "own field", field: "Own", wantFound: true, wantType: reflect.TypeOf(""), wantIndex: []int{2}},
		{name: "promoted field", field: "Promoted", wantFound: true, wantType: reflect.TypeOf(""), wantIndex: []int{0, 0}},
		{name: "the shallower declaration wins", field: "Shadowed", wantFound: true, wantType: reflect.TypeOf(""), wantIndex: []int{3}},
		{name: "an ambiguous promoted name is unreachable", field: "Twice", wantFound: false},
		{name: "unexported field resolves", field: "hidden", wantFound: true, wantType: reflect.TypeOf(0), wantIndex: []int{4}},
		{name: "missing field", field: "Nope", wantFound: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, found := structField(outer, tt.field)
			if found != tt.wantFound {
				t.Fatalf("structField(%q) found = %v, want %v", tt.field, found, tt.wantFound)
			}
			if !found {
				return
			}
			if got.Type != tt.wantType {
				t.Errorf("structField(%q).Type = %s, want %s", tt.field, got.Type, tt.wantType)
			}
			if !reflect.DeepEqual(got.Index, tt.wantIndex) {
				t.Errorf("structField(%q).Index = %v, want %v", tt.field, got.Index, tt.wantIndex)
			}
		})
	}
}

func TestFieldValue(t *testing.T) {
	t.Parallel()

	row := reflect.ValueOf(registryOuter{
		registryInner: registryInner{Promoted: "deep", Shadowed: 7},
		Own:           "mine",
		Shadowed:      "shallow",
	})
	tests := []struct {
		name      string
		field     string
		wantValid bool
		want      any
	}{
		{name: "own field", field: "Own", wantValid: true, want: "mine"},
		{name: "promoted field", field: "Promoted", wantValid: true, want: "deep"},
		{name: "shadowing field", field: "Shadowed", wantValid: true, want: "shallow"},
		{name: "missing field is the zero Value", field: "Nope", wantValid: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := fieldValue(row, tt.field)
			if got.IsValid() != tt.wantValid {
				t.Fatalf("fieldValue(%q).IsValid() = %v, want %v", tt.field, got.IsValid(), tt.wantValid)
			}
			if tt.wantValid && got.Interface() != tt.want {
				t.Errorf("fieldValue(%q) = %v, want %v", tt.field, got.Interface(), tt.want)
			}
		})
	}
}
