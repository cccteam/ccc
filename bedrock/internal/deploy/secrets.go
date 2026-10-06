// secrets.go is the deploy sequence's seam to Secret Manager: the image build reads the
// build secrets it passes to docker, and the talk-back reads the deployer app's private
// key, each as the build's deploy identity, by the pinned version the stack names.

package deploy

import (
	"context"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"github.com/go-playground/errors/v5"
	"google.golang.org/api/impersonate"
	"google.golang.org/api/option"
)

// Secrets reads secret versions: Secret Manager, or a fake in tests.
type Secrets interface {
	// Access is the payload of the version named projects/<p>/secrets/<s>/versions/<n>.
	Access(ctx context.Context, version string) ([]byte, error)
	// State is the version's state (ENABLED, DISABLED, DESTROYED), read without its
	// payload; a version that does not exist is an error.
	State(ctx context.Context, version string) (string, error)
	Close() error
}

// SecretsFunc opens Secrets.
type SecretsFunc func(ctx context.Context) (Secrets, error)

// SecretsAsFunc opens Secrets as an identity the process's credentials may impersonate.
type SecretsAsFunc func(ctx context.Context, identity string) (Secrets, error)

// NewSecretManager opens Secret Manager with the process's default credentials.
func NewSecretManager(ctx context.Context) (Secrets, error) {
	client, err := secretmanager.NewClient(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "secretmanager.NewClient()")
	}

	return &secretManager{client: client}, nil
}

// NewSecretManagerAs opens Secret Manager as the identity, impersonated with the process's
// default credentials: the plan's tests read a version's state as the identity the plan
// ran as, which may read what the deploy identity may not.
func NewSecretManagerAs(ctx context.Context, identity string) (Secrets, error) {
	source, err := impersonate.CredentialsTokenSource(ctx, impersonate.CredentialsConfig{
		TargetPrincipal: identity,
		Scopes:          []string{"https://www.googleapis.com/auth/cloud-platform"},
	})
	if err != nil {
		return nil, errors.Wrapf(err, "impersonate.CredentialsTokenSource(): %s", identity)
	}
	client, err := secretmanager.NewClient(ctx, option.WithTokenSource(source))
	if err != nil {
		return nil, errors.Wrap(err, "secretmanager.NewClient()")
	}

	return &secretManager{client: client}, nil
}

type secretManager struct {
	client *secretmanager.Client
}

func (s *secretManager) Access(ctx context.Context, version string) ([]byte, error) {
	resp, err := s.client.AccessSecretVersion(ctx, &secretmanagerpb.AccessSecretVersionRequest{Name: version})
	if err != nil {
		return nil, errors.Wrapf(err, "secretmanager.Client.AccessSecretVersion(): %s", version)
	}

	return resp.GetPayload().GetData(), nil
}

func (s *secretManager) State(ctx context.Context, version string) (string, error) {
	resp, err := s.client.GetSecretVersion(ctx, &secretmanagerpb.GetSecretVersionRequest{Name: version})
	if err != nil {
		return "", errors.Wrapf(err, "secretmanager.Client.GetSecretVersion(): %s", version)
	}

	return resp.GetState().String(), nil
}

func (s *secretManager) Close() error {
	if err := s.client.Close(); err != nil {
		return errors.Wrap(err, "secretmanager.Client.Close()")
	}

	return nil
}
