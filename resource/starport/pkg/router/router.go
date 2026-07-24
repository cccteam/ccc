// Package router handles wiring up the routes to handlers.
package router

import (
	"github.com/go-chi/chi/v5"
)

// Handlers is an interface for the http handlers.
type Handlers interface {
	GeneratedHandlers
}

// New wires the generated routes to their handlers.
func New(h Handlers) *chi.Mux {
	r := chi.NewRouter()

	generatedRoutes(r, h)

	return r
}
