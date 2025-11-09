package generation

import (
	"testing"
)

func Test_packageDir_Dir_Package(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		p           packageDir
		wantDir     string
		wantPackage string
	}{
		{
			name:        "directory path",
			p:           "path/to/package",
			wantDir:     "path/to/package",
			wantPackage: "package",
		},
		{
			name:        "relative directory path",
			p:           "./path/to/package",
			wantDir:     "./path/to/package",
			wantPackage: "package",
		},
		{
			name:        "file path",
			p:           "path/to/package/file.go",
			wantDir:     "path/to/package",
			wantPackage: "package",
		},
		{
			name:        "relative file path",
			p:           "./path/to/package/file.go",
			wantDir:     "./path/to/package",
			wantPackage: "package",
		},
		{
			name:        "just file does not panic",
			p:           "file.go",
			wantDir:     "",
			wantPackage: ".",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.p.Dir(); got != tt.wantDir {
				t.Errorf("packageDir.Dir() = %v, want %v", got, tt.wantDir)
			}
			if got := tt.p.Package(); got != tt.wantPackage {
				t.Errorf("packageDir.Dir() = %v, want %v", got, tt.wantPackage)
			}
		})
	}
}
