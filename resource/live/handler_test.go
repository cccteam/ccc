package live

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/session/sessioninfo"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// withSession seeds the session identity the way the session middleware would.
func withSession(ctx context.Context, user string) context.Context {
	return context.WithValue(ctx, sessioninfo.CtxSessionInfo, &sessioninfo.SessionData{
		SessionInfo: &sessioninfo.SessionInfo{Username: user},
	})
}

// withRoleSession seeds a session established as a role by actor.
func withRoleSession(ctx context.Context, actor string, role accesstypes.Role) context.Context {
	return context.WithValue(ctx, sessioninfo.CtxSessionInfo, &sessioninfo.SessionData{
		SessionInfo: &sessioninfo.SessionInfo{Username: actor},
		Principal:   accesstypes.RolePrincipal(role),
	})
}

// gateFunc is a Gate answering through a function.
type gateFunc func(ctx context.Context) (bool, error)

func (g gateFunc) Permitted(ctx context.Context) (bool, error) {
	return g(ctx)
}

// permit, refuse and failing are the gates the subscribe tests answer with: the
// request allowed, the request refused, and the permission check itself failing.
func permit(context.Context) (bool, error) {
	return true, nil
}

func refuse(context.Context) (bool, error) {
	return false, nil
}

func failing(context.Context) (bool, error) {
	return false, errors.New("engine down")
}

// passing is a handler that answers 200 and records that it ran.
func passing(ran *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*ran = true
		w.WriteHeader(http.StatusOK)
	})
}

func TestSubscribing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		header     string
		served     bool
		wantStatus int
		wantRan    bool
		wantBody   string
	}{
		{name: "a request without the header passes", served: true, wantStatus: http.StatusOK, wantRan: true},
		{name: "a request without the header passes an application serving no live pages too", wantStatus: http.StatusOK, wantRan: true},
		{name: "a subscribing request passes when live pages are served", header: "tab-1", served: true, wantStatus: http.StatusOK, wantRan: true},
		{name: "a subscribing request is refused when live pages are not served", header: "tab-1", wantStatus: http.StatusBadRequest, wantBody: "live subscriptions are not served"},
		{name: "a malformed tab is refused naming the header", header: "tab 1", served: true, wantStatus: http.StatusBadRequest, wantBody: "invalid X-Subscribe value"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var svc Service
			if tt.served {
				svc = NewFake()
			}
			ran := false
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/ships", http.NoBody)
			if tt.header != "" {
				req.Header.Set(SubscribeHeader, tt.header)
			}
			rr := httptest.NewRecorder()
			Subscribing(svc)(passing(&ran)).ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d: %s", rr.Code, tt.wantStatus, rr.Body.String())
			}
			if ran != tt.wantRan {
				t.Errorf("handler ran = %v, want %v", ran, tt.wantRan)
			}
			if tt.wantBody != "" && !strings.Contains(rr.Body.String(), tt.wantBody) {
				t.Errorf("body = %q, want it to contain %q", rr.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestRefusing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		header     string
		wantStatus int
		wantRan    bool
	}{
		{name: "a request without the header passes", wantStatus: http.StatusOK, wantRan: true},
		{name: "a subscribing request is refused on an outlet without sessions", header: "tab-1", wantStatus: http.StatusBadRequest},
		{name: "a malformed subscribing request is refused the same way", header: "tab 1", wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ran := false
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/droids/beacons", http.NoBody)
			if tt.header != "" {
				req.Header.Set(SubscribeHeader, tt.header)
			}
			rr := httptest.NewRecorder()
			Refusing()(passing(&ran)).ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rr.Code, tt.wantStatus)
			}
			if ran != tt.wantRan {
				t.Errorf("handler ran = %v, want %v", ran, tt.wantRan)
			}
			if tt.header != "" && !strings.Contains(rr.Body.String(), SubscribeHeader) {
				t.Errorf("body = %q, want it to name %s", rr.Body.String(), SubscribeHeader)
			}
		})
	}
}

func TestSubscribe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		header string
		served bool
		gate   gateFunc
		sub    *Subscription
		want   []Subscription
	}{
		{
			name:   "a permitted list request registers the list before the query",
			header: "tab-1",
			served: true,
			gate:   permit,
			sub:    ListSubscription("Ships", "anvil"),
			want:   []Subscription{{Principal: "dispatcher", Tab: "tab-1", Resource: "Ships", Domain: "anvil"}},
		},
		{
			name:   "a permitted read registers the row, with no domain",
			header: "tab-1",
			served: true,
			gate:   permit,
			sub:    &Subscription{Resource: "Ships", Key: "s1", Domain: "anvil"},
			want:   []Subscription{{Principal: "dispatcher", Tab: "tab-1", Resource: "Ships", Key: "s1"}},
		},
		{
			name:   "a refused request registers nothing",
			header: "tab-1",
			served: true,
			gate:   refuse,
			sub:    ListSubscription("Ships", "anvil"),
		},
		{
			name:   "a gate that fails registers nothing and the request goes on",
			header: "tab-1",
			served: true,
			gate:   failing,
			sub:    ListSubscription("Ships", "anvil"),
		},
		{
			name:   "a request without the header registers nothing",
			served: true,
			gate:   permit,
			sub:    ListSubscription("Ships", "anvil"),
		},
		{
			name:   "no live service registers nothing",
			header: "tab-1",
			gate:   permit,
			sub:    ListSubscription("Ships", "anvil"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fake := NewFake()
			var svc Service
			if tt.served {
				svc = fake
			}
			ctx := withSession(t.Context(), "dispatcher")
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/ships", http.NoBody)
			if tt.header != "" {
				req.Header.Set(SubscribeHeader, tt.header)
			}

			Subscribe(ctx, req, svc, tt.gate, tt.sub)

			got := fake.Subscriptions()
			if diff := cmp.Diff(tt.want, got, cmpopts.IgnoreFields(Subscription{}, "Expiry"), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Subscriptions() mismatch (-want +got):\n%s", diff)
			}
			for _, sub := range got {
				if sub.Expiry.IsZero() {
					t.Errorf("subscription %s has no expiry", sub.ID())
				}
			}
		})
	}
}

