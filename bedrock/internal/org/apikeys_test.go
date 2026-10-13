package org

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/api/option"
)

// fakeKeysServer answers the API Keys API's list from a map of pages by project, each
// page a list of keys as the API writes them; a project it lacks has no keys, and a
// project in refuse is answered 403. It records each call's quota project. A patch of a
// key's restrictions records the services in restricted and answers an operation that
// is done after polls reads of it, with failOp as its error when set.
type fakeKeysServer struct {
	pages      map[string][][]map[string]any
	refuse     map[string]bool
	billing    map[string]string
	restricted map[string][]string
	polls      int
	failOp     string
	mu         sync.Mutex
	polled     map[string]int
}

func (f *fakeKeysServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if strings.HasPrefix(r.URL.Path, "/v2/operations/") && r.Method == http.MethodGet {
		f.operation(w, strings.TrimPrefix(r.URL.Path, "/v2/operations/"))

		return
	}
	project, rest, found := strings.Cut(strings.TrimPrefix(r.URL.Path, "/v2/projects/"), "/")
	if found && r.Method == http.MethodPatch && strings.HasPrefix(rest, "locations/global/keys/") {
		f.patch(w, r, project)

		return
	}
	if !found || rest != "locations/global/keys" || r.Method != http.MethodGet {
		http.Error(w, `{"error": {"code": 404, "message": "not a key list"}}`, http.StatusNotFound)

		return
	}
	f.billing[project] = r.Header.Get(userProjectHeader)
	if f.refuse[project] {
		http.Error(w, `{"error": {"code": 403, "message": "Permission denied on apikeys.keys.list"}}`, http.StatusForbidden)

		return
	}
	pages := f.pages[project]
	page := 0
	if token := r.URL.Query().Get("pageToken"); token != "" {
		page = len(token)
	}
	resp := map[string]any{}
	if page < len(pages) {
		resp["keys"] = pages[page]
	}
	if page+1 < len(pages) {
		resp["nextPageToken"] = strings.Repeat("p", page+1)
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// patch records a key's new restrictions and answers the operation, done at once when
// nothing is to be polled.
func (f *fakeKeysServer) patch(w http.ResponseWriter, r *http.Request, project string) {
	if r.URL.Query().Get("updateMask") != "restrictions" {
		http.Error(w, `{"error": {"code": 400, "message": "the update mask is not restrictions"}}`, http.StatusBadRequest)

		return
	}
	if r.Header.Get(userProjectHeader) != project {
		http.Error(w, `{"error": {"code": 400, "message": "the quota project is not the key's"}}`, http.StatusBadRequest)

		return
	}
	var body struct {
		Restrictions struct {
			APITargets []struct {
				Service string `json:"service"`
			} `json:"apiTargets"`
		} `json:"restrictions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error": {"code": 400, "message": "not a key"}}`, http.StatusBadRequest)

		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/v2/")
	var services []string
	for _, t := range body.Restrictions.APITargets {
		services = append(services, t.Service)
	}
	f.mu.Lock()
	f.restricted[name] = services
	f.mu.Unlock()
	id := strings.ReplaceAll(name, "/", "-")
	if f.polls == 0 && f.failOp == "" {
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "operations/" + id, "done": true})

		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"name": "operations/" + id, "done": false})
}

