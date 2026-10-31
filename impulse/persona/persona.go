// Package persona is the contract between an application and the persona sign-in of its
// test environments: the cast the application declares, the client ID a deployment is
// known by at the persona provider, and the registration the deploy sends the provider.
//
// A test environment (a pull-request environment, or the first environment itself) seeds
// a cast of personas: accounts with names, addresses and roles that exercise the
// application's screens, which nobody can sign in as through a real identity provider. The
// persona provider, a function of Redgrade, signs a person in as one of them after that
// person has signed in to Redgrade as themselves and Redgrade has said they may enter the
// application; the application takes the sign-in through the session library's generic
// OpenID Connect method (impulse add method persona), whose subject is the persona's login.
//
// The application declares its cast in cast.json beside its auth package, which impulse
// add method persona writes and the auth package embeds: the migrate command seeds the
// cast's accounts in the environments that take the seed (the auth's SeedCast), and the
// deploy registers the deployment with the provider after traffic (bedrock deploy persona
// register): its client ID, the callback addresses of its sites and the cast, so the
// provider knows which personas to offer and where to send the browser back.
package persona

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
)

// CastFile is the cast's file in the auth package, beside the role file.
const CastFile = "cast.json"

// Persona is one member of the cast: a seeded account a person may sign in as.
type Persona struct {
	// Login is the persona's sign-in name: the username of the account the migrate
	// command seeds, and the subject of the identity the provider asserts. Lowercase
	// letters, digits, dots, hyphens and underscores, starting with a letter.
	Login string `json:"login"`
	// Name is the persona's display name, which the picker shows; the login when empty.
	Name string `json:"name,omitempty"`
	// Email is the persona's address, which the seeded account carries; none when empty.
	Email string `json:"email,omitempty"`
	// Roles are the roles the seeded account holds, by name as the auth's role file
	// defines them: a global role is held in the global partition, a domain role in
	// every tenant domain. The picker shows them beside the persona.
	Roles []string `json:"roles,omitempty"`
}

// Cast is the cast file's content.
type Cast struct {
	Personas []Persona `json:"personas"`
}

// loginRE is the shape of a login.
var loginRE = regexp.MustCompile(`^[a-z][a-z0-9._-]*$`)

// maxLogin bounds a login's length.
const maxLogin = 64

// ReadCast reads a cast file and checks it: at least one persona, each with a well-formed
// login no other persona has.
func ReadCast(data []byte) (*Cast, error) {
	var cast Cast
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cast); err != nil {
		return nil, errors.Wrap(err, "json.Decoder.Decode()")
	}
	if len(cast.Personas) == 0 {
		return nil, errors.New("the cast names no persona: list the personas the test environments seed under \"personas\"")
	}
	seen := make(map[string]bool, len(cast.Personas))
	for i := range cast.Personas {
		p := &cast.Personas[i]
		switch {
		case p.Login == "" || len(p.Login) > maxLogin || !loginRE.MatchString(p.Login):
			return nil, errors.Newf("persona %d: the login %q is not lowercase letters, digits, dots, hyphens and underscores starting with a letter, at most %d long", i+1, p.Login, maxLogin)
		case seen[p.Login]:
			return nil, errors.Newf("the login %q names two personas: each persona has its own", p.Login)
		}
		seen[p.Login] = true
		if p.Name == "" {
			p.Name = p.Login
		}
	}

	return &cast, nil
}

// ClientIDFor is the client ID a deployment is known by at the provider: the
// application's client ID root (the placement's persona.clientId) with the pull request's
// number appended for a pull-request environment (lanterns-pr12), or the environment's
// name for the environment's own deployment (lanterns-tst). The provider spells it the
// same way, so the two agree without either telling the other.
func ClientIDFor(root, environment string, pullRequest int) string {
	if pullRequest > 0 {
		return root + "-pr" + strconv.Itoa(pullRequest)
	}

	return root + "-" + environment
}

