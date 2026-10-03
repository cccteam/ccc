package resource

// These tests pin the @file route's frame: the decoder's gate (Read on the resource and
// on the route's own field, checked in one call, a Denied decision on either Forbidden
// in the read route's words, a Conditional one on the segment rendered as the row
// predicate with no CASE), the two servers (a stored file through the FileStore with
// the key as its validator, a rendered file with the function's tag), with the 404s
// naming the row and never the key, and the file headers: nosniff and the frame's
// sandbox policy on every served response, the policy added beside the application's,
// and the disposition the type decides, inline for the display-only list and
// attachment for everything else.

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

// fileRequest performs a GET on the served file, with the given request headers,
// behind a middleware that set the outlet's caching header alone.
func fileRequest(t *testing.T, headers map[string]string, serve func(w http.ResponseWriter, r *http.Request) error) *httptest.ResponseRecorder {
	t.Helper()

	return fileRequestBehind(t, nil, headers, serve)
}

// fileRequestBehind performs a GET on the served file, with the given request headers,
// behind a middleware that set the given response headers (an application's security
// headers) beside the outlet's caching header before the handler ran.
func fileRequestBehind(t *testing.T, response, headers map[string]string, serve func(w http.ResponseWriter, r *http.Request) error) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/things/1/content", http.NoBody)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	rr := httptest.NewRecorder()
	// The outlet's caching headers, as its middleware sets them before the handler.
	rr.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	for name, value := range response {
		rr.Header().Set(name, value)
	}
	// A refusal comes back for the handler to encode, as the generated handlers do.
	if err := serve(rr, req); err != nil {
		_ = httpio.NewEncoder(rr).ClientMessage(req.Context(), err)
	}

	return rr
}

// The file headers as the frame sets them.
const (
	wantNosniff = "nosniff"
	wantSandbox = "sandbox; default-src 'none'"
)

// assertFileHeaders asserts the headers every served file carries: nosniff, and the
// frame's policy after the application's policies, which stay in place.
func assertFileHeaders(t *testing.T, header http.Header, applicationPolicies ...string) {
	t.Helper()

	if got := header.Get("X-Content-Type-Options"); got != wantNosniff {
		t.Errorf("X-Content-Type-Options = %q, want %q", got, wantNosniff)
	}
	want := append(slices.Clone(applicationPolicies), wantSandbox)
	if got := header.Values("Content-Security-Policy"); !slices.Equal(got, want) {
		t.Errorf("Content-Security-Policy = %q, want %q", got, want)
	}
}

