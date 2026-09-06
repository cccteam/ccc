package names

import (
	"strings"
	"testing"
)

func TestRename(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "the name alone", text: `Name = "members"`, want: `Name = "partners"`},
		{name: "the pascal form alone", text: `TablePrefix = "Members"`, want: `TablePrefix = "Partners"`},
		{name: "an identifier stem", text: "membersAuth, err := members.New(ctx, MembersSessions)", want: "partnersAuth, err := partners.New(ctx, PartnersSessions)"},
		{name: "a path", text: `"example.com/acme/pkg/auth/members"`, want: `"example.com/acme/pkg/auth/partners"`},
		{name: "an english word that begins with the name", text: "role membership stays", want: "role membership stays"},
		{name: "a word that ends with the name", text: "nonmembers stay", want: "nonmembers stay"},
		// The PascalCase form is renamed wherever it starts a word of an identifier, since
		// the tables compose it that way (CK_MembersSessionsId, MembersMembersUserRoles).
		{name: "the pascal form inside an identifier", text: "CK_MembersSessionsId MembersMembersUserRoles", want: "CK_PartnersSessionsId PartnersPartnersUserRoles"},
		{name: "the name at the ends of the text", text: "members\nMembers", want: "partners\nPartners"},
		{name: "adjacent occurrences in a path", text: "pkg/auth/members/members.go", want: "pkg/auth/partners/partners.go"},
		{name: "the name repeated with the pascal form", text: "members.Members", want: "partners.Partners"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := Rename(tt.text, "members", "partners"); got != tt.want {
				t.Errorf("Rename() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateAuth(t *testing.T) {
	t.Parallel()

	reserved := map[string]bool{"config": true, "session": true}
	tests := []struct {
		name    string
		auth    string
		wantErr string
	}{
		{name: "a population", auth: "members"},
		{name: "digits after the first letter", auth: "tier2"},
		{name: "empty", auth: "", wantErr: `auth name ""`},
		{name: "uppercase", auth: "Members", wantErr: `auth name "Members"`},
		{name: "a hyphen", auth: "field-staff", wantErr: `auth name "field-staff"`},
		{name: "a keyword", auth: "type", wantErr: "is a Go keyword"},
		{name: "a predeclared identifier", auth: "string", wantErr: "is a Go keyword"},
		{name: "a package the application imports", auth: "session", wantErr: "is already a package or directory"},
		{name: "a directory the application has", auth: "config", wantErr: "is already a package or directory"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateAuth(tt.auth, reserved)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateAuth() error = %v", err)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ValidateAuth() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
