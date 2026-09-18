// Demonstrates: @upload, rpc.upload-store, @file.stored, execute-condition.
package integration

// This suite covers the upload form and the stored file route against the demo world:
// AttachMissionDocument declares @upload(max: 5MB), so its request is multipart — the
// JSON part first, the files after — and its body claims the streamed files with one
// MissionDocuments row each. A committed transaction leaves the rows and the objects in
// the store; a refusal or a failure before commit leaves neither; a dry run writes
// nothing and answers 200; no listing carries the store key. The generated file route
// under the read route serves the bytes to a crew member holding Read on the documents
// and on content, typed and named by the row, with the key as its validator; the client
// portal lists documents and holds no content, so it cannot download them.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/members"
	"github.com/cccteam/ccc/resource/lodestar/pkg/router"
	"github.com/cccteam/ccc/resource/lodestar/pkg/store"
	"github.com/cccteam/session/sessioninfo"
)

// uploadFile is one file part of a scripted upload.
type uploadFile struct {
	name, contentType string
	content           []byte
}

// multipartBody builds the upload form: the request part first, then the files.
func multipartBody(t *testing.T, request string, files ...uploadFile) (body *bytes.Buffer, contentType string) {
	t.Helper()

	body = &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreatePart(map[string][]string{
		"Content-Disposition": {`form-data; name="` + resource.UploadRequestPart + `"`},
		"Content-Type":        {"application/json"},
	})
	if err != nil {
		t.Fatalf("multipart.Writer.CreatePart() error = %v", err)
	}
	if _, err := io.WriteString(part, request); err != nil {
		t.Fatalf("io.WriteString() error = %v", err)
	}
	for _, file := range files {
		part, err := writer.CreatePart(map[string][]string{
			"Content-Disposition": {fmt.Sprintf(`form-data; name=%q; filename=%q`, resource.UploadFilePart, file.name)},
			"Content-Type":        {file.contentType},
		})
		if err != nil {
			t.Fatalf("multipart.Writer.CreatePart() error = %v", err)
		}
		if _, err := part.Write(file.content); err != nil {
			t.Fatalf("multipart.Part.Write() error = %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("multipart.Writer.Close() error = %v", err)
	}

	return body, writer.FormDataContentType()
}

// doUploadAs posts a multipart upload as the given user, optionally as a dry run.
func doUploadAs(t *testing.T, h http.Handler, user accesstypes.User, target string, body *bytes.Buffer, contentType string, dryRun bool) (statusCode int, respBody []byte) {
	t.Helper()

	sessionID, err := ccc.NewUUID()
	if err != nil {
		t.Fatalf("ccc.NewUUID: %v", err)
	}
	ctx := context.WithValue(t.Context(), sessioninfo.CtxSessionInfo, &sessioninfo.SessionData{
		SessionInfo: &sessioninfo.SessionInfo{ID: sessionID, Username: string(user)},
	})
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, target, body)
	req.Header.Set("Content-Type", contentType)
	if dryRun {
		req.Header.Set(resource.DryRunHeader, "true")
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	return rr.Code, rr.Body.Bytes()
}

// documentWorld is a fresh demo world whose App writes documents into a directory the
// test owns, served through the generated routes alone.
func documentWorld(t *testing.T) (h http.Handler, documents *store.DirStore, dir string) {
	t.Helper()

	db, err := prepareDatabase(t.Context(), t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	dir = t.TempDir()
	documents, err = store.NewDirStore(dir)
	if err != nil {
		t.Fatalf("store.NewDirStore() error = %v", err)
	}
	t.Cleanup(func() { _ = documents.Close() })

	return router.NewTestRouter(newAppWithDocuments(db, demoAccessClient(t), documents)), documents, dir
}

// storedFiles lists the objects in the store directory.
func storedFiles(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("os.ReadDir() error = %v", err)
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	slices.Sort(names)

	return names
}

func TestAttachMissionDocument(t *testing.T) {
	t.Parallel()

	brief := uploadFile{name: "brief.pdf", contentType: "application/pdf", content: []byte("%PDF-1.7 escort brief")}
	chart := uploadFile{name: "chart.png", contentType: "image/png", content: []byte("PNG chart")}
	request := func(missionID string) string {
		return fmt.Sprintf(`{"missionId":%q,"title":"Escort brief"}`, missionID)
	}

	tests := []struct {
		name       string
		user       accesstypes.User
		missionID  string
		files      []uploadFile
		dryRun     bool
		wantStatus int
		// wantRows is the number of MissionDocuments rows and stored objects afterwards.
		wantRows int
		wantBody string
	}{
		{name: "the marshal attaches two files: the rows claim the keys and the objects stay", user: "marshal", missionID: missionConvoyID, files: []uploadFile{brief, chart}, wantStatus: http.StatusOK, wantRows: 2},
		{name: "the dispatcher attaches to a live mission", user: "dispatcher", missionID: missionConvoyID, files: []uploadFile{brief}, wantStatus: http.StatusOK, wantRows: 1},
		{name: "the dispatcher's grant condition refuses a closed mission, and nothing is stored", user: "dispatcher", missionID: "80000000-0000-4000-8000-000000000005", files: []uploadFile{brief}, wantStatus: http.StatusForbidden},
		{name: "a mission that does not exist fails the body, and nothing is stored", user: "marshal", missionID: "80000000-0000-4000-8000-0000000000ff", files: []uploadFile{brief}, wantStatus: http.StatusNotFound},
		{name: "over the declared maximum is a 413 naming it, and nothing is stored", user: "marshal", missionID: missionConvoyID, files: []uploadFile{{name: "big.bin", contentType: "application/octet-stream", content: bytes.Repeat([]byte("x"), 5<<20+1)}}, wantStatus: http.StatusRequestEntityTooLarge, wantBody: "exceeds the declared maximum of 5MB"},
		{name: "no file part is a 400", user: "marshal", missionID: missionConvoyID, wantStatus: http.StatusBadRequest, wantBody: "at least one file part"},
		{name: "a dry run writes nothing and answers 200", user: "marshal", missionID: missionConvoyID, files: []uploadFile{brief, chart}, dryRun: true, wantStatus: http.StatusOK},
		{name: "the cadet holds no Execute on the method", user: "cadet", missionID: missionConvoyID, files: []uploadFile{brief}, wantStatus: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h, documents, dir := documentWorld(t)

			body, contentType := multipartBody(t, request(tt.missionID), tt.files...)
			status, respBody := doUploadAs(t, h, tt.user, sectorPath(anvil, "attach-mission-document"), body, contentType, tt.dryRun)
			assertStatus(t, status, tt.wantStatus, respBody)
			if tt.wantBody != "" && !strings.Contains(string(respBody), tt.wantBody) {
				t.Errorf("body = %s, want it to contain %q", respBody, tt.wantBody)
			}

			stored := storedFiles(t, dir)
			if len(stored) != tt.wantRows {
				t.Errorf("stored files = %v, want %d", stored, tt.wantRows)
			}
			keys, err := documents.Keys()
			if err != nil {
				t.Fatalf("store.DirStore.Keys() error = %v", err)
			}
			slices.Sort(keys)
			if !slices.Equal(keys, stored) {
				t.Errorf("Keys() = %v, store directory holds %v", keys, stored)
			}

			iter := readRows(t, h, tt.user, sectorPath(anvil, "mission-documents"))
			if len(iter) != tt.wantRows {
				t.Errorf("rows = %d, want %d: %v", len(iter), tt.wantRows, iter)
			}
			for _, row := range iter {
				// The store key is off the wire: the file route delivers what it names.
				if _, onWire := row["storeKey"]; onWire {
					t.Errorf("row %v carries the store key", row)
				}
				if row["uploadedBy"] != string(tt.user) || row["missionId"] != tt.missionID {
					t.Errorf("row = %v, want uploadedBy %s on mission %s", row, tt.user, tt.missionID)
				}
			}
			if status == http.StatusOK && !tt.dryRun {
				var answer map[string][]string
				if err := json.Unmarshal(respBody, &answer); err != nil || len(answer["documentIDs"]) != tt.wantRows {
					t.Errorf("answer = %s (%v), want %d document ids", respBody, err, tt.wantRows)
				}
			}
			if tt.dryRun && len(respBody) != 0 {
				t.Errorf("dry run body = %s, want none", respBody)
			}
		})
	}
}

// readRows lists a resource as the user and decodes its rows; a user the listing
// refuses sees none.
func readRows(t *testing.T, h http.Handler, user accesstypes.User, target string) []map[string]any {
	t.Helper()

	status, body := doRequestAs(t, h, user, http.MethodGet, target, "")
	if status == http.StatusForbidden {
		return nil
	}
	assertStatus(t, status, http.StatusOK, body)

	return decodeRows(t, body)
}

// attachHaulerBrief attaches one brief to Halvard's stranded hauler, the portal
// client's own company's mission, and returns the document's id.
func attachHaulerBrief(t *testing.T, h http.Handler) string {
	t.Helper()

	body, contentType := multipartBody(t, fmt.Sprintf(`{"missionId":%q,"title":"Hauler brief"}`, missionHaulerID),
		uploadFile{name: "brief.txt", contentType: "text/plain", content: []byte(haulerBrief)})
	status, respBody := doUploadAs(t, h, "marshal", sectorPath(anvil, "attach-mission-document"), body, contentType, false)
	assertStatus(t, status, http.StatusOK, respBody)

	rows := readRows(t, h, "marshal", sectorPath(anvil, "mission-documents"))
	if len(rows) != 1 {
		t.Fatalf("rows = %v, want one", rows)
	}
	assertKeys(t, rows[0], []string{"id", "missionId", "title", "fileName", "contentType", "size", "uploadedBy", "uploadedAt", "provenance", "digest"})

	return cell[string](t, rows[0], "id")
}

const haulerBrief = "Halvard hauler: crew of four, main drive lost"

// fileRequestAs GETs a file route as the user with the given request headers (the
// validator, a range) and returns the recorded response.
func fileRequestAs(t *testing.T, h http.Handler, user accesstypes.User, target string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	sessionID, err := ccc.NewUUID()
	if err != nil {
		t.Fatalf("ccc.NewUUID: %v", err)
	}
	ctx := context.WithValue(t.Context(), sessioninfo.CtxSessionInfo, &sessioninfo.SessionData{
		SessionInfo: &sessioninfo.SessionInfo{ID: sessionID, Username: string(user)},
	})
	if portalUsers[user] {
		ctx = auth.Bind(ctx, members.Name)
	}
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, target, http.NoBody)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	return rr
}

// TestMissionDocument_portalListing pins the portal's view: the client lists her
// company's documents through her own outlet with the grant's fields, no store key and
// no uploader, and, holding no grant on content, is refused the download.
func TestMissionDocument_portalListing(t *testing.T) {
	t.Parallel()

	h, _, _ := documentWorld(t)
	id := attachHaulerBrief(t, h)

	portalStatus, portalBody := doRequestAs(t, h, "client", http.MethodGet, "/portal/api/sectors/anvil/mission-documents", "")
	assertStatus(t, portalStatus, http.StatusOK, portalBody)
	portalRows := decodeRows(t, portalBody)
	if len(portalRows) != 1 {
		t.Fatalf("portal rows = %v, want one", portalRows)
	}
	assertKeys(t, portalRows[0], []string{"id", "missionId", "title", "fileName", "contentType", "size", "uploadedAt", "provenance"})

	status, content := doRequestAs(t, h, "client", http.MethodGet, portalPath(anvil, "mission-documents/"+id+"/content"), "")
	assertStatus(t, status, http.StatusForbidden, content)
	if !strings.Contains(string(content), "MissionDocuments.content") {
		t.Errorf("refusal = %s, want it to name the content field", content)
	}
}

// TestMissionDocument_fileRoute pins the generated file route: Read on the documents and
// on content serve the bytes typed and named by the row with the key as the validator,
// the validator answers 304, a crew member without Read is refused, a member with Read
// but no content is refused naming the field, and another sector's document is
// indistinguishable from none.
func TestMissionDocument_fileRoute(t *testing.T) {
	t.Parallel()

	h, _, _ := documentWorld(t)
	id := attachHaulerBrief(t, h)
	target := sectorPath(anvil, "mission-documents/"+id+"/content")

	rec := doRequestRecordedAs(t, h, "marshal", http.MethodGet, target, "")
	assertStatus(t, rec.Code, http.StatusOK, rec.Body.Bytes())
	etag := rec.Header().Get("ETag")

	tests := []struct {
		name        string
		user        accesstypes.User
		target      string
		headers     map[string]string
		wantStatus  int
		wantHeaders map[string]string
		wantBody    string
		wantMessage string
	}{
		{
			name:       "the marshal downloads the brief, typed and named by the row, with its validator",
			user:       "marshal",
			target:     target,
			wantStatus: http.StatusOK,
			wantHeaders: map[string]string{
				"Content-Type":        "text/plain",
				"Content-Disposition": `inline; filename=brief.txt`,
				"Cache-Control":       "private, no-cache",
				"Content-Length":      fmt.Sprint(len(haulerBrief)),
			},
			wantBody: haulerBrief,
		},
		{
			name:        "the dispatcher's content grant opens it too",
			user:        "dispatcher",
			target:      target,
			wantStatus:  http.StatusOK,
			wantHeaders: map[string]string{"ETag": etag},
			wantBody:    haulerBrief,
		},
		{
			name:        "a kept copy asks again with the validator and hears 304",
			user:        "marshal",
			target:      target,
			headers:     map[string]string{"If-None-Match": etag},
			wantStatus:  http.StatusNotModified,
			wantHeaders: map[string]string{"ETag": etag, "Cache-Control": "private, no-cache"},
		},
		{
			name:        "the cadet holds no Read on the documents",
			user:        "cadet",
			target:      target,
			wantStatus:  http.StatusForbidden,
			wantMessage: "does not have (Read) on [MissionDocuments",
		},
		{
			name:       "the governor, a marshal in every sector, asks under bastion: another sector's document is indistinguishable from none",
			user:       "governor",
			target:     sectorPath(bastion, "mission-documents/"+id+"/content"),
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "a document that does not exist is 404",
			user:       "marshal",
			target:     sectorPath(anvil, "mission-documents/80000000-0000-4000-8000-0000000000ff/content"),
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rr := fileRequestAs(t, h, tt.user, tt.target, tt.headers)
			assertStatus(t, rr.Code, tt.wantStatus, rr.Body.Bytes())
			for name, want := range tt.wantHeaders {
				if got := rr.Header().Get(name); got != want {
					t.Errorf("%s = %q, want %q", name, got, want)
				}
			}
			if tt.wantStatus == http.StatusOK && rr.Header().Get("ETag") == "" {
				t.Error("ETag missing: a stored file's key is its validator")
			}
			if tt.wantBody != "" && rr.Body.String() != tt.wantBody {
				t.Errorf("body = %q, want %q", rr.Body.String(), tt.wantBody)
			}
			if tt.wantMessage != "" && !strings.Contains(rr.Body.String(), tt.wantMessage) {
				t.Errorf("body = %s, want the message %q", rr.Body.String(), tt.wantMessage)
			}
		})
	}
}

