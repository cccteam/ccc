package generation

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/google/go-cmp/cmp"
)

func Test_client_pluralize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		value     string
		overrides map[string]string
		want      string
	}{
		{name: "default rule", value: "Book", want: "Books"},
		{name: "consonant y suffix", value: "City", want: "Cities"},
		{name: "vowel y suffix", value: "Day", want: "Days"},
		{name: "s suffix", value: "Class", want: "Classes"},
		{name: "x suffix", value: "Box", want: "Boxes"},
		{name: "vowel z suffix doubles", value: "Quiz", want: "Quizzes"},
		{name: "consonant z suffix", value: "Waltz", want: "Waltzes"},
		{name: "zz suffix", value: "Buzz", want: "Buzzes"},
		{name: "ch suffix", value: "LenderBranch", want: "LenderBranches"},
		{name: "sh suffix", value: "Flash", want: "Flashes"},
		{name: "override", value: "Person", overrides: map[string]string{"Person": "People"}, want: "People"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &client{pluralOverrides: tt.overrides}

			if got := c.pluralize(tt.value); got != tt.want {
				t.Errorf("pluralize(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

// pluralize must stay read-only so concurrent generation phases can call it
// without synchronization. Run with -race; this fails if cache writes are ever
// reintroduced.
func Test_client_pluralize_concurrent(t *testing.T) {
	t.Parallel()

	c := &client{pluralOverrides: map[string]string{"Person": "People"}}

	values := []string{"Book", "City", "Class", "LenderBranch", "Day", "Status", "Person"}

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for _, v := range values {
				c.pluralize(v)
			}
		})
	}
	wg.Wait()

	for _, v := range values {
		if got, want := c.pluralize(v), c.pluralize(v); got != want {
			t.Errorf("pluralize(%q) unstable: %q vs %q", v, got, want)
		}
	}
}

func Test_formatInterfaceTypes(t *testing.T) {
	t.Parallel()

	type args struct {
		types []string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "empty",
			args: args{
				types: []string{},
			},
			want: "",
		},
		{
			name: "One type",
			args: args{
				types: []string{"Resource1"},
			},
			want: "\tResource1",
		},
		{
			name: "many type",
			args: args{
				types: []string{
					"Resource1",
					"MyResource1",
					"YourResource1",
					"Resource2",
					"Resource3",
					"Resource4",
					"Resource5",
					"Resource6",
					"Resource7",
					"Resource8",
					"Resource9",
				},
			},
			want: "\tResource1 | MyResource1 | YourResource1 | Resource2 | Resource3 | Resource4 | Resource5 | Resource6 |\n\tResource7 | Resource8 | Resource9",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := formatInterfaceTypes(tt.args.types)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("formatInterfaceTypes() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_client_sanitizeEnumIdentifier(t *testing.T) {
	t.Parallel()

	type args struct {
		name string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "empty",
			args: args{
				name: "",
			},
			want: "",
		},
		{
			name: "simple",
			args: args{
				name: "test",
			},
			want: "Test",
		},
		{
			name: "with leading numbers",
			args: args{
				name: "123test",
			},
			want: "N123Test",
		},
		{
			name: "with punctuation",
			args: args{
				name: "test, test",
			},
			want: "TestTest",
		},
		{
			name: "with space",
			args: args{
				name: "test test",
			},
			want: "TestTest",
		},
		{
			name: "with space and punctuation",
			args: args{
				name: "test test.test",
			},
			want: "TestTestTest",
		},
		{
			name: "with number",
			args: args{
				name: "test1",
			},
			want: "Test1",
		},
		{
			name: "with number and punctuation",
			args: args{
				name: "test1.test",
			},
			want: "Test1Test",
		},
		{
			name: "with number and space",
			args: args{
				name: "test 1",
			},
			want: "Test1",
		},
		{
			name: "with number, space and punctuation",
			args: args{
				name: "test 1.test",
			},
			want: "Test1Test",
		},
		{
			name: "with hyphen",
			args: args{
				name: "test-test",
			},
			want: "TestTest",
		},
		{
			name: "with hyphen and punctuation",
			args: args{
				name: "test-test.test",
			},
			want: "TestTestTest",
		},
		{
			name: "with hyphen and space",
			args: args{
				name: "test- test",
			},
			want: "TestTest",
		},
		{
			name: "with hyphen, space and punctuation",
			args: args{
				name: "test- test.test",
			},
			want: "TestTestTest",
		},
		{
			name: "Bankruptcy (Chapter 12 or 13)",
			args: args{
				name: "Bankruptcy (Chapter 12 or 13)",
			},
			want: "BankruptcyChapter12Or13",
		},
		{
			name: "Defaulted, Then Bankrupt, Active, Chapter 13",
			args: args{
				name: "Defaulted, Then Bankrupt, Active, Chapter 13",
			},
			want: "DefaultedThenBankruptActiveChapter13",
		},
		{
			name: "Borrower's Bankrupt",
			args: args{
				name: "Borrower's Bankrupt",
			},
			want: "BorrowersBankrupt",
		},
		{
			name: "8-10",
			args: args{
				name: "8-10",
			},
			want: "N8N10",
		},
		{
			name: "8_10",
			args: args{
				name: "8_10",
			},
			want: "N8N10",
		},
		{
			name: "8 10",
			args: args{
				name: "8 10",
			},
			want: "N8N10",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := sanitizeEnumIdentifier(tt.args.name); got != tt.want {
				t.Errorf("sanitizeEnumIdentifier() = %v, want %v, from %s", got, tt.want, tt.args.name)
			}
		})
	}
}

func Test_client_importPaths(t *testing.T) {
	t.Parallel()

	module := &packages.Module{Path: "example.com/harbor", Dir: "/src/harbor"}
	loaded := func(pkgs ...string) map[string]*packages.Package {
		m := map[string]*packages.Package{}
		for _, p := range pkgs {
			m[path.Base(p)] = &packages.Package{Name: path.Base(p), PkgPath: p}
		}

		return m
	}
	tests := []struct {
		name     string
		client   *client
		want     []string
		wantSeed []string
	}{
		{name: "nothing loaded", client: &client{}, want: nil},
		{
			name:     "loaded packages carry their type-checked paths",
			client:   &client{loadedPackages: loaded("example.com/harbor/pkg/resources", "example.com/harbor/pkg/rpc")},
			want:     []string{"example.com/harbor/pkg/resources", "example.com/harbor/pkg/rpc"},
			wantSeed: []string{"resources=example.com/harbor/pkg/resources", "rpc=example.com/harbor/pkg/rpc"},
		},
		{
			name:     "output packages are the module path plus their directory",
			client:   &client{module: module, outputs: []packageDir{"app", "pkg/router"}, loadedPackages: loaded("example.com/harbor/pkg/resources")},
			want:     []string{"example.com/harbor/app", "example.com/harbor/pkg/resources", "example.com/harbor/pkg/router"},
			wantSeed: []string{"app=example.com/harbor/app", "resources=example.com/harbor/pkg/resources", "router=example.com/harbor/pkg/router"},
		},
		{
			name:   "a nested layout keeps its directories under the module path",
			client: &client{module: module, outputs: []packageDir{"apps/console/pkg/router"}, loadedPackages: loaded("example.com/harbor/apps/console/pkg/resources")},
			want:   []string{"example.com/harbor/apps/console/pkg/resources", "example.com/harbor/apps/console/pkg/router"},
		},
		{
			name:   "a dotted resources directory is a loaded package like any other",
			client: &client{module: module, loadedPackages: map[string]*packages.Package{"sharedresources": {Name: "sharedresources", PkgPath: "example.com/harbor/pkg/sharedresources"}}},
			want:   []string{"example.com/harbor/pkg/sharedresources"},
		},
		{
			name:   "WithImports paths join the set, standard-library ones left out",
			client: &client{imports: []string{"github.com/shopspring/decimal", "time", "cloud.google.com/go/civil"}, loadedPackages: loaded("example.com/harbor/pkg/resources")},
			want:   []string{"cloud.google.com/go/civil", "example.com/harbor/pkg/resources", "github.com/shopspring/decimal"},
		},
		{
			name:   "an output package outside the module is not derived",
			client: &client{module: module, outputs: []packageDir{"../elsewhere", "/src/other/app"}},
			want:   nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tt.want, tt.client.importPaths()); diff != "" {
				t.Errorf("importPaths() mismatch (-want +got):\n%s", diff)
			}
			if tt.wantSeed != nil {
				var got []string
				for _, imp := range tt.client.fixerImports() {
					got = append(got, imp.name+"="+imp.path)
				}
				if diff := cmp.Diff(tt.wantSeed, got); diff != "" {
					t.Errorf("fixerImports() mismatch (-want +got):\n%s", diff)
				}
			}
			block := tt.client.localPackageImports()
			for _, p := range tt.want {
				if !strings.Contains(block, `"`+p+`"`) {
					t.Errorf("localPackageImports() lacks %q:\n%s", p, block)
				}
			}
		})
	}
}

