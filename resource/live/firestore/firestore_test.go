package firestore_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/live"
	livefirestore "github.com/cccteam/ccc/resource/live/firestore"
	"github.com/cccteam/session/sessioninfo"
	"github.com/go-playground/errors/v5"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// resourceFor names a resource after the test, so parallel tests on the shared
// emulator never see each other's subscriptions.
func resourceFor(t *testing.T) accesstypes.Resource {
	t.Helper()

	return accesstypes.Resource(strings.NewReplacer("/", "_", " ", "_", ",", "", ":", "").Replace(t.Name()))
}

// principalFor names a principal after the test, for the same isolation.
func principalFor(t *testing.T, who string) string {
	t.Helper()

	return who + "-" + string(resourceFor(t))
}

// without drops the expiry for comparison: the record returns what was written.
var ignoreExpiry = cmpopts.IgnoreFields(live.Subscription{}, "Expiry")

func TestService_lookups(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		// subs are written before the lookup; principals and resources are made unique
		// per test.
		subs   []live.Subscription
		lookup func(t *testing.T, svc *livefirestore.Service, res accesstypes.Resource) ([]live.Subscription, error)
		want   []live.Subscription
	}{
		{
			name: "the subscribers of a row: its readers, not the list's, not an expired one",
			subs: []live.Subscription{
				{Principal: "p1", Tab: "t1", Key: "k1"},
				{Principal: "p2", Tab: "t2", Domain: "anvil"},
				{Principal: "p3", Tab: "t3", Key: "k2"},
				{Principal: "p4", Tab: "t4", Key: "k1", Expiry: now.Add(-time.Second)},
				{Principal: "p5", Tab: "t5", Key: "k1", Domain: "anvil"},
			},
			lookup: func(t *testing.T, svc *livefirestore.Service, res accesstypes.Resource) ([]live.Subscription, error) {
				t.Helper()

				return svc.SubscribersOfRow(t.Context(), res, "k1")
			},
			want: []live.Subscription{
				{Principal: "p1", Tab: "t1", Key: "k1"},
				// A row subscription carries no domain in the record.
				{Principal: "p5", Tab: "t5", Key: "k1"},
			},
		},
		{
			name: "the subscribers of a tenant's list: not another tenant's, not a row's",
			subs: []live.Subscription{
				{Principal: "p1", Tab: "t1", Domain: "anvil"},
				{Principal: "p2", Tab: "t2", Domain: "bastion"},
				{Principal: "p3", Tab: "t3", Key: "k1"},
				{Principal: "p4", Tab: "t4", Domain: "anvil", Expiry: now},
			},
			lookup: func(t *testing.T, svc *livefirestore.Service, res accesstypes.Resource) ([]live.Subscription, error) {
				t.Helper()

				return svc.SubscribersOfList(t.Context(), res, "anvil")
			},
			want: []live.Subscription{{Principal: "p1", Tab: "t1", Domain: "anvil"}},
		},
		{
			name: "the subscribers of a global list: the rows in the empty domain are left out",
			subs: []live.Subscription{
				{Principal: "p1", Tab: "t1"},
				{Principal: "p2", Tab: "t2", Key: "k1"},
			},
			lookup: func(t *testing.T, svc *livefirestore.Service, res accesstypes.Resource) ([]live.Subscription, error) {
				t.Helper()

				return svc.SubscribersOfList(t.Context(), res, "")
			},
			want: []live.Subscription{{Principal: "p1", Tab: "t1"}},
		},
		{
			name: "every live subscriber of a resource, rows and lists in every domain",
			subs: []live.Subscription{
				{Principal: "p1", Tab: "t1", Key: "k1"},
				{Principal: "p2", Tab: "t2", Domain: "anvil"},
				{Principal: "p3", Tab: "t3", Domain: "bastion"},
				{Principal: "p4", Tab: "t4"},
				{Principal: "p5", Tab: "t5", Key: "k2", Expiry: now.Add(-time.Minute)},
			},
			lookup: func(t *testing.T, svc *livefirestore.Service, res accesstypes.Resource) ([]live.Subscription, error) {
				t.Helper()

				return svc.SubscribersOfResource(t.Context(), res)
			},
			want: []live.Subscription{
				{Principal: "p1", Tab: "t1", Key: "k1"},
				{Principal: "p2", Tab: "t2", Domain: "anvil"},
				{Principal: "p3", Tab: "t3", Domain: "bastion"},
				{Principal: "p4", Tab: "t4"},
			},
		},
		{
			name: "a subscription written twice is one document",
			subs: []live.Subscription{
				{Principal: "p1", Tab: "t1", Key: "k1"},
				{Principal: "p1", Tab: "t1", Key: "k1"},
			},
			lookup: func(t *testing.T, svc *livefirestore.Service, res accesstypes.Resource) ([]live.Subscription, error) {
				t.Helper()

				return svc.SubscribersOfRow(t.Context(), res, "k1")
			},
			want: []live.Subscription{{Principal: "p1", Tab: "t1", Key: "k1"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := newService(t, fixedClock(now))
			res := resourceFor(t)
			subs := make([]live.Subscription, 0, len(tt.subs))
			for _, sub := range tt.subs {
				sub.Resource = res
				sub.Principal = principalFor(t, sub.Principal)
				subs = append(subs, sub)
			}
			register(t, svc, now, subs...)

			got, err := tt.lookup(t, svc, res)
			if err != nil {
				t.Fatalf("lookup error = %v", err)
			}
			want := make([]live.Subscription, 0, len(tt.want))
			for _, sub := range tt.want {
				sub.Resource = res
				sub.Principal = principalFor(t, sub.Principal)
				want = append(want, sub)
			}
			sortByPrincipal := cmpopts.SortSlices(func(a, b live.Subscription) bool {
				return a.Principal < b.Principal
			})
			if diff := cmp.Diff(want, got, ignoreExpiry, sortByPrincipal); diff != "" {
				t.Errorf("lookup mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestService_renewExtendsExpiry(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	svc := newService(t, fixedClock(now))
	res := resourceFor(t)
	sub := live.Subscription{Principal: principalFor(t, "p1"), Tab: "t1", Resource: res, Key: "k1", Expiry: now.Add(time.Minute)}
	register(t, svc, now, sub)

	sub.Expiry = now.Add(live.SubscriptionTTL)
	if err := svc.Renew(t.Context(), []live.Subscription{sub}); err != nil {
		t.Fatalf("Renew() error = %v", err)
	}

	got, err := svc.SubscribersOfRow(t.Context(), res, "k1")
	if err != nil {
		t.Fatalf("SubscribersOfRow() error = %v", err)
	}
	if len(got) != 1 || !got[0].Expiry.Equal(sub.Expiry) {
		t.Errorf("SubscribersOfRow() = %+v, want one subscription expiring at %v", got, sub.Expiry)
	}
}

func TestService_unsubscribe(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		leave func(t *testing.T, svc *livefirestore.Service, p1, p2 string) error
		want  []string // "principal|tab"
	}{
		{
			name: "a tab leaving drops its subscriptions alone",
			leave: func(t *testing.T, svc *livefirestore.Service, p1, _ string) error {
				t.Helper()

				return svc.Unsubscribe(t.Context(), p1, "t1")
			},
			want: []string{"p1|t2", "p2|t3"},
		},
		{
			name: "a logout drops every subscription of the principal",
			leave: func(t *testing.T, svc *livefirestore.Service, p1, _ string) error {
				t.Helper()

				return svc.UnsubscribeAll(t.Context(), p1)
			},
			want: []string{"p2|t3"},
		},
		{
			name: "a tab with nothing there is nothing to delete",
			leave: func(t *testing.T, svc *livefirestore.Service, p1, _ string) error {
				t.Helper()

				return svc.Unsubscribe(t.Context(), p1, "t9")
			},
			want: []string{"p1|t1", "p1|t2", "p2|t3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := newService(t, fixedClock(now))
			res := resourceFor(t)
			p1, p2 := principalFor(t, "p1"), principalFor(t, "p2")
			register(t, svc, now,
				live.Subscription{Principal: p1, Tab: "t1", Resource: res, Domain: "anvil"},
				live.Subscription{Principal: p1, Tab: "t2", Resource: res, Key: "k1"},
				live.Subscription{Principal: p2, Tab: "t3", Resource: res, Domain: "anvil"},
			)

			if err := tt.leave(t, svc, p1, p2); err != nil {
				t.Fatalf("leave error = %v", err)
			}

			subs, err := svc.SubscribersOfResource(t.Context(), res)
			if err != nil {
				t.Fatalf("SubscribersOfResource() error = %v", err)
			}
			left := make([]string, 0, len(subs))
			for _, sub := range subs {
				left = append(left, strings.TrimSuffix(sub.Principal, "-"+string(res))+"|"+sub.Tab)
			}
			sorted := cmpopts.SortSlices(func(a, b string) bool {
				return a < b
			})
			if diff := cmp.Diff(tt.want, left, sorted); diff != "" {
				t.Errorf("subscriptions left mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// manyRows returns n touched rows.
func manyRows(n int) []resource.RowChange {
	rows := make([]resource.RowChange, 0, n)
	for i := range n {
		rows = append(rows, resource.RowChange{Key: "k" + string(rune('a'+i%26)) + string(rune('0'+i/26))})
	}

	return rows
}

func TestService_Publish(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	second := now.Unix()

	tests := []struct {
		name    string
		subs    []live.Subscription
		domain  accesstypes.Domain
		touched func(res accesstypes.Resource) map[accesstypes.Resource][]resource.RowChange
		// want is each principal's change set, documents by id and content.
		want map[string][]livefirestore.Change
	}{
		{
			name:   "nobody subscribed: no document is written",
			subs:   []live.Subscription{{Principal: "p1", Tab: "t1", Domain: "bastion"}},
			domain: "anvil",
			touched: func(res accesstypes.Resource) map[accesstypes.Resource][]resource.RowChange {
				return map[accesstypes.Resource][]resource.RowChange{res: {{Key: "k1"}}}
			},
			want: map[string][]livefirestore.Change{"p1": {}},
		},
		{
			name: "one document per subscriber: a row document with its key and a list document with none",
			subs: []live.Subscription{
				{Principal: "p1", Tab: "t1", Key: "k1"},
				{Principal: "p2", Tab: "t2", Domain: "anvil"},
			},
			domain: "anvil",
			touched: func(res accesstypes.Resource) map[accesstypes.Resource][]resource.RowChange {
				return map[accesstypes.Resource][]resource.RowChange{res: {{Key: "k1", Deleted: true}}}
			},
			want: map[string][]livefirestore.Change{
				"p1": {{ChangeDocument: live.ChangeDocument{Kind: live.RowChange, Key: "k1", Deleted: true}}},
				"p2": {{ChangeDocument: live.ChangeDocument{Kind: live.ListChange, Domain: "anvil"}}},
			},
		},
		{
			name: "over the threshold every subscriber gets one resource document",
			subs: []live.Subscription{
				{Principal: "p1", Tab: "t1", Key: "ka0"},
				{Principal: "p2", Tab: "t2", Domain: "anvil"},
				{Principal: "p3", Tab: "t3", Domain: "bastion"},
			},
			domain: "anvil",
			touched: func(res accesstypes.Resource) map[accesstypes.Resource][]resource.RowChange {
				return map[accesstypes.Resource][]resource.RowChange{res: manyRows(live.BulkThreshold + 1)}
			},
			want: map[string][]livefirestore.Change{
				"p1": {{ChangeDocument: live.ChangeDocument{Kind: live.ResourceChange}}},
				"p2": {{ChangeDocument: live.ChangeDocument{Kind: live.ResourceChange}}},
				"p3": {},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := newService(t, fixedClock(now))
			res := resourceFor(t)
			subs := make([]live.Subscription, 0, len(tt.subs))
			for _, sub := range tt.subs {
				sub.Resource = res
				sub.Principal = principalFor(t, sub.Principal)
				subs = append(subs, sub)
			}
			register(t, svc, now, subs...)

			if err := svc.Publish(t.Context(), tt.domain, tt.touched(res)); err != nil {
				t.Fatalf("Publish() error = %v", err)
			}

			for who, want := range tt.want {
				got, err := svc.Changes(t.Context(), principalFor(t, who), time.Time{})
				if err != nil {
					t.Fatalf("Changes(%s) error = %v", who, err)
				}
				for i := range want {
					want[i].Resource = res
					want[i].ID = want[i].ChangeDocument.ID(second)
					want[i].Expires = now.Add(live.ChangeTTL)
				}
				if diff := cmp.Diff(want, got, cmpopts.IgnoreFields(livefirestore.Change{}, "At")); diff != "" {
					t.Errorf("Changes(%s) mismatch (-want +got):\n%s", who, diff)
				}
				for _, change := range got {
					if change.At.IsZero() {
						t.Errorf("Changes(%s): document %s has no server timestamp", who, change.ID)
					}
				}
			}
		})
	}
}

func TestService_Publish_coalesces(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	clock := now
	svc := newService(t, func() time.Time {
		return clock
	})
	res := resourceFor(t)
	p1 := principalFor(t, "p1")
	register(t, svc, now, live.Subscription{Principal: p1, Tab: "t1", Resource: res, Key: "k1"})

	// Two publishes within one second are one document, the second's deleted flag kept.
	if err := svc.Publish(t.Context(), "anvil", map[accesstypes.Resource][]resource.RowChange{res: {{Key: "k1"}}}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	clock = now.Add(700 * time.Millisecond)
	if err := svc.Publish(t.Context(), "anvil", map[accesstypes.Resource][]resource.RowChange{res: {{Key: "k1", Deleted: true}}}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	// The next second is a new document.
	clock = now.Add(time.Second)
	if err := svc.Publish(t.Context(), "anvil", map[accesstypes.Resource][]resource.RowChange{res: {{Key: "k1"}}}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	got, err := svc.Changes(t.Context(), p1, time.Time{})
	if err != nil {
		t.Fatalf("Changes() error = %v", err)
	}
	want := []livefirestore.Change{
		{ID: live.ChangeDocument{Kind: live.RowChange, Resource: res, Key: "k1"}.ID(now.Unix()), ChangeDocument: live.ChangeDocument{Kind: live.RowChange, Resource: res, Key: "k1", Deleted: true}},
		{ID: live.ChangeDocument{Kind: live.RowChange, Resource: res, Key: "k1"}.ID(now.Unix() + 1), ChangeDocument: live.ChangeDocument{Kind: live.RowChange, Resource: res, Key: "k1"}},
	}
	if diff := cmp.Diff(want, got, cmpopts.IgnoreFields(livefirestore.Change{}, "At", "Expires")); diff != "" {
		t.Errorf("Changes() mismatch (-want +got):\n%s", diff)
	}
	if len(got) == 2 && !got[1].At.After(got[0].At) {
		t.Errorf("documents are not ordered by their server timestamp: %v then %v", got[0].At, got[1].At)
	}
}

func TestService_Token_emulator(t *testing.T) {
	t.Parallel()

	svc := newService(t, time.Now)
	got, err := svc.Token(t.Context(), "dispatcher")
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	want := &live.TokenPayload{UID: "dispatcher", Project: testProject, Database: testDatabase, Emulator: firestoreEmulator(t)}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Token() mismatch (-want +got):\n%s", diff)
	}
	if err := svc.Revoke(t.Context(), "dispatcher"); err != nil {
		t.Errorf("Revoke() error = %v, want nothing against the emulator", err)
	}
}

func TestNew_requiresProject(t *testing.T) {
	t.Parallel()

	if _, err := livefirestore.New(t.Context(), livefirestore.Config{EmulatorHost: "127.0.0.1:1"}); err == nil {
		t.Fatal("New() error = nil, want a refusal without a project")
	}
}

// revokeRecorder is the Firestore service with the uids it revoked recorded: against the
// emulator Revoke has no tokens to end, so the uid a logout hands it is what the logout
// proves.
type revokeRecorder struct {
	*livefirestore.Service

	mu      sync.Mutex
	revoked []string
}

func (r *revokeRecorder) Revoke(ctx context.Context, uid string) error {
	r.mu.Lock()
	r.revoked = append(r.revoked, uid)
	r.mu.Unlock()

	if err := r.Service.Revoke(ctx, uid); err != nil {
		return errors.Wrap(err, "firestore.Service.Revoke()")
	}

	return nil
}

// Revoked returns the uids revoked so far, in order.
func (r *revokeRecorder) Revoked() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.revoked)
}

// admitted is the gate of a request the grants admit.
type admitted struct{}

func (admitted) Permitted(context.Context) (bool, error) {
	return true, nil
}

// serveAs runs one request through an outlet bound to auth (live.Subscribing in front of
// handler, as the generated routes compose it), signed in as user, carrying the tab in
// the subscribe header as the browser's live requests do.
func serveAs(t *testing.T, auth, user string, handler http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()

	ctx := context.WithValue(t.Context(), sessioninfo.CtxSessionInfo, &sessioninfo.SessionData{
		SessionInfo: &sessioninfo.SessionInfo{Username: user},
	})
	req := httptest.NewRequestWithContext(ctx, method, target, strings.NewReader(body))
	req.Header.Set(live.SubscribeHeader, "tab-1")
	rr := httptest.NewRecorder()
	live.Subscribing(auth)(handler).ServeHTTP(rr, req)

	return rr
}

// TestService_twoAuthsOneName plays two people of one name through two auths over one
// database, a password user of the crew auth and a directory user of the members auth
// whose email the password user name equals: they hold two uids and two change sets, a
// commit writes into each only what that person watches, each browser identity reads its
// own set alone, and one's logout ends that person's subscriptions and identity and
// leaves the other's.
func TestService_twoAuthsOneName(t *testing.T) {
	t.Parallel()

	svc := &revokeRecorder{Service: newService(t, time.Now)}
	res := resourceFor(t)
	shared := principalFor(t, "alice") + "@example.com"

	people := []struct {
		name    string
		auth    string
		watches *live.Subscription
		wantUID string
		want    []live.ChangeDocument
	}{
		{
			name:    "the password user watching the anvil list",
			auth:    "crew",
			watches: live.ListSubscription(res, "anvil"),
			wantUID: "crew|" + shared,
			want:    []live.ChangeDocument{{Kind: live.ListChange, Resource: res, Domain: "anvil"}},
		},
		{
			name:    "the directory user watching one row",
			auth:    "members",
			watches: live.RowSubscription(res, "k2"),
			wantUID: "members|" + shared,
			want:    []live.ChangeDocument{{Kind: live.RowChange, Resource: res, Key: "k2"}},
		},
	}

	// Each opens a live page through its own outlet, as a generated list or read handler
	// registers it, and asks for its identity.
	for _, p := range people {
		watch := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			live.Subscribe(r.Context(), r, svc, admitted{}, p.watches)
			w.WriteHeader(http.StatusOK)
		})
		if rr := serveAs(t, p.auth, shared, watch, http.MethodGet, "/api/ships", ""); rr.Code != http.StatusOK {
			t.Fatalf("%s: the live page answered %d: %s", p.name, rr.Code, rr.Body.String())
		}
		rr := serveAs(t, p.auth, shared, live.TokenHandler(svc), http.MethodGet, "/api/live/token", "")
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: the token route answered %d: %s", p.name, rr.Code, rr.Body.String())
		}
		var payload live.TokenPayload
		if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
			t.Fatalf("%s: json.Unmarshal() error = %v", p.name, err)
		}
		if payload.UID != p.wantUID {
			t.Errorf("%s: uid = %q, want %q", p.name, payload.UID, p.wantUID)
		}
	}

	// One commit writes row k2 in anvil: the list's watcher and the row's watcher each
	// get their own document in their own set.
	if err := svc.Publish(t.Context(), "anvil", map[accesstypes.Resource][]resource.RowChange{res: {{Key: "k2"}}}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	for _, p := range people {
		changes, err := svc.Changes(t.Context(), p.wantUID, time.Time{})
		if err != nil {
			t.Fatalf("%s: Changes() error = %v", p.name, err)
		}
		got := make([]live.ChangeDocument, 0, len(changes))
		for _, change := range changes {
			got = append(got, change.ChangeDocument)
		}
		if diff := cmp.Diff(p.want, got); diff != "" {
			t.Errorf("%s: change set mismatch (-want +got):\n%s", p.name, diff)
		}
	}

	// Each browser identity reads its own change set, its document there, and is refused
	// the other's.
	reads := []struct {
		name       string
		reader     string
		set        string
		wantStatus int
		wantBody   string
	}{
		{name: "the password user reads their own set", reader: people[0].wantUID, set: people[0].wantUID, wantStatus: http.StatusOK, wantBody: `"list"`},
		{name: "the password user cannot read the directory user's set", reader: people[0].wantUID, set: people[1].wantUID, wantStatus: http.StatusForbidden},
		{name: "the directory user reads their own set", reader: people[1].wantUID, set: people[1].wantUID, wantStatus: http.StatusOK, wantBody: `"row"`},
		{name: "the directory user cannot read the password user's set", reader: people[1].wantUID, set: people[0].wantUID, wantStatus: http.StatusForbidden},
	}
	for _, tt := range reads {
		status, body := restCall(t, http.MethodGet, "users/"+tt.set+"/changes", unsignedToken(tt.reader), "")
		if status != tt.wantStatus {
			t.Errorf("%s: status = %d, want %d: %s", tt.name, status, tt.wantStatus, body)
		}
		if !strings.Contains(body, tt.wantBody) {
			t.Errorf("%s: body = %s, want it to contain %s", tt.name, body, tt.wantBody)
		}
	}

	// The password user logs out: their subscriptions and identity end, the directory
	// user's stay.
	logout := serveAs(t, "crew", shared, live.UnsubscribeHandler(svc), http.MethodPost, "/api/live/unsubscribe", `{"tab":"tab-1","all":true}`)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("the logout answered %d: %s", logout.Code, logout.Body.String())
	}
	left, err := svc.SubscribersOfResource(t.Context(), res)
	if err != nil {
		t.Fatalf("SubscribersOfResource() error = %v", err)
	}
	want := []live.Subscription{{Principal: people[1].wantUID, Tab: "tab-1", Resource: res, Key: "k2"}}
	if diff := cmp.Diff(want, left, ignoreExpiry); diff != "" {
		t.Errorf("subscriptions left after the logout mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{people[0].wantUID}, svc.Revoked()); diff != "" {
		t.Errorf("Revoked() mismatch (-want +got):\n%s", diff)
	}
}
