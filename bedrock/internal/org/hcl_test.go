package org

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2/hclparse"
)

// TestRenderedOrganizationParsesAsHCL parses every rendered .tf file of the organization's layers under
// testdata, so a template whose rendering is not valid HCL fails here and not in an apply.
func TestRenderedOrganizationParsesAsHCL(t *testing.T) {
	t.Parallel()

	tests := make([]struct {
		name string
		dir  string
	}, 0, len(Layers))
	for _, layer := range Layers {
		tests = append(tests, struct {
			name string
			dir  string
		}{name: layer, dir: filepath.Join("testdata", "imp", layer)})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var files []string
			err := filepath.WalkDir(tt.dir, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() && strings.HasSuffix(path, ".tf") {
					files = append(files, path)
				}

				return nil
			})
			if err != nil {
				t.Fatalf("walk %s: %v", tt.dir, err)
			}
			if len(files) == 0 {
				t.Fatalf("no .tf files under %s", tt.dir)
			}
			parser := hclparse.NewParser()
			for _, file := range files {
				if _, diags := parser.ParseHCLFile(file); diags.HasErrors() {
					t.Errorf("%s: %s", file, diags.Error())
				}
			}
		})
	}
}
