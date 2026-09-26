package domain

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2/hclwrite"
)

// edits is where the tfvars edit cases live: <case>.tfvars and <case>.want.tfvars.
const edits = "testdata/edit"

func TestAddRegistration(t *testing.T) {
	t.Parallel()

	withNotice := Registration{Domain: "example.dev", YearlyPriceUSD: 12, Notices: []string{"HSTS_PRELOADED"}}
	tests := []struct {
		name    string
		src     string
		r       Registration
		want    string
		wantErr string
	}{
		{name: "an absent map is created at the end", src: "absent.tfvars", r: withNotice, want: "absent.want.tfvars"},
		{name: "an empty map takes the first entry", src: "empty.tfvars", r: withNotice, want: "empty.want.tfvars"},
		{name: "an entry joins the existing ones", src: "existing.tfvars", r: withNotice, want: "existing.want.tfvars"},
		{name: "notices are omitted when there are none", src: "existing.tfvars", r: Registration{Domain: "example.com", YearlyPriceUSD: 12}, want: "existing.nonotices.want.tfvars"},
		{name: "every comment is kept", src: "comments.tfvars", r: withNotice, want: "comments.want.tfvars"},
		{name: "a domain already listed is refused", src: "existing.tfvars", r: Registration{Domain: "example.app", YearlyPriceUSD: 14}, wantErr: "example.app is already in registrations"},
		{name: "a value that is not a map written out is refused", src: "notmap.tfvars", r: withNotice, wantErr: "not a map written out"},
		{name: "a file that does not parse is refused", src: "broken.tfvars", r: withNotice, wantErr: "hclsyntax.ParseConfig()"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			src, err := os.ReadFile(filepath.Join(edits, tt.src))
			if err != nil {
				t.Fatal(err)
			}
			got, err := AddRegistration(src, tt.src, tt.r)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("AddRegistration() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("AddRegistration() error = %v", err)
			}
			// The source is already formatted, so the only difference the golden file
			// may show is the entry added.
			if formatted := hclwrite.Format(src); !bytes.Equal(formatted, src) {
				t.Errorf("%s is not formatted; hclwrite.Format() gives:\n%s", tt.src, formatted)
			}
			want, err := os.ReadFile(filepath.Join(edits, tt.want))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("AddRegistration() =\n%s\nwant\n%s", got, want)
			}
		})
	}
}
