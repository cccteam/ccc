package generation

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_packageDir_Dir_Package(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		p           packageDir
		wantDir     string
		wantPackage string
	}{
		{
			name:        "directory path",
			p:           "path/to/package",
			wantDir:     "path/to/package",
			wantPackage: "package",
		},
		{
			name:        "relative directory path",
			p:           "./path/to/package",
			wantDir:     "./path/to/package",
			wantPackage: "package",
		},
		{
			name:        "file path",
			p:           "path/to/package/file.go",
			wantDir:     "path/to/package",
			wantPackage: "package",
		},
		{
			name:        "relative file path",
			p:           "./path/to/package/file.go",
			wantDir:     "./path/to/package",
			wantPackage: "package",
		},
		{
			name:        "just file does not panic",
			p:           "file.go",
			wantDir:     "",
			wantPackage: ".",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.p.Dir(); got != tt.wantDir {
				t.Errorf("packageDir.Dir() = %v, want %v", got, tt.wantDir)
			}
			if got := tt.p.Package(); got != tt.wantPackage {
				t.Errorf("packageDir.Dir() = %v, want %v", got, tt.wantPackage)
			}
		})
	}
}

func Test_generatedRoute_SharedHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		handlerType HandlerType
		want        bool
	}{
		{name: "read handler is shared", handlerType: ReadHandler, want: true},
		{name: "list handler is shared", handlerType: ListHandler, want: true},
		{name: "patch handler is not shared", handlerType: PatchHandler, want: false},
		{name: "no handler type is not shared", handlerType: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := generatedRoute{HandlerType: tt.handlerType}
			if got := g.SharedHandler(); got != tt.want {
				t.Errorf("generatedRoute.SharedHandler() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_generatedRoute_TestMethods(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		route generatedRoute
		want  []string
	}{
		{
			name:  "read handler also tests POST",
			route: generatedRoute{Method: "GET", HandlerType: ReadHandler},
			want:  []string{"http.MethodGet", "http.MethodPost"},
		},
		{
			name:  "list handler also tests POST",
			route: generatedRoute{Method: "GET", HandlerType: ListHandler},
			want:  []string{"http.MethodGet", "http.MethodPost"},
		},
		{
			name:  "patch handler tests PATCH only",
			route: generatedRoute{Method: "PATCH", HandlerType: PatchHandler},
			want:  []string{"http.MethodPatch"},
		},
		{
			name:  "rpc route tests POST only",
			route: generatedRoute{Method: "POST"},
			want:  []string{"http.MethodPost"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tt.want, tt.route.TestMethods()); diff != "" {
				t.Errorf("generatedRoute.TestMethods() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_generatedRoute_appendParamsToPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		route       generatedRoute
		wantPath    string
		wantTestURL string
	}{
		{
			name: "compound key route",
			route: generatedRoute{
				Path:    "/api/widget-orders",
				TestURL: "/api/widget-orders",
				TestParams: []routeTestParam{
					{Key: "widgetOrderWidgetID", Value: "testWidgetOrderWidgetID"},
					{Key: "widgetOrderOrderID", Value: "testWidgetOrderOrderID"},
				},
			},
			wantPath:    "/api/widget-orders/{widgetOrderWidgetID}/{widgetOrderOrderID}",
			wantTestURL: "/api/widget-orders/testWidgetOrderWidgetID/testWidgetOrderOrderID",
		},
		{
			name: "no parameters leaves paths unchanged",
			route: generatedRoute{
				Path:    "/api/widgets",
				TestURL: "/api/widgets",
			},
			wantPath:    "/api/widgets",
			wantTestURL: "/api/widgets",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.route.appendParamsToPaths()
			if tt.route.Path != tt.wantPath {
				t.Errorf("generatedRoute.Path = %v, want %v", tt.route.Path, tt.wantPath)
			}
			if tt.route.TestURL != tt.wantTestURL {
				t.Errorf("generatedRoute.TestURL = %v, want %v", tt.route.TestURL, tt.wantTestURL)
			}
		})
	}
}