func TestServeStoredFile(t *testing.T) {
	t.Parallel()

	const key = "0193e2a7-522c-708f-bfd0-4adf33486bb1"

	tests := []struct {
		name    string
		file    StoredFile
		headers map[string]string
		openErr error
		// noStore serves with no store wired for the column.
		noStore     bool
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
				"Content-Type":            "text/plain",
				"Content-Disposition":     `inline; filename=brief.txt`,
				"ETag":                    `"` + key + `"`,
				"Cache-Control":           "private, no-cache",
				"Content-Length":          "17",
				"X-Content-Type-Options":  wantNosniff,
				"Content-Security-Policy": wantSandbox,
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
			wantHeaders: map[string]string{"ETag": `"` + key + `"`, "Cache-Control": "private, no-cache", "X-Content-Type-Options": wantNosniff, "Content-Security-Policy": wantSandbox},
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
			wantHeaders: map[string]string{"Content-Range": "bytes 0-6/17", "Content-Disposition": "inline", "X-Content-Type-Options": wantNosniff, "Content-Security-Policy": wantSandbox},
			wantBody:    "Halvard",
			wantOpened:  true,
		},
		{
			name:       "a few ranges are honored as one multipart answer",
			file:       StoredFile{Key: key, ContentType: "text/plain"},
			headers:    map[string]string{"Range": "bytes=0-2,4-6"},
			wantStatus: http.StatusPartialContent,
			wantOpened: true,
		},
		{
			name:        "more ranges than the cap get the whole file, since each costs the store a read",
			file:        StoredFile{Key: key, ContentType: "text/plain"},
			headers:     map[string]string{"Range": "bytes=0-0,1-1,2-2,3-3,4-4,5-5,6-6,7-7,8-8"},
			wantStatus:  http.StatusOK,
			wantHeaders: map[string]string{"Content-Length": "17", "Content-Type": "text/plain"},
			wantBody:    "Halvard hauler ok",
			wantOpened:  true,
		},
		{
			name:        "no store wired for the column is an error naming the wiring, never a 404",
			file:        StoredFile{Key: key, ContentType: "text/plain"},
			noStore:     true,
			wantStatus:  http.StatusInternalServerError,
			wantMessage: "",
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
			var served FileStore = store
			if tt.noStore {
				served = nil
			}

			var servedErr error
			rr := fileRequest(t, tt.headers, func(w http.ResponseWriter, r *http.Request) error {
				servedErr = ServeStoredFile(r.Context(), w, r, served, tt.file, "content", "MissionDocument", mustUUIDFromString("0193e2a7-522c-708f-bfd0-4adf33486bb9"))

				return servedErr
			})

			if rr.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", rr.Code, tt.wantStatus, rr.Body.String())
			}
			if tt.noStore && (servedErr == nil || !strings.Contains(servedErr.Error(), "no file store is wired for the store the content of MissionDocument is kept in")) {
				t.Errorf("ServeStoredFile() error = %v, want it to name the wiring", servedErr)
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
			if rr.Code != http.StatusNotFound && rr.Code != http.StatusInternalServerError {
				assertFileHeaders(t, rr.Header())
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
				"Content-Type":            "text/csv",
				"Content-Disposition":     `attachment; filename=manifest.csv`,
				"Content-Length":          "8",
				"Last-Modified":           "Fri, 18 Sep 2026 12:00:00 GMT",
				"ETag":                    `"v1"`,
				"Cache-Control":           "private, no-cache",
				"X-Content-Type-Options":  wantNosniff,
				"Content-Security-Policy": wantSandbox,
			},
			wantBody: "a,b\n1,2\n",
		},
		{
			name:        "a matching tag answers 304 and closes the body",
			content:     renderedContent("a,b\n1,2\n", `"v1"`),
			headers:     map[string]string{"If-None-Match": `"v1"`},
			wantStatus:  http.StatusNotModified,
			wantHeaders: map[string]string{"ETag": `"v1"`, "X-Content-Type-Options": wantNosniff, "Content-Security-Policy": wantSandbox},
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
			wantHeaders: map[string]string{"Content-Type": "image/png", "Content-Disposition": `inline; filename=chart.png`},
			wantBody:    "PNG",
			wantNoETag:  true,
		},
		{
			name: "no name sends the disposition without a filename",
			content: &Content{
				ContentType: "text/csv",
				Size:        -1,
				Body:        io.NopCloser(strings.NewReader("a,b\n")),
			},
			wantStatus:  http.StatusOK,
			wantHeaders: map[string]string{"Content-Disposition": "attachment"},
			wantBody:    "a,b\n",
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
			if rr.Code != http.StatusNotFound {
				assertFileHeaders(t, rr.Header())
			}
		})
	}
}

