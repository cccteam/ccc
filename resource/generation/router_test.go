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

// Test_negativeTestsForOutlet_sessionRoutes pins the permission-route half of the
// isolation cases: a session-less outlet's prefix must 404 for the permission routes,
// while a session-serving outlet contributes none.
func Test_negativeTestsForOutlet_sessionRoutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		outlet routerOutlet
		want   []negativeRouterTest
	}{
		{
			name:   "session-less outlet gets 404 cases for the permission routes",
			outlet: routerOutlet{name: "automation", prefix: "automation"},
			want: []negativeRouterTest{
				{Method: "http.MethodGet", URL: "/automation/permission-digest"},
				{Method: "http.MethodGet", URL: "/automation/user-domains"},
			},
		},
		{
			name:   "session-serving outlet contributes no permission-route cases",
			outlet: routerOutlet{name: "portal", prefix: "portal", servesSessions: true},
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rg := &resourceGenerator{client: &client{}}
			got, err := rg.negativeTestsForOutlet(tt.outlet)
			if err != nil {
				t.Fatalf("negativeTestsForOutlet() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("negativeTestsForOutlet() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