// operation answers a patch's operation: pending until polled polls times, then done,
// with the error failOp names when set.
func (f *fakeKeysServer) operation(w http.ResponseWriter, id string) {
	f.mu.Lock()
	if f.polled == nil {
		f.polled = map[string]int{}
	}
	f.polled[id]++
	polled := f.polled[id]
	f.mu.Unlock()
	if polled < f.polls {
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "operations/" + id, "done": false})

		return
	}
	resp := map[string]any{"name": "operations/" + id, "done": true}
	if f.failOp != "" {
		resp["error"] = map[string]any{"code": 7, "message": f.failOp}
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// TestUnrestrictedKeys lists the environment projects' keys through the API Keys client
// over a fake server and keeps those with no API restriction: a key restricted to the
// sign-in APIs is not named, Firebase's browser key unrestricted is, a key restricted by
// referrer alone is too (an application restriction is not an API restriction), a
// project with no keys names none, the keys of every page are read, a project the
// placement does not record is not read, and a refused list is an error. Each list is
// billed to the project it reads.
func TestUnrestrictedKeys(t *testing.T) {
	t.Parallel()

	const (
		tst = "imp-tst-gbl-core-1a2b"
		stg = "imp-stg-gbl-core-3c4d"
	)
	firebase := func(project string, restrictions map[string]any) map[string]any {
		key := map[string]any{"name": "projects/" + project + "/locations/global/keys/0f1e2d3c", "displayName": FirebaseBrowserKey}
		if restrictions != nil {
			key["restrictions"] = restrictions
		}

		return key
	}
	signIn := map[string]any{"apiTargets": []map[string]any{{"service": "identitytoolkit.googleapis.com"}, {"service": "securetoken.googleapis.com"}}}
	referrers := map[string]any{"browserKeyRestrictions": map[string]any{"allowedReferrers": []string{"https://harbor.imp.example/*"}}}
	tests := []struct {
		name           string
		pages          map[string][][]map[string]any
		refuse         map[string]bool
		unrecord       []string
		want           []UnrestrictedKey
		wantUnrecorded []string
		wantErr        string
	}{
		{
			name:  "the browser key restricted to the sign-in APIs",
			pages: map[string][][]map[string]any{tst: {{firebase(tst, signIn)}}},
		},
		{
			name:  "the browser key unrestricted",
			pages: map[string][][]map[string]any{stg: {{firebase(stg, nil)}}},
			want: []UnrestrictedKey{{
				Environment: "stg", Project: stg,
				Key: APIKey{Name: "projects/" + stg + "/locations/global/keys/0f1e2d3c", DisplayName: FirebaseBrowserKey},
			}},
		},
		{
			name:  "a key restricted by referrer alone",
			pages: map[string][][]map[string]any{tst: {{{"name": "projects/" + tst + "/locations/global/keys/9a8b", "displayName": "a referrer key", "restrictions": referrers}}}},
			want: []UnrestrictedKey{{
				Environment: "tst", Project: tst,
				Key: APIKey{Name: "projects/" + tst + "/locations/global/keys/9a8b", DisplayName: "a referrer key"},
			}},
		},
		{
			name: "no keys anywhere",
		},
		{
			name: "every page read",
			pages: map[string][][]map[string]any{tst: {
				{{"name": "projects/" + tst + "/locations/global/keys/a", "displayName": "Firebase web API key - harbor", "restrictions": signIn}},
				{firebase(tst, nil)},
			}},
			want: []UnrestrictedKey{{
				Environment: "tst", Project: tst,
				Key: APIKey{Name: "projects/" + tst + "/locations/global/keys/0f1e2d3c", DisplayName: FirebaseBrowserKey},
			}},
		},
		{
			name:           "projects the placement does not record are not read",
			pages:          map[string][][]map[string]any{stg: {{firebase(stg, nil)}}},
			unrecord:       []string{"stg", "prd"},
			wantUnrecorded: []string{"stg", "prd"},
		},
		{
			name:    "a refused list",
			refuse:  map[string]bool{tst: true},
			wantErr: "apikeys.ProjectsLocationsKeysListCall.Pages(): " + tst,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fake := &fakeKeysServer{pages: tt.pages, refuse: tt.refuse, billing: map[string]string{}}
			srv := httptest.NewServer(fake)
			t.Cleanup(srv.Close)
			lister, err := newKeyClient(t.Context(), option.WithEndpoint(srv.URL), option.WithoutAuthentication(), option.WithHTTPClient(srv.Client()))
			if err != nil {
				t.Fatalf("newKeyClient() error = %v", err)
			}
			defer lister.Close()
			p := testPlacement(t)
			for _, env := range tt.unrecord {
				delete(p.Projects, env)
			}
			got, unrecorded, err := UnrestrictedKeys(t.Context(), p, lister)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("UnrestrictedKeys() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("UnrestrictedKeys() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("UnrestrictedKeys() = %+v, want %+v", got, tt.want)
			}
			if !reflect.DeepEqual(unrecorded, tt.wantUnrecorded) {
				t.Errorf("unrecorded = %v, want %v", unrecorded, tt.wantUnrecorded)
			}
			for project, billed := range fake.billing {
				if billed != project {
					t.Errorf("the list of %s was billed to %q, want the project itself", project, billed)
				}
			}
			if len(fake.billing) != len(Environments)-len(tt.unrecord) {
				t.Errorf("%d project(s) read, want %d", len(fake.billing), len(Environments)-len(tt.unrecord))
			}
		})
	}
}

