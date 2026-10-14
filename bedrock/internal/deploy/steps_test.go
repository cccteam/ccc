package deploy

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/go-playground/errors/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/github/githubtest"
)

// fakeRunner is Runner in tests: it records every command, answers Output from outputs
// by the whole command line ("tofu output -json substitutions") or else by the command's
// name and first argument ("tofu show"), fails the commands fail names the same two ways,
// and runs effect on each Run (docker writing its metadata file).
type fakeRunner struct {
	mu      sync.Mutex
	ran     []Command
	outputs map[string]string
	fail    map[string]error
	effect  func(c Command) error
}

func (r *fakeRunner) key(c Command) string {
	if len(c.Args) == 0 {
		return c.Name
	}

	return c.Name + " " + c.Args[0]
}

// failure is the error set for the command, by its whole line first.
func (r *fakeRunner) failure(c Command) error {
	if err, ok := r.fail[c.String()]; ok {
		return err
	}

	return r.fail[r.key(c)]
}

// output is what the command answers, by its whole line first.
func (r *fakeRunner) output(c Command) string {
	if out, ok := r.outputs[c.String()]; ok {
		return out
	}

	return r.outputs[r.key(c)]
}

func (r *fakeRunner) Run(_ context.Context, c Command, out io.Writer) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ran = append(r.ran, c)
	if err := r.failure(c); err != nil {
		return err
	}
	if r.effect != nil {
		return r.effect(c)
	}
	_, _ = io.WriteString(out, "ran "+c.String()+"\n")

	return nil
}

func (r *fakeRunner) Output(_ context.Context, c Command, _ io.Writer) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ran = append(r.ran, c)
	if err := r.failure(c); err != nil {
		return nil, err
	}

	return []byte(r.output(c)), nil
}

// lines are the commands run, as a log shows them.
func (r *fakeRunner) lines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	lines := make([]string, 0, len(r.ran))
	for _, c := range r.ran {
		lines = append(lines, c.String())
	}

	return lines
}

// fakeSecrets is Secrets in tests: payloads by version name.
type fakeSecrets struct {
	payloads map[string]string
	// states are the versions that exist, by name, with their state.
	states map[string]string
	// containers are the containers that exist with no version (created here, or given);
	// a container with a version in states exists too. denied is how many adds are
	// refused as PermissionDenied first, a fresh grant not yet in effect.
	containers map[string]map[string]string
	denied     int
	// events are what the derivation did: create, grant and add lines.
	events []string
}

func (s *fakeSecrets) openAs(context.Context, string) (Secrets, error) {
	return s, nil
}

func (s *fakeSecrets) State(_ context.Context, version string) (string, error) {
	state, ok := s.states[version]
	if !ok {
		return "", errors.Newf("NotFound: %s", version)
	}

	return state, nil
}

func (s *fakeSecrets) open(context.Context) (Secrets, error) {
	return s, nil
}

func (s *fakeSecrets) Access(_ context.Context, version string) ([]byte, error) {
	payload, ok := s.payloads[version]
	if !ok {
		return nil, errors.Newf("NotFound: %s", version)
	}

	return []byte(payload), nil
}

func (s *fakeSecrets) Container(_ context.Context, name string) (bool, error) {
	if _, ok := s.containers[name]; ok {
		return true, nil
	}
	for version := range s.states {
		if strings.HasPrefix(version, name+"/versions/") {
			return true, nil
		}
	}

	return false, nil
}

func (s *fakeSecrets) Create(_ context.Context, project, id string, labels map[string]string) error {
	if s.containers == nil {
		s.containers = map[string]map[string]string{}
	}
	name := "projects/" + project + "/secrets/" + id
	s.containers[name] = labels
	s.events = append(s.events, "create "+name+" "+labelsLine(labels))

	return nil
}