// TestDirStore_sweep pins the application's safety net: an object no row claims, older
// than the window, is deleted; a claimed one and a young one are left alone; and a key
// that is not a UUID never reaches the directory.
func TestDirStore_sweep(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	documents, err := store.NewDirStore(dir)
	if err != nil {
		t.Fatalf("store.NewDirStore() error = %v", err)
	}
	t.Cleanup(func() { _ = documents.Close() })

	ctx := t.Context()
	put := func(key string, old bool) {
		t.Helper()
		if err := documents.Put(ctx, key, "text/plain", strings.NewReader(key)); err != nil {
			t.Fatalf("Put(%s) error = %v", key, err)
		}
		if old {
			stale := time.Now().Add(-2 * time.Hour)
			if err := os.Chtimes(filepath.Join(dir, key), stale, stale); err != nil {
				t.Fatalf("os.Chtimes() error = %v", err)
			}
		}
	}
	const (
		claimed   = "0193e2a7-522c-708f-bfd0-4adf33486bb1"
		unclaimed = "0193e2a7-522c-708f-bfd0-4adf33486bb2"
		young     = "0193e2a7-522c-708f-bfd0-4adf33486bb3"
	)
	put(claimed, true)
	put(unclaimed, true)
	put(young, false)

	deleted, err := documents.Sweep(ctx, time.Hour, func(_ context.Context, key string) (bool, error) {
		return key == claimed, nil
	})
	if err != nil {
		t.Fatalf("Sweep() error = %v", err)
	}
	if deleted != 1 {
		t.Errorf("Sweep() deleted %d, want 1", deleted)
	}
	if got := storedFiles(t, dir); !slices.Equal(got, []string{claimed, young}) {
		t.Errorf("stored = %v, want the claimed and the young key", got)
	}
	if err := documents.Put(ctx, "../escape", "text/plain", strings.NewReader("x")); err == nil {
		t.Error("Put() with a non-UUID key succeeded, want a refusal")
	}
	if _, err := documents.Open(ctx, "0193e2a7-522c-708f-bfd0-4adf33486bb4"); !errors.Is(err, resource.ErrFileNotFound) {
		t.Errorf("Open() of an absent key error = %v, want resource.ErrFileNotFound", err)
	}
}
