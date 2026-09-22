// Demonstrates: @file.released, @file.replaced.
package integration

// This suite covers the release of a stored object against the demo world: a mission
// document deleted through the patch handler, or pointed at a new file by
// ReplaceMissionDocument, releases the object it held, which the resource client deletes
// from the store once the transaction commits. A refusal, a dry run of the replacement,
// or an update that leaves the key alone releases nothing, and the object stays.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

// patchAs applies one consolidated patch as the user. The patch handler has no dry-run
// form (X-Dry-Run is a transaction-form method's), so the dry-run proof is the
// replacement's.
func patchAs(t *testing.T, h http.Handler, user accesstypes.User, body string) (statusCode int, respBody []byte) {
	t.Helper()

	return doRequestAs(t, h, user, http.MethodPatch, "/api/resources", body)
}

// removeOp is the consolidated delete of one mission document.
func removeOp(id string) string {
	return fmt.Sprintf(`[{"op":"remove","path":"/sectors/anvil/mission-documents/%s"}]`, id)
}

// retitleOp is the consolidated update of one mission document's title.
func retitleOp(id, title string) string {
	return fmt.Sprintf(`[{"op":"patch","path":"/sectors/anvil/mission-documents/%s","value":{"title":%q}}]`, id, title)
}

// TestMissionDocument_release pins the release: the object a row held leaves the store
// with the commit that deletes the row or points it at another object, and stays through
// everything that does not commit or does not touch the key.
func TestMissionDocument_release(t *testing.T) {
	t.Parallel()

	replacement := uploadFile{name: "brief-v2.txt", contentType: "text/plain", content: []byte(haulerBrief + "; amended: crew of five")}

	tests := []struct {
		name string
		// act runs the case against the world holding the attached brief.
		act        func(t *testing.T, h http.Handler, id string) (statusCode int, respBody []byte)
		wantStatus int
		wantBody   string
		// wantRows is the number of documents listed afterwards; wantObjects the number
		// of objects in the store; wantReplaced asserts the row and the object changed.
		wantRows     int
		wantObjects  int
		wantReplaced bool
	}{
		{
			name: "the registrar deletes a document: the row goes with the commit and the object with the row",
			act: func(t *testing.T, h http.Handler, id string) (int, []byte) {
				return patchAs(t, h, "registrar", removeOp(id))
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "the cadet holds no Delete on the documents, and the object stays",
			act: func(t *testing.T, h http.Handler, id string) (int, []byte) {
				return patchAs(t, h, "cadet", removeOp(id))
			},
			wantStatus:  http.StatusForbidden,
			wantRows:    1,
			wantObjects: 1,
		},
		{
			name: "a retitle leaves the key alone and the object in place",
			act: func(t *testing.T, h http.Handler, id string) (int, []byte) {
				return patchAs(t, h, "registrar", retitleOp(id, "Hauler brief, revised"))
			},
			wantStatus:  http.StatusOK,
			wantRows:    1,
			wantObjects: 1,
		},
		{
			name: "the registrar replaces the file: the row points at the new object and the old one is released",
			act: func(t *testing.T, h http.Handler, id string) (int, []byte) {
				body, contentType := multipartBody(t, fmt.Sprintf(`{"documentId":%q}`, id), replacement)

				return doUploadAs(t, h, "registrar", sectorPath(anvil, "replace-mission-document"), body, contentType, false)
			},
			wantStatus:   http.StatusOK,
			wantRows:     1,
			wantObjects:  1,
			wantReplaced: true,
		},
		{
			name: "a dry run of the replacement streams nothing and releases nothing",
			act: func(t *testing.T, h http.Handler, id string) (int, []byte) {
				body, contentType := multipartBody(t, fmt.Sprintf(`{"documentId":%q}`, id), replacement)

				return doUploadAs(t, h, "registrar", sectorPath(anvil, "replace-mission-document"), body, contentType, true)
			},
			wantStatus:  http.StatusOK,
			wantRows:    1,
			wantObjects: 1,
		},
		{
			name: "the marshal holds no Execute on the replacement, and nothing is stored or released",
			act: func(t *testing.T, h http.Handler, id string) (int, []byte) {
				body, contentType := multipartBody(t, fmt.Sprintf(`{"documentId":%q}`, id), replacement)

				return doUploadAs(t, h, "marshal", sectorPath(anvil, "replace-mission-document"), body, contentType, false)
			},
			wantStatus:  http.StatusForbidden,
			wantRows:    1,
			wantObjects: 1,
		},
		{
			name: "a replacement carries one file",
			act: func(t *testing.T, h http.Handler, id string) (int, []byte) {
				body, contentType := multipartBody(t, fmt.Sprintf(`{"documentId":%q}`, id), replacement, replacement)

				return doUploadAs(t, h, "registrar", sectorPath(anvil, "replace-mission-document"), body, contentType, false)
			},
			wantStatus:  http.StatusBadRequest,
			wantBody:    "a replacement is one file; got 2",
			wantRows:    1,
			wantObjects: 1,
		},
		{
			name: "a document that does not exist is 404, and nothing is stored",
			act: func(t *testing.T, h http.Handler, _ string) (int, []byte) {
				body, contentType := multipartBody(t, `{"documentId":"80000000-0000-4000-8000-0000000000ff"}`, replacement)

				return doUploadAs(t, h, "registrar", sectorPath(anvil, "replace-mission-document"), body, contentType, false)
			},
			wantStatus:  http.StatusNotFound,
			wantRows:    1,
			wantObjects: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h, _, dir := documentWorld(t)
			id := attachHaulerBrief(t, h)
			before := storedFiles(t, dir)
			if len(before) != 1 {
				t.Fatalf("stored files before = %v, want the brief's one object", before)
			}
			original := readRows(t, h, "marshal", sectorPath(anvil, "mission-documents"))[0]

			status, respBody := tt.act(t, h, id)
			assertStatus(t, status, tt.wantStatus, respBody)
			if tt.wantBody != "" && !strings.Contains(string(respBody), tt.wantBody) {
				t.Errorf("body = %s, want it to contain %q", respBody, tt.wantBody)
			}

			after := storedFiles(t, dir)
			if len(after) != tt.wantObjects {
				t.Errorf("stored files = %v, want %d", after, tt.wantObjects)
			}
			rows := readRows(t, h, "marshal", sectorPath(anvil, "mission-documents"))
			if len(rows) != tt.wantRows {
				t.Fatalf("rows = %v, want %d", rows, tt.wantRows)
			}
			if tt.wantRows == 0 {
				rec := fileRequestAs(t, h, "marshal", sectorPath(anvil, "mission-documents/"+id+"/content"), nil)
				assertStatus(t, rec.Code, http.StatusNotFound, rec.Body.Bytes())

				return
			}

			row := rows[0]
			rec := fileRequestAs(t, h, "marshal", sectorPath(anvil, "mission-documents/"+id+"/content"), nil)
			assertStatus(t, rec.Code, http.StatusOK, rec.Body.Bytes())
			if tt.wantReplaced {
				if slices.Equal(before, after) {
					t.Errorf("stored files = %v before and after: the old object was not released for the new one", after)
				}
				if row["fileName"] != replacement.name || row["size"] != json.Number(fmt.Sprint(len(replacement.content))) && fmt.Sprint(row["size"]) != fmt.Sprint(len(replacement.content)) {
					t.Errorf("row = %v, want fileName %s and size %d", row, replacement.name, len(replacement.content))
				}
				if row["digest"] == original["digest"] {
					t.Errorf("digest unchanged at %v, want the replacement's", row["digest"])
				}
				if got := rec.Body.String(); got != string(replacement.content) {
					t.Errorf("download = %q, want the replacement's bytes", got)
				}

				return
			}
			if !slices.Equal(before, after) {
				t.Errorf("stored files = %v, want the original %v", after, before)
			}
			if row["fileName"] != original["fileName"] || row["digest"] != original["digest"] {
				t.Errorf("row = %v, want the original file %v", row, original)
			}
			if got := rec.Body.String(); got != haulerBrief {
				t.Errorf("download = %q, want the original brief", got)
			}
		})
	}
}
