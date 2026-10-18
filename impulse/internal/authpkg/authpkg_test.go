package authpkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cccteam/ccc/impulse/app"
)

// smokeSpecs are the auth packages the rendering is held to: the skeleton's two, and one
// of every slot and rule shape the proof application composes.
func smokeSpecs() []*Spec {
	return []*Spec{
		{Module: "example.com/beacon", AuthDir: "pkg/auth", Name: "staff", Doc: "the people who sign in to the console with a password", Methods: []Method{{Kind: app.MethodPassword}}},
		{Module: "example.com/beacon", AuthDir: "pkg/auth", Name: "members", Doc: "the people who sign in through the organization's directory", Methods: []Method{
			{Kind: app.MethodAzure, Name: "directory", Authority: app.AuthorityApplication},
		}},
		{Module: "example.com/beacon", AuthDir: "pkg/auth", Name: "proof", Methods: []Method{
			{Kind: app.MethodPassword},
			{Kind: app.MethodAzure, Name: "entra-one", Authority: app.AuthorityDirectory},
			{Kind: app.MethodAzure, Name: "entra-listed", Slot: SlotListed, Authority: app.AuthorityApplication},
			{Kind: app.MethodAzure, Name: "entra-any-open", Slot: SlotAny, Accounts: AccountsOpen, Authority: app.AuthorityApplication},
			{Kind: app.MethodAzure, Name: "entra-personal", Slot: SlotPersonal, Accounts: AccountsInvited, Authority: app.AuthorityApplication},
			{Kind: app.MethodAzure, Name: "entra-one-personal", Slot: SlotOnePersonal, Accounts: AccountsInvited, Authority: app.AuthorityApplication},
			{Kind: app.MethodAzure, Name: "entra-any-personal", Slot: SlotAnyPersonal, Accounts: AccountsOpen, Authority: app.AuthorityApplication},
			{Kind: app.MethodGoogle, Name: "google-one", Authority: app.AuthorityDirectory},
			{Kind: app.MethodGoogle, Name: "google-listed", Slot: SlotListed, Accounts: AccountsInvited, Authority: app.AuthorityApplication},
			{Kind: app.MethodGoogle, Name: "google-anyws", Slot: SlotAnyWorkspace, Accounts: AccountsOpen, Authority: app.AuthorityApplication},
			{Kind: app.MethodGoogle, Name: "google-any", Slot: SlotAny, Accounts: AccountsInvited, Authority: app.AuthorityApplication},
			{Kind: app.MethodGoogle, Name: "google-mixed", Slot: SlotMixed, Accounts: AccountsOpen, Authority: app.AuthorityApplication},
		}},
	}
}

// TestFiles renders the smoke specs and, when AUTHPKG_OUT names a directory, writes each
// package under it as pkg/auth/<name>/ so the output can be compiled against the
// framework pins.
func TestFiles(t *testing.T) {
	t.Parallel()
	out := os.Getenv("AUTHPKG_OUT")
	for _, spec := range smokeSpecs() {
		files, err := Files(spec)
		if err != nil {
			t.Fatalf("Files(%s) error = %v", spec.Name, err)
		}
		if got, want := len(files), len(spec.Methods)+1; got != want {
			t.Errorf("Files(%s) wrote %d files, want %d", spec.Name, got, want)
		}
		for name, data := range files {
			if !strings.HasPrefix(string(data), "// Package "+spec.Name) && !strings.HasPrefix(string(data), "package "+spec.Name) {
				t.Errorf("%s/%s does not start with the package clause", spec.Name, name)
			}
			if out == "" {
				continue
			}
			dir := filepath.Join(out, spec.AuthDir, spec.Name)
			if err := os.MkdirAll(dir, 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "roles.json"), []byte("{}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		method  Method
		wantErr string
	}{
		{name: "password takes no slot", method: Method{Kind: app.MethodPassword, Slot: SlotAny}, wantErr: "takes no slot"},
		{name: "open sign-up needs an open slot", method: Method{Kind: app.MethodAzure, Name: "x", Accounts: AccountsOpen, Authority: app.AuthorityApplication}, wantErr: "open sign-up is for any organization"},
		{name: "members with any tenant", method: Method{Kind: app.MethodAzure, Name: "x", Slot: SlotAny, Authority: app.AuthorityApplication}, wantErr: "sign up the world"},
		{name: "role sync beyond one tenant", method: Method{Kind: app.MethodAzure, Name: "x", Slot: SlotListed, Authority: app.AuthorityDirectory}, wantErr: "role synchronization follows one tenant"},
		{name: "google slot on azure", method: Method{Kind: app.MethodAzure, Name: "x", Slot: SlotMixed, Authority: app.AuthorityApplication}, wantErr: "a azure method takes"},
		{name: "authority required", method: Method{Kind: app.MethodGoogle, Name: "x"}, wantErr: "authority"},
		{name: "bad registration name", method: Method{Kind: app.MethodGoogle, Name: "Dir", Authority: app.AuthorityApplication}, wantErr: "registration"},
		{name: "fine", method: Method{Kind: app.MethodGoogle, Name: "directory", Authority: app.AuthorityDirectory}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.method.Validate()
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("Validate() error = %v", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("Validate() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
