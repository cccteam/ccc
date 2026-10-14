// secrets.go is the deploy sequence's seam to Secret Manager: the image build reads the
// build secrets it passes to docker, and the talk-back reads the deployer app's private
// key, each as the build's deploy identity, by the pinned version the stack names; the
// plan steps read the generated secrets' containers as the identity they plan as, and a
// tag build's seeds one (generated.go).

package deploy

import (
	"context"
	"path"
	"strconv"

	"cloud.google.com/go/iam"
	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"github.com/go-playground/errors/v5"
	"google.golang.org/api/impersonate"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Secrets reads secret versions: Secret Manager, or a fake in tests.
type Secrets interface {
	// Access is the payload of the version named projects/<p>/secrets/<s>/versions/<n>.
	Access(ctx context.Context, version string) ([]byte, error)
	// State is the version's state (ENABLED, DISABLED, DESTROYED), read without its
	// payload; a version that does not exist is an error.
	State(ctx context.Context, version string) (string, error)
	// Container reports whether the container named projects/<p>/secrets/<s> exists.
	Container(ctx context.Context, name string) (bool, error)
	// Create creates the container in the project, with automatic replication and the
	// labels.
	Create(ctx context.Context, project, id string, labels map[string]string) error
	// Newest is the number of the container's newest enabled version, "" when it has
	// none.
	Newest(ctx context.Context, container string) (string, error)
	// Grant adds the member to the role on the container's own policy; a member already
	// in the role is left as it is.
	Grant(ctx context.Context, container, role, member string) error
	// Add adds the payload as a new version of the container and answers its number.
	Add(ctx context.Context, container string, payload []byte) (string, error)
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

func (s *secretManager) Container(ctx context.Context, name string) (bool, error) {
	if _, err := s.client.GetSecret(ctx, &secretmanagerpb.GetSecretRequest{Name: name}); err != nil {
		if status.Code(err) == codes.NotFound {
			return false, nil
		}

		return false, errors.Wrapf(err, "secretmanager.Client.GetSecret(): %s", name)
	}

	return true, nil
}

func (s *secretManager) Create(ctx context.Context, project, id string, labels map[string]string) error {
	_, err := s.client.CreateSecret(ctx, &secretmanagerpb.CreateSecretRequest{
		Parent:   "projects/" + project,
		SecretId: id,
		Secret: &secretmanagerpb.Secret{
			Labels:      labels,
			Replication: &secretmanagerpb.Replication{Replication: &secretmanagerpb.Replication_Automatic_{Automatic: &secretmanagerpb.Replication_Automatic{}}},
		},
	})
	if err != nil {
		return errors.Wrapf(err, "secretmanager.Client.CreateSecret(): %s in %s", id, project)
	}

	return nil
}

func (s *secretManager) Newest(ctx context.Context, container string) (string, error) {
	it := s.client.ListSecretVersions(ctx, &secretmanagerpb.ListSecretVersionsRequest{Parent: container, Filter: "state:" + enabledState})
	newest := 0
	for {
		v, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return "", errors.Wrapf(err, "secretmanager.Client.ListSecretVersions(): %s", container)
		}
		if v.GetState() != secretmanagerpb.SecretVersion_ENABLED {
			continue
		}
		if n, err := strconv.Atoi(path.Base(v.GetName())); err == nil && n > newest {
			newest = n
		}
	}
	if newest == 0 {
		return "", nil
	}

	return strconv.Itoa(newest), nil
}

func (s *secretManager) Grant(ctx context.Context, container, role, member string) error {
	handle := s.client.IAM(container)
	policy, err := handle.Policy(ctx)
	if err != nil {
		return errors.Wrapf(err, "iam.Handle.Policy(): %s", container)
	}
	if policy.HasRole(member, iam.RoleName(role)) {
		return nil
	}
	policy.Add(member, iam.RoleName(role))
	if err := handle.SetPolicy(ctx, policy); err != nil {
		return errors.Wrapf(err, "iam.Handle.SetPolicy(): %s", container)
	}

	return nil
}

func (s *secretManager) Add(ctx context.Context, container string, payload []byte) (string, error) {
	v, err := s.client.AddSecretVersion(ctx, &secretmanagerpb.AddSecretVersionRequest{Parent: container, Payload: &secretmanagerpb.SecretPayload{Data: payload}})
	if err != nil {
		return "", errors.Wrapf(err, "secretmanager.Client.AddSecretVersion(): %s", container)
	}

	return path.Base(v.GetName()), nil
}

func (s *secretManager) Close() error {
	if err := s.client.Close(); err != nil {
		return errors.Wrap(err, "secretmanager.Client.Close()")
	}

	return nil
}
