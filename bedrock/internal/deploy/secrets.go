// secrets.go is the deploy sequence's seam to Secret Manager: the image build reads the
// build secrets it passes to docker, and the talk-back reads the deployer app's private
// key, each as the build's deploy identity, by the pinned version the stack names.

package deploy

import (
	"context"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"github.com/go-playground/errors/v5"
)

// Secrets reads secret versions: Secret Manager, or a fake in tests.
type Secrets interface {
	// Access is the payload of the version named projects/<p>/secrets/<s>/versions/<n>.
	Access(ctx context.Context, version string) ([]byte, error)
	Close() error
}

// SecretsFunc opens Secrets.
type SecretsFunc func(ctx context.Context) (Secrets, error)

// NewSecretManager opens Secret Manager with the process's default credentials.
func NewSecretManager(ctx context.Context) (Secrets, error) {
	client, err := secretmanager.NewClient(ctx)
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

func (s *secretManager) Close() error {
	if err := s.client.Close(); err != nil {
		return errors.Wrap(err, "secretmanager.Client.Close()")
	}

	return nil
}
