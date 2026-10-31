// persona.go registers each deployment with the persona provider and removes a torn-down
// pull request's registration.

package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-playground/errors/v5"
	"google.golang.org/api/idtoken"

	"github.com/cccteam/ccc/impulse/persona"
)

// personaSub is the substitution the stack's triggers carry in the first environment: the
// persona registrations, as a JSON list (locals.tf, persona_registrations); empty beyond
// it, where the persona method is not registered. The triggers say what the environment's
// last release applied, so a tag build reads it.
const personaSub = "_PERSONA"

// personaFact is the same list as the pull request's own stack carries it, read off that
// stack's substitutions output by the stack job and written for the preview build
// (ReadPullRequestStack). A pull-request build prefers it to the trigger's substitution:
// a pull request that introduces or changes the method registers what its own code says,
// before any release carries it to the triggers.
const personaFact = "PERSONA"

// personaRegistration is one entry of the _PERSONA substitution: a persona method's
// provider, the application's client ID root (the deployment's client ID is the root with
// the pull request or the environment appended), the site whose canonical hostname the
// callback is on, the callback's path and the cast file in the checkout.
type personaRegistration struct {
	Auth         string `json:"auth"`
	Registration string `json:"registration"`
	Issuer       string `json:"issuer"`
	ClientIDRoot string `json:"clientIdRoot"`
	Site         string `json:"site"`
	CallbackPath string `json:"callbackPath"`
	CastFile     string `json:"castFile"`
}

// PersonaAction is what deploy persona does: register the deployment with the provider
// after traffic, or remove a torn-down pull request's registration.
type PersonaAction string

// The actions.
const (
	PersonaRegister PersonaAction = "register"
	PersonaRemove   PersonaAction = "remove"
)

// PersonaActions lists the actions.
var PersonaActions = []PersonaAction{PersonaRegister, PersonaRemove}

// IDTokenFunc mints a Google identity token for the audience as the process's identity:
// the build's deploy identity on Cloud Build, whose email the persona provider verifies
// against the application's deploy identities.
type IDTokenFunc func(ctx context.Context, audience string) (string, error)

// GoogleIDToken is IDTokenFunc over the process's default credentials.
func GoogleIDToken(ctx context.Context, audience string) (string, error) {
	source, err := idtoken.NewTokenSource(ctx, audience)
	if err != nil {
		return "", errors.Wrap(err, "idtoken.NewTokenSource()")
	}
	token, err := source.Token()
	if err != nil {
		return "", errors.Wrap(err, "oauth2.TokenSource.Token()")
	}

	return token.AccessToken, nil
}

// Persona registers each persona method's deployment with the persona provider, or
// removes it: the client ID the placement's root and this deployment make
// (<root>-pr<N>, <root>-<environment>), the callback on the deployment's canonical
// hostname, and the cast from the checkout, sent as the build's deploy identity with its
// Google identity token for the provider. A build with no registrations to send (no
// persona method, or an environment beyond the first) does nothing; a torn-down
// environment registers nothing, and only a teardown removes. A refusal fails the step.
func Persona(ctx context.Context, clients *Clients, w Workspace, action PersonaAction, out io.Writer) error {
	env, err := w.Environment()
	if err != nil {
		return err
	}
	build, err := w.Build()
	if err != nil {
		return err
	}
	registrations, err := personaRegistrations(build, env)
	if err != nil {
		return err
	}
	switch action {
	case PersonaRegister:
		switch {
		case env[downFact] == trueValue:
			fmt.Fprintln(out, "A teardown: nothing to register with the persona provider.")
		case env[skipDeploy] == trueValue:
			fmt.Fprintln(out, skipped(env))
		case len(registrations) == 0:
			fmt.Fprintf(out, "No persona provider in %s: nothing to register.\n", build.Substitutions[envSub])
		default:
			return registerPersonas(ctx, clients, w, env, build, registrations, out)
		}
	case PersonaRemove:
		switch {
		case env[downFact] != trueValue:
			fmt.Fprintln(out, "Not a teardown: nothing to remove from the persona provider.")
		case len(registrations) == 0:
			fmt.Fprintf(out, "No persona provider in %s: nothing to remove.\n", build.Substitutions[envSub])
		default:
			return removePersonas(ctx, clients, build, registrations, out)
		}
	default:
		return errors.Newf("%q is not a persona action (the actions are %v)", action, PersonaActions)
	}

	return nil
}

