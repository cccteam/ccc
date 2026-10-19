package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

// loginMethods is what an outlet's methods route tells the login page: the sign-in
// methods the outlet offers, in the order the generator program registered them, each
// with the path the router mounted it at, and the pending sign-in's routes.
type loginMethods struct {
	Methods []struct {
		Name   string `json:"name"`
		Method string `json:"method"`
		Kind   string `json:"kind"`
		Label  string `json:"label"`
		Path   string `json:"path"`
	} `json:"methods"`
	Pending struct {
		Status  string `json:"status"`
		Confirm string `json:"confirm"`
		Cancel  string `json:"cancel"`
	} `json:"pending"`
}

// TestLoginMethods reads each outlet's methods route the way its login page does, before
// any sign-in: the console offers the password and the fleet's Google directory, the
// portal the company directory alone, each method at the path the generated router
// mounted it, and every path is under the outlet's own prefix.
//
// Demonstrates: auth.multi-method, auth.login-methods.
func TestLoginMethods(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)

	tests := []struct {
		name   string
		prefix string
		want   string
	}{
		{
			name:   "the console offers the password and the directory",
			prefix: consoleAPI,
			want: `{"methods":[` +
				`{"name":"password","method":"password","kind":"password","label":"Password","path":"/console/api/user/password/login"},` +
				`{"name":"google","method":"google","kind":"redirect","label":"Google","path":"/console/api/user/google/login"}],` +
				`"pending":{"status":"/console/api/user/pending","confirm":"/console/api/user/pending/confirm","cancel":"/console/api/user/pending/cancel"}}`,
		},
		{
			name:   "the portal offers the company directory alone",
			prefix: portalAPI,
			want: `{"methods":[` +
				`{"name":"directory","method":"google","kind":"redirect","label":"Company directory","path":"/portal/api/user/directory/login"}],` +
				`"pending":{"status":"/portal/api/user/pending","confirm":"/portal/api/user/pending/confirm","cancel":"/portal/api/user/pending/cancel"}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			b := newBrowser(t, s, tt.prefix)
			status, body := b.do(ctx, http.MethodGet, tt.prefix+"/user/methods", nil)
			if status != http.StatusOK {
				t.Fatalf("GET %s/user/methods: status %d: %s", tt.prefix, status, body)
			}
			// Both sides are read into the same shape and written back out, so the
			// comparison is over the fields the page reads, in one spelling.
			if got, want := canonical(t, body), canonical(t, []byte(tt.want)); got != want {
				t.Errorf("GET %s/user/methods = %s, want %s", tt.prefix, got, want)
			}
		})
	}
}

// canonical reads a methods answer and writes it back in the struct's field order.
func canonical(t *testing.T, body []byte) string {
	t.Helper()

	var list loginMethods
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("decoding the methods: %v: %s", err, body)
	}
	out, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}

	return string(out)
}
