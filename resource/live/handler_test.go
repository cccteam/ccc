package live

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/httpio"
	"github.com/cccteam/session/sessioninfo"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// The auths the tests sign people in through: a password auth and a directory auth over
// one database, as an application with two populations has.
const (
	crewAuth    = "crew"
	membersAuth = "members"
)

// withAuth binds the auth's name the way an outlet's Subscribing middleware does.
func withAuth(ctx context.Context, auth string) context.Context {
	return context.WithValue(ctx, authKey{}, auth)
}

// withUser seeds the session identity the way the session middleware would, with no
// auth bound: what a request carries before the outlet's Subscribing runs.
func withUser(ctx context.Context, user string) context.Context {
	return context.WithValue(ctx, sessioninfo.CtxSessionInfo, &sessioninfo.SessionData{
		SessionInfo: &sessioninfo.SessionInfo{Username: user},
	})
}

// withSession seeds the session identity behind an outlet bound to auth.
func withSession(ctx context.Context, auth, user string) context.Context {
	return withUser(withAuth(ctx, auth), user)
}

// withRoleSession seeds a session established as a role by actor, behind an outlet bound
// to auth.
func withRoleSession(ctx context.Context, auth, actor string, role accesstypes.Role) context.Context {
	return context.WithValue(withAuth(ctx, auth), sessioninfo.CtxSessionInfo, &sessioninfo.SessionData{
		SessionInfo: &sessioninfo.SessionInfo{Username: actor},
		Principal:   accesstypes.RolePrincipal(role),
	})
}

// withViewAsSession seeds a session in which actor views the application as user,
// behind an outlet bound to auth.
func withViewAsSession(ctx context.Context, auth, actor, user string) context.Context {
	return context.WithValue(withAuth(ctx, auth), sessioninfo.CtxSessionInfo, &sessioninfo.SessionData{
		SessionInfo: &sessioninfo.SessionInfo{Username: actor},
		Principal:   accesstypes.UserPrincipal(accesstypes.User(user)),
	})
}

// longName is a user name that makes the crew auth's principal id exactly n bytes.
func longName(n int) string {
	return strings.Repeat("a", n-len(crewAuth+AuthSeparator))
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
		auth       string
		header     string
		wantStatus int
		wantRan    bool
		wantID     string
		wantBody   string
	}{
		{name: "a request without the header passes with the auth bound", auth: crewAuth, wantStatus: http.StatusOK, wantRan: true, wantID: "crew|dispatcher"},
		{name: "a subscribing request passes with the auth bound", auth: crewAuth, header: "tab-1", wantStatus: http.StatusOK, wantRan: true, wantID: "crew|dispatcher"},
		{name: "another outlet binds its own auth", auth: membersAuth, header: "tab-1", wantStatus: http.StatusOK, wantRan: true, wantID: "members|dispatcher"},
		{name: "a malformed tab is refused naming the header", auth: crewAuth, header: "tab 1", wantStatus: http.StatusBadRequest, wantBody: "invalid X-Subscribe value"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ran, gotID := false, ""
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ran = true
				id, err := PrincipalID(r.Context())
				if err != nil {
					t.Errorf("PrincipalID() error = %v", err)
				}
				gotID = id
				w.WriteHeader(http.StatusOK)
			})
			req := httptest.NewRequestWithContext(withUser(t.Context(), "dispatcher"), http.MethodGet, "/api/ships", http.NoBody)
			if tt.header != "" {
				req.Header.Set(SubscribeHeader, tt.header)
			}
			rr := httptest.NewRecorder()
			Subscribing(tt.auth)(next).ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d: %s", rr.Code, tt.wantStatus, rr.Body.String())
			}
			if ran != tt.wantRan {
				t.Errorf("handler ran = %v, want %v", ran, tt.wantRan)
			}
			if gotID != tt.wantID {
				t.Errorf("principal id = %q, want %q", gotID, tt.wantID)
			}
			if tt.wantBody != "" && !strings.Contains(rr.Body.String(), tt.wantBody) {
				t.Errorf("body = %q, want it to contain %q", rr.Body.String(), tt.wantBody)
			}
		})
	}
}

