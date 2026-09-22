package accesstypes

import (
	"testing"
)

func TestPrincipal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		principal  Principal
		wantUser   User
		wantUserOK bool
		wantRole   Role
		wantRoleOK bool
		wantIsRole bool
		wantString string
	}{
		{
			name:       "user principal",
			principal:  UserPrincipal("alice@example.com"),
			wantUser:   "alice@example.com",
			wantUserOK: true,
			wantString: "user:alice@example.com",
		},
		{
			name:       "role principal",
			principal:  RolePrincipal("PartnerViewer"),
			wantRole:   "PartnerViewer",
			wantRoleOK: true,
			wantIsRole: true,
			wantString: "role:PartnerViewer",
		},
		{
			name:       "a user named like a role is a user principal",
			principal:  UserPrincipal("PartnerViewer"),
			wantUser:   "PartnerViewer",
			wantUserOK: true,
			wantString: "user:PartnerViewer",
		},
		{
			name:       "a role named like a user is a role principal",
			principal:  RolePrincipal("alice@example.com"),
			wantRole:   "alice@example.com",
			wantRoleOK: true,
			wantIsRole: true,
			wantString: "role:alice@example.com",
		},
		{
			name:       "zero principal is the zero user's principal",
			principal:  Principal{},
			wantUserOK: true,
			wantString: "user:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			user, ok := tt.principal.User()
			if user != tt.wantUser || ok != tt.wantUserOK {
				t.Errorf("User() = (%q, %v), want (%q, %v)", user, ok, tt.wantUser, tt.wantUserOK)
			}
			role, ok := tt.principal.Role()
			if role != tt.wantRole || ok != tt.wantRoleOK {
				t.Errorf("Role() = (%q, %v), want (%q, %v)", role, ok, tt.wantRole, tt.wantRoleOK)
			}
			if got := tt.principal.IsRole(); got != tt.wantIsRole {
				t.Errorf("IsRole() = %v, want %v", got, tt.wantIsRole)
			}
			if got := tt.principal.String(); got != tt.wantString {
				t.Errorf("String() = %q, want %q", got, tt.wantString)
			}
		})
	}
}

func TestPrincipal_Comparable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		a, b      Principal
		wantEqual bool
	}{
		{
			name:      "same user",
			a:         UserPrincipal("alice"),
			b:         UserPrincipal("alice"),
			wantEqual: true,
		},
		{
			name:      "same role",
			a:         RolePrincipal("Editor"),
			b:         RolePrincipal("Editor"),
			wantEqual: true,
		},
		{
			name: "user and role with the same name differ",
			a:    UserPrincipal("Editor"),
			b:    RolePrincipal("Editor"),
		},
		{
			name: "different users differ",
			a:    UserPrincipal("alice"),
			b:    UserPrincipal("bob"),
		},
		{
			name:      "zero principal equals the zero user's principal",
			a:         Principal{},
			b:         UserPrincipal(""),
			wantEqual: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.a == tt.b; got != tt.wantEqual {
				t.Errorf("(%v == %v) = %v, want %v", tt.a, tt.b, got, tt.wantEqual)
			}

			m := map[Principal]int{tt.a: 1}
			if _, found := m[tt.b]; found != tt.wantEqual {
				t.Errorf("map lookup of %v in {%v} = %v, want %v", tt.b, tt.a, found, tt.wantEqual)
			}
		})
	}
}
