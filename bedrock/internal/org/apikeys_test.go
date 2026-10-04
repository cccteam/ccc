package org

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/api/option"
)

// fakeKeysServer answers the API Keys API's list from a map of pages by project, each
// page a list of keys as the API writes them; a project it lacks has no keys, and a
// project in refuse is answered 403. It records each list's quota project.
type fakeKeysServer struct {
	pages   map[string][][]map[string]any
	refuse  map[string]bool
	billing map[string]string
}

func (f *fakeKeysServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	project, rest, found := strings.Cut(strings.TrimPrefix(r.URL.Path, "/v2/projects/"), "/")
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
	w.Header().Set("Content-Type", "application/json")
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
			lister, err := newKeyLister(t.Context(), option.WithEndpoint(srv.URL), option.WithoutAuthentication(), option.WithHTTPClient(srv.Client()))
			if err != nil {
				t.Fatalf("newKeyLister() error = %v", err)
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

// browserKeyStep is the name of the layers workflow's step that restricts Firebase's
// browser key.
const browserKeyStep = "Restrict Firebase's browser key to the sign-in APIs"

// stepScript is the run script of the named step of a rendered workflow, with the step's
// indentation taken off, failing the test when there is none.
func stepScript(t *testing.T, workflow, name string) string {
	t.Helper()

	_, step, found := strings.Cut(workflow, "      - name: "+name+"\n")
	if !found {
		t.Fatalf("the workflow has no step %q", name)
	}
	_, body, found := strings.Cut(step, "        run: |\n")
	if !found {
		t.Fatalf("the step %q has no run script", name)
	}
	const indent = "          "
	var lines []string
	for _, line := range strings.Split(body, "\n") {
		if line != "" && !strings.HasPrefix(line, indent) {
			break
		}
		lines = append(lines, strings.TrimPrefix(line, indent))
	}

	return strings.Join(lines, "\n")
}

// fakeGcloud is a gcloud, a shell function defined ahead of the step's script, that
// writes each call's arguments to the file CALLS names, one call a line and each argument
// in brackets (so a value split by the shell shows), and answers a key list with the
// names in KEYS, or refuses it when LIST_FAILS is set.
const fakeGcloud = `gcloud() {
  printf '[%s]' "$@" >> "$CALLS"
  printf '\n' >> "$CALLS"
  case "$*" in
    "services api-keys list "*)
      if [ -n "$LIST_FAILS" ]; then
        echo "ERROR: (gcloud.services.api-keys.list) PERMISSION_DENIED" >&2
        return 1
      fi
      printf '%s' "$KEYS"
      ;;
  esac
}
`

// TestBrowserKeyStep runs the layers workflow's step that restricts Firebase's browser key,
// as the rendered workflow carries it, over a fake gcloud: it lists the keys of that name
// in the environment project, the apply identity's own, and restricts each to the two
// sign-in APIs; with none it says so and changes nothing; a refused list fails the step.
// It also reads where the step stands: in the apply job, for 2-env alone, after the
// apply, with gcloud set up for it.
func TestBrowserKeyStep(t *testing.T) {
	t.Parallel()

	const identity = "imp-stg-gbl-tofu@imp-stg-gbl-core-3c4d.iam.gserviceaccount.com"
	list := "[services][api-keys][list][--project][imp-stg-gbl-core-3c4d][--filter][displayName=\"Browser key (auto created by Firebase)\"][--format][value(name)]"
	update := func(key string) string {
		return "[services][api-keys][update][" + key + "][--api-target=service=identitytoolkit.googleapis.com][--api-target=service=securetoken.googleapis.com]"
	}
	tests := []struct {
		name      string
		keys      string
		listFails bool
		wantCalls []string
		wantOut   string
		wantFail  bool
	}{
		{
			name:      "one key of that name, restricted",
			keys:      "projects/100000000003/locations/global/keys/0f1e\n",
			wantCalls: []string{list, update("projects/100000000003/locations/global/keys/0f1e")},
			wantOut:   "Restricted projects/100000000003/locations/global/keys/0f1e (\"Browser key (auto created by Firebase)\") to identitytoolkit.googleapis.com and securetoken.googleapis.com.",
		},
		{
			name:      "two keys of that name, each restricted",
			keys:      "projects/100000000003/locations/global/keys/0f1e\nprojects/100000000003/locations/global/keys/2d3c\n",
			wantCalls: []string{list, update("projects/100000000003/locations/global/keys/0f1e"), update("projects/100000000003/locations/global/keys/2d3c")},
			wantOut:   "Restricted projects/100000000003/locations/global/keys/2d3c",
		},
		{
			name:      "none of that name",
			wantCalls: []string{list},
			wantOut:   "No API key named \"Browser key (auto created by Firebase)\" in imp-stg-gbl-core-3c4d.",
		},
		{
			name:      "a refused list",
			listFails: true,
			wantCalls: []string{list},
			wantOut:   "PERMISSION_DENIED",
			wantFail:  true,
		},
	}
	workflow := renderedFile(t, WorkflowFile)
	script := stepScript(t, workflow, browserKeyStep)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := exec.LookPath("bash"); err != nil {
				t.Skip("no bash to run the step with")
			}
			calls := filepath.Join(t.TempDir(), "calls")
			if err := os.WriteFile(calls, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.CommandContext(t.Context(), "bash", "-e", "-c", fakeGcloud+script)
			cmd.Env = append(os.Environ(), "CALLS="+calls, "KEYS="+tt.keys, "IDENTITY="+identity, "KEY_NAME="+FirebaseBrowserKey)
			if tt.listFails {
				cmd.Env = append(cmd.Env, "LIST_FAILS=1")
			}
			out, err := cmd.CombinedOutput()
			if failed := err != nil; failed != tt.wantFail {
				t.Fatalf("the step failed = %v (%v), want %v; output:\n%s", failed, err, tt.wantFail, out)
			}
			if !strings.Contains(string(out), tt.wantOut) {
				t.Errorf("the step's output lacks %q:\n%s", tt.wantOut, out)
			}
			got, err := os.ReadFile(calls)
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}
			if want := strings.Join(tt.wantCalls, "\n") + "\n"; string(got) != want {
				t.Errorf("gcloud was called with\n%s\nwant\n%s", got, want)
			}
		})
	}

	_, apply, _ := strings.Cut(workflow, "\n  apply:\n")
	wants := []string{
		"      - uses: google-github-actions/setup-gcloud@aa5489c8933f4cc7a4f7d45035b3b1440c9c10db # v3.0.1\n        if: matrix.layer == '1-org' || matrix.layer == '2-env'\n",
		"          exit \"$status\"\n",
		"      - name: " + browserKeyStep + "\n        if: matrix.layer == '2-env'\n        env:\n          KEY_NAME: \"Browser key (auto created by Firebase)\"\n        run: |\n",
	}
	for _, want := range wants {
		if !strings.Contains(apply, want) {
			t.Errorf("the apply job lacks:\n%s", want)
		}
	}
	if strings.Index(apply, "tofu apply -input=false") > strings.Index(apply, browserKeyStep) {
		t.Errorf("the step %q comes before the apply", browserKeyStep)
	}
}

// TestBrowserKeyRestriction reads what the restriction of Firebase's browser key needs and
// says beyond the workflow's step: the role the 2-env layer identity lists and updates
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
			want: []string{"      \"roles/run.admin\",\n      # The layers workflow restricts Firebase's browser key, which initializing\n", "      \"roles/serviceusage.apiKeysAdmin\",\n      \"roles/serviceusage.serviceUsageAdmin\",\n"},
		},
		{
			name: "the role set's table names it",
			path: "1-org/README.md",
			want: []string{"serviceusage.apiKeysAdmin (the layers workflow restricts Firebase's browser key after each apply of `2-env`)"},
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
