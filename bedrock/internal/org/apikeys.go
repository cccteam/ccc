// apikeys.go reads the API keys of the environment projects, for org check, and restricts
// Firebase's browser key, for org apply. A key with no API restriction answers every API
// in its project that accepts an API key. Initializing Identity Platform in an environment
// project (2-env's identity-platform.tf) makes Firebase create one such key, "Browser key
// (auto created by Firebase)", which no layer declares: bedrock org apply restricts it to
// the two sign-in APIs after each apply of 2-env, as the environment's layer identity, and
// org check names any key in an environment project still without an API restriction,
// that one recreated since or any other.

package org

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
	"google.golang.org/api/apikeys/v2"
	"google.golang.org/api/impersonate"
	"google.golang.org/api/option"
)

// FirebaseBrowserKey is the display name Firebase gives the API key it creates in a
// project when Identity Platform is initialized there.
const FirebaseBrowserKey = "Browser key (auto created by Firebase)"

// SignInAPIs are the services the applications' web API keys admit, the Identity Toolkit
// API and the Secure Token API, and the ones bedrock org apply restricts Firebase's
// browser key to: the key is then no more capable than the applications' own.
var SignInAPIs = []string{identityToolkitAPI, secureTokenAPI}

// The two sign-in APIs: Identity Platform's, and the one that refreshes its tokens.
const (
	identityToolkitAPI = "identitytoolkit.googleapis.com"
	secureTokenAPI     = "securetoken.googleapis.com"
)

const (
	// userProjectHeader names the project a call is billed to (the quota project).
	userProjectHeader = "X-Goog-User-Project"
	// cloudPlatformScope is the scope an impersonated identity's token is asked for.
	cloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"
	// restrictionWait is how long a restriction's operation is waited on, and
	// restrictionPoll how often it is asked about.
	restrictionWait = 2 * time.Minute
	restrictionPoll = 2 * time.Second
)

// APIKey is one API key of a project: its resource name, its display name, and the
// services its API restriction admits, none when it carries no API restriction.
type APIKey struct {
	Name        string
	DisplayName string
	APITargets  []string
}

// KeyLister lists a project's API keys. NewKeyLister is the real one, over the API Keys
// API; tests pass a fake.
type KeyLister interface {
	// ProjectKeys is the project's API keys, deleted ones left out.
	ProjectKeys(ctx context.Context, project string) ([]APIKey, error)
	// Close releases the connection.
	Close() error
}

// KeyListerFunc opens a KeyLister.
type KeyListerFunc func(ctx context.Context) (KeyLister, error)

// KeyClient lists a project's API keys and restricts one: what bedrock org apply opens
// as the environment's layer identity after each apply of 2-env.
type KeyClient interface {
	KeyLister
	// RestrictKey sets the key's API restriction to the services, waiting for the
	// change to land.
	RestrictKey(ctx context.Context, name string, services []string) error
}

// KeyClientAsFunc opens a KeyClient as an identity the process's credentials may
// impersonate.
type KeyClientAsFunc func(ctx context.Context, identity string) (KeyClient, error)

// RestrictBrowserKeys restricts every API key in the project named as Firebase names its
// browser key to the sign-in APIs, saying what it did, or that there was none. The
// client acts as the environment's layer identity, which holds the project's API key
// administration (1-org's app role set).
func RestrictBrowserKeys(ctx context.Context, client KeyClient, project string, out io.Writer) error {
	keys, err := client.ProjectKeys(ctx, project)
	if err != nil {
		return err
	}
	restricted := 0
	for _, k := range keys {
		if k.DisplayName != FirebaseBrowserKey {
			continue
		}
		if err := client.RestrictKey(ctx, k.Name, SignInAPIs); err != nil {
			return err
		}
		fmt.Fprintf(out, "Restricted %s (%q) to %s.\n", k.Name, k.DisplayName, prose(SignInAPIs))
		restricted++
	}
	if restricted == 0 {
		fmt.Fprintf(out, "No API key named %q in %s.\n", FirebaseBrowserKey, project)
	}

	return nil
}

// UnrestrictedKey is one API key in an environment project that carries no API
// restriction.
type UnrestrictedKey struct {
	Environment string
	Project     string
	Key         APIKey
}