func (s *fakeSecrets) Newest(_ context.Context, container string) (string, error) {
	newest := 0
	for version, state := range s.states {
		if !strings.HasPrefix(version, container+"/versions/") || state != enabledState {
			continue
		}
		if n, err := strconv.Atoi(path.Base(version)); err == nil && n > newest {
			newest = n
		}
	}
	if newest == 0 {
		return "", nil
	}

	return strconv.Itoa(newest), nil
}

func (s *fakeSecrets) Grant(_ context.Context, container, role, member string) error {
	s.events = append(s.events, "grant "+container+" "+role+" "+member)

	return nil
}

func (s *fakeSecrets) Add(_ context.Context, container string, payload []byte) (string, error) {
	if s.denied > 0 {
		s.denied--

		return "", errors.Wrap(status.Error(codes.PermissionDenied, "Permission 'secretmanager.versions.add' denied"), "secretmanager.Client.AddSecretVersion()")
	}
	newest := 0
	for version := range s.states {
		if strings.HasPrefix(version, container+"/versions/") {
			if n, err := strconv.Atoi(path.Base(version)); err == nil && n > newest {
				newest = n
			}
		}
	}
	version := container + "/versions/" + strconv.Itoa(newest+1)
	if s.states == nil {
		s.states = map[string]string{}
	}
	s.states[version] = enabledState
	decoded, err := base64.StdEncoding.DecodeString(string(payload))
	if err != nil {
		return "", errors.Wrap(err, "base64.Encoding.DecodeString()")
	}
	s.events = append(s.events, fmt.Sprintf("add %s (%d random bytes, base64)", version, len(decoded)))

	return strconv.Itoa(newest + 1), nil
}

// labelsLine spells labels as key=value, sorted.
func labelsLine(labels map[string]string) string {
	pairs := make([]string, 0, len(labels))
	for _, k := range slices.Sorted(maps.Keys(labels)) {
		pairs = append(pairs, k+"="+labels[k])
	}

	return strings.Join(pairs, ",")
}

func (*fakeSecrets) Close() error {
	return nil
}

var (
	appKeyOnce sync.Once
	appKey     *rsa.PrivateKey
	appKeyPEM  string
)

// deployerKey is the test deployer app's private key, and its PEM as GitHub hands it out
// (PKCS#1).
func deployerKey(t *testing.T) (key *rsa.PrivateKey, pemKey string) {
	t.Helper()

	appKeyOnce.Do(func() {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
		appKey = key
		appKeyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	})

	return appKey, appKeyPEM
}

// The deployer app the talking tests register: its App ID, the key's pinned version and
// its installation.
const (
	deployerAppID      = "4242"
	deployerKeyVersion = "projects/p/secrets/deployer-key/versions/3"
	deployerInstall    = 77
)

// githubStandIn is the GitHub stand-in with the deployer app registered and the
// repository added, and the clients' GitHub pointed at it with whatever token a step
// holds.
func githubStandIn(t *testing.T, repo *githubtest.Repo) (*githubtest.Server, GitHubFunc) {
	t.Helper()

	key, _ := deployerKey(t)
	srv := githubtest.New(t)
	srv.AppID, srv.AppKey, srv.AppInstallation = deployerAppID, &key.PublicKey, deployerInstall
	srv.AddRepo("acme", "quill", repo)

	return srv, func(token string) *github.Client {
		return github.New(srv.URL, token)
	}
}

// deployerSubs are the substitutions naming the deployer app.
func deployerSubs(subs map[string]string) map[string]string {
	subs[deployerAppSub], subs[deployerKeySub] = deployerAppID, deployerKeyVersion

	return subs
}

// deployerSecrets holds the deployer app's key at its pinned version.
func deployerSecrets(t *testing.T) *fakeSecrets {
	t.Helper()

	_, pemKey := deployerKey(t)

	return &fakeSecrets{payloads: map[string]string{deployerKeyVersion: pemKey}}
}

// contains reports whether every want is in got.
func containsAll(t *testing.T, got string, wants ...string) {
	t.Helper()

	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}
}
