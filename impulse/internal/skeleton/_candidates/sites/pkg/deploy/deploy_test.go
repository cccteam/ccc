package deploy_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/sites/pkg/auth/staff"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/sites/pkg/deploy"
)

// TestRoles_validateAgainstTheCollection runs each auth's committed roles file through
// the validation MigrateRoles performs before it touches the store (the grammar, every
// condition against the generated collection, each grant at its role's scope) and pins
// the warnings the deploy would print. Every expected set is empty: a warning here is
// accepted by adding its typed value (access.GrantWarning, access.ConcealingKeyWarning)
// to the row's wantWarnings, so the acceptance is recorded in code, or removed by fixing
// the grant it names. A new auth adds a row.
func TestRoles_validateAgainstTheCollection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		rolesPath    string
		wantWarnings []access.Warning
	}{
		{name: "staff", rolesPath: staff.RolesPath},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			raw, err := os.ReadFile(filepath.Join("..", "..", tt.rolesPath))
			if err != nil {
				t.Fatalf("os.ReadFile() error = %v", err)
			}
			var roles access.RoleConfig
			if err := json.Unmarshal(raw, &roles); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}

			// The sites share one policy store, so the roles are validated against the
			// union of every site's collection, as MigrateRoles reconciles them.
			collection, err := deploy.Collection()
			if err != nil {
				t.Fatalf("deploy.Collection() error = %v", err)
			}

			warnings, err := access.ValidateRoles(collection, &roles)
			if err != nil {
				t.Fatalf("access.ValidateRoles() error = %v", err)
			}
			if len(warnings) == 0 && len(tt.wantWarnings) == 0 {
				return
			}
			if !reflect.DeepEqual(tt.wantWarnings, warnings) {
				t.Errorf("access.ValidateRoles() warnings = %d, want %d:", len(warnings), len(tt.wantWarnings))
				for _, w := range warnings {
					t.Logf("got:  %s", w)
				}
				for _, w := range tt.wantWarnings {
					t.Logf("want: %s", w)
				}
			}
		})
	}
}
