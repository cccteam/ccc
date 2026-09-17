package generation

import "testing"

// Test_matrixListQuery pins the query the generated authorization matrix sends with a
// List request so the request pins the gate and not the order: nothing where the
// struct declares an @order, a sort on the first key where it declares none, and
// nothing for a key-less resource, which is served whole and needs no order.
func Test_matrixListQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		declaresOrder bool
		keys          []string
		want          string
	}{
		{name: "a declared order needs nothing", declaresOrder: true, keys: []string{"id"}},
		{name: "no order: a sort on the first key", keys: []string{"id", "code"}, want: "sort=id"},
		{name: "no order and no key: the whole list, nothing to send"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := matrixListQuery(tt.declaresOrder, tt.keys); got != tt.want {
				t.Errorf("matrixListQuery() = %q, want %q", got, tt.want)
			}
		})
	}
}
