package render

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestUndeclaredVariables(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		files map[string][]byte
		want  []UndeclaredVariable
	}{
		{
			name: "a reference declared in another file is not a finding",
			files: map[string][]byte{
				"locals.tf":    []byte("locals {\n  owner = var.owner\n}\n"),
				"variables.tf": []byte("variable \"owner\" {\n  type = string\n}\n"),
			},
		},
		{
			name: "a reference no file declares is found at its line",
			files: map[string][]byte{
				"locals.tf":    []byte("locals {\n  owner = var.owner\n}\n"),
				"variables.tf": []byte("variable \"other\" {\n  type = string\n}\n"),
			},
			want: []UndeclaredVariable{{Path: "locals.tf", Line: 2, Name: "owner"}},
		},
		{
			name: "a reference in a comment line is not read",
			files: map[string][]byte{
				"locals.tf": []byte("# set from var.owner once\n// or var.holder\nlocals {\n  owner = \"x\"\n}\n"),
			},
		},
		{
			name: "a reference in a string or a heredoc is read",
			files: map[string][]byte{
				"outputs.tf": []byte("output \"o\" {\n  value = \"${var.owner}-x\"\n  description = <<-EOT\n    from var.holder\n  EOT\n}\n"),
			},
			want: []UndeclaredVariable{{Path: "outputs.tf", Line: 2, Name: "owner"}, {Path: "outputs.tf", Line: 4, Name: "holder"}},
		},
		{
			name: "one name twice on a line is one finding; several names are each found",
			files: map[string][]byte{
				"a.tf": []byte("locals {\n  x = var.one == \"\" || can(regex(\"^a$\", var.one)) ? var.two : var.one\n}\n"),
			},
			want: []UndeclaredVariable{{Path: "a.tf", Line: 2, Name: "one"}, {Path: "a.tf", Line: 2, Name: "two"}},
		},
		{
			name: "findings are ordered by file, then line, then name",
			files: map[string][]byte{
				"b.tf": []byte("locals {\n  x = var.zed\n  y = var.alpha\n}\n"),
				"a.tf": []byte("locals {\n  x = var.mid\n}\n"),
			},
			want: []UndeclaredVariable{{Path: "a.tf", Line: 2, Name: "mid"}, {Path: "b.tf", Line: 2, Name: "zed"}, {Path: "b.tf", Line: 3, Name: "alpha"}},
		},
		{
			name: "a declaration with a hyphenated name and odd spacing is read",
			files: map[string][]byte{
				"variables.tf": []byte("  variable   \"my-var\"{\n}\n"),
				"locals.tf":    []byte("locals {\n  x = var.my-var\n}\n"),
			},
		},
		{
			name: "a local or a module attribute is not a variable reference",
			files: map[string][]byte{
				"locals.tf": []byte("locals {\n  x = local.var.owner\n  y = module.vars.var_name\n}\n"),
			},
			want: []UndeclaredVariable{{Path: "locals.tf", Line: 2, Name: "owner"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tt.want, UndeclaredVariables(tt.files)); diff != "" {
				t.Errorf("UndeclaredVariables() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
