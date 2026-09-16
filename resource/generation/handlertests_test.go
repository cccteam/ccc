package generation

import "testing"

// Test_matrixListQuery pins the query the generated authorization matrix sends with a
// List request so the request pins the gate and not the order: nothing where the
// struct declares an @order, a sort on the first key where it declares none,
// limit=all for a key-less resource with no maximum, and a sort on the first field
// that is not a list column for a key-less resource with a maximum.
func Test_matrixListQuery(t *testing.T) {
	t.Parallel()

	notList := func(int) bool { return false }

	tests := []struct {
		name          string
		declaresOrder bool
		keys          []string
		fields        []string
		list          func(i int) bool
		pageMax       uint64
		want          string
	}{
		{name: "a declared order needs nothing", declaresOrder: true, keys: []string{"id"}, fields: []string{"id", "name"}, list: notList},
		{name: "no order: a sort on the first key", keys: []string{"id", "code"}, fields: []string{"id", "code", "name"}, list: notList, want: "sort=id"},
		{name: "no order and no key, no maximum: the whole list", fields: []string{"name"}, list: notList, want: "limit=all"},
		{name: "no order and no key, a maximum: a sort on the first non-list field", fields: []string{"tags", "name"}, list: func(i int) bool { return i == 0 }, pageMax: 100, want: "sort=name"},
		{name: "no order, no key, a maximum, and every field a list: nothing to send", fields: []string{"tags"}, list: func(int) bool { return true }, pageMax: 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := matrixListQuery(tt.declaresOrder, tt.keys, tt.fields, tt.list, tt.pageMax); got != tt.want {
				t.Errorf("matrixListQuery() = %q, want %q", got, tt.want)
			}
		})
	}
}
