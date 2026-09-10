package cli

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	transition_ "github.com/cccteam/ccc/impulse/internal/transition"
)

func TestComposedOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		opts          composedOptions
		want          []transition
		wantErr       string
		wantDescribe  string
		wantReference string
	}{
		{name: "nothing composed", opts: composedOptions{tenantTable: "Tenants"}, wantReference: transition_.TenancyReferenceCandidate},
		{
			name: "tenancy alone", opts: composedOptions{tenancy: true, tenantTable: "Clients"},
			want:         []transition{transition_.Tenancy{Table: "Clients"}},
			wantDescribe: "tenancy (Clients)", wantReference: transition_.TenancyReferenceCandidate,
		},
		{
			name: "tenancy then outlets, in the order given", opts: composedOptions{tenancy: true, tenantTable: "Tenants", outlets: []string{"portal=portal/api", "kiosk=kiosk/api"}, apiOutlets: []string{"machines=machines"}},
			want: []transition{
				transition_.Tenancy{Table: "Tenants"},
				transition_.Outlet{Name: "portal", Prefix: "portal/api", Sessions: true},
				transition_.Outlet{Name: "kiosk", Prefix: "kiosk/api", Sessions: true},
				transition_.Outlet{Name: "machines", Prefix: "machines"},
			},
			wantDescribe:  "tenancy (Tenants), the session outlet portal=portal/api, the session outlet kiosk=kiosk/api, the API-key outlet machines=machines",
			wantReference: transition_.ReferenceCandidate,
		},
		{
			name: "sites promote the base and add the rest", opts: composedOptions{tenancy: true, tenantTable: "Tenants", sites: []string{"console", "portal", "kiosk"}},
			want: []transition{
				transition_.Tenancy{Table: "Tenants"},
				transition_.Site{Name: "portal", First: "console"},
				transition_.Site{Name: "kiosk"},
			},
			wantDescribe:  "tenancy (Tenants), the sites console, portal, kiosk (the base site becomes console)",
			wantReference: transition_.SitesReference,
		},
		{
			name: "a directory flavor for the first auth composes first, fresh", opts: composedOptions{authName: "staff", flavor: transition_.FlavorOIDCGoogle, authority: transition_.AuthorityDirectory, tenancy: true, tenantTable: "Tenants"},
			want: []transition{
				transition_.AuthFlavor{Name: "staff", Flavor: transition_.FlavorOIDCGoogle, Authority: transition_.AuthorityDirectory, Fresh: true},
				transition_.Tenancy{Table: "Tenants"},
			},
			wantDescribe:  "the oidc-google flavor for the staff auth, role membership the directory's, tenancy (Tenants)",
			wantReference: transition_.ReferenceCandidate,
		},
		{name: "one site is no layout", opts: composedOptions{sites: []string{"console"}}, wantErr: "name at least two sites"},
		{name: "an outlet without a prefix", opts: composedOptions{outlets: []string{"portal"}}, wantErr: `--outlet "portal": name the outlet and its prefix`},
		{name: "an API outlet without a name", opts: composedOptions{apiOutlets: []string{"=machines"}}, wantErr: `--api-outlet "=machines"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.opts.transitions()
			switch {
			case tt.wantErr != "":
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("transitions() error = %v, want %q", err, tt.wantErr)
				}

				return
			case err != nil:
				t.Fatalf("transitions() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("transitions() mismatch (-want +got):\n%s", diff)
			}
			if d := tt.opts.describe(); d != tt.wantDescribe {
				t.Errorf("describe() = %q, want %q", d, tt.wantDescribe)
			}
			if r := tt.opts.reference(); r != tt.wantReference {
				t.Errorf("reference() = %q, want %q", r, tt.wantReference)
			}
		})
	}
}
