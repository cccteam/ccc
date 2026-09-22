package resource

// These tests pin the @file route's frame: the decoder's gate (Read on the resource and
// on the route's own field, checked in one call, a Denied decision on either Forbidden
// in the read route's words, a Conditional one on the segment rendered as the row
// predicate with no CASE), and the two servers (a stored file through the FileStore
// with the key as its validator, a rendered file with the function's tag), with the 404s
// naming the row and never the key.

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"go.uber.org/mock/gomock"
)

// enforcementFileRequest is the frame's projection of the enforcement fixture: the key,
// Locked as the store-key column, Public as the name column, every one exempt.
type enforcementFileRequest struct {
	ID     ccc.UUID `json:"id" perm:"-"`
	Locked string   `json:"-"  perm:"-"`
	Public string   `json:"-"  perm:"-"`
}

// enforcementGrantedFileRequest carries a grant-bearing field, which the frame's
// projection must not: construction refuses it.
type enforcementGrantedFileRequest struct {
	ID     ccc.UUID `json:"id"     perm:"-"`
	Public string   `json:"public"`
}

func TestNewFileDecoder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		build   func() error
		wantErr string
	}{
		{
			name: "a projection of exempt fields builds",
			build: func() error {
				_, err := NewFileDecoder[enforcementResource, enforcementFileRequest](enforcementCollection(t), "content")

				return err
			},
		},
		{
			name: "a grant-bearing field in the projection is refused",
			build: func() error {
				_, err := NewFileDecoder[enforcementResource, enforcementGrantedFileRequest](enforcementCollection(t), "content")

				return err
			},
			wantErr: `field Public of the frame's projection must carry perm:"-"`,
		},
		{
			name: "an empty segment is refused",
			build: func() error {
				_, err := NewComputedFileDecoder[enforcementResource, enforcementFileRequest]("")

				return err
			},
			wantErr: "the route needs a segment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.build()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("error = %v, want none", err)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestFileDecoder_Decode(t *testing.T) {
	t.Parallel()

	const gate = enforcedResource + ".content"

	tests := []struct {
		name        string
		computed    bool
		grants      map[accesstypes.Permission][]accesstypes.Resource
		conditional map[accesstypes.Permission][]accesstypes.Resource
		wantStatus  int
		wantErr     string
		// wantPredicate says whether the statement carries the fixture condition's
		// column as its row predicate.
		wantPredicate bool
	}{
		{
			name:   "Read on the resource and the segment opens the route with no row predicate",
			grants: map[accesstypes.Permission][]accesstypes.Resource{accesstypes.Read: {enforcedResource, gate}},
		},
		{
			name:       "no Read on the resource is Forbidden",
			grants:     map[accesstypes.Permission][]accesstypes.Resource{accesstypes.Read: {gate}},
			wantStatus: http.StatusForbidden,
			wantErr:    "does not have (Read) on [enforcementResources]",
		},
		{
			name:       "Read on the resource without the segment is Forbidden: the key column is not the gate",
			grants:     map[accesstypes.Permission][]accesstypes.Resource{accesstypes.Read: {enforcedResource, enforcedResource + ".locked"}},
			wantStatus: http.StatusForbidden,
			wantErr:    "does not have (Read) on [enforcementResources.content]",
		},
		{
			name:          "a conditional grant on the segment is the row predicate",
			grants:        map[accesstypes.Permission][]accesstypes.Resource{accesstypes.Read: {enforcedResource}},
			conditional:   map[accesstypes.Permission][]accesstypes.Resource{accesstypes.Read: {gate}},
			wantPredicate: true,
		},
		{
			name:        "a conditional grant on the resource alone is the gate and renders nothing",
			grants:      map[accesstypes.Permission][]accesstypes.Resource{accesstypes.Read: {gate}},
			conditional: map[accesstypes.Permission][]accesstypes.Resource{accesstypes.Read: {enforcedResource}},
		},
		{
			name:     "a computed resource's gate passes with unconditional grants",
			computed: true,
			grants:   map[accesstypes.Permission][]accesstypes.Resource{accesstypes.Read: {enforcedResource, gate}},
		},
		{
			name:        "a computed resource meets a conditional decision as the invariant breach it is",
			computed:    true,
			grants:      map[accesstypes.Permission][]accesstypes.Resource{accesstypes.Read: {enforcedResource}},
			conditional: map[accesstypes.Permission][]accesstypes.Resource{accesstypes.Read: {gate}},
			wantErr:     "invariant breach: conditional grants for (Read) on [enforcementResources.content]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			userPermissions := &fakeUserPermissions{granted: tt.grants, conditional: tt.conditional}
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)

			var (
				qSet *QuerySet[enforcementResource]
				err  error
			)
			if tt.computed {
				qSet, err = MustNewComputedFileDecoder[enforcementResource, enforcementFileRequest]("content").Decode(req, userPermissions, testScope)
			} else {
				qSet, err = MustNewFileDecoder[enforcementResource, enforcementFileRequest](enforcementCollection(t), "content").Decode(req, userPermissions, testScope)
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Decode() error = %v, want containing %q", err, tt.wantErr)
				}
				if tt.wantStatus != 0 && !httpio.HasForbidden(err) {
					t.Errorf("Decode() error = %v, want Forbidden", err)
				}

				return
			}
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}

			// One check for the gate: the resource and the segment together, in the
			// request's scope, with the decision context stamped.
			if userPermissions.checkCalls != 1 {
				t.Fatalf("Check() called %d times at decode, want once", userPermissions.checkCalls)
			}
			if got := userPermissions.gotResources[0]; !slices.Equal(got, []accesstypes.Resource{enforcedResource, gate}) {
				t.Errorf("Check() resources = %v, want the resource and the segment", got)
			}
			if userPermissions.gotScopes[0] != testScope {
				t.Errorf("Check() scope = %v, want %v", userPermissions.gotScopes[0], testScope)
			}
			if _, ok := userPermissions.gotEnvs[0].Now(); !ok {
				t.Error("Check() Environment carries no now; the decoder must stamp the decision context")
			}
			if got := slices.Sorted(slices.Values(qSet.Fields())); !slices.Equal(got, []accesstypes.Field{"ID", "Locked", "Public"}) {
				t.Errorf("Fields() = %v, want the frame's projection", got)
			}
			if qSet.Scope() != testScope || qSet.User() != "testUser" || qSet.RequiredPermission() != accesstypes.Read {
				t.Errorf("QuerySet carries scope %v, user %v, permission %v", qSet.Scope(), qSet.User(), qSet.RequiredPermission())
			}
			if tt.computed {
				// The computed row is the application's function's; nothing runs a statement.
				return
			}

			qSet.SetKey("ID", mustUUIDFromString("00000000-0000-0000-0000-000000000001"))
			var stmt *Statement
			ctrl := gomock.NewController(t)
			reader := NewMockReader[enforcementResource](ctrl)
			reader.EXPECT().DBType().MinTimes(1).Return(SpannerDBType)
			reader.EXPECT().Read(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, s *Statement) (*Row[enforcementResource], error) {
				stmt = s

				return &Row[enforcementResource]{}, nil
			})
			if _, err := qSet.Read(t.Context(), NewMockClient(nil, []any{reader}, nil)); err != nil {
				t.Fatalf("QuerySet.Read() error = %v", err)
			}

			// The row is located within the tenant, by key, and the segment's
			// condition, where it is one, filters rows without a CASE or a mask column.
			if !strings.Contains(stmt.SQL, "Station") {
				t.Errorf("statement carries no tenancy predicate:\n%s", stmt.SQL)
			}
			if got := strings.Contains(stmt.SQL, "Owner"); got != tt.wantPredicate {
				t.Errorf("statement names the condition's column = %v, want %v:\n%s", got, tt.wantPredicate, stmt.SQL)
			}
			if strings.Contains(stmt.SQL, "CASE") || stmt.maskedNamesColumn != "" {
				t.Errorf("statement renders a CASE or a mask column, which a frame projection never needs:\n%s", stmt.SQL)
			}
			for _, column := range []string{"Locked", "Public", "Id"} {
				if !strings.Contains(stmt.SQL, column) {
					t.Errorf("statement does not select %s:\n%s", column, stmt.SQL)
				}
			}
		})
	}
}