// TestSubscribing_authName proves the refusal of an auth name that cannot begin a
// principal id: it panics as the routes are registered, naming what is wrong.
func TestSubscribing_authName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		auth      string
		wantPanic string
	}{
		{name: "an auth's name is accepted", auth: crewAuth},
		{name: "a name with a hyphen is accepted", auth: "field-crew"},
		{name: "an empty name is refused", auth: "", wantPanic: "the auth's name is empty"},
		{name: "a name carrying the separator is refused", auth: "crew|night", wantPanic: `cannot carry "|"`},
		{name: "a name carrying a slash is refused", auth: "crew/night", wantPanic: `cannot carry "/"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got string
			func() {
				defer func() {
					if r := recover(); r != nil {
						got = fmt.Sprint(r)
					}
				}()
				Subscribing(tt.auth)
			}()

			switch {
			case tt.wantPanic == "" && got != "":
				t.Errorf("Subscribing(%q) panicked: %s", tt.auth, got)
			case tt.wantPanic != "" && !strings.Contains(got, tt.wantPanic):
				t.Errorf("Subscribing(%q) panic = %q, want it to contain %q", tt.auth, got, tt.wantPanic)
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
		ctx    func(ctx context.Context) context.Context
		header string
		gate   gateFunc
		sub    *Subscription
		want   []Subscription
	}{
		{
			name:   "a permitted list request registers the list before the query",
			header: "tab-1",
			gate:   permit,
			sub:    ListSubscription("Ships", "anvil"),
			want:   []Subscription{{Principal: "crew|dispatcher", Tab: "tab-1", Resource: "Ships", Domain: "anvil"}},
		},
		{
			name:   "a permitted read registers the row, with no domain",
			header: "tab-1",
			gate:   permit,
			sub:    &Subscription{Resource: "Ships", Key: "s1", Domain: "anvil"},
			want:   []Subscription{{Principal: "crew|dispatcher", Tab: "tab-1", Resource: "Ships", Key: "s1"}},
		},
		{
			name: "the same name through another auth registers under that auth",
			ctx: func(ctx context.Context) context.Context {
				return withSession(ctx, membersAuth, "dispatcher")
			},
			header: "tab-1",
			gate:   permit,
			sub:    ListSubscription("Ships", "anvil"),
			want:   []Subscription{{Principal: "members|dispatcher", Tab: "tab-1", Resource: "Ships", Domain: "anvil"}},
		},
		{
			name:   "a refused request registers nothing",
			header: "tab-1",
			gate:   refuse,
			sub:    ListSubscription("Ships", "anvil"),
		},
		{
			name:   "a gate that fails registers nothing and the request goes on",
			header: "tab-1",
			gate:   failing,
			sub:    ListSubscription("Ships", "anvil"),
		},
		{
			name: "a request without the header registers nothing",
			gate: permit,
			sub:  ListSubscription("Ships", "anvil"),
		},
		{
			name: "a principal id over the uid limit registers nothing and the request goes on",
			ctx: func(ctx context.Context) context.Context {
				return withSession(ctx, crewAuth, longName(MaxPrincipalIDLength+1))
			},
			header: "tab-1",
			gate:   permit,
			sub:    ListSubscription("Ships", "anvil"),
		},
		{
			name: "a request no auth was bound to registers nothing and the request goes on",
			ctx: func(ctx context.Context) context.Context {
				return withUser(ctx, "dispatcher")
			},
			header: "tab-1",
			gate:   permit,
			sub:    ListSubscription("Ships", "anvil"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fake := NewFake()
			ctx := withSession(t.Context(), crewAuth, "dispatcher")
			if tt.ctx != nil {
				ctx = tt.ctx(t.Context())
			}
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/ships", http.NoBody)
			if tt.header != "" {
				req.Header.Set(SubscribeHeader, tt.header)
			}

			Subscribe(ctx, req, fake, tt.gate, tt.sub)

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
	ctx := withSession(t.Context(), crewAuth, "dispatcher")
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
		writes []write
		domain accesstypes.Domain
		want   []FakePublish
	}{
		{
			name:   "a committed request publishes under its route domain",
			writes: []write{{res: "Ships", key: "s1"}},
			domain: "anvil",
			want:   []FakePublish{{Domain: "anvil", Touched: map[accesstypes.Resource][]resource.RowChange{"Ships": {{Key: "s1"}}}}},
		},
		{
			name:   "rows decoded in their own tenant publish under it",
			writes: []write{{domain: "bastion", res: "Hangars", key: "h1"}},
			domain: "",
			want:   []FakePublish{{Domain: "bastion", Touched: map[accesstypes.Resource][]resource.RowChange{"Hangars": {{Key: "h1"}}}}},
		},
		{name: "a request that committed nothing publishes nothing", domain: "anvil"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fake := NewFake()
			ctx, touched := resource.CollectTouchedRows(t.Context())
			client := resource.NewMockClient(&bufferingTxn{}, nil, nil)
			for _, w := range tt.writes {
				if err := client.ExecuteFunc(ctx, func(ctx context.Context, txn resource.ReadWriteTransaction) error {
					return bufferRow(ctx, txn, w.domain, w.key)
				}); err != nil {
					t.Fatalf("ExecuteFunc() error = %v", err)
				}
			}

			Publish(ctx, fake, tt.domain, touched)

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
		name        string
		ctx         context.Context
		want        string
		wantErr     string
		wantRefusal bool
	}{
		{name: "a password user is the user, qualified by the password auth", ctx: withSession(context.Background(), crewAuth, "mechanic"), want: "crew|mechanic"},
		{name: "a directory user is the email, qualified by the directory auth", ctx: withSession(context.Background(), membersAuth, "mechanic@example.com"), want: "members|mechanic@example.com"},
		{name: "a password user named like an email stays the password auth's", ctx: withSession(context.Background(), crewAuth, "mechanic@example.com"), want: "crew|mechanic@example.com"},
		{name: "a role session is the role, marked, qualified by its auth", ctx: withRoleSession(context.Background(), crewAuth, "alice", "Auditor"), want: "crew|role:Auditor"},
		{name: "a role session through the directory auth is that auth's", ctx: withRoleSession(context.Background(), membersAuth, "alice@example.com", "Auditor"), want: "members|role:Auditor"},
		{name: "a view-as session is the user viewed, qualified by the auth", ctx: withViewAsSession(context.Background(), crewAuth, "alice", "mechanic"), want: "crew|mechanic"},
		{name: "a user name holding the separator keeps it after the auth's", ctx: withSession(context.Background(), crewAuth, "night|shift"), want: "crew|night|shift"},
		{name: "an id of exactly the uid limit is served", ctx: withSession(context.Background(), crewAuth, longName(MaxPrincipalIDLength)), want: "crew|" + longName(MaxPrincipalIDLength)},
		{
			name:        "an id one byte over the uid limit is refused, never shortened",
			ctx:         withSession(context.Background(), crewAuth, longName(MaxPrincipalIDLength+1)),
			wantErr:     "129 bytes, over Firebase's uid limit of 128",
			wantRefusal: true,
		},
		{
			name:        "a role session over the uid limit is refused the same way",
			ctx:         withRoleSession(context.Background(), crewAuth, "alice", accesstypes.Role(longName(MaxPrincipalIDLength))),
			wantErr:     "over Firebase's uid limit of 128",
			wantRefusal: true,
		},
		{
			name:    "a request no auth was bound to fails",
			ctx:     withUser(context.Background(), "mechanic"),
			wantErr: "no auth is bound to the request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := PrincipalID(tt.ctx)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("PrincipalID() error = %v", err)
				}
				if got != tt.want {
					t.Errorf("PrincipalID() = %q, want %q", got, tt.want)
				}
				if len(got) > MaxPrincipalIDLength {
					t.Errorf("PrincipalID() = %d bytes, over %d", len(got), MaxPrincipalIDLength)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("PrincipalID() error = %v, want it to contain %q", err, tt.wantErr)
			}
			if got != "" {
				t.Errorf("PrincipalID() = %q alongside its error, want nothing", got)
			}
			if refused := httpio.Message(err) != ""; refused != tt.wantRefusal {
				t.Errorf("PrincipalID() client message = %q, want one: %v", httpio.Message(err), tt.wantRefusal)
			}
		})
	}
}

// TestPrincipalID_twoAuthsOneName is the collision the qualification exists for: a
// password user and a directory user who share one name are two principal ids.
func TestPrincipalID_twoAuthsOneName(t *testing.T) {
	t.Parallel()

	const shared = "alice@example.com"
	tests := []struct {
		name string
		ctx  context.Context
		want string
	}{
		{name: "the password user", ctx: withSession(context.Background(), crewAuth, shared), want: "crew|alice@example.com"},
		{name: "the directory user", ctx: withSession(context.Background(), membersAuth, shared), want: "members|alice@example.com"},
	}

	seen := make(map[string]string, len(tests))
	for _, tt := range tests {
		got, err := PrincipalID(tt.ctx)
		if err != nil {
			t.Fatalf("%s: PrincipalID() error = %v", tt.name, err)
		}
		if got != tt.want {
			t.Errorf("%s: PrincipalID() = %q, want %q", tt.name, got, tt.want)
		}
		if prior, ok := seen[got]; ok {
			t.Errorf("%s and %s share the principal id %q", prior, tt.name, got)
		}
		seen[got] = tt.name
	}
}
