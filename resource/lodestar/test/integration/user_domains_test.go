package integration

// Bootstrap parity for the user-domains endpoint — the star chart's source: the
// sectors where each persona holds at least one grant, on the same foothold predicate
// as concealed tenancy. Cinder lights only for headquarters, the archivist, and the
// hazard analyst.

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

	_, h, _ := sharedWorld(t)

	tests := []struct {
		name string
		user accesstypes.User
		want []accesstypes.Domain
	}{
		{name: "the governor's three sectors list sorted", user: "governor", want: []accesstypes.Domain{anvil, bastion, cinder}},
		{name: "the archivist reads every sector", user: "archivist", want: []accesstypes.Domain{anvil, bastion, cinder}},
		{name: "the hazard analyst holds Anvil and Cinder", user: "hazards", want: []accesstypes.Domain{anvil, cinder}},
		{name: "the pilot holds Anvil and Bastion", user: "pilot", want: []accesstypes.Domain{anvil, bastion}},
		{name: "the marshal's single sector, global roles excluded", user: "marshal", want: []accesstypes.Domain{anvil}},
		{name: "the client's portal role lights Anvil", user: "client", want: []accesstypes.Domain{anvil}},
		{name: "a session with no sector roles lists none, as an array", user: "visitor-nobody", want: []accesstypes.Domain{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequestAs(t, h, tt.user, http.MethodGet, "/api/user-domains", "")
			assertStatus(t, status, http.StatusOK, body)

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
