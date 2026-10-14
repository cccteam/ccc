// generated.go derives the pins of the generated secrets, the variables the code declares
// secret:"generated": values the tooling makes and nobody types. The placement carries the
// word generated for them, and the version an environment runs is read at each plan from
// Secret Manager, the way the image digest is read at each build: the newest enabled
// version of the secret's container, passed to the plan as var.generated_secret_versions,
// so the revision template pins versions/N exactly as a committed pin does, and the record
// names N. A tag build's plan step, as the apply identity, also seeds: a container the
// project lacks is created with the stack's labels (the apply then adopts it with no
// change), the identity grants itself the adding of versions on it (the stack declares the
// same grant, durably, for the apply and operations identities), and a version is added
// (32 random bytes, base64) when no enabled version exists. A pull request's reads seed
// nothing: a generated secret with no version yet is left unmounted and said.

package deploy

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/secret"
)

const (
	// generatedFact is what the plan step leaves for the record: VARIABLE=N, comma
	// separated, sorted by variable.
	generatedFact = "GENERATED_SECRETS"
	// generatedVar is the stack variable the derived versions are passed in.
	generatedVar = "generated_secret_versions"
	// generatedBytes is the size of a generated value, before base64.
	generatedBytes = 32
	// versionAdderRole adds versions to a container and reads none.
	versionAdderRole = "roles/secretmanager.secretVersionAdder"
	// grantWait and grantTries bound the wait for a fresh grant to take effect: IAM
	// propagates a secret's policy in seconds, up to minutes.
	grantWait  = 10 * time.Second
	grantTries = 18
	// identityDomain is what a service account's email ends with, after its project.
	identityDomain = ".iam.gserviceaccount.com"
)

// generatedPins reads, as identity, the newest enabled version of each generated secret's
// container in the environment's project, and answers the versions by variable. With
// seed, a container the project lacks is created and one with no enabled version gets a
// version added (seedGenerated): the tag build's plan step, as the apply identity. Without
// seed (a pull request's plans, as the plan identity of each environment or tst's apply
// identity), such a secret is left out and said. Nothing is opened when the code declares
// no generated secret.
func generatedPins(ctx context.Context, clients *Clients, identity string, m *derive.Model, app, env, project string, seed bool, out io.Writer) (map[string]string, error) {
	var generated []derive.Secret
	for _, s := range m.Secrets {
		if s.Generated {
			generated = append(generated, s)
		}
	}
	if len(generated) == 0 {
		return nil, nil
	}
	secrets, err := clients.SecretsAs(ctx, identity)
	if err != nil {
		return nil, err
	}
	defer secrets.Close()
	pins := map[string]string{}
	for _, s := range generated {
		id := m.Placement.Prefix + "-" + env + "-gbl-" + app + "-" + s.Name
		container := "projects/" + project + "/secrets/" + id
		exists, err := secrets.Container(ctx, container)
		if err != nil {
			return nil, err
		}
		version := ""
		if exists {
			if version, err = secrets.Newest(ctx, container); err != nil {
				return nil, err
			}
		}
		switch {
		case version != "":
			fmt.Fprintf(out, "%s: version %s of %s, its newest enabled, is the pin (a generated secret: its version is derived at each plan).\n", s.Variable.Name, version, id)
		case !seed:
			fmt.Fprintf(out, "%s: %s has no enabled version yet (a generated secret, whose first version %s's next release build adds): the plan mounts nothing for it.\n", s.Variable.Name, id, env)

			continue
		default:
			if version, err = seedGenerated(ctx, clients, secrets, identity, m, app, env, project, s.Variable.Name, id, exists, out); err != nil {
				return nil, err
			}
		}
		pins[s.Variable.Name] = version
	}

	return pins, nil
}

// seedGenerated gives a generated secret its first enabled version as the identity: the
// container, when the project lacks it, with the stack's labels (bedrock secret add's,
// which the apply adopts with no change); the grant that lets the identity add; then a
// version of generatedBytes random bytes, base64, never printed. A fresh grant may take
// a while to be in effect, so the add is tried again while Secret Manager refuses it.
func seedGenerated(ctx context.Context, clients *Clients, secrets Secrets, identity string, m *derive.Model, app, env, project, variable, id string, exists bool, out io.Writer) (string, error) {
	container := "projects/" + project + "/secrets/" + id
	if !exists {
		labels := secret.ContainerLabels(app, env, m.Placement.Repository, variable, m.Placement.Labels)
		if err := secrets.Create(ctx, project, id, labels); err != nil {
			return "", err
		}
		fmt.Fprintf(out, "%s: created %s in %s with the stack's labels, ahead of the apply, which adopts it.\n", variable, id, project)
	}
	if err := secrets.Grant(ctx, container, versionAdderRole, "serviceAccount:"+identity); err != nil {
		return "", err
	}
	value := make([]byte, generatedBytes)
	if _, err := rand.Read(value); err != nil {
		return "", errors.Wrap(err, "rand.Read()")
	}
	payload := []byte(base64.StdEncoding.EncodeToString(value))
	var version string
	for try := 1; ; try++ {
		var err error
		if version, err = secrets.Add(ctx, container, payload); err == nil {
			break
		}
		if status.Code(errors.Cause(err)) != codes.PermissionDenied || try >= grantTries {
			return "", err
		}
		fmt.Fprintf(out, "Waiting for the grant on %s to take effect: Secret Manager refused the add (%ds).\n", id, int((time.Duration(try) * grantWait).Seconds()))
		if err := clients.sleep()(ctx, grantWait); err != nil {
			return "", err
		}
	}
	fmt.Fprintf(out, "%s: added version %s of %s as %s (%d random bytes, base64, never printed): the pin; a rotation (bedrock secret rotate %s %s) adds the next.\n", variable, version, id, identity, generatedBytes, env, variable)

	return version, nil
}

// generatedArgs is the plan's variable for the derived versions, as tofu takes a map on
// the command line; none without any.
func generatedArgs(pins map[string]string) []string {
	if len(pins) == 0 {
		return nil
	}
	entries := make([]string, 0, len(pins))
	for _, name := range slices.Sorted(maps.Keys(pins)) {
		entries = append(entries, name+`="`+pins[name]+`"`)
	}

	return []string{varFlag, generatedVar + "={" + strings.Join(entries, ", ") + "}"}
}

// generatedFactValue is the fact the record reads back (generatedOf).
func generatedFactValue(pins map[string]string) string {
	entries := make([]string, 0, len(pins))
	for _, name := range slices.Sorted(maps.Keys(pins)) {
		entries = append(entries, name+"="+pins[name])
	}

	return strings.Join(entries, ",")
}

// generatedOf is the record's map of the derived versions, nil when the run derived none.
func generatedOf(env map[string]string) map[string]string {
	if env[generatedFact] == "" {
		return nil
	}
	pins := map[string]string{}
	for _, pair := range strings.Split(env[generatedFact], ",") {
		name, version, ok := strings.Cut(pair, "=")
		if ok {
			pins[name] = version
		}
	}

	return pins
}

// identityProject is the project a service account's email belongs to:
// <name>@<project>.iam.gserviceaccount.com.
func identityProject(email string) string {
	_, domain, _ := strings.Cut(email, "@")

	return strings.TrimSuffix(domain, identityDomain)
}
