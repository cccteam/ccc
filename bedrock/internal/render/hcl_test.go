package render

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2/hclparse"
)

// TestRenderedStackParsesAsHCL parses every rendered .tf file under testdata, so a template whose rendering
// is not valid HCL (a stray escape, an unbalanced quote) fails here and not in a pipeline's plan step.
func TestRenderedStackParsesAsHCL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		dir  string
	}{
		{name: "beacon", dir: filepath.Join("testdata", "beacon")},
		{name: "harbor", dir: filepath.Join("testdata", "harbor")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			files := tfFiles(t, tt.dir)
			parser := hclparse.NewParser()
			for _, file := range files {
				if _, diags := parser.ParseHCLFile(file); diags.HasErrors() {
					t.Errorf("%s: %s", file, diags.Error())
				}
			}
		})
	}
}

// tfFiles lists the .tf files under dir, failing the test when there are none.
func tfFiles(t *testing.T, dir string) []string {
	t.Helper()

	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".tf") {
			files = append(files, path)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	if len(files) == 0 {
		t.Fatalf("no .tf files under %s", dir)
	}

	return files
}
