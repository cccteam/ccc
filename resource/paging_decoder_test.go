package resource

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
)

func newQueryTestDecoder(t *testing.T, paging Paging, key *CursorKey) *QueryDecoder[enforcementResource, enforcementQueryRequest] {
	t.Helper()

	resSet, err := NewSet[enforcementResource, enforcementQueryRequest](accesstypes.List)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}
	decoder, err := NewQueryDecoder[enforcementResource, enforcementQueryRequest](resSet)
	if err != nil {
		t.Fatalf("NewQueryDecoder() error = %v", err)
	}

	return decoder.WithPaging(paging).WithCursorKey(key)
}

// TestQueryDecoder_declaredPageSizes pins the declared contract: the default page
// a limit-less request takes, the maximum a request may not exceed, and that a
// maximum also closes limit=all.
func TestQueryDecoder_declaredPageSizes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		paging    Paging
		target    string
		wantPage  pageRequest
		wantLimit string
		wantErr   string
	}{
		{
			name:      "generator-wide default page",
			target:    "/",
			wantPage:  pageRequest{size: DefaultPageSize},
			wantLimit: "LIMIT 51",
		},
		{
			name:      "declared default page",
			paging:    Paging{DefaultLimit: 25},
			target:    "/",
			wantPage:  pageRequest{size: 25},
			wantLimit: "LIMIT 26",
		},
		{
			name:      "a request within the maximum",
			paging:    Paging{DefaultLimit: 25, MaxLimit: 200},
			target:    "/?limit=200",
			wantPage:  pageRequest{size: 200},
			wantLimit: "LIMIT 201",
		},
		{
			name:    "a request over the maximum is refused naming it",
			paging:  Paging{DefaultLimit: 25, MaxLimit: 200},
			target:  "/?limit=201",
			wantErr: "limit 201 exceeds this resource's maximum page size of 200",
		},
		{
			name:    "limit=all is refused where a maximum is declared",
			paging:  Paging{MaxLimit: 200},
			target:  "/?limit=all",
			wantErr: "limit=all is not permitted: this resource serves at most 200 rows per page",
		},
		{
			name:      "limit=all reads every row where no maximum is declared",
			target:    "/?limit=all",
			wantPage:  pageRequest{size: DefaultPageSize, all: true},
			wantLimit: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			decoder := newQueryTestDecoder(t, tt.paging, nil)
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.target, http.NoBody)
			qSet, err := decoder.DecodeWithoutPermissions(req)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("DecodeWithoutPermissions() error = %v, want error containing %q", err, tt.wantErr)
				}
				if !httpio.HasBadRequest(err) {
					t.Errorf("DecodeWithoutPermissions() error = %v, want a 400", err)
				}

				return
			}
			if err != nil {
				t.Fatalf("DecodeWithoutPermissions() error = %v", err)
			}
			if *qSet.page != tt.wantPage {
				t.Errorf("page = %+v, want %+v", *qSet.page, tt.wantPage)
			}
			limit, err := qSet.limitClause()
			if err != nil {
				t.Fatalf("limitClause() error = %v", err)
			}
			if limit != tt.wantLimit {
				t.Errorf("limitClause() = %q, want %q", limit, tt.wantLimit)
			}
		})
	}
}

// TestQuerySet_limitClause_handBuilt pins that a QuerySet built in code reads
// exactly the limit its builder set: the page-plus-one fetch belongs to decoded
// requests alone.
func TestQuerySet_limitClause_handBuilt(t *testing.T) {
	t.Parallel()

	qSet := NewQuerySet(NewMetadata[SortTestResource]())
	if limit, err := qSet.limitClause(); err != nil || limit != "" {
		t.Errorf("limitClause() = %q, %v; want no LIMIT", limit, err)
	}
	qSet.SetLimit(new(uint64(10)))
	if limit, err := qSet.limitClause(); err != nil || limit != "LIMIT 10" {
		t.Errorf("limitClause() = %q, %v; want LIMIT 10", limit, err)
	}
}

