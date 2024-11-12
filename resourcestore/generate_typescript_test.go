package resourcestore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/google/go-cmp/cmp"
)

type fields struct {
	tagStore      map[accesstypes.PermissionScope]tagStore
	resourceStore map[accesstypes.PermissionScope]resourceStore
}

func TestStore_GenerateTypeScript(t *testing.T) {
	type args struct {
		dst string
	}
	tests := []struct {
		name     string
		fields   fields
		args     args
		wantErr  bool
		wantPath string
		wantDiff bool
	}{
		{
			name:     "Generated TS Should Match",
			fields:   fakeStores(t),
			args:     args{"permissions.ts"},
			wantErr:  false,
			wantPath: "testdata/Generate_Typescript/permissions.ts",
			wantDiff: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := &Store{
				tagStore:      tt.fields.tagStore,
				resourceStore: tt.fields.resourceStore,
			}

			tempPath := filepath.Join(t.TempDir(), tt.args.dst)
			if err := s.GenerateTypeScript(tempPath); (err != nil) != tt.wantErr {
				t.Fatalf("Store.GenerateTypeScript() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			got, err := os.ReadFile(tempPath)
			if err != nil {
				t.Fatalf("unexpected error occurred when calling os.ReadFile() with tempPath %s, error = %s", tempPath, err)
			}
			want, err := os.ReadFile(tt.wantPath)
			if err != nil {
				t.Fatalf("unexpected error occurred when calling os.ReadFile() with wantPath %s, error = %s", tt.wantPath, err)
			}

			if diff := cmp.Diff(want, got); (diff != "") != tt.wantDiff {
				t.Errorf("Store.GenerateTypeScript() output mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func fakeStores(t *testing.T) fields {
	t.Helper()

	rStore := map[accesstypes.Resource][]accesstypes.Permission{
		"Prototype1": {"Create", "Delete"},
		"Prototype2": {"Update", "List", "Read"},
		"Prototype3": {"Delete", "Read", "List"},
	}

	tStore := map[accesstypes.Resource]map[accesstypes.Tag][]accesstypes.Permission{
		"Prototype1": {
			"id":       []accesstypes.Permission{"Create", "Delete"},
			"protocol": []accesstypes.Permission{"Create", "Delete"},
		},
		"Prototype2": {
			"id":   []accesstypes.Permission{"Create", "Delete"},
			"uuid": []accesstypes.Permission{"List", "Read", "Update", "Delete"},
			"addr": []accesstypes.Permission{"Create", "Delete"},
		},
		"Prototype3": {
			"socket":  []accesstypes.Permission{},
			"sockopt": []accesstypes.Permission{"Read", "List"},
		},
	}

	return fields{map[accesstypes.PermissionScope]tagStore{"global": tStore}, map[accesstypes.PermissionScope]resourceStore{"global": rStore}}
}
