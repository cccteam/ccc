package resource

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/go-chi/chi/v5"
	"github.com/go-playground/errors/v5"
)

// OperationType defines the type of a patch operation (add, patch, remove).
type OperationType string

const (
	// OperationCreate corresponds to an "add" operation.
	OperationCreate OperationType = "add"
	// OperationUpdate corresponds to a "patch" operation.
	OperationUpdate OperationType = "patch"
	// OperationDelete corresponds to a "remove" operation.
	OperationDelete OperationType = "remove"
)

// Operation represents a single operation within a batch request, containing its type and a corresponding http.Request.
type Operation struct {
	Type       OperationType
	Req        *http.Request
	pathPrefix string
}

// ReqWithPattern creates a new http.Request from the operation, applying a new URL pattern to its context.
// This is useful when a batch operation's path contains more segments than the initial prefix pattern.
func (o *Operation) ReqWithPattern(pattern string, opts ...Option) (*http.Request, error) {
	var os options
	for _, opt := range opts {
		os = opt(os)
	}

	method, err := httpMethod(string(o.Type))
	if err != nil {
		return nil, err
	}

	ctx, _, err := withParams(o.Req.Context(), method, pattern, o.Req.URL.Path, o.pathPrefix, os)
	if err != nil {
		return nil, err
	}

	return o.Req.WithContext(ctx), nil
}

type patchOperation struct {
	Op    string          `json:"op"`
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value"`
}

type options struct {
	requireCreatePath bool
	matchPrefix       bool
}

// Option is a function that configures the behavior of the Operations parser.
type Option func(opt options) options

// RequireCreatePath is an option that mandates a path for "add" operations.
func RequireCreatePath() Option {
	return func(o options) options {
		o.requireCreatePath = true

		return o
	}
}

// MatchPrefix is an option that allows matching only the prefix of an operation's path against the provided pattern.
func MatchPrefix() Option {
	return func(o options) options {
		o.matchPrefix = true

		return o
	}
}

// Operations parses a batch JSON patch request and yields an iterator of individual Operation objects.
func Operations(r *http.Request, pattern string, opts ...Option) iter.Seq2[*Operation, error] {
	var o options
	for _, opt := range opts {
		o = opt(o)
	}

	return func(yield func(r *Operation, err error) bool) {
		if !strings.HasPrefix(pattern, "/") {
			yield(nil, errors.New("pattern must start with /"))

			return
		}

		dec := json.NewDecoder(r.Body)

		for {
			t, err := dec.Token()
			if err != nil {
				yield(nil, err)

				return
			}
			token := fmt.Sprintf("%s", t)
			if token == "[" {
				break
			}
			if strings.TrimSpace(token) != "" {
				yield(nil, httpio.NewBadRequestMessagef("expected start of array, got %q", t))

				return
			}
		}

		for dec.More() {
			var op patchOperation
			if err := dec.Decode(&op); err != nil {
				yield(nil, err)

				return
			}

			if op.Value == nil {
				op.Value = []byte("{}")
			}

			method, err := httpMethod(op.Op)
			if err != nil {
				yield(nil, err)

				return
			}

			ctx, pathPrefix, err := withParams(r.Context(), method, pattern, op.Path, "", o)
			if err != nil {
				yield(nil, err)

				return
			}

			r2, err := http.NewRequestWithContext(ctx, method, op.Path, bytes.NewReader([]byte(op.Value)))
			if err != nil {
				yield(nil, err)

				return
			}

			if !yield(&Operation{Type: OperationType(op.Op), Req: r2, pathPrefix: pathPrefix}, nil) {
				return
			}
		}

		t, err := dec.Token()
		if err != nil {
			yield(nil, httpio.NewBadRequestMessageWithErrorf(err, "failed find end of array"))

			return
		}

		token := fmt.Sprintf("%s", t)
		if token == "]" {
			return
		}
	}
}

func httpMethod(op string) (string, error) {
	switch OperationType(strings.ToLower(op)) {
	case OperationCreate:
		return http.MethodPost, nil
	case OperationUpdate:
		return http.MethodPatch, nil
	case OperationDelete:
		return http.MethodDelete, nil
	default:
		return "", httpio.NewBadRequestMessagef("unsupported operation %q", op)
	}
}

func withParams(ctx context.Context, method, pattern, path, pathPrefix string, o options) (context.Context, string, error) {
	servePath := path
	if o.matchPrefix {
		patternParts := strings.Split(pattern, "/")
		pathParts := strings.Split(path, "/")
		if len(pathParts) > len(patternParts) {
			servePath = strings.Join(pathParts[:len(patternParts)], "/")
		}
		pathPrefix = servePath
	}

	switch method {
	case http.MethodPost:
		if !o.matchPrefix {
			p := strings.TrimSuffix(path, "/")
			if o.requireCreatePath && p == pathPrefix {
				return ctx, pathPrefix, httpio.NewBadRequestMessage("path is required for create operation")
			}

			if !o.requireCreatePath && p != pathPrefix {
				return ctx, pathPrefix, httpio.NewBadRequestMessage("path is not allowed for create operation")
			}

			if p == pathPrefix {
				return ctx, pathPrefix, nil
			}
		}

		fallthrough
	case http.MethodPatch, http.MethodDelete:
		if path == "" {
			return ctx, pathPrefix, httpio.NewBadRequestMessage("path is required for patch and delete operations")
		}

		var chiContext *chi.Context
		r := chi.NewRouter()

		r.Handle(pattern, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			chiContext = chi.RouteContext(r.Context())
		}))
		r.ServeHTTP(httptest.NewRecorder(), &http.Request{Method: method, Header: make(map[string][]string), URL: &url.URL{Path: servePath}})

		if chiContext == nil {
			return ctx, pathPrefix, httpio.NewBadRequestMessagef("path %q does not match pattern %q", path, pattern)
		}

		ctx = context.WithValue(ctx, chi.RouteCtxKey, chiContext)
	}

	return ctx, pathPrefix, nil
}

func permissionFromType(typ OperationType) accesstypes.Permission {
	switch typ {
	case OperationCreate:
		return accesstypes.Create
	case OperationUpdate:
		return accesstypes.Update
	case OperationDelete:
		return accesstypes.Delete
	}

	panic("implementation error")
}
