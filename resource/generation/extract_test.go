package generation

import (
	"slices"
	"testing"
)

func Test_loadPackages(t *testing.T) {
	t.Parallel()
	type args struct {
		packagePatterns []string
	}

	tests := []struct {
		name             string
		args             args
		WantPackageNames []string
		wantErr          bool
	}{
		{
			name:             "loads 1 package by file name",
			args:             args{packagePatterns: []string{"testdata/resources/res1.go"}},
			WantPackageNames: []string{"resources"},
			wantErr:          false,
		},
		{
			name:             "loads 1 package by name",
			args:             args{packagePatterns: []string{"./testdata/resources"}},
			WantPackageNames: []string{"resources"},
			wantErr:          false,
		},
		{
			name:             "loads 1 package by 2 file names",
			args:             args{packagePatterns: []string{"testdata/resources/res1.go", "testdata/resources/res2.go"}},
			WantPackageNames: []string{"resources"},
			wantErr:          false,
		},
		{
			name:             "loads 2 packages by name",
			args:             args{packagePatterns: []string{"./testdata/resources", "./testdata/otherresources"}},
			WantPackageNames: []string{"resources", "otherresources"},
			wantErr:          false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packageMap, err := loadPackages(tt.args.packagePatterns...)
			if (err != nil) != tt.wantErr {
				t.Errorf("loadPackages() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			packageNames := make([]string, 0, len(packageMap))
			for k := range packageMap {
				packageNames = append(packageNames, k)
			}

			for _, packageName := range tt.WantPackageNames {
				if !slices.Contains(packageNames, packageName) {
					t.Errorf("loadPackages() = `%v`, does not contain expected package %s", packageNames, packageName)
				}
			}
		})
	}
}
