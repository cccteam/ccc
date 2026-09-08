package resource

// These tests pin the readability rule for sort and filter fields: a request may
// order or filter only by a field the caller holds an unconditional grant on. A
// denied field is an inference channel; a conditionally granted field is masked on
// some rows, so ordering by it would order masked rows by their real values.

import (
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"go.uber.org/mock/gomock"
)

// enforcementQueryRequest is the list request over the enforcement fixture with
// filterable fields, so the readability rule can be exercised through the filter
// grammar as well as the sort parameter.
type enforcementQueryRequest struct {
	ID      ccc.UUID `json:"id"     perm:"-"`
	Public  string   `json:"public" index:"true"`
	Tagged  string   `json:"tagged" allow_filter:"true"`
	Locked  string   `json:"locked"`
	Station string   `json:"-"`
}

func TestQuerySet_List_sortAndFilterReadability(t *testing.T) {
	t.Parallel()

	granted := map[accesstypes.Permission][]accesstypes.Resource{
		accesstypes.List: {enforcedResource, enforcedResource + ".public"},
	}
	conditional := map[accesstypes.Permission][]accesstypes.Resource{
		accesstypes.List: {enforcedResource + ".tagged"},
	}

	tests := []struct {
		name            string
		target          string
		wantForbidden   bool
		wantErrContains string
	}{
		{
			name:   "sort on a granted field is admitted",
			target: "/?sort=public",
		},
		{
			name:   "sort on the exempt primary key follows the resource grant",
			target: "/?sort=id:desc",
		},
		{
			name:            "sort on a denied field is refused naming the field",
			target:          "/?sort=locked",
			wantForbidden:   true,
			wantErrContains: "sort or filter on locked",
		},
		{
			name:            "sort on a conditionally granted field is refused",
			target:          "/?sort=public,tagged:desc",
			wantForbidden:   true,
			wantErrContains: "sort or filter on tagged",
		},
		{
			name:   "filter on a granted field is admitted",
			target: "/?filter=public:eq:x",
		},
		{
			name:            "filter on a conditionally granted field is refused",
			target:          "/?filter=public:eq:x,tagged:eq:y",
			wantForbidden:   true,
			wantErrContains: "sort or filter on tagged",
		},
		{
			name:            "sort and filter refusals name the unconditional requirement",
			target:          "/?sort=tagged&filter=public:eq:x",
			wantForbidden:   true,
			wantErrContains: "must be granted unconditionally",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resSet, err := NewSet[enforcementResource, enforcementQueryRequest](accesstypes.List)
			if err != nil {
				t.Fatalf("NewSet() error = %v", err)
			}
			decoder, err := NewQueryDecoder[enforcementResource, enforcementQueryRequest](resSet)
			if err != nil {
				t.Fatalf("NewQueryDecoder() error = %v", err)
			}
			decoder.collection = enforcementCollection(t)

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.target, http.NoBody)
			qSet, err := decoder.Decode(req, &fakeUserPermissions{granted: granted, conditional: conditional}, testScope)
			if err != nil {
				t.Fatalf("QueryDecoder.Decode() error = %v", err)
			}

			wantErr := tt.wantForbidden || tt.wantErrContains != ""

			ctrl := gomock.NewController(t)
			reader := NewMockReader[enforcementResource](ctrl)
			reader.EXPECT().DBType().MinTimes(1).Return(SpannerDBType)
			if !wantErr {
				reader.EXPECT().List(gomock.Any(), gomock.Any()).Return(iter.Seq2[*Row[enforcementResource], error](func(func(*Row[enforcementResource], error) bool) {}))
			}
			client := NewMockClient(nil, []any{reader}, nil)

			for _, err = range qSet.List(t.Context(), client) {
				if err != nil {
					break
				}
			}

			if wantErr {
				if err == nil {
					t.Fatal("QuerySet.List() expected an error, got nil")
				}
				if httpio.HasForbidden(err) != tt.wantForbidden {
					t.Errorf("QuerySet.List() error forbidden = %v, want %v: %v", httpio.HasForbidden(err), tt.wantForbidden, err)
				}
				if !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Errorf("QuerySet.List() error = %v, want error containing %q", err, tt.wantErrContains)
				}

				return
			}
			if err != nil {
				t.Fatalf("QuerySet.List() error = %v", err)
			}
		})
	}
}
