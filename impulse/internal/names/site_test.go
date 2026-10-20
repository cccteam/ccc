package names

import (
	"strings"
	"testing"
)

func TestValidateSite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		site    string
		wantErr string
	}{
		{name: "the console", site: "console"},
		{name: "the portal", site: "portal"},
		{name: "a kiosk", site: "kiosk"},
		{name: "digits after the first letter", site: "app2"},
		{name: "the longest allowed", site: strings.Repeat("s", 25)},
		{name: "empty", site: "", wantErr: `site name "": ` + SiteGuidance},
		{name: "uppercase", site: "Portal", wantErr: `site name "Portal": ` + SiteGuidance},
		{name: "a hyphen", site: "my-site", wantErr: `site name "my-site": ` + SiteGuidance},
		{name: "a leading digit", site: "2nd", wantErr: `site name "2nd": ` + SiteGuidance},
		{name: "an underscore", site: "con_sole", wantErr: `site name "con_sole": ` + SiteGuidance},
		{name: "one over the limit", site: strings.Repeat("s", 26), wantErr: `site name "` + strings.Repeat("s", 26) + `" is 26 characters, and 25 is the most a site name may take`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateSite(tt.site)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateSite() error = %v", err)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ValidateSite() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestMaxSiteName(t *testing.T) {
	t.Parallel()

	// The cap is what Cloud Run's limit leaves the site after the service name's other
	// segments at the organization's maxima, and the guidance states it.
	if maxSiteName != 25 {
		t.Errorf("maxSiteName = %d, want 25", maxSiteName)
	}
	if !strings.Contains(SiteGuidance, "at most 25 characters") {
		t.Errorf("SiteGuidance = %q, want it to state the cap", SiteGuidance)
	}
}
