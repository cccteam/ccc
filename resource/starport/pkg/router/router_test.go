package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestNew(t *testing.T) {
	t.Parallel()

	type testDef struct {
		url            string
		method         string
		wantError      bool
		wantHandler    string
		wantParameters map[string]string
		wantMiddleware map[string]int
	}

	tests := []testDef{
		{
			url: "/does/not/exist/get", method: http.MethodGet,
			wantHandler:    "Assets",
			wantMiddleware: map[string]int{"Logger": 1, "WithParamsHTTP": 1, "SecurityHeaders": 1, "DeepLink": 1},
		},
		{
			url: "/does/not/exist/post", method: http.MethodPost,
			wantMiddleware: map[string]int{"Logger": 1, "WithParamsHTTP": 1, "SecurityHeaders": 1, "StartSession": 1, "SetXSRFToken": 1},
			wantError:      true,
		},
		{
			url: "/does/not/exist/patch", method: http.MethodPatch,
			wantMiddleware: map[string]int{"Logger": 1, "WithParamsHTTP": 1, "SecurityHeaders": 1, "StartSession": 1, "SetXSRFToken": 1},
			wantError:      true,
		},
		{
			url: "/does/not/exist/delete", method: http.MethodDelete,
			wantMiddleware: map[string]int{"Logger": 1, "WithParamsHTTP": 1, "SecurityHeaders": 1, "StartSession": 1, "SetXSRFToken": 1},
			wantError:      true,
		},
		{
			url: "/does/not/exist/put", method: http.MethodPut,
			wantMiddleware: map[string]int{"Logger": 1, "WithParamsHTTP": 1, "SecurityHeaders": 1, "StartSession": 1, "SetXSRFToken": 1},
			wantError:      true,
		},
		{
			url: "/api/not/exist/put", method: http.MethodGet,
			wantMiddleware: map[string]int{"Logger": 1, "NoCaching": 1, "CompressionMiddleware": 1, "WithParamsHTTP": 1, "SecurityHeaders": 1, "StartSession": 1, "SetXSRFToken": 1},
			wantError:      true,
		},
		{
			url: "/api/user/login", method: http.MethodPost,
			wantHandler:    "Login",
			wantMiddleware: map[string]int{"Logger": 1, "NoCaching": 1, "CompressionMiddleware": 1, "WithParamsHTTP": 1, "SecurityHeaders": 1, "StartSession": 1, "SetXSRFToken": 1},
		},
		{
			url: "/api/user/session", method: http.MethodGet,
			wantHandler:    "Authenticated",
			wantMiddleware: map[string]int{"Logger": 1, "NoCaching": 1, "CompressionMiddleware": 1, "WithParamsHTTP": 1, "SecurityHeaders": 1, "StartSession": 1, "SetXSRFToken": 1},
		},
		{
			url: "/api/user/session-data", method: http.MethodGet,
			wantHandler:    "SessionData",
			wantMiddleware: map[string]int{"Logger": 1, "NoCaching": 1, "CompressionMiddleware": 1, "WithParamsHTTP": 1, "SecurityHeaders": 1, "StartSession": 1, "SetXSRFToken": 1},
		},
		{
			url: "/api/user/session", method: http.MethodDelete,
			wantHandler:    "Logout",
			wantMiddleware: map[string]int{"Logger": 1, "NoCaching": 1, "CompressionMiddleware": 1, "WithParamsHTTP": 1, "SecurityHeaders": 1, "StartSession": 1, "SetXSRFToken": 1},
		},
		{
			url: "/api/station-directory", method: http.MethodGet,
			wantHandler:    "StationDirectory",
			wantMiddleware: map[string]int{"Logger": 1, "NoCaching": 1, "CompressionMiddleware": 1, "WithParamsHTTP": 1, "SecurityHeaders": 1, "StartSession": 1, "SetXSRFToken": 1, "ValidateSession": 1, "ValidateXSRFToken": 1},
		},
	}
	for _, r := range generatedRouterTests() {
		tests = append(tests, testDef{
			url:            r.url,
			method:         r.method,
			wantHandler:    r.handlerFunc,
			wantParameters: r.parameters,
			wantMiddleware: map[string]int{"Logger": 1, "NoCaching": 1, "CompressionMiddleware": 1, "WithParamsHTTP": 1, "SecurityHeaders": 1, "StartSession": 1, "SetXSRFToken": 1, "ValidateSession": 1, "ValidateXSRFToken": 1},
		})
	}
	for _, tt := range tests {
		t.Run(tt.method+"-url"+strings.ReplaceAll(tt.url, "/", "-"), func(t *testing.T) {
			t.Parallel()

			rec := newGeneratedCallRecorder()
			got := New(newHandlersStub(rec))

			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.url, http.NoBody)
			rr := httptest.NewRecorder()
			got.ServeHTTP(rr, req)

			if tt.wantError && rr.Code == http.StatusOK {
				t.Error("expected error but did not receive one")
			}

			if tt.wantError {
				return
			}

			if got := rr.Code; got != http.StatusOK {
				t.Errorf("response.Code = %v, want %v", got, http.StatusOK)
			}

			if cnt := len(rec.handlers); cnt != 1 {
				t.Fatalf("expected %d handlers calls, got: %v", 1, rec.handlers)
			}
			if cnt := rec.handlers[tt.wantHandler]; cnt != 1 {
				t.Fatalf("handlers %s, expected %d call, got: %d", tt.wantHandler, 1, cnt)
			}

			if cnt := len(tt.wantMiddleware); cnt != rec.MiddlewareCount() {
				t.Fatalf("expected %d middleware calls, got: %#v", cnt, rec.middlewares)
			}

			for m, c := range tt.wantMiddleware {
				if cnt := rec.MiddlewareCallCount(m); cnt != c {
					t.Fatalf("middleware %s, expected %d call, got: %d", m, c, cnt)
				}
			}

			for key, value := range tt.wantParameters {
				if got := rec.Parameter(tt.wantHandler, key); got != value {
					t.Fatalf("%s = %s, expected %s", key, got, value)
				}
			}
		})
	}
}