func Test_client_formatGoBytes_resolution(t *testing.T) {
	t.Parallel()

	module := &packages.Module{Path: "example.com/harbor", Dir: "/src/harbor"}
	loaded := map[string]*packages.Package{"resources": {Name: "resources", PkgPath: "example.com/harbor/pkg/resources"}}
	const source = "// Code generated. DO NOT EDIT.\n\npackage app\n\nimport (\n\t\"context\"\n)\n\nfunc use(_ context.Context) any { return resources.Thing{} }\n\nfunc other() any { return widgets.New() }\n"
	tests := []struct {
		name    string
		client  *client
		src     string
		want    []string
		wantErr []string
	}{
		{
			name:   "a loaded package's qualifier resolves to its type-checked path",
			client: &client{module: module, loadedPackages: loaded},
			src:    strings.Replace(source, "\nfunc other() any { return widgets.New() }\n", "", 1),
			want:   []string{`"example.com/harbor/pkg/resources"`},
		},
		{
			name:    "a qualifier the derived set lacks is a generation error naming the file, the template and the qualifier",
			client:  &client{module: module, loadedPackages: loaded},
			src:     source,
			wantErr: []string{"import resolution for app/zz_gen_x.go (template xTemplate)", "cannot resolve qualifier(s) [widgets]", "generator gap"},
		},
		{
			name:   "WithImports resolves it, the package name assumed from the path",
			client: &client{module: module, loadedPackages: loaded, imports: []string{"example.com/vendor/widgets"}},
			src:    source,
			want:   []string{`"example.com/harbor/pkg/resources"`, `"example.com/vendor/widgets"`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.client.formatGoBytes("app/zz_gen_x.go", "xTemplate", []byte(tt.src), nil)
			if len(tt.wantErr) > 0 {
				if err == nil {
					t.Fatalf("formatGoBytes() error = nil, want one naming %v", tt.wantErr)
				}
				for _, want := range tt.wantErr {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error %q lacks %q", err.Error(), want)
					}
				}

				return
			}
			if err != nil {
				t.Fatalf("formatGoBytes() error = %v", err)
			}
			for _, want := range tt.want {
				if !strings.Contains(string(got), want) {
					t.Errorf("output lacks %s:\n%s", want, got)
				}
			}
		})
	}
}

