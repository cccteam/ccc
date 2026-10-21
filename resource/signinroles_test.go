package resource

import (
	"context"
	"testing"
	"time"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/session/signin"
	"github.com/google/go-cmp/cmp"
)

// recordingRoleManager is a signin.UserRoleManager that records the context each
// operation received, by the operation's name, and answers a fixed collection.
type recordingRoleManager struct {
	held accesstypes.RoleCollection
	ctxs map[string]context.Context
}

func (m *recordingRoleManager) UserRoles(ctx context.Context, _ accesstypes.User, _ ...accesstypes.PolicyScope) (accesstypes.RoleCollection, error) {
	m.ctxs["UserRoles"] = ctx

	return m.held, nil
}

func (m *recordingRoleManager) ResolveRole(ctx context.Context, _ accesstypes.PolicyScope, name string) (accesstypes.Role, bool, error) {
	m.ctxs["ResolveRole"] = ctx

	return accesstypes.Role(name), true, nil
}

func (m *recordingRoleManager) AddUserRoles(ctx context.Context, _ accesstypes.PolicyScope, _ accesstypes.User, _ ...accesstypes.Role) error {
	m.ctxs["AddUserRoles"] = ctx

	return nil
}

func (m *recordingRoleManager) DeleteUserRoles(ctx context.Context, _ accesstypes.PolicyScope, _ accesstypes.User, _ ...accesstypes.Role) error {
	m.ctxs["DeleteUserRoles"] = ctx

	return nil
}

// transactionKey stands in for the Spanner client's transaction marker: a value the
// sign-in's context carries and the adapter's read must not.
type transactionKey struct{}

// TestSignInRoles pins the adapter: UserRoles runs under a context that carries the
// sign-in's deadline and cancellation and none of its values, answering the manager's
// collection; the other operations receive the sign-in's context as it is.
func TestSignInRoles(t *testing.T) {
	t.Parallel()

	held := accesstypes.RoleCollection{accesstypes.DomainPolicyScope("north"): {"Reader"}}
	tests := []struct {
		name      string
		operation string
		call      func(t *testing.T, ctx context.Context, manager signin.UserRoleManager)
		// wantOutside says the operation runs outside the context's transaction: without
		// its values, with its deadline.
		wantOutside bool
	}{
		{
			name:      "UserRoles reads outside the transaction and answers the memberships",
			operation: "UserRoles",
			call: func(t *testing.T, ctx context.Context, manager signin.UserRoleManager) {
				t.Helper()
				got, err := manager.UserRoles(ctx, "someone")
				if err != nil {
					t.Fatalf("UserRoles() error = %v", err)
				}
				if diff := cmp.Diff(held, got); diff != "" {
					t.Errorf("UserRoles() mismatch (-want +got):\n%s", diff)
				}
			},
			wantOutside: true,
		},
		{
			name:      "ResolveRole passes the context through",
			operation: "ResolveRole",
			call: func(t *testing.T, ctx context.Context, manager signin.UserRoleManager) {
				t.Helper()
				if _, _, err := manager.ResolveRole(ctx, accesstypes.GlobalPolicyScope(), "Reader"); err != nil {
					t.Fatalf("ResolveRole() error = %v", err)
				}
			},
		},
		{
			name:      "AddUserRoles passes the context through",
			operation: "AddUserRoles",
			call: func(t *testing.T, ctx context.Context, manager signin.UserRoleManager) {
				t.Helper()
				if err := manager.AddUserRoles(ctx, accesstypes.GlobalPolicyScope(), "someone", "Reader"); err != nil {
					t.Fatalf("AddUserRoles() error = %v", err)
				}
			},
		},
		{
			name:      "DeleteUserRoles passes the context through",
			operation: "DeleteUserRoles",
			call: func(t *testing.T, ctx context.Context, manager signin.UserRoleManager) {
				t.Helper()
				if err := manager.DeleteUserRoles(ctx, accesstypes.GlobalPolicyScope(), "someone", "Reader"); err != nil {
					t.Fatalf("DeleteUserRoles() error = %v", err)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			recorder := &recordingRoleManager{held: held, ctxs: map[string]context.Context{}}
			deadline := time.Now().Add(time.Hour)
			parent, cancel := context.WithDeadline(context.WithValue(context.Background(), transactionKey{}, "open"), deadline)
			defer cancel()

			tt.call(t, parent, SignInRoles(recorder))

			got := recorder.ctxs[tt.operation]
			if got == nil {
				t.Fatalf("%s was not called", tt.operation)
			}
			if carries := got.Value(transactionKey{}) != nil; carries == tt.wantOutside {
				t.Errorf("%s context carries the transaction = %v, want %v", tt.operation, carries, !tt.wantOutside)
			}
			if gotDeadline, ok := got.Deadline(); !ok || !gotDeadline.Equal(deadline) {
				t.Errorf("%s context deadline = %v, %v, want %v", tt.operation, gotDeadline, ok, deadline)
			}
			cancel()
			select {
			case <-got.Done():
			case <-time.After(time.Second):
				t.Errorf("%s context did not end with the sign-in's", tt.operation)
			}
		})
	}
}