// TestRestrictBrowserKeys restricts Firebase's browser key through the API Keys client
// over a fake server, as bedrock org apply does after each apply of 2-env: each key of
// that name in the environment project is patched to the two sign-in APIs and the
// patch's operation is waited for; a key of another name is left alone; with none it
// says so and changes nothing; a refused list and a failed operation are errors.
func TestRestrictBrowserKeys(t *testing.T) {
	t.Parallel()

	const project = "imp-stg-gbl-core-3c4d"
	key := func(id, name string) map[string]any {
		return map[string]any{"name": "projects/" + project + "/locations/global/keys/" + id, "displayName": name}
	}
	signIn := SignInAPIs
	tests := []struct {
		name           string
		pages          [][]map[string]any
		refuse         bool
		polls          int
		failOp         string
		wantRestricted map[string][]string
		wantOut        []string
		wantErr        string
	}{
		{
			name:           "one key of that name, restricted after its operation finishes",
			pages:          [][]map[string]any{{key("0f1e", FirebaseBrowserKey)}},
			polls:          2,
			wantRestricted: map[string][]string{"projects/" + project + "/locations/global/keys/0f1e": signIn},
			wantOut:        []string{"Restricted projects/" + project + "/locations/global/keys/0f1e (\"Browser key (auto created by Firebase)\") to identitytoolkit.googleapis.com and securetoken.googleapis.com.\n"},
		},
		{
			name:  "two keys of that name, each restricted, and one of another name left alone",
			pages: [][]map[string]any{{key("0f1e", FirebaseBrowserKey), key("9a8b", "a referrer key")}, {key("2d3c", FirebaseBrowserKey)}},
			wantRestricted: map[string][]string{
				"projects/" + project + "/locations/global/keys/0f1e": signIn,
				"projects/" + project + "/locations/global/keys/2d3c": signIn,
			},
			wantOut: []string{"Restricted projects/" + project + "/locations/global/keys/0f1e", "Restricted projects/" + project + "/locations/global/keys/2d3c"},
		},
		{
			name:    "none of that name",
			pages:   [][]map[string]any{{key("9a8b", "a referrer key")}},
			wantOut: []string{"No API key named \"Browser key (auto created by Firebase)\" in " + project + ".\n"},
		},
		{
			name:    "a refused list",
			refuse:  true,
			wantErr: "apikeys.ProjectsLocationsKeysListCall.Pages(): " + project,
		},
		{
			name:    "a failed operation",
			pages:   [][]map[string]any{{key("0f1e", FirebaseBrowserKey)}},
			polls:   1,
			failOp:  "the caller does not have permission",
			wantErr: "the restriction of projects/" + project + "/locations/global/keys/0f1e failed: the caller does not have permission",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fake := &fakeKeysServer{pages: map[string][][]map[string]any{project: tt.pages}, refuse: map[string]bool{project: tt.refuse}, billing: map[string]string{}, restricted: map[string][]string{}, polls: tt.polls, failOp: tt.failOp}
			srv := httptest.NewServer(fake)
			t.Cleanup(srv.Close)
			client, err := newKeyClient(t.Context(), option.WithEndpoint(srv.URL), option.WithoutAuthentication(), option.WithHTTPClient(srv.Client()))
			if err != nil {
				t.Fatalf("newKeyClient() error = %v", err)
			}
			defer client.Close()
			keys, ok := client.(*apiKeys)
			if !ok {
				t.Fatalf("newKeyClient() = %T, want *apiKeys", client)
			}
			keys.poll = time.Millisecond
			var out bytes.Buffer
			err = RestrictBrowserKeys(t.Context(), client, project, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("RestrictBrowserKeys() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("RestrictBrowserKeys() error = %v", err)
			}
			if len(fake.restricted)+len(tt.wantRestricted) > 0 && !reflect.DeepEqual(fake.restricted, tt.wantRestricted) {
				t.Errorf("restricted = %v, want %v", fake.restricted, tt.wantRestricted)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output lacks %q:\n%s", want, out.String())
				}
			}
			if billed := fake.billing[project]; billed != project {
				t.Errorf("billed to %q, want the project itself", billed)
			}
		})
	}
}

// TestBrowserKeyRestriction reads what the restriction of Firebase's browser key needs and
// says beyond bedrock org apply: the role the 2-env layer identity lists and updates
// the project's keys with, in 1-org's app role set, and the 2-env README's Identity
// Platform section, which says what the key is, why it is restricted rather than
// declared or deleted, and how to restrict it by hand.
func TestBrowserKeyRestriction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want []string
	}{
		{
			name: "the app role set holds the API keys admin role",
			path: "1-org/variables.tf",
			want: []string{"      \"roles/run.admin\",\n      # bedrock org apply restricts Firebase's browser key, which initializing\n", "      \"roles/serviceusage.apiKeysAdmin\",\n      \"roles/serviceusage.serviceUsageAdmin\",\n"},
		},
		{
			name: "the role set's table names it",
			path: "1-org/README.md",
			want: []string{"serviceusage.apiKeysAdmin (`bedrock org apply` restricts Firebase's browser key after each apply of `2-env`)"},
		},
		{
			name: "the README's Identity Platform section",
			path: "2-env/README.md",
			want: []string{
				"### Identity Platform\n",
				"the project, named \"Browser key (auto created by Firebase)\", with no restriction\nof any kind.",
				"`identitytoolkit.googleapis.com` and `securetoken.googleapis.com`. Left alone, it would\nbe a standing credential with no owner.",
				"No layer can declare it. OpenTofu adopts a key that already exists only by\nits id, which Firebase assigns",
				"`bedrock org check` names every key in an environment project that carries\nno API restriction",
				"gcloud services api-keys update \"$key\" --api-target=service=identitytoolkit.googleapis.com --api-target=service=securetoken.googleapis.com\n",
			},
		},
		{
			name: "the layer's Identity Platform file points at it",
			path: "2-env/identity-platform.tf",
			want: []string{"# restriction, \"Browser key (auto created by Firebase)\". No resource here\n", "(README.md, Identity Platform)"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			content := renderedFile(t, tt.path)
			for _, w := range tt.want {
				if !strings.Contains(content, w) {
					t.Errorf("%s lacks:\n%s", tt.path, w)
				}
			}
		})
	}
}