func Test_removeGeneratedFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		create    bool
		method    generatedFileDeleteMethod
		files     map[string]string
		written   []string
		wantKept  []string
		wantError bool
	}{
		{
			name:     "missing directory holds nothing to remove",
			create:   false,
			method:   prefix,
			wantKept: nil,
		},
		{
			name:     "stale generated files go, handwritten files stay",
			create:   true,
			method:   prefix,
			files:    map[string]string{genPrefix + "_ships.go": "x", "ships.go": "x", genPrefix + "_api.ts": "x", "notes.txt": "x"},
			wantKept: []string{"notes.txt", "ships.go"},
		},
		{
			name:     "files the run wrote survive the sweep",
			create:   true,
			method:   prefix,
			files:    map[string]string{genPrefix + "_ships.go": "x", genPrefix + "_hangars.go": "x", genPrefix + "_workflow_ships.dot": "x", "ships.go": "x"},
			written:  []string{genPrefix + "_ships.go", genPrefix + "_workflow_ships.dot"},
			wantKept: []string{"ships.go", genPrefix + "_ships.go", genPrefix + "_workflow_ships.dot"},
		},
		{
			name:     "header files go by their header, written ones stay",
			create:   true,
			method:   headerComment,
			files:    map[string]string{genPrefix + "_api.ts": generationHeader + "\n", genPrefix + "_resources.ts": generationHeader + "\n", "api.service.ts": "// hand-written\n", "old.ts": generationHeader + "\n"},
			written:  []string{genPrefix + "_api.ts"},
			wantKept: []string{"api.service.ts", genPrefix + "_api.ts"},
		},
		{
			name:     "method files go by name alone, other generated files are not the sweep's",
			create:   true,
			method:   methodFiles,
			files:    map[string]string{generatedGoFileName(storageOutputName): "x", generatedGoFileName(jsonOutputName): "x", genPrefix + "_other.go": "x", "payload.go": "x"},
			written:  []string{generatedGoFileName(jsonOutputName)},
			wantKept: []string{"payload.go", genPrefix + "_other.go", generatedGoFileName(jsonOutputName)},
		},
		{
			name:     "empty directory is left as is",
			create:   true,
			method:   prefix,
			wantKept: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(t.TempDir(), "target")
			if tt.create {
				if err := os.MkdirAll(dir, 0o750); err != nil {
					t.Fatalf("os.MkdirAll() error = %v", err)
				}
				for name, content := range tt.files {
					if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
						t.Fatalf("os.WriteFile() error = %v", err)
					}
				}
			}
			written := make(map[string]struct{}, len(tt.written))
			for _, name := range tt.written {
				written[filepath.Join(dir, name)] = struct{}{}
			}

			err := removeGeneratedFiles(dir, tt.method, written)
			if (err != nil) != tt.wantError {
				t.Fatalf("removeGeneratedFiles() error = %v, wantError %v", err, tt.wantError)
			}

			if !tt.create {
				if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("removeGeneratedFiles() created %q, want it untouched", dir)
				}

				return
			}

			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatalf("os.ReadDir() error = %v", err)
			}
			kept := make([]string, 0, len(entries))
			for _, e := range entries {
				kept = append(kept, e.Name())
			}
			slices.Sort(tt.wantKept)
			if diff := cmp.Diff(tt.wantKept, kept); diff != "" {
				t.Errorf("removeGeneratedFiles() kept files mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
