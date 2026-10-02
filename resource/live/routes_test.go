package live

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// grantTable decides every check from a table keyed "<perm>|<scope>|<resource>", where
// scope is "global" or the domain; a resource absent from the table is denied, as the
// engine denies a resource it knows no grant for.
type grantTable struct {
	grantAll
	decisions map[string]accesstypes.Decision
}

func (g *grantTable) Check(_ context.Context, _ accesstypes.Environment, scope accesstypes.Scope, perm accesstypes.Permission, resources ...accesstypes.Resource) (accesstypes.Decisions, error) {
	decisions := make(accesstypes.Decisions, len(resources))
	for _, res := range resources {
		decision, ok := g.decisions[string(perm)+"|"+scope.String()+"|"+string(res)]
		if !ok {
			decision = accesstypes.Denied()
		}
		decisions[res] = decision
	}

	return decisions, nil
}

// digest is the grants the renewal re-checks against: the dispatcher lists Ships and
// reads one in anvil, lists Clients globally, and holds Refits in anvil under a condition.
var digest = map[string]accesstypes.Decision{
	"List|anvil|Ships":    accesstypes.Granted(),
	"Read|anvil|Ships":    accesstypes.Granted(),
	"List|global|Clients": accesstypes.Granted(),
	"List|anvil|Refits":   accesstypes.Conditional(accesstypes.ConditionGroup{}),
}

func permissionsFor(perms resource.UserPermissions) func(*http.Request) resource.UserPermissions {
	return func(*http.Request) resource.UserPermissions {
		return perms
	}
}

func TestRenewHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantKept   []SubscriptionRequest
		wantDrop   []SubscriptionRequest
		wantSubs   []Subscription
		wantBody   string
	}{
		{
			name: "kept subscriptions are written with a fresh expiry, dropped ones echoed",
			body: `{"tab":"tab-1","subscriptions":[
				{"resource":"Ships","domain":"anvil"},
				{"resource":"Ships","key":"s1","domain":"anvil"},
				{"resource":"Clients"},
				{"resource":"Refits","domain":"anvil"},
				{"resource":"Ships","domain":"bastion"},
				{"resource":"Unknown","domain":"anvil"}]}`,
			wantStatus: http.StatusOK,
			wantKept: []SubscriptionRequest{
				{Resource: "Ships", Domain: "anvil"},
				{Resource: "Ships", Key: "s1", Domain: "anvil"},
				{Resource: "Clients"},
				{Resource: "Refits", Domain: "anvil"},
			},
			wantDrop: []SubscriptionRequest{
				{Resource: "Ships", Domain: "bastion"},
				{Resource: "Unknown", Domain: "anvil"},
			},
			wantSubs: []Subscription{
				{Principal: "dispatcher", Tab: "tab-1", Resource: "Clients"},
				{Principal: "dispatcher", Tab: "tab-1", Resource: "Refits", Domain: "anvil"},
				{Principal: "dispatcher", Tab: "tab-1", Resource: "Ships", Key: "s1"},
				{Principal: "dispatcher", Tab: "tab-1", Resource: "Ships", Domain: "anvil"},
			},
		},
		{
			name:       "a row subscription without its domain is re-checked in the global scope, where a domain-scoped resource has no grant",
			body:       `{"tab":"tab-1","subscriptions":[{"resource":"Ships","key":"s1"}]}`,
			wantStatus: http.StatusOK,
			wantKept:   []SubscriptionRequest{},
			wantDrop:   []SubscriptionRequest{{Resource: "Ships", Key: "s1"}},
		},
		{
			name:       "nothing to renew answers with nothing kept and nothing written",
			body:       `{"tab":"tab-1","subscriptions":[]}`,
			wantStatus: http.StatusOK,
			wantKept:   []SubscriptionRequest{},
			wantDrop:   []SubscriptionRequest{},
		},
		{name: "a malformed tab is refused", body: `{"tab":"tab 1","subscriptions":[]}`, wantStatus: http.StatusBadRequest, wantBody: "invalid X-Subscribe value"},
		{name: "a subscription without a resource is refused", body: `{"tab":"tab-1","subscriptions":[{"key":"s1"}]}`, wantStatus: http.StatusBadRequest, wantBody: "names its resource"},
		{name: "an unknown field is refused", body: `{"tab":"tab-1","subs":[]}`, wantStatus: http.StatusBadRequest, wantBody: "invalid request body"},
		{name: "a body that is not JSON is refused", body: `tab-1`, wantStatus: http.StatusBadRequest, wantBody: "invalid request body"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fake := NewFake()
			handler := RenewHandler(fake, permissionsFor(&grantTable{decisions: digest}))
			req := httptest.NewRequestWithContext(withSession(t.Context(), "dispatcher"), http.MethodPost, "/api/live/renew", strings.NewReader(tt.body))
			rr := httptest.NewRecorder()
			before := time.Now()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rr.Code, tt.wantStatus, rr.Body.String())
			}
			if tt.wantBody != "" && !strings.Contains(rr.Body.String(), tt.wantBody) {
				t.Errorf("body = %q, want it to contain %q", rr.Body.String(), tt.wantBody)
			}
			if tt.wantStatus != http.StatusOK {
				return
			}
			var resp RenewResponse
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("json.Unmarshal() error = %v: %s", err, rr.Body.String())
			}
			if diff := cmp.Diff(tt.wantKept, resp.Kept); diff != "" {
				t.Errorf("kept mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantDrop, resp.Dropped); diff != "" {
				t.Errorf("dropped mismatch (-want +got):\n%s", diff)
			}
			if want := before.Add(SubscriptionTTL); resp.ExpiresAt.Before(want.Add(-time.Second)) || resp.ExpiresAt.After(want.Add(time.Minute)) {
				t.Errorf("expiresAt = %v, want about %v", resp.ExpiresAt, want)
			}
			if diff := cmp.Diff(tt.wantSubs, fake.Subscriptions(), cmpopts.IgnoreFields(Subscription{}, "Expiry"), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Subscriptions() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRenewHandler_tooMany(t *testing.T) {
	t.Parallel()

	var sb strings.Builder
	sb.WriteString(`{"tab":"tab-1","subscriptions":[`)
	for i := range MaxRenewSubscriptions + 1 {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"resource":"Clients"}`)
	}
	sb.WriteString(`]}`)

	handler := RenewHandler(NewFake(), permissionsFor(&grantTable{decisions: digest}))
	req := httptest.NewRequestWithContext(withSession(t.Context(), "dispatcher"), http.MethodPost, "/api/live/renew", strings.NewReader(sb.String()))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", rr.Code, http.StatusBadRequest, rr.Body.String())
	}
}

func TestUnsubscribeHandler(t *testing.T) {
	t.Parallel()

	held := func(now time.Time) []Subscription {
		return []Subscription{
			{Principal: "dispatcher", Tab: "tab-1", Resource: "Ships", Domain: "anvil", Expiry: now.Add(time.Minute)},
			{Principal: "dispatcher", Tab: "tab-2", Resource: "Ships", Key: "s1", Expiry: now.Add(time.Minute)},
			{Principal: "mechanic", Tab: "tab-3", Resource: "Refits", Domain: "anvil", Expiry: now.Add(time.Minute)},
		}
	}

	tests := []struct {
		name        string
		body        string
		wantStatus  int
		wantLeft    []string // "principal|tab"
		wantRevoked []string
	}{
		{
			name:       "a tab leaving drops its subscriptions alone",
			body:       `{"tab":"tab-1"}`,
			wantStatus: http.StatusNoContent,
			wantLeft:   []string{"dispatcher|tab-2", "mechanic|tab-3"},
		},
		{
			name:        "a logout drops every subscription of the principal and revokes the identity",
			body:        `{"tab":"tab-1","all":true}`,
			wantStatus:  http.StatusNoContent,
			wantLeft:    []string{"mechanic|tab-3"},
			wantRevoked: []string{"dispatcher"},
		},
		{
			name:       "a tab with nothing there still answers 204",
			body:       `{"tab":"tab-9"}`,
			wantStatus: http.StatusNoContent,
			wantLeft:   []string{"dispatcher|tab-1", "dispatcher|tab-2", "mechanic|tab-3"},
		},
		{name: "neither a tab nor all is refused", body: `{}`, wantStatus: http.StatusBadRequest, wantLeft: []string{"dispatcher|tab-1", "dispatcher|tab-2", "mechanic|tab-3"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fake := NewFake()
			if err := fake.Register(t.Context(), held(time.Now())); err != nil {
				t.Fatalf("Register() error = %v", err)
			}
			req := httptest.NewRequestWithContext(withSession(t.Context(), "dispatcher"), http.MethodPost, "/api/live/unsubscribe", strings.NewReader(tt.body))
			rr := httptest.NewRecorder()
			UnsubscribeHandler(fake).ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rr.Code, tt.wantStatus, rr.Body.String())
			}
			subs := fake.Subscriptions()
			left := make([]string, 0, len(subs))
			for _, sub := range subs {
				left = append(left, sub.Principal+"|"+sub.Tab)
			}
			if diff := cmp.Diff(tt.wantLeft, left); diff != "" {
				t.Errorf("subscriptions left mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantRevoked, fake.Revoked(), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Revoked() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTokenHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		ctx        context.Context
		wantStatus int
		want       *TokenPayload
	}{
		{
			name:       "the payload names the session principal",
			ctx:        withSession(context.Background(), "dispatcher"),
			wantStatus: http.StatusOK,
			want:       &TokenPayload{UID: "dispatcher", Project: fakeProject, Database: fakeDatabase, Emulator: fakeEmulator},
		},
		{
			name:       "a role principal is the role",
			ctx:        withRoleSession(context.Background(), "alice", "Auditor"),
			wantStatus: http.StatusOK,
			want:       &TokenPayload{UID: "role:Auditor", Project: fakeProject, Database: fakeDatabase, Emulator: fakeEmulator},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(tt.ctx, http.MethodGet, "/api/live/token", http.NoBody)
			rr := httptest.NewRecorder()
			TokenHandler(NewFake()).ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rr.Code, tt.wantStatus, rr.Body.String())
			}
			if tt.want == nil {
				return
			}
			var got TokenPayload
			if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
				t.Fatalf("json.Unmarshal() error = %v: %s", err, rr.Body.String())
			}
			if diff := cmp.Diff(*tt.want, got); diff != "" {
				t.Errorf("payload mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