// Registration is what a deploy sends the provider to register a deployment, as the
// provider's registration endpoint takes it.
type Registration struct {
	// ApplicationClientID is the application's client ID root (the placement's
	// persona.clientId), which the provider resolves to the application.
	ApplicationClientID string `json:"applicationClientId"`
	// Environment and PullRequest name the deployment; its client ID follows from them
	// (ClientIDFor) and is the one in the request's path.
	Environment string `json:"environment"`
	PullRequest int    `json:"pullRequest"`
	// RedirectURIs are the callback addresses of every site the deployment serves: the
	// persona method's redirect URL on each.
	RedirectURIs []string `json:"redirectUris"`
	// Cast are the personas the deployment seeded.
	Cast []Persona `json:"cast"`
}

// Registered is the provider's answer to a registration.
type Registered struct {
	ClientID  string    `json:"clientId"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// maxAnswer bounds the provider's answer.
const maxAnswer = 1 << 16

// Register registers the deployment with the provider at issuer: PUT
// {issuer}/registrations/{clientID} with the registration as JSON, authenticated by a
// bearer token the deploy identity holds for the issuer (a Google identity token whose
// audience is the issuer). The provider refuses an identity the application does not
// allow to register, and a client ID that is not the deployment's.
func Register(ctx context.Context, client *http.Client, issuer, clientID, token string, reg *Registration) (*Registered, error) {
	body, err := json.Marshal(reg)
	if err != nil {
		return nil, errors.Wrap(err, "json.Marshal()")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, registrationURL(issuer, clientID), bytes.NewReader(body))
	if err != nil {
		return nil, errors.Wrap(err, "http.NewRequestWithContext()")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	answer, err := do(client, req, http.StatusOK)
	if err != nil {
		return nil, err
	}
	var registered Registered
	if err := json.Unmarshal(answer, &registered); err != nil {
		return nil, errors.Wrap(err, "json.Unmarshal(): the provider's answer")
	}

	return &registered, nil
}

// Deregister removes the deployment's registration: DELETE {issuer}/registrations/{clientID}
// with the same token. A deployment the provider does not know is not an error.
func Deregister(ctx context.Context, client *http.Client, issuer, clientID, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, registrationURL(issuer, clientID), http.NoBody)
	if err != nil {
		return errors.Wrap(err, "http.NewRequestWithContext()")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if _, err := do(client, req, http.StatusNoContent); err != nil {
		return err
	}

	return nil
}

// registrationURL is the registration endpoint for a deployment.
func registrationURL(issuer, clientID string) string {
	return strings.TrimSuffix(issuer, "/") + "/registrations/" + clientID
}

// do sends the request and answers the body of an answer with the status wanted, or an
// error carrying the provider's message otherwise.
func do(client *http.Client, req *http.Request, want int) ([]byte, error) {
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.Wrapf(err, "http.Client.Do(): %s %s", req.Method, req.URL)
	}
	defer resp.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return nil, errors.Wrap(err, "io.ReadAll()")
	}
	if resp.StatusCode != want {
		return nil, &ProviderError{Method: req.Method, URL: req.URL.String(), Status: resp.StatusCode, Message: message(answer)}
	}

	return answer, nil
}

// ProviderError is the provider's answer to a request it did not grant: the request, the
// status and the provider's message.
type ProviderError struct {
	Method  string
	URL     string
	Status  int
	Message string
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("%s %s: the provider answered %d: %s", e.Method, e.URL, e.Status, e.Message)
}

// IsNotFound reports a provider's answer that the registration does not exist: a removal
// of one the provider never held, or holds no longer.
func IsNotFound(err error) bool {
	var pe *ProviderError

	return errors.As(err, &pe) && pe.Status == http.StatusNotFound
}

// message is the provider's message from its JSON refusal, or the body itself.
func message(answer []byte) string {
	var refusal struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(answer, &refusal); err == nil && refusal.Message != "" {
		return refusal.Message
	}

	return fmt.Sprintf("%.300s", strings.TrimSpace(string(answer)))
}