// TestQueryDecoder_bindCursor pins how a presented cursor is admitted: sealed under
// the application's key, fingerprinted to this query in this scope, on a list with
// an order worth walking; and how each departure is refused.
func TestQueryDecoder_bindCursor(t *testing.T) {
	t.Parallel()

	key := mustCursorKey(t, testCookieKey)
	grants := map[accesstypes.Permission][]accesstypes.Resource{
		accesstypes.List: {enforcedResource, enforcedResource + ".public", enforcedResource + ".tagged"},
	}
	order := []SortField{{Field: "Public", Direction: SortAscending}, {Field: "ID", Direction: SortAscending}}
	seal := func(t *testing.T, k *CursorKey, scope accesstypes.Scope, filter string, order []SortField, limit string) string {
		t.Helper()

		token, err := k.seal(cursor{Query: queryHash(enforcedResource, scope, filter, order, limit), Direction: pageNext, Keys: []*string{strPtr("m"), strPtr("id")}})
		if err != nil {
			t.Fatal(err)
		}

		return token
	}

	tests := []struct {
		name          string
		paging        Paging
		key           *CursorKey
		target        func(t *testing.T) string
		wantBound     bool
		wantErr       string
		wantBadReq    bool
		wantPlainErr  bool
		wantDirection pageDirection
	}{
		{
			name:          "a genuine cursor for this query binds",
			key:           key,
			target:        func(t *testing.T) string { return "/?sort=public&cursor=" + seal(t, key, testScope, "", order, "50") },
			wantBound:     true,
			wantDirection: pageNext,
		},
		{
			name:          "a cursor issued under the declared order binds on a sort-less request",
			paging:        Paging{Order: []SortField{{Field: "Public", Direction: SortAscending}}},
			key:           key,
			target:        func(t *testing.T) string { return "/?cursor=" + seal(t, key, testScope, "", order, "50") },
			wantBound:     true,
			wantDirection: pageNext,
		},
		{
			name: "a cursor from another tenant's walk is refused",
			key:  key,
			target: func(t *testing.T) string {
				return "/?sort=public&cursor=" + seal(t, key, accesstypes.DomainScope("otherDomain"), "", order, "50")
			},
			wantErr:    "invalid cursor",
			wantBadReq: true,
		},
		{
			name: "a cursor presented with a different filter is refused",
			key:  key,
			target: func(t *testing.T) string {
				return "/?sort=public&filter=public:eq:x&cursor=" + seal(t, key, testScope, "", order, "50")
			},
			wantErr:    "invalid cursor",
			wantBadReq: true,
		},
		{
			name: "a cursor presented with a different sort is refused",
			key:  key,
			target: func(t *testing.T) string {
				return "/?sort=public:desc&cursor=" + seal(t, key, testScope, "", order, "50")
			},
			wantErr:    "invalid cursor",
			wantBadReq: true,
		},
		{
			name: "a cursor presented with a different page size is refused",
			key:  key,
			target: func(t *testing.T) string {
				return "/?sort=public&limit=10&cursor=" + seal(t, key, testScope, "", order, "50")
			},
			wantErr:    "invalid cursor",
			wantBadReq: true,
		},
		{
			name: "a cursor on a primary-key-only order is refused: paging further requires a sort",
			key:  key,
			target: func(t *testing.T) string {
				return "/?cursor=" + seal(t, key, testScope, "", []SortField{{Field: "ID", Direction: SortAscending}}, "50")
			},
			wantErr:    "paging past the first page requires a sort",
			wantBadReq: true,
		},
		{
			name:         "a cursor reaching a decoder with no key is a wiring error, not a client error",
			key:          nil,
			target:       func(t *testing.T) string { return "/?sort=public&cursor=" + seal(t, key, testScope, "", order, "50") },
			wantErr:      "no cursor key",
			wantPlainErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			decoder := newQueryTestDecoder(t, tt.paging, tt.key)
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.target(t), http.NoBody)
			qSet, err := decoder.Decode(req, &fakeUserPermissions{granted: grants}, testScope)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Decode() error = %v, want error containing %q", err, tt.wantErr)
				}
				if httpio.HasBadRequest(err) != tt.wantBadReq {
					t.Errorf("Decode() error = %v, bad request = %v, want %v", err, httpio.HasBadRequest(err), tt.wantBadReq)
				}
				if tt.wantPlainErr && httpio.HasClientMessage(err) {
					t.Errorf("Decode() error = %v, want a server-side error with no client message", err)
				}

				return
			}
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if (qSet.cursor != nil) != tt.wantBound {
				t.Fatalf("cursor bound = %v, want %v", qSet.cursor != nil, tt.wantBound)
			}
			if qSet.cursor.Direction != tt.wantDirection {
				t.Errorf("cursor direction = %v, want %v", qSet.cursor.Direction, tt.wantDirection)
			}
		})
	}
}

// TestQuerySet_stmt_unboundCursorFailsClosed pins that a request decoded without
// permissions, and so without a scope to bind its cursor to, never runs.
func TestQuerySet_stmt_unboundCursorFailsClosed(t *testing.T) {
	t.Parallel()

	decoder := newQueryTestDecoder(t, Paging{}, mustCursorKey(t, testCookieKey))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/?sort=public&cursor=v4.local.abc", http.NoBody)
	qSet, err := decoder.DecodeWithoutPermissions(req)
	if err != nil {
		t.Fatalf("DecodeWithoutPermissions() error = %v", err)
	}
	if _, err := qSet.stmt(SpannerDBType); err == nil || !strings.Contains(err.Error(), "never bound to a scope") {
		t.Errorf("stmt() error = %v, want the unbound-cursor refusal", err)
	}
}
