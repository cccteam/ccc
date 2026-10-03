package deploy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-playground/errors/v5"
	"golang.org/x/oauth2/google"
)

// artifactRegistryAPI is where an image's tags are read and added; keyVersion is the
// field of a tag that names its version, the digest.
const (
	artifactRegistryAPI = "https://artifactregistry.googleapis.com"
	keyVersion          = "version"
)

// Registry reads and names the images in the application's Artifact Registry
// repository: the API, or a fake in tests.
type Registry interface {
	// Digest is the digest the tag names, or empty when the tag does not exist.
	Digest(ctx context.Context, image, tag string) (string, error)
	// AddTag names the digest by another tag.
	AddTag(ctx context.Context, image, tag, digest string) error
}

// RegistryFunc opens the Registry.
type RegistryFunc func(ctx context.Context) (Registry, error)

// NewArtifactRegistry opens the Artifact Registry REST API with the process's default
// credentials: the build's own identity on Cloud Build.
func NewArtifactRegistry(ctx context.Context) (Registry, error) {
	client, err := google.DefaultClient(ctx, cloudPlatformScope)
	if err != nil {
		return nil, errors.Wrap(err, "google.DefaultClient()")
	}

	return &artifactRegistry{http: client, base: artifactRegistryAPI}, nil
}

// artifactRegistry is Registry over the Artifact Registry REST API: a tag is a resource
// under the image's package, naming a version, which is the digest.
type artifactRegistry struct {
	http *http.Client
	base string
}

// imagePackage is an image's place in Artifact Registry: <location>-docker.pkg.dev/
// <project>/<repository>/<package>, as the API names it.
type imagePackage struct {
	location, project, repository, pkg string
}

// parseImage reads an Artifact Registry image path.
func parseImage(image string) (*imagePackage, error) {
	host, rest, ok := strings.Cut(image, "/")
	location, isRegistry := strings.CutSuffix(host, "-docker.pkg.dev")
	if !ok || !isRegistry || location == "" {
		return nil, errors.Newf("image %q is not in Artifact Registry (<location>-docker.pkg.dev/<project>/<repository>/<package>)", image)
	}
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return nil, errors.Newf("image %q is not <location>-docker.pkg.dev/<project>/<repository>/<package>", image)
	}

	return &imagePackage{location: location, project: parts[0], repository: parts[1], pkg: parts[2]}, nil
}

// name is the package's resource name.
func (p *imagePackage) name() string {
	return "projects/" + p.project + "/locations/" + p.location + "/repositories/" + p.repository + "/packages/" + url.PathEscape(p.pkg)
}

func (r *artifactRegistry) Digest(ctx context.Context, image, tag string) (string, error) {
	p, err := parseImage(image)
	if err != nil {
		return "", err
	}
	status, data, err := r.call(ctx, http.MethodGet, "/v1/"+p.name()+"/tags/"+url.PathEscape(tag), nil)
	if err != nil {
		return "", err
	}
	if status == http.StatusNotFound {
		return "", nil
	}
	var answer struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &answer); err != nil {
		return "", errors.Wrap(err, "json.Unmarshal()")
	}
	_, digest, ok := strings.Cut(answer.Version, "/versions/")
	if !ok || digest == "" {
		return "", errors.Newf("tag %s of %s names no version (%q)", tag, image, answer.Version)
	}

	return digest, nil
}

func (r *artifactRegistry) AddTag(ctx context.Context, image, tag, digest string) error {
	p, err := parseImage(image)
	if err != nil {
		return err
	}
	body := map[string]string{keyVersion: p.name() + "/versions/" + digest}
	if _, _, err := r.call(ctx, http.MethodPost, "/v1/"+p.name()+"/tags?tagId="+url.QueryEscape(tag), body); err != nil {
		return err
	}

	return nil
}

// call sends one request and returns the status and the answer's body; a status outside
// 2xx other than 404 is an error carrying the API's message.
func (r *artifactRegistry) call(ctx context.Context, method, path string, body any) (status int, data []byte, err error) {
	var payload io.Reader = http.NoBody
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, nil, errors.Wrap(err, "json.Marshal()")
		}
		payload = strings.NewReader(string(encoded))
	}
	req, err := http.NewRequestWithContext(ctx, method, r.base+path, payload)
	if err != nil {
		return 0, nil, errors.Wrap(err, "http.NewRequestWithContext()")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := r.http.Do(req)
	if err != nil {
		return 0, nil, errors.Wrap(err, "http.Client.Do()")
	}
	defer resp.Body.Close()
	data, err = io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return 0, nil, errors.Wrap(err, "io.ReadAll()")
	}
	if resp.StatusCode == http.StatusNotFound {
		return resp.StatusCode, data, nil
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode > 299 {
		return resp.StatusCode, nil, errors.Newf("Artifact Registry answered HTTP %d to %s %s: %s", resp.StatusCode, method, path, apiMessage(data))
	}

	return resp.StatusCode, data, nil
}

// apiMessage is a Google API refusal's message, or the raw answer when there is none.
func apiMessage(data []byte) string {
	var refusal struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(data, &refusal)
	if refusal.Error.Message != "" {
		return refusal.Error.Message
	}

	return strings.TrimSpace(string(data))
}