// UnrestrictedKeys lists, for each environment project the placement records, in
// promotion order, every API key that carries no API restriction. Unrecorded names the
// environments the placement records no project for, which are not read.
func UnrestrictedKeys(ctx context.Context, p *Placement, list KeyLister) (keys []UnrestrictedKey, unrecorded []string, err error) {
	for _, env := range Environments {
		project := p.Projects[env]
		if project == "" {
			unrecorded = append(unrecorded, env)

			continue
		}
		projectKeys, err := list.ProjectKeys(ctx, project)
		if err != nil {
			return nil, nil, err
		}
		for _, k := range projectKeys {
			if len(k.APITargets) == 0 {
				keys = append(keys, UnrestrictedKey{Environment: env, Project: project, Key: k})
			}
		}
	}

	return keys, unrecorded, nil
}

// apiKeys is the KeyClient over the API Keys API.
type apiKeys struct {
	service *apikeys.Service
	// poll is how often a restriction's operation is asked about.
	poll time.Duration
}

// NewKeyLister opens the API Keys client with Application Default Credentials; without
// any, the open fails and says so. Each list is billed to the project it reads.
func NewKeyLister(ctx context.Context) (KeyLister, error) {
	return newKeyClient(ctx)
}

// NewKeyClientAs opens the API Keys client as the identity, impersonated with the
// process's default credentials: bedrock org apply restricts Firebase's browser key as
// the environment's layer identity.
func NewKeyClientAs(ctx context.Context, identity string) (KeyClient, error) {
	source, err := impersonate.CredentialsTokenSource(ctx, impersonate.CredentialsConfig{TargetPrincipal: identity, Scopes: []string{cloudPlatformScope}})
	if err != nil {
		return nil, errors.Wrapf(err, "impersonate.CredentialsTokenSource(): %s", identity)
	}

	return newKeyClient(ctx, option.WithTokenSource(source))
}

// newKeyClient opens the API Keys client with the options given, which a test points at
// its own server.
func newKeyClient(ctx context.Context, opts ...option.ClientOption) (KeyClient, error) {
	service, err := apikeys.NewService(ctx, opts...)
	if err != nil {
		return nil, errors.Wrap(err, "apikeys.NewService()")
	}

	return &apiKeys{service: service, poll: restrictionPoll}, nil
}

// ProjectKeys lists the project's API keys, page by page.
func (c *apiKeys) ProjectKeys(ctx context.Context, project string) ([]APIKey, error) {
	call := c.service.Projects.Locations.Keys.List("projects/" + project + "/locations/global")
	call.Header().Set(userProjectHeader, project)
	var keys []APIKey
	err := call.Pages(ctx, func(resp *apikeys.V2ListKeysResponse) error {
		for _, k := range resp.Keys {
			key := APIKey{Name: k.Name, DisplayName: k.DisplayName}
			if k.Restrictions != nil {
				for _, t := range k.Restrictions.ApiTargets {
					key.APITargets = append(key.APITargets, t.Service)
				}
			}
			keys = append(keys, key)
		}

		return nil
	})
	if err != nil {
		return nil, errors.Wrapf(err, "apikeys.ProjectsLocationsKeysListCall.Pages(): %s", project)
	}

	return keys, nil
}

// RestrictKey sets the key's API restriction and waits for the operation to finish:
// the key's project is the quota project (the name is projects/<project>/...).
func (c *apiKeys) RestrictKey(ctx context.Context, name string, services []string) error {
	targets := make([]*apikeys.V2ApiTarget, 0, len(services))
	for _, service := range services {
		targets = append(targets, &apikeys.V2ApiTarget{Service: service})
	}
	project := strings.Split(name, "/")[1]
	call := c.service.Projects.Locations.Keys.Patch(name, &apikeys.V2Key{Restrictions: &apikeys.V2Restrictions{ApiTargets: targets}}).UpdateMask("restrictions")
	call.Header().Set(userProjectHeader, project)
	op, err := call.Context(ctx).Do()
	if err != nil {
		return errors.Wrapf(err, "apikeys.ProjectsLocationsKeysPatchCall.Do(): %s", name)
	}
	deadline := time.Now().Add(restrictionWait)
	for !op.Done {
		if time.Now().After(deadline) {
			return errors.Newf("the restriction of %s is still in progress after %s (operation %s)", name, restrictionWait, op.Name)
		}
		time.Sleep(c.poll)
		get := c.service.Operations.Get(op.Name)
		get.Header().Set(userProjectHeader, project)
		if op, err = get.Context(ctx).Do(); err != nil {
			return errors.Wrapf(err, "apikeys.OperationsGetCall.Do(): %s", name)
		}
	}
	if op.Error != nil {
		return errors.Newf("the restriction of %s failed: %s", name, op.Error.Message)
	}

	return nil
}

// Close releases nothing: the REST client holds no connection of its own.
func (*apiKeys) Close() error {
	return nil
}
