package integration

// Bootstrap parity for the user-domains endpoint: the shipped personas fetch
// their domain membership through the real engine — the tenant picker's
// source, on the same foothold predicate as concealed tenancy. Structural like
// the digest, so these pins move only when demo_access.json does.

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

func TestUserDomains(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	h := newDemoApp(ctx, t, db)

	tests := []struct {
		name string
		user accesstypes.User
		want []accesstypes.Domain
	}{
		{
			name: "the commander's three stations list sorted",
			user: "commander",
			want: []accesstypes.Domain{"ws-alpha", "ws-beta", "ws-ceres"},
		},
		{
			name: "a technician on two stations lists both",
			user: "tech-rivera",
			want: []accesstypes.Domain{"ws-alpha", "ws-beta"},
		},
		{
			// Global roles (CrewCommon, CatalogViewer, VendorBrowser) never
			// surface: the global scope is not a domain.
			name: "the foreman's single station, global roles excluded",
			user: "foreman-okafor",
			want: []accesstypes.Domain{"ws-alpha"},
		},
		{
			name: "a session with no station roles lists none, as an array",
			user: "visitor-nobody",
			want: []accesstypes.Domain{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequestAs(t, h, tt.user, http.MethodGet, "/api/user-domains", "")
			if status != http.StatusOK {
				t.Fatalf("GET /api/user-domains status = %d, want %d (body %s)", status, http.StatusOK, body)
			}

			var got []accesstypes.Domain
			if err := json.Unmarshal(body, &got); err != nil {
				t.Fatalf("unmarshaling domains %s: %v", body, err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("domains = %v, want %v", got, tt.want)
			}
			if len(tt.want) == 0 && strings.TrimSpace(string(body)) != "[]" {
				t.Errorf("empty membership body = %q, want [] (never null)", body)
			}
		})
	}
}
