package app

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestRoleWriters(t *testing.T) {
	t.Parallel()

	const bootstrap = `package main

import "context"

func run(ctx context.Context, data *Data) error {
	if err := data.Members().Access().UserManager().AddUserRoles(ctx, scope, "client", "Administrator"); err != nil {
		return err
	}
	if err := data.Staff().Access().UserManager().AddUserRoles(ctx, scope, "admin", "Administrator"); err != nil {
		return err
	}
	if err := assign(ctx, data.Members().Access().UserManager(), "client"); err != nil {
		return err
	}
	if err := report(ctx, data.Members().Access().UserManager()); err != nil {
		return err
	}
	membersAuth := data.Members()
	_ = membersAuth.Access().UserManager().DeleteUserRoles(ctx, scope, "client", "Administrator")

	return deploy.MigrateRoles(ctx, data.Members().Access().UserManager(), members.RolesPath)
}

func assign(ctx context.Context, manager Manager, user string) error {
	return manager.AddRoleUsers(ctx, scope, "Administrator", user)
}

func report(ctx context.Context, manager Manager) error {
	_, err := manager.UserRoles(ctx, "client", scope)

	return err
}
`
	tests := []struct {
		name string
		auth string
		src  string
		want []int
	}{
		{name: "the members store's writers, direct, through a helper, and through a variable", auth: "members", src: bootstrap, want: []int{6, 12, 19}},
		{name: "the staff store's writer", auth: "staff", src: bootstrap, want: []int{9}},
		{name: "an auth nothing writes to", auth: "partners", src: bootstrap, want: nil},
		{name: "a file that does not parse", auth: "members", src: "package main\n\nfunc {", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tt.want, RoleWriters("cmd/bootstrap/main.go", []byte(tt.src), tt.auth)); diff != "" {
				t.Errorf("RoleWriters() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
