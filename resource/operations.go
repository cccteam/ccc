package resource

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/http"
	"net/url"
	"strings"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/go-chi/chi/v5"
	"github.com/go-playground/errors/v5"
)

type OperationType string

const (
	OperationCreate OperationType = "add"
	OperationUpdate OperationType = "patch"
	OperationDelete OperationType = "remove"
)

type Operation struct {
	Type OperationType
	Req  *http.Request
}

type patchOperation struct {
	Op    string          `json:"op"`
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value"`
}

type options struct {
	requireCreatePath bool
}

type Option func(opt options) options

func RequireCreatePath() Option {
	return func(o options) options {
		o.requireCreatePath = true

		return o
	}
}

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

			ctx, err := withParams(r.Context(), method, pattern, op.Path, o.requireCreatePath)
			if err != nil {
				yield(nil, err)

				return
			}

			r2, err := http.NewRequestWithContext(ctx, method, op.Path, bytes.NewReader([]byte(op.Value)))
			if err != nil {
				yield(nil, err)

				return
			}

			if !yield(&Operation{Type: OperationType(op.Op), Req: r2}, nil) {
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
		return "", errors.Newf("unsupported operation %q", op)
	}
}

func withParams(ctx context.Context, method, pattern, path string, requireCreatePath bool) (context.Context, error) {
	switch method {
	case http.MethodPost:
		p := strings.TrimPrefix(path, "/")
		if requireCreatePath && p == "" {
			return ctx, httpio.NewBadRequestMessage("path is required for create operation")
		}

		if !requireCreatePath && p != "" {
			return ctx, httpio.NewBadRequestMessage("path is not allowed for create operation")
		}

		if p == "" {
			return ctx, nil
		}

		fallthrough
	case http.MethodPatch, http.MethodDelete:
		if path == "" {
			return ctx, httpio.NewBadRequestMessage("path is required for patch and delete operations")
		}

		var chiContext *chi.Context
		r := chi.NewRouter()
		r.Handle(pattern, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			chiContext = chi.RouteContext(r.Context())
		}))
		r.ServeHTTP(nil, &http.Request{Method: method, Header: make(map[string][]string), URL: &url.URL{Path: path}})

		if chiContext == nil {
			return ctx, httpio.NewBadRequestMessagef("path %q does not match pattern %q", path, pattern)
		}

		ctx = context.WithValue(ctx, chi.RouteCtxKey, chiContext)
	}

	return ctx, nil
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