// TestServeStoredFile_uploadedTypes drives an upload through the frame for each shape of
// file and asserts on the headers of the response that serves it back: the type the part
// declared is the Content-Type, under nosniff and the sandbox policy; a type that can
// carry script, and one the frame does not know, downloads; the display-only types show
// inline; every disposition carries the file's name.
func TestServeStoredFile_uploadedTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// part is the file part as the client sent it: its name, its declared type
		// (none for the octet-stream case), and its bytes.
		part            uploadPart
		wantType        string
		wantDisposition string
	}{
		{
			name:            "an HTML page downloads",
			part:            uploadPart{fileName: "page.html", contentType: "text/html", content: `<script>fetch("/api/user/session")</script>`},
			wantType:        "text/html",
			wantDisposition: `attachment; filename=page.html`,
		},
		{
			name:            "an XHTML page downloads",
			part:            uploadPart{fileName: "page.xhtml", contentType: "application/xhtml+xml", content: `<html xmlns="http://www.w3.org/1999/xhtml"/>`},
			wantType:        "application/xhtml+xml",
			wantDisposition: `attachment; filename=page.xhtml`,
		},
		{
			name:            "a JavaScript file downloads",
			part:            uploadPart{fileName: "evil.js", contentType: "text/javascript", content: `fetch("/api/user/session")`},
			wantType:        "text/javascript",
			wantDisposition: `attachment; filename=evil.js`,
		},
		{
			name:            "an SVG downloads",
			part:            uploadPart{fileName: "logo.svg", contentType: "image/svg+xml", content: `<svg xmlns="http://www.w3.org/2000/svg"><script/></svg>`},
			wantType:        "image/svg+xml",
			wantDisposition: `attachment; filename=logo.svg`,
		},
		{
			name:            "an XML document downloads",
			part:            uploadPart{fileName: "feed.xml", contentType: "application/xml", content: `<?xml version="1.0"?><feed/>`},
			wantType:        "application/xml",
			wantDisposition: `attachment; filename=feed.xml`,
		},
		{
			name:            "a type the frame does not know downloads",
			part:            uploadPart{fileName: "brief.dat", contentType: "application/x-brief", content: "brief"},
			wantType:        "application/x-brief",
			wantDisposition: `attachment; filename=brief.dat`,
		},
		{
			name:            "a type that does not parse downloads",
			part:            uploadPart{fileName: "odd.bin", contentType: "not a type", content: "odd"},
			wantType:        "not a type",
			wantDisposition: `attachment; filename=odd.bin`,
		},
		{
			name:            "no declared type is application/octet-stream and downloads",
			part:            uploadPart{fileName: "blob", content: "bytes"},
			wantType:        octetStream,
			wantDisposition: `attachment; filename=blob`,
		},
		{
			name:            "a CSV is not on the display list and downloads",
			part:            uploadPart{fileName: "rows.csv", contentType: "text/csv", content: "a,b\n"},
			wantType:        "text/csv",
			wantDisposition: `attachment; filename=rows.csv`,
		},
		{
			name:            "a PNG displays",
			part:            uploadPart{fileName: "chart.png", contentType: "image/png", content: "PNG"},
			wantType:        "image/png",
			wantDisposition: `inline; filename=chart.png`,
		},
		{
			name:            "a JPEG displays",
			part:            uploadPart{fileName: "photo.jpg", contentType: "image/jpeg", content: "JPEG"},
			wantType:        "image/jpeg",
			wantDisposition: `inline; filename=photo.jpg`,
		},
		{
			name:            "a GIF displays",
			part:            uploadPart{fileName: "anim.gif", contentType: "image/gif", content: "GIF"},
			wantType:        "image/gif",
			wantDisposition: `inline; filename=anim.gif`,
		},
		{
			name:            "a WebP displays",
			part:            uploadPart{fileName: "photo.webp", contentType: "image/webp", content: "WEBP"},
			wantType:        "image/webp",
			wantDisposition: `inline; filename=photo.webp`,
		},
		{
			name:            "an AVIF displays",
			part:            uploadPart{fileName: "photo.avif", contentType: "image/avif", content: "AVIF"},
			wantType:        "image/avif",
			wantDisposition: `inline; filename=photo.avif`,
		},
		{
			name:            "a PDF displays",
			part:            uploadPart{fileName: "brief.pdf", contentType: "application/pdf", content: "%PDF-1.7 brief"},
			wantType:        "application/pdf",
			wantDisposition: `inline; filename=brief.pdf`,
		},
		{
			name:            "plain text displays",
			part:            uploadPart{fileName: "brief.txt", contentType: "text/plain", content: "brief"},
			wantType:        "text/plain",
			wantDisposition: `inline; filename=brief.txt`,
		},
		{
			name:            "plain text with a charset parameter displays, the parameter kept on the type",
			part:            uploadPart{fileName: "brief.txt", contentType: "text/plain; charset=utf-8", content: "brief"},
			wantType:        "text/plain; charset=utf-8",
			wantDisposition: `inline; filename=brief.txt`,
		},
		{
			name:            "the list is matched without regard to case, and the type is sent as declared",
			part:            uploadPart{fileName: "chart.png", contentType: "Image/PNG", content: "PNG"},
			wantType:        "Image/PNG",
			wantDisposition: `inline; filename=chart.png`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			part := tt.part
			part.name = UploadFilePart
			req := multipartRequest(t,
				uploadPart{name: UploadRequestPart, contentType: "application/json", content: `{"missionId":"m1","title":"Brief"}`},
				part,
			)
			upload, err := OpenUpload(req, 1<<20)
			if err != nil {
				t.Fatalf("OpenUpload() error = %v", err)
			}
			store := newMemoryStore()
			files, err := upload.Stream(t.Context(), store, false)
			if err != nil {
				t.Fatalf("Upload.Stream() error = %v", err)
			}
			if len(files) != 1 {
				t.Fatalf("Stream() = %d files, want one", len(files))
			}
			// The row as an upload method's body records it: the key, the name, and the
			// type the part declared.
			file := StoredFile{Key: files[0].Key, Name: files[0].Name, ContentType: files[0].ContentType}

			rr := fileRequest(t, nil, func(w http.ResponseWriter, r *http.Request) error {
				return ServeStoredFile(r.Context(), w, r, store, file, "content", "MissionDocument", mustUUIDFromString("0193e2a7-522c-708f-bfd0-4adf33486bb9"))
			})
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body: %s", rr.Code, rr.Body.String())
			}
			if got := rr.Header().Get("Content-Type"); got != tt.wantType {
				t.Errorf("Content-Type = %q, want %q", got, tt.wantType)
			}
			if got := rr.Header().Get("Content-Disposition"); got != tt.wantDisposition {
				t.Errorf("Content-Disposition = %q, want %q", got, tt.wantDisposition)
			}
			assertFileHeaders(t, rr.Header())
			if rr.Body.String() != tt.part.content {
				t.Errorf("body = %q, want %q", rr.Body.String(), tt.part.content)
			}
		})
	}
}

