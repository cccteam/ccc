// Demonstrates: @upload, rpc.upload-store, hand-written-route, execute-condition.
package integration

// This suite covers the upload form against the demo world: AttachMissionDocument
// declares @upload(max: 5MB), so its request is multipart — the JSON part first, the
// files after — and its body claims the streamed files with one MissionDocuments row
// each. A committed transaction leaves the rows with their keys and the files in the
// store; a refusal or a failure before commit leaves neither; a dry run writes
// nothing and answers 200; the portal lists documents without the store key; the
// download route serves the bytes to a crew member with an unconditional Read.

import (
	"bytes"
	"context"
	"encoding/json"
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

// documentWorld is a fresh demo world whose App writes documents into a directory
// the test owns, served with the hand-written download route beside the generated
// ones.
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

	a := newAppWithDocuments(db, demoAccessClient(t), documents)
	r := router.NewTestRouter(a)
	r.Get(router.MissionDocumentContentRoute, a.MissionDocumentContent())

	return r, documents, dir
}

// storedFiles lists the promoted objects in the store directory.
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
		// wantRows is the number of MissionDocuments rows and promoted files
		// afterwards; the pending directory is always empty afterwards.
		wantRows int
		wantBody string
	}{
		{name: "the marshal attaches two files: rows claim the keys and the files are promoted", user: "marshal", missionID: missionConvoyID, files: []uploadFile{brief, chart}, wantStatus: http.StatusOK, wantRows: 2},
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

			pending, err := documents.Pending()
			if err != nil {
				t.Fatalf("store.DirStore.Pending() error = %v", err)
			}
			if len(pending) != 0 {
				t.Errorf("pending = %v, want none left behind", pending)
			}
			stored := storedFiles(t, dir)
			if len(stored) != tt.wantRows {
				t.Errorf("stored files = %v, want %d", stored, tt.wantRows)
			}

			var keys []string
			iter := readRows(t, h, tt.user, sectorPath(anvil, "mission-documents"))
			for _, row := range iter {
				key, _ := row["storeKey"].(string)
				keys = append(keys, key)
				if row["uploadedBy"] != string(tt.user) || row["missionId"] != tt.missionID {
					t.Errorf("row = %v, want uploadedBy %s on mission %s", row, tt.user, tt.missionID)
				}
			}
			slices.Sort(keys)
			if tt.wantRows > 0 && !slices.Equal(keys, stored) {
				t.Errorf("rows claim %v, store holds %v", keys, stored)
			}
			if tt.wantRows == 0 && len(iter) != 0 {
				t.Errorf("rows = %v, want none", iter)
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

func TestMissionDocument_portalAndDownload(t *testing.T) {
	t.Parallel()

	h, _, _ := documentWorld(t)

	// The marshal attaches a brief to Halvard's stranded hauler — the portal client's
	// own company's mission.
	const haulerID = "80000000-0000-4000-8000-000000000001"
	body, contentType := multipartBody(t, fmt.Sprintf(`{"missionId":%q,"title":"Hauler brief"}`, haulerID),
		uploadFile{name: "brief.txt", contentType: "text/plain", content: []byte("Halvard hauler: crew of four, main drive lost")})
	status, respBody := doUploadAs(t, h, "marshal", sectorPath(anvil, "attach-mission-document"), body, contentType, false)
	assertStatus(t, status, http.StatusOK, respBody)

	rows := readRows(t, h, "marshal", sectorPath(anvil, "mission-documents"))
	if len(rows) != 1 {
		t.Fatalf("rows = %v, want one", rows)
	}
	id, _ := rows[0]["id"].(string)
	assertKeys(t, rows[0], []string{"id", "missionId", "title", "fileName", "contentType", "size", "storeKey", "uploadedBy", "uploadedAt"})

	// The portal lists the client's documents through its own outlet, with the
	// grant's fields: no store key, no uploader.
	portalStatus, portalBody := doRequestAs(t, h, "client", http.MethodGet, "/portal/api/sectors/anvil/mission-documents", "")
	assertStatus(t, portalStatus, http.StatusOK, portalBody)
	portalRows := decodeRows(t, portalBody)
	if len(portalRows) != 1 {
		t.Fatalf("portal rows = %v, want one", portalRows)
	}
	assertKeys(t, portalRows[0], []string{"id", "missionId", "title", "fileName", "contentType", "size", "uploadedAt"})

	// The download route serves the bytes to the crew, and refuses the client, whose
	// Read is conditional and holds no storeKey.
	target := sectorPath(anvil, "mission-documents/"+id+"/content")
	status, content := doRequestAs(t, h, "marshal", http.MethodGet, target, "")
	assertStatus(t, status, http.StatusOK, content)
	if string(content) != "Halvard hauler: crew of four, main drive lost" {
		t.Errorf("content = %q, want the uploaded bytes", content)
	}
	status, content = doRequestAs(t, h, "client", http.MethodGet, target, "")
	assertStatus(t, status, http.StatusForbidden, content)
	// The governor, a marshal in every sector, asks for the anvil document under
	// bastion: another sector's document is indistinguishable from none.
	status, content = doRequestAs(t, h, "governor", http.MethodGet, sectorPath(bastion, "mission-documents/"+id+"/content"), "")
	assertStatus(t, status, http.StatusNotFound, content)
}

// TestDirStore_sweep pins the application's answer to a crash between commit and
// promotion: a pending object older than the window is promoted when a row claims
// it and deleted when none does; a young one is left alone.
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
			if err := os.Chtimes(filepath.Join(dir, "pending", key), stale, stale); err != nil {
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

	promoted, deleted, err := documents.Sweep(ctx, time.Hour, func(_ context.Context, key string) (bool, error) {
		return key == claimed, nil
	})
	if err != nil {
		t.Fatalf("Sweep() error = %v", err)
	}
	if promoted != 1 || deleted != 1 {
		t.Errorf("Sweep() = %d promoted, %d deleted; want 1 and 1", promoted, deleted)
	}
	if got := storedFiles(t, dir); !slices.Equal(got, []string{claimed}) {
		t.Errorf("promoted = %v, want the claimed key", got)
	}
	pending, err := documents.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(pending, []string{young}) {
		t.Errorf("pending = %v, want the young key alone", pending)
	}
	if err := documents.Put(ctx, "../escape", "text/plain", strings.NewReader("x")); err == nil {
		t.Error("Put() with a non-UUID key succeeded, want a refusal")
	}
}
