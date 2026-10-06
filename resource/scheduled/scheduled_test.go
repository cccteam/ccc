package scheduled

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/api/idtoken"
)

// TestGuard drives a scheduled call through the guard: a token Google issued for the
// endpoint's URL carrying the invoker as its verified email runs the handler, and every
// other call answers 401 without reaching it, the reason in the guard's check and never
// in the answer.
func TestGuard(t *testing.T) {
	t.Parallel()

	const (
		invoker = "imp-tst-gbl-app-sched@imp-tst-gbl-core.iam.gserviceaccount.com"
		host    = "app.example.com"
		path    = Prefix + "/prune-droid-reports"
	)
	audience := "https://" + host + path
	fake := NewFake()
	tests := []struct {
		name          string
		guard         *Guard
		authorization string
		wantStatus    int
		wantReason    string
	}{
		{name: "a valid token runs the handler", guard: NewGuard(invoker, fake), authorization: bearerPrefix + fake.Mint(audience, invoker), wantStatus: http.StatusOK},
		{name: "a call without a token is refused", guard: NewGuard(invoker, fake), wantStatus: http.StatusUnauthorized, wantReason: "carries no bearer token"},
		{name: "a credential that is not a bearer token is refused", guard: NewGuard(invoker, fake), authorization: "Basic " + fake.Mint(audience, invoker), wantStatus: http.StatusUnauthorized, wantReason: "carries no bearer token"},
		{name: "a token Google did not sign is refused", guard: NewGuard(invoker, fake), authorization: bearerPrefix + "forged", wantStatus: http.StatusUnauthorized, wantReason: "does not verify for the audience " + audience},
		{name: "a token for another endpoint is refused: a bad audience", guard: NewGuard(invoker, fake), authorization: bearerPrefix + fake.Mint("https://"+host+Prefix+"/another-route", invoker), wantStatus: http.StatusUnauthorized, wantReason: "does not verify for the audience " + audience},
		{name: "a token of another identity is refused: the wrong email", guard: NewGuard(invoker, fake), authorization: bearerPrefix + fake.Mint(audience, "someone@example.com"), wantStatus: http.StatusUnauthorized, wantReason: `the token's email "someone@example.com" is not the invoker`},
		{
			name: "a token whose email is not verified is refused", guard: NewGuard(invoker, fake),
			authorization: bearerPrefix + fake.Issue(&idtoken.Payload{Issuer: googleIssuers[0], Audience: audience, Claims: map[string]any{emailClaim: invoker, emailVerifiedClaim: false}}),
			wantStatus:    http.StatusUnauthorized, wantReason: "carries no verified email",
		},
		{
			name: "a token another issuer issued is refused", guard: NewGuard(invoker, fake),
			authorization: bearerPrefix + fake.Issue(&idtoken.Payload{Issuer: "https://issuer.example.com", Audience: audience, Claims: map[string]any{emailClaim: invoker, emailVerifiedClaim: true}}),
			wantStatus:    http.StatusUnauthorized, wantReason: `the token's issuer "https://issuer.example.com" is not Google's`,
		},
		{name: "with no invoker the routes are off and a valid token is refused", guard: NewGuard("", fake), authorization: bearerPrefix + fake.Mint(audience, invoker), wantStatus: http.StatusUnauthorized, wantReason: "the scheduled routes are off"},
		{name: "a nil guard refuses", authorization: bearerPrefix + fake.Mint(audience, invoker), wantStatus: http.StatusUnauthorized, wantReason: "the scheduled routes are off"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, audience, http.NoBody)
			if tt.authorization != "" {
				req.Header.Set("Authorization", tt.authorization)
			}
			called := false
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			})
			rr := httptest.NewRecorder()
			tt.guard.Middleware(next).ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d: %s", rr.Code, tt.wantStatus, rr.Body.String())
			}
			if want := tt.wantStatus == http.StatusOK; called != want {
				t.Errorf("handler called = %v, want %v", called, want)
			}
			err := tt.guard.Check(req)
			if tt.wantReason == "" {
				if err != nil {
					t.Errorf("Check() = %v, want nil", err)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantReason) {
				t.Fatalf("Check() = %v, want a reason containing %q", err, tt.wantReason)
			}
			if strings.Contains(rr.Body.String(), tt.wantReason) {
				t.Errorf("the answer carries the reason %q: %s", tt.wantReason, rr.Body.String())
			}
		})
	}
}

// TestFromEnvironment reads the invoker from APP_SCHEDULER_INVOKER: set, the guard
// checks that identity's tokens; empty, the scheduled routes are off. It sets the
// process's environment, so it does not run in parallel.
func TestFromEnvironment(t *testing.T) {
	tests := []struct {
		name        string
		invoker     string
		wantEnabled bool
	}{
		{name: "an invoker turns the routes on", invoker: "imp-tst-gbl-app-sched@imp-tst-gbl-core.iam.gserviceaccount.com", wantEnabled: true},
		{name: "no invoker leaves them off", invoker: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(InvokerVariable, tt.invoker)

			guard, err := FromEnvironment(t.Context())
			if err != nil {
				t.Fatalf("FromEnvironment() error = %v", err)
			}
			if got := guard.Enabled(); got != tt.wantEnabled {
				t.Errorf("Enabled() = %v, want %v", got, tt.wantEnabled)
			}
		})
	}
}

// TestAudience is the URL a scheduled call's token is minted for: https, the host the
// call names, and the route's path without its query.
func TestAudience(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
		want string
	}{
		{name: "the host and the path", url: "http://app.example.com/_scheduled/prune-droid-reports", want: "https://app.example.com/_scheduled/prune-droid-reports"},
		{name: "a query is not part of it", url: "http://app.example.com/_scheduled/prune-droid-reports?x=1", want: "https://app.example.com/_scheduled/prune-droid-reports"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, tt.url, http.NoBody)
			if got := Audience(req); got != tt.want {
				t.Errorf("Audience() = %q, want %q", got, tt.want)
			}
		})
	}
}
