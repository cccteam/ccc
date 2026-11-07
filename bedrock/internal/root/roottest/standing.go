// standing.go seeds the stand-in as the bootstrap leaves a root organization before the
// engine's first apply, so a test starts from an organization that stands in part.

package roottest

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"slices"
	"strconv"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/github/githubtest"
	"github.com/cccteam/ccc/bedrock/internal/org"
	"github.com/cccteam/ccc/bedrock/internal/root"
)

// Standing is a root organization as the bootstrap leaves it before the engine's first
// apply, in a fresh stand-in: the folder (id 100), the root project with its number and
// its APIs, the state bucket, the Bot Account's token and the apps' keys in their secrets
// (as many versions as the placement pins), and on GitHub's stand-in the three apps with
// those keys, installed on the organization, the Cloud Build app, and the repository.
// Nothing the apply makes is there, and the calls recorded start empty.
func Standing(t *testing.T, p *org.Placement, srv *githubtest.Server) *Fake {
	t.Helper()

	f := New()
	ctx := context.Background()
	project := p.Projects[org.RootProjectKey]
	f.Folders["organizations/"+p.OrganizationID+"|"+p.Prefix] = "100"
	f.Projects[project], f.Numbers[project] = true, p.ProjectNumbers[org.RootProjectKey]
	f.Buckets[p.StateBucket] = true
	f.Enabled[project] = slices.Clone(root.Services)
	// The billing export, as it stands once a person pointed it at the dataset the apply
	// makes: the engine's export check is clean from the first apply on.
	f.Landed = root.ExportTables(p.BillingAccount)
	srv.AddRepo(p.GithubOrganization, p.SourceRepo, &githubtest.Repo{})
	srv.PublicApps["google-cloud-build"] = github.App{ID: 10529, Slug: "google-cloud-build", Name: "Google Cloud Build"}
	for _, m := range p.AppManifests() {
		container := p.SecretContainer(m.Role.Key())
		name := "projects/" + project + "/secrets/" + container
		if err := f.CreateSecret(ctx, project, container, nil); err != nil {
			t.Fatal(err)
		}
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
		pinned, _ := strconv.Atoi(p.KeyVersion(m.Role.Key()))
		for range max(pinned, 1) {
			if _, err := f.AddSecretVersion(ctx, name, keyPEM); err != nil {
				t.Fatal(err)
			}
		}
		id, err := strconv.ParseInt(p.AppID(m.Role), 10, 64)
		if err != nil {
			t.Fatalf("%s is App %q: %v", m.Role.Title(), p.AppID(m.Role), err)
		}
		srv.Apps[p.AppID(m.Role)] = &githubtest.App{
			Key:           &key.PublicKey,
			Record:        github.App{ID: id, Slug: m.Name, Name: m.Name, Owner: github.Account{Login: p.GithubOrganization, Type: "Organization"}, Permissions: m.Permissions},
			Installations: []github.Installation{{ID: id + 1, AppID: id, Account: github.Account{Login: p.GithubOrganization, Type: "Organization"}, RepositorySelection: github.AllRepositories}},
		}
	}
	if err := f.CreateSecret(ctx, project, p.TokenSecretID(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := f.AddSecretVersion(ctx, "projects/"+project+"/secrets/"+p.TokenSecretID(), []byte("test-token")); err != nil {
		t.Fatal(err)
	}
	f.Calls = nil

	return f
}
