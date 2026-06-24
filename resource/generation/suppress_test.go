package generation

import (
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_applySuppressDirectives(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                   string
		args                   []string
		isConsolidated         bool
		wantSuppressedRoutes   []RouteType
		wantSuppressedHandlers []HandlerType
		wantIsConsolidated     bool
		wantErr                bool
	}{
		{
			name: "no arguments",
		},
		{
			name:                 "allRoutes on a non-consolidated resource",
			args:                 []string{string(AllRoutes)},
			wantSuppressedRoutes: []RouteType{AllRoutes},
		},
		{
			// A consolidated resource's patch handler is served by the shared consolidated route, which
			// cannot be suppressed per resource, so @suppress(allRoutes) is contradictory here.
			name:           "allRoutes on a consolidated resource errors",
			args:           []string{string(AllRoutes)},
			isConsolidated: true,
			wantErr:        true,
		},
		{
			// @suppress(allHandlers) clears IsConsolidated, so combining it with @suppress(allRoutes) is
			// allowed: nothing is generated and there is no consolidated patch route to conflict with.
			name:                   "allHandlers and allRoutes on a consolidated resource",
			args:                   []string{string(AllHandlers), string(AllRoutes)},
			isConsolidated:         true,
			wantSuppressedRoutes:   []RouteType{AllRoutes},
			wantSuppressedHandlers: []HandlerType{ListHandler, ReadHandler, PatchHandler},
		},
		{
			// @suppress(patchHandler) also clears IsConsolidated, so @suppress(allRoutes) is allowed.
			name:                   "patchHandler and allRoutes on a consolidated resource",
			args:                   []string{string(PatchHandler), string(AllRoutes)},
			isConsolidated:         true,
			wantSuppressedRoutes:   []RouteType{AllRoutes},
			wantSuppressedHandlers: []HandlerType{PatchHandler},
		},
		{
			name:                   "readHandler and allRoutes",
			args:                   []string{string(ReadHandler), string(AllRoutes)},
			wantSuppressedRoutes:   []RouteType{AllRoutes},
			wantSuppressedHandlers: []HandlerType{ReadHandler},
		},
		{
			name:                   "allHandlers clears consolidation",
			args:                   []string{string(AllHandlers)},
			isConsolidated:         true,
			wantSuppressedHandlers: []HandlerType{ListHandler, ReadHandler, PatchHandler},
		},
		{
			name:    "unknown argument errors",
			args:    []string{"bogus"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := &resourceInfo{IsConsolidated: tt.isConsolidated}
			err := applySuppressDirectives(res, slices.Values(tt.args))
			if tt.wantErr {
				if err == nil {
					t.Fatal("applySuppressDirectives() expected an error, got nil")
				}

				return
			}
			if err != nil {
				t.Fatalf("applySuppressDirectives() error = %v", err)
			}

			if diff := cmp.Diff(tt.wantSuppressedRoutes, res.SuppressedRoutes); diff != "" {
				t.Errorf("SuppressedRoutes mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantSuppressedHandlers, res.SuppressedHandlers); diff != "" {
				t.Errorf("SuppressedHandlers mismatch (-want +got):\n%s", diff)
			}
			if res.IsConsolidated != tt.wantIsConsolidated {
				t.Errorf("IsConsolidated = %v, want %v", res.IsConsolidated, tt.wantIsConsolidated)
			}
		})
	}
}

func Test_applyComputedSuppressDirectives(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		args                 []string
		wantSuppressList     bool
		wantSuppressRead     bool
		wantSuppressedRoutes []RouteType
		wantErr              bool
	}{
		{
			name: "no arguments",
		},
		{
			name:                 "allRoutes",
			args:                 []string{string(AllRoutes)},
			wantSuppressedRoutes: []RouteType{AllRoutes},
		},
		{
			name:             "listHandler",
			args:             []string{string(ListHandler)},
			wantSuppressList: true,
		},
		{
			name:             "readHandler",
			args:             []string{string(ReadHandler)},
			wantSuppressRead: true,
		},
		{
			name:             "allHandlers suppresses list and read",
			args:             []string{string(AllHandlers)},
			wantSuppressList: true,
			wantSuppressRead: true,
		},
		{
			name:                 "listHandler and allRoutes combined",
			args:                 []string{string(ListHandler), string(AllRoutes)},
			wantSuppressList:     true,
			wantSuppressedRoutes: []RouteType{AllRoutes},
		},
		{
			// Computed resources have no patch handler, so patchHandler is rejected.
			name:    "patchHandler errors",
			args:    []string{string(PatchHandler)},
			wantErr: true,
		},
		{
			name:    "unknown argument errors",
			args:    []string{"bogus"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := &computedResource{}
			err := applyComputedSuppressDirectives(res, slices.Values(tt.args))
			if tt.wantErr {
				if err == nil {
					t.Fatal("applyComputedSuppressDirectives() expected an error, got nil")
				}

				return
			}
			if err != nil {
				t.Fatalf("applyComputedSuppressDirectives() error = %v", err)
			}

			if res.SuppressListHandler != tt.wantSuppressList {
				t.Errorf("SuppressListHandler = %v, want %v", res.SuppressListHandler, tt.wantSuppressList)
			}
			if res.SuppressReadHandler != tt.wantSuppressRead {
				t.Errorf("SuppressReadHandler = %v, want %v", res.SuppressReadHandler, tt.wantSuppressRead)
			}
			if diff := cmp.Diff(tt.wantSuppressedRoutes, res.SuppressedRoutes); diff != "" {
				t.Errorf("SuppressedRoutes mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_suppressArgs(t *testing.T) {
	t.Parallel()

	want := []string{
		string(AllHandlers),
		string(ListHandler),
		string(ReadHandler),
		string(PatchHandler),
		string(AllRoutes),
	}
	if diff := cmp.Diff(want, validSuppressArgs()); diff != "" {
		t.Errorf("suppressArgs() mismatch (-want +got):\n%s", diff)
	}
}

func Test_validComputedSuppressArgs(t *testing.T) {
	t.Parallel()

	want := []string{
		string(AllHandlers),
		string(ListHandler),
		string(ReadHandler),
		string(AllRoutes),
	}
	if diff := cmp.Diff(want, validComputedSuppressArgs()); diff != "" {
		t.Errorf("validComputedSuppressArgs() mismatch (-want +got):\n%s", diff)
	}
}