// fileRequest performs a GET on the served file, with the given headers.
func fileRequest(t *testing.T, headers map[string]string, serve func(w http.ResponseWriter, r *http.Request) error) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/things/1/content", http.NoBody)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	rr := httptest.NewRecorder()
	// The outlet's caching headers, as its middleware sets them before the handler.
	rr.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	// A refusal comes back for the handler to encode, as the generated handlers do.
	if err := serve(rr, req); err != nil {
		_ = httpio.NewEncoder(rr).ClientMessage(req.Context(), err)
	}

	return rr
}

func TestServeStoredFile(t *testing.T) {
	t.Parallel()

	const key = "0193e2a7-522c-708f-bfd0-4adf33486bb1"

	tests := []struct {
		name        string
		file        StoredFile
		headers     map[string]string
		openErr     error
		wantStatus  int
		wantHeaders map[string]string
		wantBody    string
		wantMessage string
		wantOpened  bool
	}{
		{
			name:       "the stored bytes, typed and named by the row, with the key as the validator",
			file:       StoredFile{Key: key, Name: "brief.txt", ContentType: "text/plain"},
			wantStatus: http.StatusOK,
			wantHeaders: map[string]string{
				"Content-Type":        "text/plain",
				"Content-Disposition": `inline; filename=brief.txt`,
				"ETag":                `"` + key + `"`,
				"Cache-Control":       "private, no-cache",
				"Content-Length":      "17",
			},
			wantBody:   "Halvard hauler ok",
			wantOpened: true,
		},
		{
			name:        "with no type column the object's type stands",
			file:        StoredFile{Key: key, Name: "brief.txt"},
			wantStatus:  http.StatusOK,
			wantHeaders: map[string]string{"Content-Type": "application/x-brief"},
			wantBody:    "Halvard hauler ok",
			wantOpened:  true,
		},
		{
			name:        "a matching If-None-Match answers 304 before the store is opened",
			file:        StoredFile{Key: key, Name: "brief.txt", ContentType: "text/plain"},
			headers:     map[string]string{"If-None-Match": `W/"other", "` + key + `"`},
			wantStatus:  http.StatusNotModified,
			wantHeaders: map[string]string{"ETag": `"` + key + `"`, "Cache-Control": "private, no-cache"},
		},
		{
			name:        "a stale If-None-Match serves the bytes",
			file:        StoredFile{Key: key, ContentType: "text/plain"},
			headers:     map[string]string{"If-None-Match": `"stale"`},
			wantStatus:  http.StatusOK,
			wantHeaders: map[string]string{"ETag": `"` + key + `"`},
			wantBody:    "Halvard hauler ok",
			wantOpened:  true,
		},
		{
			name:        "a range over the seekable body is honored",
			file:        StoredFile{Key: key, ContentType: "text/plain"},
			headers:     map[string]string{"Range": "bytes=0-6"},
			wantStatus:  http.StatusPartialContent,
			wantHeaders: map[string]string{"Content-Range": "bytes 0-6/17"},
			wantBody:    "Halvard",
			wantOpened:  true,
		},
		{
			name:        "a NULL key is 404 in the row's words",
			file:        StoredFile{},
			wantStatus:  http.StatusNotFound,
			wantMessage: "MissionDocument 0193e2a7-522c-708f-bfd0-4adf33486bb9 has no content",
		},
		{
			name:        "a key the store does not hold is 404 in the row's words, never the key",
			file:        StoredFile{Key: "0193e2a7-522c-708f-bfd0-4adf33486bb2"},
			wantStatus:  http.StatusNotFound,
			wantMessage: "the content of MissionDocument 0193e2a7-522c-708f-bfd0-4adf33486bb9 was not found in the store",
			wantOpened:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := newMemoryStore()
			store.objects[key] = []byte("Halvard hauler ok")
			store.types[key] = "application/x-brief"
			store.openErr = tt.openErr

			rr := fileRequest(t, tt.headers, func(w http.ResponseWriter, r *http.Request) error {
				return ServeStoredFile(r.Context(), w, r, store, tt.file, "content", "MissionDocument", mustUUIDFromString("0193e2a7-522c-708f-bfd0-4adf33486bb9"))
			})

			if rr.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", rr.Code, tt.wantStatus, rr.Body.String())
			}
			for name, want := range tt.wantHeaders {
				if got := rr.Header().Get(name); got != want {
					t.Errorf("%s = %q, want %q", name, got, want)
				}
			}
			if tt.wantBody != "" && rr.Body.String() != tt.wantBody {
				t.Errorf("body = %q, want %q", rr.Body.String(), tt.wantBody)
			}
			if tt.wantMessage != "" {
				if !strings.Contains(rr.Body.String(), tt.wantMessage) {
					t.Errorf("body = %s, want the message %q", rr.Body.String(), tt.wantMessage)
				}
				if strings.Contains(rr.Body.String(), tt.file.Key) && tt.file.Key != "" {
					t.Errorf("body = %s, names the store key", rr.Body.String())
				}
			}
			if got := len(store.opened) > 0; got != tt.wantOpened {
				t.Errorf("store opened = %v, want %v", got, tt.wantOpened)
			}
			if rr.Code == http.StatusNotModified && rr.Body.Len() != 0 {
				t.Errorf("a 304 carries a body: %q", rr.Body.String())
			}
		})
	}
}

