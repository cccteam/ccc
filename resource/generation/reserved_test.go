package generation

import (
	"strings"
	"testing"
)

// Test_rejectReservedStem pins the refusal of a resource whose plural file stem is one
// the generator writes for itself, and that the tenant record's natural name, Tenant, is
// not one of them now that the roster constructor's file is zz_gen_tenant_roster.go.
func Test_rejectReservedStem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		structName string
		wantErr    string
	}{
		{name: "a resource named Feature takes the feature flags' file", structName: "Feature", wantErr: "struct Feature: its generated files would be named zz_gen_features.go, the file the generator writes the feature flags to; rename the resource"},
		{name: "a resource named Permission takes the permission endpoints' file", structName: "Permission", wantErr: "zz_gen_permissions.go, the file the generator writes the permission endpoints to"},
		{name: "a resource named Decoder takes the decoders' file", structName: "Decoder", wantErr: "zz_gen_decoders.go"},
		{name: "a resource named Enum takes the enumerations' file", structName: "Enum", wantErr: "zz_gen_enums.go"},
		{name: "a tenant record named Tenant is fine", structName: "Tenant"},
		{name: "a resource named TenantRoster pluralizes past the constructor's file", structName: "TenantRoster"},
		{name: "an ordinary name is fine", structName: "Sector"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &resourceGenerator{client: &client{}}
			err := rejectReservedStem(tt.structName, fileStem(r.pluralize(tt.structName)))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("rejectReservedStem(%s) error = %v, want nil", tt.structName, err)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("rejectReservedStem(%s) error = %v, want it to contain %q", tt.structName, err, tt.wantErr)
			}
		})
	}
}
