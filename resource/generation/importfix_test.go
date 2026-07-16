package generation

import (
	"go/format"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_importFixer_fix(t *testing.T) {
	t.Parallel()

	known := []fixerImport{
		{name: "civil", path: "cloud.google.com/go/civil"},
		{name: "ccc", path: "github.com/cccteam/ccc"},
		{name: "errors", path: "github.com/go-playground/errors/v5"},
		{name: "spanner", path: "cloud.google.com/go/spanner"},
		{name: "spanner", path: "example.com/app/pkg/spanner"},
		{name: "foo", path: "example.com/bar"},
	}
	assumed := []string{
		"example.com/app/pkg/router",
		"example.com/app/pkg/mock/mock_router",
		"cloud.google.com/go/civil", // duplicate of an authoritative entry: must not conflict
	}

	tests := []struct {
		name        string
		src         string
		want        string
		wantUnknown []string
	}{
		{
			name: "prunes unused imports",
			src: `package resources

import (
	"context"
	"reflect"
	"time"

	"github.com/cccteam/ccc"
)

func f(ctx context.Context) time.Time { return time.Time{} }
`,
			want: `package resources

import (
	"context"
	"time"
)

func f(ctx context.Context) time.Time { return time.Time{} }
`,
		},
		{
			name: "adds missing imports from parsed types and stdlib seed",
			src: `package resources

import (
	"context"
)

type Widget struct {
	Day civil.Date
	ID  ccc.UUID
}

func f(ctx context.Context) iter.Seq[int] { return nil }
`,
			want: `package resources

import (
	"context"
	"iter"

	"cloud.google.com/go/civil"
	"github.com/cccteam/ccc"
)

type Widget struct {
	Day civil.Date
	ID  ccc.UUID
}

func f(ctx context.Context) iter.Seq[int] { return nil }
`,
		},
		{
			name: "keeps declared import for a version-suffixed path",
			src: `package app

import (
	"github.com/go-playground/errors/v5"
)

func f() error { return errors.New("x") }
`,
			want: `package app

import "github.com/go-playground/errors/v5"

func f() error { return errors.New("x") }
`,
		},
		{
			name: "adds named import when package name cannot be assumed from path",
			src: `package app

func f() { foo.Bar() }
`,
			want: `package app

import foo "example.com/bar"

func f() { foo.Bar() }
`,
		},
		{
			name: "resolves assumed local package paths",
			src: `package app

import (
	"net/http"
)

func f(r *http.Request) { router.Register(); mock_router.New() }
`,
			want: `package app

import (
	"net/http"

	"example.com/app/pkg/mock/mock_router"
	"example.com/app/pkg/router"
)

func f(r *http.Request) { router.Register(); mock_router.New() }
`,
		},
		{
			name: "declared import wins over ambiguity",
			src: `package app

import (
	"cloud.google.com/go/spanner"
)

func f() spanner.NullString { return spanner.NullString{} }
`,
			want: `package app

import "cloud.google.com/go/spanner"

func f() spanner.NullString { return spanner.NullString{} }
`,
		},
		{
			name: "ambiguous qualifier is unknown",
			src: `package app

func f() spanner.NullString { return spanner.NullString{} }
`,
			wantUnknown: []string{"spanner (ambiguous: cloud.google.com/go/spanner, example.com/app/pkg/spanner)"},
		},
		{
			name: "unresolvable qualifier is unknown",
			src: `package app

func f() { mystery.Call() }
`,
			wantUnknown: []string{"mystery"},
		},
		{
			name: "local objects are not import qualifiers",
			src: `package resources

type widgetQuery struct{ qSet map[string]any }

func (q *widgetQuery) ID() string {
	v, _ := q.qSet["ID"].(string)

	return v
}
`,
			want: `package resources

type widgetQuery struct{ qSet map[string]any }

func (q *widgetQuery) ID() string {
	v, _ := q.qSet["ID"].(string)

	return v
}
`,
		},
		{
			name: "removes import block when nothing is referenced",
			src: `package app

import (
	"time"
)

type marker struct{}
`,
			want: `package app

type marker struct{}
`,
		},
		{
			name: "dot import defers to goimports",
			src: `package app

import . "time"

func f() Time { return Time{} }
`,
			wantUnknown: []string{notLocallyFixable},
		},
		{
			name: "comment inside import block defers to goimports",
			src: `package app

import (
	// pinned
	"time"
)

func f() time.Time { return time.Time{} }
`,
			wantUnknown: []string{notLocallyFixable},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fixer := newImportFixer(known, assumed)
			fixed, unknown, err := fixer.fix("test.go", []byte(tt.src))
			if err != nil {
				t.Fatalf("importFixer.fix() error = %v", err)
			}

			if diff := cmp.Diff(tt.wantUnknown, unknown); diff != "" {
				t.Fatalf("importFixer.fix() unknown mismatch (-want +got):\n%s", diff)
			}

			if len(tt.wantUnknown) > 0 {
				return
			}

			formatted, err := format.Source(fixed)
			if err != nil {
				t.Fatalf("format.Source() error = %v on fixed source:\n%s", err, fixed)
			}

			if diff := cmp.Diff(tt.want, string(formatted)); diff != "" {
				t.Errorf("importFixer.fix() mismatch (-want +got):\n%s", diff)
			}

			// A second pass over its own output must be a no-op: generated files
			// must be a fixed point of the fixer, or regeneration would churn.
			refixed, unknown, err := fixer.fix("test.go", formatted)
			if err != nil || len(unknown) > 0 {
				t.Fatalf("importFixer.fix() second pass: err = %v, unknown = %v", err, unknown)
			}

			reformatted, err := format.Source(refixed)
			if err != nil {
				t.Fatalf("format.Source() second pass error = %v", err)
			}

			if diff := cmp.Diff(string(formatted), string(reformatted)); diff != "" {
				t.Errorf("importFixer.fix() is not idempotent (-first +second):\n%s", diff)
			}
		})
	}
}

func Test_assumedPackageName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want string
	}{
		{path: "time", want: "time"},
		{path: "net/http", want: "http"},
		{path: "github.com/cccteam/ccc", want: "ccc"},
		{path: "github.com/go-playground/errors/v5", want: "errors"},
		{path: "github.com/go-chi/chi/v5", want: "chi"},
		{path: "example.com/app/pkg/mock/mock_router", want: "mock_router"},
		{path: "cloud.google.com/go/civil", want: "civil"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			if got := assumedPackageName(tt.path); got != tt.want {
				t.Errorf("assumedPackageName(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func Test_importFixer_fallbackWarningCarriesScenario(t *testing.T) {
	t.Parallel()

	// The unknown list is the payload of the fallback warning: it must name every
	// unresolved qualifier so the scenario can be reproduced in a test and fixed.
	fixer := newImportFixer(nil, nil)
	_, unknown, err := fixer.fix("test.go", []byte(`package app

func f() { first.Call(); second.Call() }
`))
	if err != nil {
		t.Fatalf("importFixer.fix() error = %v", err)
	}

	want := []string{"first", "second"}
	if diff := cmp.Diff(want, unknown); diff != "" {
		t.Errorf("unknown qualifiers mismatch (-want +got):\n%s", diff)
	}

	for _, q := range unknown {
		if strings.Contains(q, " ") && !strings.Contains(q, "ambiguous") {
			t.Errorf("unknown qualifier %q should be a bare qualifier or an ambiguity report", q)
		}
	}
}
