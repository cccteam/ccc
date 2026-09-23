package generation

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

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

func Test_client_localPackageImports(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		localPackages []string
		want          string
	}{
		{name: "empty", localPackages: nil, want: ""},
		{
			name:          "external packages render as one quoted group",
			localPackages: []string{"cloud.google.com/go/civil", "github.com/shopspring/decimal"},
			want:          "\"cloud.google.com/go/civil\"\n\t\"github.com/shopspring/decimal\"",
		},
		{
			name:          "stdlib packages are skipped",
			localPackages: []string{"time", "net/http", "github.com/shopspring/decimal"},
			want:          "\"github.com/shopspring/decimal\"",
		},
		{
			name:          "all stdlib renders empty",
			localPackages: []string{"time", "encoding/json"},
			want:          "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &client{localPackages: tt.localPackages}

			if got := c.localPackageImports(); got != tt.want {
				t.Errorf("localPackageImports() = %q, want %q", got, tt.want)
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
