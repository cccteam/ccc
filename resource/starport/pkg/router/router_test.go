package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cccteam/ccc/resource/starport/pkg/mock/mock_router"
	"github.com/go-chi/chi/v5"
	"go.uber.org/mock/gomock"
)

func TestNew(t *testing.T) {
	t.Parallel()

	for _, tt := range generatedRouterTests() {
		t.Run(tt.method+"-url"+strings.ReplaceAll(tt.url, "/", "-"), func(t *testing.T) {
			t.Parallel()

			rec := newCallRecorder()
			ctrl := gomock.NewController(t)
			handlers := mock_router.NewMockHandlers(ctrl)
			generatedExpectCalls(handlers.EXPECT(), rec)

			got := New(handlers)

			req, err := http.NewRequestWithContext(t.Context(), tt.method, tt.url, http.NoBody)
			if err != nil {
				t.Fatal(err)
			}

			rr := httptest.NewRecorder()
			got.ServeHTTP(rr, req)

			if got := rr.Code; got != http.StatusOK {
				t.Errorf("response.Code = %v, want %v", got, http.StatusOK)
			}

			if cnt := len(rec.handlers); cnt != 1 {
				t.Fatalf("expected %d handler calls, got: %v", 1, rec.handlers)
			}
			if cnt := rec.handlers[tt.handlerFunc]; cnt != 1 {
				t.Fatalf("handler %s, expected %d call, got: %d", tt.handlerFunc, 1, cnt)
			}

			for key, value := range tt.parameters {
				if got := rec.Parameter(tt.handlerFunc, key); got != value {
					t.Fatalf("%s = %s, expected %s", key, got, value)
				}
			}
		})
	}
}

type callRecorder struct {
	handlers   map[string]int
	parameters map[string]map[string]string
}

func newCallRecorder() *callRecorder {
	return &callRecorder{
		handlers:   make(map[string]int),
		parameters: make(map[string]map[string]string),
	}
}

func (rec *callRecorder) RecordHandlerCall(name string) http.HandlerFunc {
	return func(_ http.ResponseWriter, r *http.Request) {
		for _, key := range generatedRouteParameters() {
			if value := chi.URLParam(r, key); value != "" {
				if _, found := rec.parameters[name]; !found {
					rec.parameters[name] = make(map[string]string)
				}
				rec.parameters[name][key] = value
			}
		}

		rec.handlers[name]++
	}
}

func (rec *callRecorder) Parameter(name, key string) string {
	if _, ok := rec.parameters[name]; !ok {
		return ""
	}

	return rec.parameters[name][key]
}