// personaRegistrations reads the registrations: the pull request's own stack's (the
// PERSONA fact) when the build has them, else the build's _PERSONA substitution; none
// when both are empty.
func personaRegistrations(build *Build, env map[string]string) ([]personaRegistration, error) {
	source := personaFact
	raw := strings.TrimSpace(env[personaFact])
	if raw == "" {
		source = personaSub + " substitution"
		raw = strings.TrimSpace(build.Substitutions[personaSub])
	}
	if raw == "" {
		return nil, nil
	}
	var registrations []personaRegistration
	if err := json.Unmarshal([]byte(raw), &registrations); err != nil {
		return nil, errors.Wrapf(err, "json.Unmarshal(): the %s", source)
	}

	return registrations, nil
}

// registerPersonas sends each registration: the deployment's client ID, its callback and
// its cast, under the deploy identity's token for the provider.
func registerPersonas(ctx context.Context, clients *Clients, w Workspace, env map[string]string, build *Build, registrations []personaRegistration, out io.Writer) error {
	environment, pullRequest, err := personaDeployment(build)
	if err != nil {
		return err
	}
	for i := range registrations {
		r := &registrations[i]
		hostname, err := personaHostname(env, build, r.Site, pullRequest)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(filepath.Join(string(w), filepath.FromSlash(r.CastFile)))
		if err != nil {
			return errors.Wrapf(err, "os.ReadFile(): the %s auth's %s registration has no cast at %s", r.Auth, r.Registration, r.CastFile)
		}
		cast, err := persona.ReadCast(data)
		if err != nil {
			return errors.Wrapf(err, "%s", r.CastFile)
		}
		clientID := persona.ClientIDFor(r.ClientIDRoot, environment, pullRequest)
		redirect := "https://" + hostname + r.CallbackPath
		token, err := personaToken(ctx, clients, r.Issuer)
		if err != nil {
			return err
		}
		registered, err := persona.Register(ctx, personaClient(clients), r.Issuer, clientID, token, &persona.Registration{
			ApplicationClientID: r.ClientIDRoot, Environment: environment, PullRequest: pullRequest, RedirectURIs: []string{redirect}, Cast: cast.Personas,
		})
		if err != nil {
			return errors.Wrapf(err, "the %s auth's %s registration", r.Auth, r.Registration)
		}
		fmt.Fprintf(out, "Registered %s with %s for the %s auth's %s registration: %d persona(s), callback %s, kept until %s.\n", clientID, r.Issuer, r.Auth, r.Registration, len(cast.Personas), redirect, registered.ExpiresAt.UTC().Format("2006-01-02 15:04 UTC"))
	}

	return nil
}

// removePersonas removes each registration; one the provider does not hold is reported,
// not an error, since nothing is left to remove.
func removePersonas(ctx context.Context, clients *Clients, build *Build, registrations []personaRegistration, out io.Writer) error {
	environment, pullRequest, err := personaDeployment(build)
	if err != nil {
		return err
	}
	for i := range registrations {
		r := &registrations[i]
		clientID := persona.ClientIDFor(r.ClientIDRoot, environment, pullRequest)
		token, err := personaToken(ctx, clients, r.Issuer)
		if err != nil {
			return err
		}
		switch err := persona.Deregister(ctx, personaClient(clients), r.Issuer, clientID, token); {
		case err == nil:
			fmt.Fprintf(out, "Removed %s from %s.\n", clientID, r.Issuer)
		case persona.IsNotFound(err):
			fmt.Fprintf(out, "%s was not registered with %s: nothing to remove.\n", clientID, r.Issuer)
		default:
			return errors.Wrapf(err, "the %s auth's %s registration", r.Auth, r.Registration)
		}
	}

	return nil
}

