// apikeys.go reads the API keys of the environment projects, for org check. A key with no
// API restriction answers every API in its project that accepts an API key. Initializing
// Identity Platform in an environment project (2-env's identity-platform.tf) makes Firebase
// create one such key, "Browser key (auto created by Firebase)", which no layer declares:
// the layers workflow restricts it to the two sign-in APIs after each apply of 2-env, and
// org check names any key in an environment project still without an API restriction,
// that one recreated since or any other.

package org

import (
	"context"

	"github.com/go-playground/errors/v5"
	"google.golang.org/api/apikeys/v2"
	"google.golang.org/api/option"
)

// FirebaseBrowserKey is the display name Firebase gives the API key it creates in a
// project when Identity Platform is initialized there.
const FirebaseBrowserKey = "Browser key (auto created by Firebase)"

// SignInAPIs are the services the applications' web API keys admit, the Identity Toolkit
// API and the Secure Token API, and the ones the layers workflow restricts Firebase's
// browser key to: the key is then no more capable than the applications' own.
var SignInAPIs = []string{"identitytoolkit.googleapis.com", "securetoken.googleapis.com"}

// userProjectHeader names the project a call is billed to (the quota project).
const userProjectHeader = "X-Goog-User-Project"

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

// apiKeys is the KeyLister over the API Keys API.
type apiKeys struct {
	service *apikeys.Service
}

// NewKeyLister opens the API Keys client with Application Default Credentials; without
// any, the open fails and says so. Each list is billed to the project it reads.
func NewKeyLister(ctx context.Context) (KeyLister, error) {
	return newKeyLister(ctx)
}

// newKeyLister opens the API Keys client with the options given, which a test points at
// its own server.
func newKeyLister(ctx context.Context, opts ...option.ClientOption) (KeyLister, error) {
	service, err := apikeys.NewService(ctx, opts...)
	if err != nil {
		return nil, errors.Wrap(err, "apikeys.NewService()")
	}

	return &apiKeys{service: service}, nil
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

// Close releases nothing: the REST client holds no connection of its own.
func (*apiKeys) Close() error {
	return nil
}
