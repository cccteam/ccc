package deploy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// fakeRegistry is Registry in tests: digests by image:tag, and the tags added.
type fakeRegistry struct {
	digests map[string]string
	added   []string
	fail    error
}

func (r *fakeRegistry) open(context.Context) (Registry, error) {
	return r, nil
}

func (r *fakeRegistry) Digest(_ context.Context, image, tag string) (string, error) {
	if r.fail != nil {
		return "", r.fail
	}

	return r.digests[image+":"+tag], nil
}

func (r *fakeRegistry) AddTag(_ context.Context, image, tag, digest string) error {
	r.added = append(r.added, image+":"+tag+"="+digest)
	r.digests[image+":"+tag] = digest

	return nil
}

func TestCheckRelease(t *testing.T) {
	t.Parallel()

	const (
		image       = "us-central1-docker.pkg.dev/shr/repo/harbor"
		environment = "export SKIP_DEPLOY=\"\"\nexport IMAGE=\"" + image + "\"\nexport IMAGE_TAG=\"v1.2.3-tst\"\nexport COMMIT_TAG=\"deadbeef-tst\"\nexport RELEASE=\"v1.2.3\"\n"
	)
	tests := []struct {
		name    string
		env     string
		digests map[string]string
		wantOut []string
		// wantEnv are the facts appended to the environment file; wantAdded the tags added.
		wantEnv   map[string]string
		wantAdded []string
		wantErr   string
	}{
		{
			name:    "a torn-down environment does nothing",
			env:     "export SKIP_DEPLOY=\"true\"\n",
			wantOut: []string{tornDown},
		},
		{
			name:    "a release not built yet is built",
			env:     environment,
			wantOut: []string{"Release v1.2.3 is not in the registry for tst yet; BuildImage builds it."},
			wantEnv: map[string]string{reuseImageFact: ""},
		},
		{
			name:      "a commit already built is named by the release and reused",
			env:       environment,
			digests:   map[string]string{image + ":deadbeef-tst": "sha256:abc"},
			wantOut:   []string{"Commit deadbeef is already built for tst as " + image + "@sha256:abc; naming it v1.2.3-tst and reusing it."},
			wantEnv:   map[string]string{digestFact: "sha256:abc", reuseImageFact: "true"},
			wantAdded: []string{image + ":v1.2.3-tst=sha256:abc"},
		},
		{
			name:    "a release built from this commit is reused",
			env:     environment,
			digests: map[string]string{image + ":deadbeef-tst": "sha256:abc", image + ":v1.2.3-tst": "sha256:abc"},
			wantOut: []string{"Release tag digest: sha256:abc; commit tag digest: sha256:abc", "Reusing " + image + ":v1.2.3-tst@sha256:abc, built from this commit by an earlier run."},
			wantEnv: map[string]string{digestFact: "sha256:abc", reuseImageFact: "true"},
		},
		{
			name:    "a release that names another build is refused",
			env:     environment,
			digests: map[string]string{image + ":v1.2.3-tst": "sha256:other"},
			wantOut: []string{"Release tag digest: sha256:other; commit tag digest: none"},
			wantErr: "Build REJECTED: " + image + ":v1.2.3-tst already exists and was not built from commit deadbeef; a release names one build per environment.",
		},
		{
			name:    "a workspace without the image is refused",
			env:     "export SKIP_DEPLOY=\"\"\n",
			wantErr: "environment.sh names no image or tags (IMAGE, IMAGE_TAG, COMMIT_TAG): the resolve step writes them",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registry := &fakeRegistry{digests: map[string]string{}}
			for key, digest := range tt.digests {
				registry.digests[key] = digest
			}
			w := workspaceFiles(t, map[string]string{EnvironmentFile: tt.env, BuildFile: buildJSON})
			var out strings.Builder
			err := CheckRelease(t.Context(), &Clients{Registry: registry.open}, w, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("CheckRelease() error = %v, wantErr %q; output:\n%s", err, tt.wantErr, out.String())
				}
			} else if err != nil {
				t.Fatalf("CheckRelease() error = %v; output:\n%s", err, out.String())
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output lacks %q:\n%s", want, out.String())
				}
			}
			env, err := w.Environment()
			if err != nil {
				t.Fatal(err)
			}
			for name, want := range tt.wantEnv {
				if got, ok := env[name]; !ok || got != want {
					t.Errorf("%s = %q (present %t), want %q", name, got, ok, want)
				}
			}
			if tt.wantEnv == nil {
				if _, ok := env[reuseImageFact]; ok {
					t.Errorf("environment.sh gained %s: %q", reuseImageFact, env[reuseImageFact])
				}
			}
			if diff := cmp.Diff(tt.wantAdded, registry.added); diff != "" {
				t.Errorf("tags added mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestWorkspaceAppend(t *testing.T) {
	t.Parallel()

	w := workspaceFiles(t, map[string]string{EnvironmentFile: "export IMAGE=\"i\"\n"})
	if err := w.Append(map[string]string{"IMAGE_DIGEST": "sha256:abc", "REUSE_IMAGE": ""}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(string(w), EnvironmentFile))
	if err != nil {
		t.Fatal(err)
	}
	want := "export IMAGE=\"i\"\nexport IMAGE_DIGEST=\"sha256:abc\"\nexport REUSE_IMAGE=\"\"\n"
	if string(data) != want {
		t.Errorf("environment.sh = %q, want %q", data, want)
	}
}

func TestArtifactRegistry(t *testing.T) {
	t.Parallel()

	const pkg = "/v1/projects/shr/locations/us-central1/repositories/repo/packages/harbor"
	// answer encodes what the stand-in says; a status other than 200 comes first.
	answer := func(w http.ResponseWriter, status int, body map[string]any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}
	refusal := func(code int, message string) map[string]any {
		return map[string]any{"error": map[string]any{"code": code, "message": message}}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.RequestURI() {
		case "GET " + pkg + "/tags/v1.2.3-tst":
			answer(w, http.StatusOK, map[string]any{"name": pkg[4:] + "/tags/v1.2.3-tst", keyVersion: pkg[4:] + "/versions/sha256:abc"})
		case "GET " + pkg + "/tags/missing":
			answer(w, http.StatusNotFound, refusal(http.StatusNotFound, "Requested entity was not found."))
		case "POST " + pkg + "/tags?tagId=v1.2.4-tst":
			answer(w, http.StatusOK, map[string]any{"name": pkg[4:] + "/tags/v1.2.4-tst", keyVersion: pkg[4:] + "/versions/sha256:abc"})
		case "GET /v1/projects/shr/locations/us-central1/repositories/repo/packages/nested%2Fapp/tags/t":
			answer(w, http.StatusOK, map[string]any{keyVersion: "x/versions/sha256:nested"})
		default:
			answer(w, http.StatusForbidden, refusal(http.StatusForbidden, "Permission denied on resource"))
		}
	}))
	t.Cleanup(srv.Close)
	registry := &artifactRegistry{http: srv.Client(), base: srv.URL}
	tests := []struct {
		name    string
		call    func(ctx context.Context) (string, error)
		want    string
		wantErr string
	}{
		{
			name: "a tag names its digest",
			call: func(ctx context.Context) (string, error) {
				return registry.Digest(ctx, "us-central1-docker.pkg.dev/shr/repo/harbor", "v1.2.3-tst")
			},
			want: "sha256:abc",
		},
		{
			name: "an absent tag is empty",
			call: func(ctx context.Context) (string, error) {
				return registry.Digest(ctx, "us-central1-docker.pkg.dev/shr/repo/harbor", "missing")
			},
		},
		{
			name: "a nested package is escaped",
			call: func(ctx context.Context) (string, error) {
				return registry.Digest(ctx, "us-central1-docker.pkg.dev/shr/repo/nested/app", "t")
			},
			want: "sha256:nested",
		},
		{
			name: "a tag is added to a digest",
			call: func(ctx context.Context) (string, error) {
				return "", registry.AddTag(ctx, "us-central1-docker.pkg.dev/shr/repo/harbor", "v1.2.4-tst", "sha256:abc")
			},
		},
		{
			name: "a refusal carries the status and the message",
			call: func(ctx context.Context) (string, error) {
				return "", registry.AddTag(ctx, "us-central1-docker.pkg.dev/shr/repo/harbor", "denied", "sha256:abc")
			},
			wantErr: "Artifact Registry answered HTTP 403 to POST " + pkg + "/tags?tagId=denied: Permission denied on resource",
		},
		{
			name: "an image outside Artifact Registry is refused",
			call: func(ctx context.Context) (string, error) {
				return registry.Digest(ctx, "gcr.io/shr/harbor", "v1")
			},
			wantErr: `image "gcr.io/shr/harbor" is not in Artifact Registry`,
		},
		{
			name: "an image without a package is refused",
			call: func(ctx context.Context) (string, error) {
				return registry.Digest(ctx, "us-central1-docker.pkg.dev/shr/repo", "v1")
			},
			wantErr: `image "us-central1-docker.pkg.dev/shr/repo" is not <location>-docker.pkg.dev/<project>/<repository>/<package>`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.call(t.Context())
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
