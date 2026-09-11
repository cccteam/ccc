package deploy_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/crew"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/members"
	"github.com/cccteam/ccc/resource/lodestar/pkg/router"
)

// TestRoles_validateAgainstTheCollection runs each auth's committed role file
// through the validation MigrateRoles performs before it touches the store:
// the grammar holds, every condition resolves against the generated
// collection, and no role holds a conditional write on a row it can neither
// Read nor List. The deploy would print any such warning; here it fails.
func TestRoles_validateAgainstTheCollection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		rolesPath string
	}{
		{name: "crew", rolesPath: crew.RolesPath},
		{name: "members", rolesPath: members.RolesPath},
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

			warnings, err := access.ValidateRoles(router.Collection(), &roles)
			if err != nil {
				t.Fatalf("access.ValidateRoles() error = %v", err)
			}
			for _, w := range warnings {
				t.Errorf("access.ValidateRoles() warning: %s", w)
			}
		})
	}
}
