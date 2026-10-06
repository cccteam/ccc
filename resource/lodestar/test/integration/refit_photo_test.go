// Demonstrates: @upload, rpc.upload-store, filestore.named, @file.stored.
package integration

// This suite covers the default store beside the Documents store: AttachRefitPhoto is an
// @upload with no store, so its file streams to the default store and its key is a plain
// string in RefitTask.PhotoKey, where the mission documents stream to the Documents
// store under typed keys. One application, two stores: a photo never lands in the
// documents' store, a document never in the photos', and each row's file route opens
// the store its column names.

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// photoTarget is the task the photographer photographs: the Samaritan refit's second
// task, seeded with no photo.
var photoTarget = fmt.Sprintf(`{"refitId":%q,"taskNumber":2}`, refitSamaritanID)

// photoRoute is the task's generated file route under its compound read route.
var photoRoute = sectorPath(anvil, "refit-tasks/"+refitSamaritanID+"/2/photo")

func TestAttachRefitPhoto(t *testing.T) {
	t.Parallel()

	hull := uploadFile{name: "hull.png", contentType: "image/png", content: []byte("PNG hull seals")}
	tests := []struct {
		name string
		// act runs the case; the world holds no photo before it.
		act func(t *testing.T, h http.Handler) (statusCode int, respBody []byte)
		// wantFiles is the number of objects in the default store afterwards.
		wantStatus int
		wantBody   string
		wantFiles  int
	}{
		{
			name: "the photographer attaches a photo, which lands in the default store alone",
			act: func(t *testing.T, h http.Handler) (int, []byte) {
				body, contentType := multipartBody(t, photoTarget, hull)

				return doUploadAs(t, h, "photographer", sectorPath(anvil, "attach-refit-photo"), body, contentType, false)
			},
			wantStatus: http.StatusOK,
			wantFiles:  1,
		},
		{
			name: "a dry run streams nothing",
			act: func(t *testing.T, h http.Handler) (int, []byte) {
				body, contentType := multipartBody(t, photoTarget, hull)

				return doUploadAs(t, h, "photographer", sectorPath(anvil, "attach-refit-photo"), body, contentType, true)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "the engineer holds no Execute on the photo method, and nothing is stored",
			act: func(t *testing.T, h http.Handler) (int, []byte) {
				body, contentType := multipartBody(t, photoTarget, hull)

				return doUploadAs(t, h, "engineer", sectorPath(anvil, "attach-refit-photo"), body, contentType, false)
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name: "a photo is one file",
			act: func(t *testing.T, h http.Handler) (int, []byte) {
				body, contentType := multipartBody(t, photoTarget, hull, hull)

				return doUploadAs(t, h, "photographer", sectorPath(anvil, "attach-refit-photo"), body, contentType, false)
			},
			wantStatus: http.StatusBadRequest,
			wantBody:   "a photo is one file; got 2",
		},
		{
			name: "a task the refit does not hold is 404, and nothing is stored",
			act: func(t *testing.T, h http.Handler) (int, []byte) {
				body, contentType := multipartBody(t, fmt.Sprintf(`{"refitId":%q,"taskNumber":9}`, refitSamaritanID), hull)

				return doUploadAs(t, h, "photographer", sectorPath(anvil, "attach-refit-photo"), body, contentType, false)
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h, stores := documentWorld(t)
			status, respBody := tt.act(t, h)
			assertStatus(t, status, tt.wantStatus, respBody)
			if tt.wantBody != "" && !strings.Contains(string(respBody), tt.wantBody) {
				t.Errorf("body = %s, want it to contain %q", respBody, tt.wantBody)
			}
			if files := stores.files.Keys(); len(files) != tt.wantFiles {
				t.Errorf("default store = %v, want %d objects", files, tt.wantFiles)
			}
			if documents := stores.documents.Keys(); len(documents) != 0 {
				t.Errorf("the Documents store holds %v; a photo never lands there", documents)
			}
		})
	}
}

// TestRefitTask_photoRoute pins the task's file route over the default store, and the
// second photo releasing the first: the row points at the new object and the old one
// leaves the default store with the commit.
func TestRefitTask_photoRoute(t *testing.T) {
	t.Parallel()

	h, stores := documentWorld(t)
	hull := uploadFile{name: "hull.png", contentType: "image/png", content: []byte("PNG hull seals")}
	body, contentType := multipartBody(t, photoTarget, hull)
	status, respBody := doUploadAs(t, h, "photographer", sectorPath(anvil, "attach-refit-photo"), body, contentType, false)
	assertStatus(t, status, http.StatusOK, respBody)
	first := stores.files.Keys()
	if len(first) != 1 {
		t.Fatalf("default store = %v, want the photo's one object", first)
	}

	rec := fileRequestAs(t, h, "photographer", photoRoute, nil)
	assertStatus(t, rec.Code, http.StatusOK, rec.Body.Bytes())
	if got := rec.Body.String(); got != string(hull.content) {
		t.Errorf("download = %q, want the photo's bytes", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", got)
	}
	if got := rec.Header().Get("ETag"); got != `"`+first[0]+`"` {
		t.Errorf("ETag = %q, want the key %q", got, first[0])
	}
	// The cadet holds no Read on the tasks: the route refuses, and the key is never
	// in the refusal.
	rec = fileRequestAs(t, h, "cadet", photoRoute, nil)
	assertStatus(t, rec.Code, http.StatusForbidden, rec.Body.Bytes())
	if strings.Contains(rec.Body.String(), first[0]) {
		t.Errorf("the refusal carries the key: %s", rec.Body.Bytes())
	}

	// A second photo: the row points at it, and the first leaves the default store.
	again := uploadFile{name: "hull-2.png", contentType: "image/png", content: []byte("PNG hull seals, sealed")}
	body, contentType = multipartBody(t, photoTarget, again)
	status, respBody = doUploadAs(t, h, "photographer", sectorPath(anvil, "attach-refit-photo"), body, contentType, false)
	assertStatus(t, status, http.StatusOK, respBody)
	second := stores.files.Keys()
	if len(second) != 1 || second[0] == first[0] {
		t.Fatalf("default store = %v after the second photo, want one new object in place of %v", second, first)
	}
	rec = fileRequestAs(t, h, "photographer", photoRoute, nil)
	assertStatus(t, rec.Code, http.StatusOK, rec.Body.Bytes())
	if got := rec.Body.String(); got != string(again.content) {
		t.Errorf("download = %q, want the second photo's bytes", got)
	}
}
