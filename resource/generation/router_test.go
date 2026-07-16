package generation

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_readRouteTestParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		resourceName string
		pkNames      []string
		want         []routeTestParam
	}{
		{
			name:         "single primary key",
			resourceName: "Widget",
			pkNames:      []string{"ID"},
			want: []routeTestParam{
				{Key: "widgetID", Value: "testWidgetID"},
			},
		},
		{
			name:         "compound primary key",
			resourceName: "WidgetOrder",
			pkNames:      []string{"WidgetID", "OrderID"},
			want: []routeTestParam{
				{Key: "widgetOrderWidgetID", Value: "testWidgetOrderWidgetID"},
				{Key: "widgetOrderOrderID", Value: "testWidgetOrderOrderID"},
			},
		},
		{
			name:         "no primary keys",
			resourceName: "Widget",
			pkNames:      nil,
			want:         []routeTestParam{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := readRouteTestParams(tt.resourceName, tt.pkNames)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("readRouteTestParams() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
