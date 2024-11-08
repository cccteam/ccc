package resourcestore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/google/go-cmp/cmp"
)

func TestStore_GenerateTypeScript(t *testing.T) {
	type fields struct {
		tagStore      map[accesstypes.PermissionScope]tagStore
		resourceStore map[accesstypes.PermissionScope]resourceStore
	}
	type args struct {
		dst string
	}
	tests := []struct {
		name     string
		fields   fields
		args     args
		wantErr  bool
		wantPath string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := &Store{
				tagStore:      tt.fields.tagStore,
				resourceStore: tt.fields.resourceStore,
			}

			tempPath := filepath.Join(t.TempDir(), "permissions.ts")
			if err := s.GenerateTypeScript(tempPath); (err != nil) != tt.wantErr {
				t.Fatalf("Store.GenerateTypeScript() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			got, err := os.ReadFile(tempPath)
			if err != nil {
				t.Fatalf("unexpected error occurred when calling os.ReadFile() with name %s, error = %s", tempPath, err)
			}
			want, err := os.ReadFile(tt.wantPath)
			if err != nil {
				t.Fatalf("unexpected error occurred when calling os.ReadFile() with name %s, error = %s", tt.wantPath, err)
			}

			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Store.GenerateTypeScript() output mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