// TestNewRouter_securityEnforcement pins the security contract of the API slot: any
// route registered through the composition seam runs behind the full security stack —
// session start, XSRF issuance, and both validation middlewares — independent of what
// the generated route table contains. TestNew above covers the production composition
// (New mounts the generated routes in this slot).
func TestNewRouter_securityEnforcement(t *testing.T) {
	t.Parallel()

	rec := newGeneratedCallRecorder()
	got := newRouter(newHandlersStub(rec), func(r chi.Router) {
		r.Get("/api/probe", rec.RecordHandlerCall("Probe"))
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/probe", http.NoBody)
	rr := httptest.NewRecorder()
	got.ServeHTTP(rr, req)

	if got := rr.Code; got != http.StatusOK {
		t.Errorf("response.Code = %v, want %v", got, http.StatusOK)
	}

	if cnt := rec.handlers["Probe"]; cnt != 1 {
		t.Fatalf("probe handler, expected 1 call, got: %d", cnt)
	}

	wantMiddleware := map[string]int{
		"Logger": 1, "NoCaching": 1, "CompressionMiddleware": 1, "WithParamsHTTP": 1,
		"SecurityHeaders": 1, "StartSession": 1, "SetXSRFToken": 1,
		"ValidateSession": 1, "ValidateXSRFToken": 1,
	}
	if cnt := len(wantMiddleware); cnt != rec.MiddlewareCount() {
		t.Fatalf("expected %d middleware calls, got: %#v", cnt, rec.middlewares)
	}
	for m, c := range wantMiddleware {
		if cnt := rec.MiddlewareCallCount(m); cnt != c {
			t.Fatalf("middleware %s, expected %d call, got: %d", m, c, cnt)
		}
	}
}

// handlersStub satisfies Handlers for the structure test: the generated route surface
// comes from the generated stub, and the handwritten surface (session, middleware,
// demo endpoints, assets) records through the same recorder so middleware counts
// cover both.
type handlersStub struct {
	*generatedHandlersStub
	rec *generatedCallRecorder
}

func newHandlersStub(rec *generatedCallRecorder) *handlersStub {
	return &handlersStub{
		generatedHandlersStub: newGeneratedHandlersStub(rec.RecordHandlerCall),
		rec:                   rec,
	}
}

// session handlers
func (s *handlersStub) Login() http.HandlerFunc {
	return s.rec.RecordHandlerCall("Login")
}

func (s *handlersStub) Logout() http.HandlerFunc {
	return s.rec.RecordHandlerCall("Logout")
}

func (s *handlersStub) Authenticated() http.HandlerFunc {
	return s.rec.RecordHandlerCall("Authenticated")
}

// user-management handlers the router does not route; they exist to satisfy
// session.PasswordAuthHandlers.
func (s *handlersStub) ActivateUser() http.HandlerFunc {
	return s.rec.RecordHandlerCall("ActivateUser")
}

func (s *handlersStub) ChangeUsername() http.HandlerFunc {
	return s.rec.RecordHandlerCall("ChangeUsername")
}

func (s *handlersStub) ChangeUserPassword() http.HandlerFunc {
	return s.rec.RecordHandlerCall("ChangeUserPassword")
}

func (s *handlersStub) CreateUser() http.HandlerFunc {
	return s.rec.RecordHandlerCall("CreateUser")
}

func (s *handlersStub) DeactivateUser() http.HandlerFunc {
	return s.rec.RecordHandlerCall("DeactivateUser")
}

func (s *handlersStub) DeleteUser() http.HandlerFunc {
	return s.rec.RecordHandlerCall("DeleteUser")
}

// session middleware
func (s *handlersStub) StartSession(next http.Handler) http.Handler {
	return s.rec.RecordMiddlewareCall("StartSession")(next)
}

func (s *handlersStub) ValidateSession(next http.Handler) http.Handler {
	return s.rec.RecordMiddlewareCall("ValidateSession")(next)
}

func (s *handlersStub) SetXSRFToken(next http.Handler) http.Handler {
	return s.rec.RecordMiddlewareCall("SetXSRFToken")(next)
}

func (s *handlersStub) ValidateXSRFToken(next http.Handler) http.Handler {
	return s.rec.RecordMiddlewareCall("ValidateXSRFToken")(next)
}

// app middleware
func (s *handlersStub) LoggerMiddleware() func(http.Handler) http.Handler {
	return s.rec.RecordMiddlewareCall("Logger")
}

func (s *handlersStub) SecurityHeaders(next http.Handler) http.Handler {
	return s.rec.RecordMiddlewareCall("SecurityHeaders")(next)
}

func (s *handlersStub) WithParamsHTTP() func(http.Handler) http.Handler {
	return s.rec.RecordMiddlewareCall("WithParamsHTTP")
}

func (s *handlersStub) NoCaching(next http.Handler) http.Handler {
	return s.rec.RecordMiddlewareCall("NoCaching")(next)
}

func (s *handlersStub) CompressionMiddleware() func(http.Handler) http.Handler {
	return s.rec.RecordMiddlewareCall("CompressionMiddleware")
}

// demo endpoints
func (s *handlersStub) SessionData() http.HandlerFunc {
	return s.rec.RecordHandlerCall("SessionData")
}

func (s *handlersStub) StationDirectory() http.HandlerFunc {
	return s.rec.RecordHandlerCall("StationDirectory")
}

// Angular app assets
func (s *handlersStub) DeepLink(next http.Handler) http.Handler {
	return s.rec.RecordMiddlewareCall("DeepLink")(next)
}

func (s *handlersStub) Assets() http.HandlerFunc {
	return s.rec.RecordHandlerCall("Assets")
}
