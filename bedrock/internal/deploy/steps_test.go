package deploy

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/github/githubtest"
)

// fakeRunner is Runner in tests: it records every command, answers Output from outputs
// by the command's name and first argument ("tofu show"), fails the commands fail
// names, and runs effect on each Run (docker writing its metadata file).
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

func (r *fakeRunner) Run(_ context.Context, c Command, out io.Writer) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ran = append(r.ran, c)
	if err := r.fail[r.key(c)]; err != nil {
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
	if err := r.fail[r.key(c)]; err != nil {
		return nil, err
	}

	return []byte(r.outputs[r.key(c)]), nil
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
