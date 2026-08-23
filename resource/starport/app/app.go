// Package app contains the http handlers for the starport. Most handlers are generated;
// this file provides the application plumbing the generated code depends on.
package app

import (
	"context"
	"net/http"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/starport/pkg/router"
	"github.com/cccteam/ccc/resource/starport/pkg/rpc"
	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
)

var _ http.Handler = &App{}

// Config carries the dependencies for an App. UserPermissions is injected so tests can
// script the permission table per request. DomainExists is the application's tenancy
// source for the generated handlers' unknown-domain 404 guard; it is required iff any
// resource or RPC method is domain-scoped.
type Config struct {
	ResourceClient  resource.Client
	RPCClient       *rpc.Client
	UserPermissions func(*http.Request) resource.UserPermissions
	DomainExists    func(ctx context.Context, domain accesstypes.Domain) (bool, error)
	Validator       *validator.Validate
}

// App implements the http handlers for the starport, most of which are generated.
type App struct {
	router          *chi.Mux
	resourceClient  resource.Client
	rpcClient       *rpc.Client
	userPermissions func(*http.Request) resource.UserPermissions
	domainExists    func(ctx context.Context, domain accesstypes.Domain) (bool, error)
	validate        *validator.Validate
}

// New constructs an App from its dependencies.
func New(cfg Config) *App {
	a := &App{
		resourceClient:  cfg.ResourceClient,
		rpcClient:       cfg.RPCClient,
		userPermissions: cfg.UserPermissions,
		domainExists:    cfg.DomainExists,
		validate:        cfg.Validator,
	}

	a.router = router.New(a)

	return a
}

// ServeHTTP implements the http.Handler interface.
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.router.ServeHTTP(w, r)
}

// UserPermissions returns the permission checker for a request.
func (a *App) UserPermissions(r *http.Request) resource.UserPermissions {
	return a.userPermissions(r)
}

// DomainExists reports whether the application recognizes the domain; the generated
// domain-scoped handlers 404 unknown domains before decoding.
func (a *App) DomainExists(ctx context.Context, domain accesstypes.Domain) (bool, error) {
	return a.domainExists(ctx, domain)
}

// ResourceClient returns the database client used by the resource layer.
func (a *App) ResourceClient() resource.Client {
	return a.resourceClient
}

// RPCClient returns the dependencies for RPC method implementations.
func (a *App) RPCClient() *rpc.Client {
	return a.rpcClient
}

// handleError allows the application to translate resource errors per resource type
// before they are encoded to the client. The starport performs no translation.
func handleError[T any](err error) error {
	return err
}
