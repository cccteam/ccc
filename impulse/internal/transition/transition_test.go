package transition

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/mod/modfile"
)

func TestRequirementChanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		before string
		after  string
		want   []string
	}{
		{
			name:   "go.mod stands",
			before: "module m\n\ngo 1.26\n\nrequire github.com/spf13/cobra v1.10.2 // indirect\n",
			after:  "module m\n\ngo 1.26\n\nrequire github.com/spf13/cobra v1.10.2 // indirect\n",
		},
		{
			name:   "a module the options are known to pull in, required anew",
			before: "module m\n\ngo 1.26\n",
			after:  "module m\n\ngo 1.26\n\nrequire github.com/coreos/go-oidc/v3 v3.21.0 // indirect\n",
			want:   []string{"go.mod: requires github.com/coreos/go-oidc/v3 v3.21.0, the OIDC library the session library's directory methods sign in through (go mod tidy)"},
		},
		{
			name:   "a module outside the list, required anew",
			before: "module m\n\ngo 1.26\n",
			after:  "module m\n\ngo 1.26\n\nrequire example.com/lib v1.2.3\n",
			want:   []string{"go.mod: requires example.com/lib v1.2.3 (go mod tidy)"},
		},
		{
			name:   "a requirement made direct",
			before: "module m\n\ngo 1.26\n\nrequire github.com/spf13/cobra v1.10.2 // indirect\n",
			after:  "module m\n\ngo 1.26\n\nrequire github.com/spf13/cobra v1.10.2\n",
			want:   []string{"go.mod: requires github.com/spf13/cobra v1.10.2 directly, the command tree's library (go mod tidy)"},
		},
		{
			name:   "a requirement moved to another version",
			before: "module m\n\ngo 1.26\n\nrequire golang.org/x/oauth2 v0.36.0 // indirect\n",
			after:  "module m\n\ngo 1.26\n\nrequire golang.org/x/oauth2 v0.37.0 // indirect\n",
			want:   []string{"go.mod: requires golang.org/x/oauth2 v0.37.0 in place of v0.36.0 (go mod tidy)"},
		},
		{
			name:   "a requirement dropped, after the ones added",
			before: "module m\n\ngo 1.26\n\nrequire example.com/old v1.0.0\n",
			after:  "module m\n\ngo 1.26\n\nrequire example.com/new v1.0.0\n",
			want: []string{
				"go.mod: requires example.com/new v1.0.0 (go mod tidy)",
				"go.mod: no longer requires example.com/old (go mod tidy)",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			before, err := modfile.Parse("go.mod", []byte(tt.before), nil)
			if err != nil {
				t.Fatal(err)
			}
			after, err := modfile.Parse("go.mod", []byte(tt.after), nil)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tt.want, requirementChanges(before, after)); diff != "" {
				t.Errorf("requirementChanges() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