// removePersonaRegistrations is the sweep's removal of a closed pull request's
// registrations, best effort: the provider forgets a registration nothing refreshes on
// its own, so a refusal is said and the sweep goes on.
func removePersonaRegistrations(ctx context.Context, clients *Clients, subs map[string]string, number string, out io.Writer) {
	registrations, err := personaRegistrations(&Build{ID: "sweep", Substitutions: subs}, nil)
	if err != nil {
		fmt.Fprintf(out, "Notice: the persona registrations could not be read (%v); pull request %s's expire on their own.\n", err, number)

		return
	}
	n, _ := strconv.Atoi(number)
	for i := range registrations {
		r := &registrations[i]
		clientID := persona.ClientIDFor(r.ClientIDRoot, subs[envSub], n)
		token, err := personaToken(ctx, clients, r.Issuer)
		if err == nil {
			err = persona.Deregister(ctx, personaClient(clients), r.Issuer, clientID, token)
		}
		switch {
		case err == nil:
			fmt.Fprintf(out, "Removed %s from %s.\n", clientID, r.Issuer)
		case persona.IsNotFound(err):
		default:
			fmt.Fprintf(out, "Notice: %s stays registered with %s (%v); it expires on its own.\n", clientID, r.Issuer, err)
		}
	}
}

// personaDeployment is the deployment a build is of: the environment, and the pull
// request's number (0 for the environment itself).
func personaDeployment(build *Build) (environment string, pullRequest int, err error) {
	environment = build.Substitutions[envSub]
	if environment == "" {
		return "", 0, errors.Newf("the build names no environment (%s)", envSub)
	}
	if number := build.Substitutions[prNumberSub]; number != "" {
		pullRequest, err = strconv.Atoi(number)
		if err != nil || pullRequest <= 0 {
			return "", 0, errors.Newf("%s is %q, not a pull request's number", prNumberSub, number)
		}
	}

	return environment, pullRequest, nil
}

// personaHostname is the hostname the registration's callback is on: the deployment's
// canonical hostname as the stack applied it (a pull request's own in a pull-request
// build), the site's where the stack names one per site.
func personaHostname(env map[string]string, build *Build, site string, pullRequest int) (string, error) {
	sites, hostname := canonicalHostnames(env, build), canonicalHostname(env, build)
	if pullRequest > 0 {
		sites, hostname = env[prHostnamesFact], env[prHostnameFact]
	}
	if sites != "" {
		for _, pair := range strings.Split(sites, ",") {
			if name, host, ok := strings.Cut(pair, "="); ok && name == site {
				return host, nil
			}
		}

		return "", errors.Newf("the stack names no hostname for the %s site (%s): the persona callback is on it", site, sites)
	}
	if hostname == "" {
		return "", errors.New("the stack names no hostname for the deployment: the persona callback is on it")
	}

	return hostname, nil
}

// personaToken is the deploy identity's identity token for the provider.
func personaToken(ctx context.Context, clients *Clients, issuer string) (string, error) {
	if clients.IDToken == nil {
		return "", errors.New("no identity token source: the persona provider takes the deploy identity's Google identity token")
	}
	token, err := clients.IDToken(ctx, issuer)
	if err != nil {
		return "", errors.Wrapf(err, "the identity token for %s", issuer)
	}

	return token, nil
}

// personaClient is the HTTP client the registrations go through: the clients', else a
// plain one.
func personaClient(clients *Clients) *http.Client {
	if clients.HTTP != nil {
		return clients.HTTP
	}

	return http.DefaultClient
}