// TestServeFile_policyBesideApplication pins where the frame's policy goes: beside a
// policy an application's middleware set, never in its place, so both are enforced, and
// alone when no middleware set one; on the bytes, on a range of them, and on a 304, from
// the stored path and the rendered one alike.
func TestServeFile_policyBesideApplication(t *testing.T) {
	t.Parallel()

	const (
		key               = "0193e2a7-522c-708f-bfd0-4adf33486bb1"
		applicationPolicy = "default-src 'self'; frame-ancestors 'none'"
	)
	// applicationHeaders are what a security-headers middleware set before the frame ran.
	applicationHeaders := map[string]string{
		"Content-Security-Policy": applicationPolicy,
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
	}
	stored := func(w http.ResponseWriter, r *http.Request) error {
		store := newMemoryStore()
		store.objects[key] = []byte("Halvard hauler ok")
		store.types[key] = "text/html"

		return ServeStoredFile(r.Context(), w, r, store, StoredFile{Key: key, Name: "page.html"}, "content", "MissionDocument", "1")
	}
	rendered := func(w http.ResponseWriter, r *http.Request) error {
		return ServeRenderedFile(w, r, renderedContent("a,b\n1,2\n", "v1"), "content", "ExpenseManifest", "8000")
	}

	tests := []struct {
		name     string
		serve    func(w http.ResponseWriter, r *http.Request) error
		response map[string]string
		headers  map[string]string
		// wantPolicies is the application's policies, in order, ahead of the frame's.
		wantPolicies    []string
		wantStatus      int
		wantDisposition string
	}{
		{
			name:            "no middleware policy: the frame's alone",
			serve:           stored,
			wantStatus:      http.StatusOK,
			wantDisposition: `attachment; filename=page.html`,
		},
		{
			name:            "the stored bytes behind a middleware policy carry both",
			serve:           stored,
			response:        applicationHeaders,
			wantPolicies:    []string{applicationPolicy},
			wantStatus:      http.StatusOK,
			wantDisposition: `attachment; filename=page.html`,
		},
		{
			name:            "a range answer keeps both",
			serve:           stored,
			response:        applicationHeaders,
			headers:         map[string]string{"Range": "bytes=0-6"},
			wantPolicies:    []string{applicationPolicy},
			wantStatus:      http.StatusPartialContent,
			wantDisposition: `attachment; filename=page.html`,
		},
		{
			name:         "a stored 304 keeps both",
			serve:        stored,
			response:     applicationHeaders,
			headers:      map[string]string{"If-None-Match": `"` + key + `"`},
			wantPolicies: []string{applicationPolicy},
			wantStatus:   http.StatusNotModified,
		},
		{
			name:            "the rendered bytes behind a middleware policy carry both",
			serve:           rendered,
			response:        applicationHeaders,
			wantPolicies:    []string{applicationPolicy},
			wantStatus:      http.StatusOK,
			wantDisposition: `attachment; filename=manifest.csv`,
		},
		{
			name:         "a rendered 304 keeps both",
			serve:        rendered,
			response:     applicationHeaders,
			headers:      map[string]string{"If-None-Match": `"v1"`},
			wantPolicies: []string{applicationPolicy},
			wantStatus:   http.StatusNotModified,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rr := fileRequestBehind(t, tt.response, tt.headers, tt.serve)
			if rr.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", rr.Code, tt.wantStatus, rr.Body.String())
			}
			assertFileHeaders(t, rr.Header(), tt.wantPolicies...)
			if got := rr.Header().Get("Content-Disposition"); got != tt.wantDisposition {
				t.Errorf("Content-Disposition = %q, want %q", got, tt.wantDisposition)
			}
			if tt.response != nil {
				if got := rr.Header().Get("X-Frame-Options"); got != "DENY" {
					t.Errorf("X-Frame-Options = %q, want the application's DENY kept", got)
				}
			}
		})
	}
}