func TestSubscribe_recordFailureIsLogged(t *testing.T) {
	t.Parallel()

	fake := NewFake()
	fake.Err = errors.New("record down")
	ctx := withSession(t.Context(), "dispatcher")
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/ships", http.NoBody)
	req.Header.Set(SubscribeHeader, "tab-1")

	// The request is still answered: Subscribe returns, nothing panics.
	Subscribe(ctx, req, fake, gateFunc(permit), ListSubscription("Ships", "anvil"))
}

func TestSetCacheControl(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		url         string
		wantControl string
		wantPragma  string
		wantExpires string
	}{
		{name: "a request carrying the version parameter is cacheable", url: "/api/ships?_v=1727900000123456", wantControl: CacheControl},
		{name: "a request without it keeps the outlet's no-cache headers", url: "/api/ships", wantControl: "no-cache, no-store, must-revalidate", wantPragma: "no-cache", wantExpires: "0"},
		{name: "another parameter is not the version", url: "/api/ships?v=1", wantControl: "no-cache, no-store, must-revalidate", wantPragma: "no-cache", wantExpires: "0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.url, http.NoBody)
			rr := httptest.NewRecorder()
			// What the outlet's NoCaching middleware set before the handler ran.
			rr.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			rr.Header().Set("Pragma", "no-cache")
			rr.Header().Set("Expires", "0")

			SetCacheControl(rr, req)

			if got := rr.Header().Get("Cache-Control"); got != tt.wantControl {
				t.Errorf("Cache-Control = %q, want %q", got, tt.wantControl)
			}
			if got := rr.Header().Get("Pragma"); got != tt.wantPragma {
				t.Errorf("Pragma = %q, want %q", got, tt.wantPragma)
			}
			if got := rr.Header().Get("Expires"); got != tt.wantExpires {
				t.Errorf("Expires = %q, want %q", got, tt.wantExpires)
			}
		})
	}
}

func TestPublish(t *testing.T) {
	t.Parallel()

	type write struct {
		domain accesstypes.Domain
		res    accesstypes.Resource
		key    string
	}
	tests := []struct {
		name   string
		served bool
		writes []write
		domain accesstypes.Domain
		want   []FakePublish
	}{
		{
			name:   "a committed request publishes under its route domain",
			served: true,
			writes: []write{{res: "Ships", key: "s1"}},
			domain: "anvil",
			want:   []FakePublish{{Domain: "anvil", Touched: map[accesstypes.Resource][]resource.RowChange{"Ships": {{Key: "s1"}}}}},
		},
		{
			name:   "rows decoded in their own tenant publish under it",
			served: true,
			writes: []write{{domain: "bastion", res: "Hangars", key: "h1"}},
			domain: "",
			want:   []FakePublish{{Domain: "bastion", Touched: map[accesstypes.Resource][]resource.RowChange{"Hangars": {{Key: "h1"}}}}},
		},
		{name: "a request that committed nothing publishes nothing", served: true, domain: "anvil"},
		{name: "no live service publishes nothing", writes: []write{{res: "Ships", key: "s1"}}, domain: "anvil"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fake := NewFake()
			var svc Service
			if tt.served {
				svc = fake
			}
			ctx, touched := resource.CollectTouchedRows(t.Context())
			client := resource.NewMockClient(&bufferingTxn{}, nil, nil)
			for _, w := range tt.writes {
				if err := client.ExecuteFunc(ctx, func(ctx context.Context, txn resource.ReadWriteTransaction) error {
					return bufferRow(ctx, txn, w.domain, w.key)
				}); err != nil {
					t.Fatalf("ExecuteFunc() error = %v", err)
				}
			}

			Publish(ctx, svc, tt.domain, touched)

			if diff := cmp.Diff(tt.want, fake.Publishes(), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Publishes() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPublish_failureIsLogged(t *testing.T) {
	t.Parallel()

	fake := NewFake()
	fake.Err = errors.New("firestore down")
	ctx, touched := resource.CollectTouchedRows(t.Context())
	client := resource.NewMockClient(&bufferingTxn{}, nil, nil)
	if err := client.ExecuteFunc(ctx, func(ctx context.Context, txn resource.ReadWriteTransaction) error {
		return bufferRow(ctx, txn, "", "s1")
	}); err != nil {
		t.Fatalf("ExecuteFunc() error = %v", err)
	}

	// The request still answers: Publish returns, nothing panics.
	Publish(ctx, fake, "anvil", touched)
}

func TestPrincipalID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ctx  context.Context
		want string
	}{
		{name: "a user principal is the user", ctx: withSession(context.Background(), "mechanic"), want: "mechanic"},
		{name: "a role principal is the role, marked", ctx: withRoleSession(context.Background(), "alice", "Auditor"), want: "role:Auditor"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := PrincipalID(tt.ctx); got != tt.want {
				t.Errorf("PrincipalID() = %q, want %q", got, tt.want)
			}
		})
	}
}
