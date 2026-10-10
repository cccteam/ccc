// Package spanner is the Spanner database driver: from the variables it declares, it
// opens the application's database, the Spanner client over it and the resource client
// the generated handlers read and write through. An application embeds Settings in its
// configuration and calls Open, and names no database vendor in its own code; moving to
// another database swaps this import and the embedded settings for that database's
// driver (resource/database/postgres).
package spanner

import (
	"context"
	"fmt"
	"strings"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/resource"
	"github.com/go-playground/errors/v5"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Settings are the variables the driver reads, the database's identity, declared on the
// application's configuration by embedding, so the stack and the development environment
// render them from the declaration. The declaration the tools read is
// declaration.Settings, in the package beside this one, which holds nothing of the
// driver. The migration and bootstrap commands read the settings on their own, before
// any client opens, and hand them to the migrator.
type Settings struct {
	// ProjectID is the Google Cloud project the Spanner instance belongs to.
	ProjectID string `env:"GOOGLE_CLOUD_SPANNER_PROJECT,required"`
	// InstanceID is the Spanner instance the database is on.
	InstanceID string `env:"GOOGLE_CLOUD_SPANNER_INSTANCE_ID,required"`
	// DatabaseName is the database on the instance.
	DatabaseName string `env:"GOOGLE_CLOUD_SPANNER_DATABASE_NAME,required"`
	// EmulatorHost is the Spanner emulator's host:port, the development stack's. Set,
	// the client talks to it in the clear with no credential, the way the client
	// library does when it reads the same variable; a deployment leaves it empty.
	EmulatorHost string `env:"SPANNER_EMULATOR_HOST"`
}

// DatabasePath is the database's resource name
// (projects/<project>/instances/<instance>/databases/<database>): what the Spanner
// client opens, and what the migrator and the bootstrap name it by.
func (s Settings) DatabasePath() string {
	return fmt.Sprintf("projects/%s/instances/%s/databases/%s", s.ProjectID, s.InstanceID, s.DatabaseName)
}

// Driver is what Open built: the two clients over the database, closed together.
type Driver struct {
	// ResourceClient is the resource client over the database: what the generated
	// handlers, the feature flags and the tenant roster read and write through.
	ResourceClient *resource.SpannerClient
	// SpannerClient is the Spanner client the resource client is built over, for what
	// else the application opens on the database: the permission engine's store and the
	// session manager's.
	SpannerClient *cloudspanner.Client
}

// Open opens the database the settings name: the Spanner client over it, authenticated
// with the application's default credentials, or talking to the emulator EmulatorHost
// names, and the resource client over that client, with the file stores the options wire
// on it (resource.WithFileStore, resource.WithNamedFileStore). Nothing is read here: the
// Spanner client connects at the first request, so a database that does not exist fails
// there.
func Open(ctx context.Context, s Settings, opts ...resource.ClientOption) (*Driver, error) {
	client, err := cloudspanner.NewClient(ctx, s.DatabasePath(), emulatorOptions(s.EmulatorHost)...)
	if err != nil {
		return nil, errors.Wrap(err, "spanner.NewClient()")
	}

	return &Driver{ResourceClient: resource.NewSpannerClient(client, opts...), SpannerClient: client}, nil
}

// emulatorOptions are the client options that point the Spanner client at the emulator
// host, none for an empty host: the endpoint, a transport in the clear and no
// credential, the same three the client library adds when SPANNER_EMULATOR_HOST is set
// in the process, so the driver follows its settings whichever way they were filled.
func emulatorOptions(host string) []option.ClientOption {
	if host == "" {
		return nil
	}
	for _, scheme := range []string{"http://", "https://", "passthrough:///"} {
		host = strings.TrimPrefix(host, scheme)
	}

	return []option.ClientOption{
		option.WithEndpoint("passthrough:///" + host),
		option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
		option.WithoutAuthentication(),
	}
}

// Close releases the Spanner client, and with it the sessions the resource client held.
func (d *Driver) Close() {
	d.SpannerClient.Close()
}