// renderedContent is a rendered document as a content function returns it.
func renderedContent(body, tag string) *Content {
	return &Content{
		Name:        "manifest.csv",
		ContentType: "text/csv",
		Size:        int64(len(body)),
		ModTime:     time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC),
		Tag:         tag,
		Body:        io.NopCloser(strings.NewReader(body)),
	}
}

func TestServeRenderedFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		content     *Content
		headers     map[string]string
		wantStatus  int
		wantHeaders map[string]string
		wantBody    string
		wantMessage string
		// wantNoETag pins that a content with no tag sends no validator and leaves
		// the outlet's caching headers standing.
		wantNoETag bool
	}{
		{
			name:       "the rendered bytes with their type, name, length, time, and the quoted tag",
			content:    renderedContent("a,b\n1,2\n", "v1"),
			wantStatus: http.StatusOK,
			wantHeaders: map[string]string{
				"Content-Type":        "text/csv",
				"Content-Disposition": `inline; filename=manifest.csv`,
				"Content-Length":      "8",
				"Last-Modified":       "Fri, 18 Sep 2026 12:00:00 GMT",
				"ETag":                `"v1"`,
				"Cache-Control":       "private, no-cache",
			},
			wantBody: "a,b\n1,2\n",
		},
		{
			name:        "a matching tag answers 304 and closes the body",
			content:     renderedContent("a,b\n1,2\n", `"v1"`),
			headers:     map[string]string{"If-None-Match": `"v1"`},
			wantStatus:  http.StatusNotModified,
			wantHeaders: map[string]string{"ETag": `"v1"`},
		},
		{
			name:        "no tag sends no validator, and the outlet's caching headers stand",
			content:     renderedContent("a,b\n", ""),
			wantStatus:  http.StatusOK,
			wantHeaders: map[string]string{"Cache-Control": "no-cache, no-store, must-revalidate"},
			wantBody:    "a,b\n",
			wantNoETag:  true,
		},
		{
			name: "no type resolves from the name's extension",
			content: &Content{
				Name: "chart.png",
				Size: -1,
				Body: io.NopCloser(bytes.NewReader([]byte("PNG"))),
			},
			wantStatus:  http.StatusOK,
			wantHeaders: map[string]string{"Content-Type": "image/png"},
			wantBody:    "PNG",
			wantNoETag:  true,
		},
		{
			name:        "a nil content is 404 in the row's words",
			wantStatus:  http.StatusNotFound,
			wantMessage: "ExpenseManifest 8000 has no content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rr := fileRequest(t, tt.headers, func(w http.ResponseWriter, r *http.Request) error {
				return ServeRenderedFile(w, r, tt.content, "content", "ExpenseManifest", "8000")
			})

			if rr.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", rr.Code, tt.wantStatus, rr.Body.String())
			}
			for name, want := range tt.wantHeaders {
				if got := rr.Header().Get(name); got != want {
					t.Errorf("%s = %q, want %q", name, got, want)
				}
			}
			if tt.wantNoETag && rr.Header().Get("ETag") != "" {
				t.Errorf("ETag = %q, want none", rr.Header().Get("ETag"))
			}
			if tt.wantBody != "" && rr.Body.String() != tt.wantBody {
				t.Errorf("body = %q, want %q", rr.Body.String(), tt.wantBody)
			}
			if tt.wantMessage != "" && !strings.Contains(rr.Body.String(), tt.wantMessage) {
				t.Errorf("body = %s, want the message %q", rr.Body.String(), tt.wantMessage)
			}
			if tt.content != nil && tt.content.Size < 0 && rr.Code == http.StatusOK && rr.Header().Get("Content-Length") != "" {
				t.Errorf("Content-Length = %q on an unknown size, want none", rr.Header().Get("Content-Length"))
			}
		})
	}
}
