package resource

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
	"github.com/google/go-cmp/cmp"
)

// memoryStore is a FileStore that records what the frame asked of it.
type memoryStore struct {
	objects map[string][]byte
	types   map[string]string
	deleted []string
	opened  []string
	// putErr refuses a write; putErrAfter is how many writes land before it does.
	putErr      error
	putErrAfter int
	puts        int
	deleteErr   error
	openErr     error
}

func newMemoryStore() *memoryStore {
	return &memoryStore{objects: map[string][]byte{}, types: map[string]string{}}
}

func (m *memoryStore) Put(_ context.Context, key, contentType string, r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return errors.Wrap(err, "io.ReadAll()")
	}
	m.puts++
	if m.putErr != nil && m.puts > m.putErrAfter {
		return m.putErr
	}
	m.objects[key] = data
	m.types[key] = contentType

	return nil
}

func (m *memoryStore) Delete(_ context.Context, keys []string) error {
	m.deleted = append(m.deleted, keys...)
	for _, key := range keys {
		delete(m.objects, key)
		delete(m.types, key)
	}

	return m.deleteErr
}

// Open answers with a seekable body, as a file on disk would.
func (m *memoryStore) Open(_ context.Context, key string) (*Content, error) {
	m.opened = append(m.opened, key)
	if m.openErr != nil {
		return nil, m.openErr
	}
	data, ok := m.objects[key]
	if !ok {
		return nil, ErrFileNotFound
	}

	return &Content{ContentType: m.types[key], Size: int64(len(data)), Body: readSeekCloser{bytes.NewReader(data)}}, nil
}

// readSeekCloser gives a bytes.Reader the Close the Content body needs.
type readSeekCloser struct {
	*bytes.Reader
}

func (readSeekCloser) Close() error {
	return nil
}

// uploadPart is one part of a scripted multipart body.
type uploadPart struct {
	name, fileName, contentType, content string
}

// multipartRequest builds an upload request from parts, in order.
func multipartRequest(t *testing.T, parts ...uploadPart) *http.Request {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, part := range parts {
		header := make(map[string][]string)
		if part.fileName != "" {
			header["Content-Disposition"] = []string{`form-data; name="` + part.name + `"; filename="` + part.fileName + `"`}
		} else {
			header["Content-Disposition"] = []string{`form-data; name="` + part.name + `"`}
		}
		if part.contentType != "" {
			header["Content-Type"] = []string{part.contentType}
		}
		w, err := writer.CreatePart(header)
		if err != nil {
			t.Fatalf("multipart.Writer.CreatePart() error = %v", err)
		}
		if _, err := io.WriteString(w, part.content); err != nil {
			t.Fatalf("io.WriteString() error = %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("multipart.Writer.Close() error = %v", err)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/attach", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	return req
}

func TestUpload_intake(t *testing.T) {
	t.Parallel()

	request := uploadPart{name: UploadRequestPart, contentType: "application/json", content: `{"missionId":"m1","title":"Brief"}`}
	brief := uploadPart{name: UploadFilePart, fileName: "brief.pdf", contentType: "application/pdf", content: "%PDF-1.7 brief"}
	chart := uploadPart{name: UploadFilePart, fileName: "chart.png", content: "PNG"}

	tests := []struct {
		name        string
		parts       []uploadPart
		contentType string // overrides the multipart content type when set
		maxBytes    int64
		dryRun      bool
		wantOpenErr func(error) bool
		wantStream  func(error) bool
		wantRequest string
		wantFiles   []File
		wantStored  int
		wantErrText string
	}{
		{
			name:        "the request part is decoded and the files are streamed under minted keys",
			parts:       []uploadPart{request, brief, chart},
			maxBytes:    1 << 20,
			wantRequest: request.content,
			wantFiles:   []File{{Name: "brief.pdf", ContentType: "application/pdf", Size: 14}, {Name: "chart.png", ContentType: "application/octet-stream", Size: 3}},
			wantStored:  2,
		},
		{
			name:        "a dry run measures the parts and stores nothing",
			parts:       []uploadPart{request, brief},
			maxBytes:    1 << 20,
			dryRun:      true,
			wantRequest: request.content,
			wantFiles:   []File{{Name: "brief.pdf", ContentType: "application/pdf", Size: 14}},
		},
		{
			name:        "a body over the maximum is a 413 naming it",
			parts:       []uploadPart{request, brief, chart},
			maxBytes:    300,
			wantRequest: request.content,
			wantStream:  httpio.HasRequestEntityTooLarge,
			wantErrText: "exceeds the declared maximum of 300 bytes",
		},
		{
			name:        "no file part is a 400",
			parts:       []uploadPart{request},
			maxBytes:    1 << 20,
			wantRequest: request.content,
			wantStream:  httpio.HasBadRequest,
			wantErrText: "at least one file part",
		},
		{
			name:        "a part under another name is a 400",
			parts:       []uploadPart{request, {name: "attachment", fileName: "x", content: "x"}},
			maxBytes:    1 << 20,
			wantRequest: request.content,
			wantStream:  httpio.HasBadRequest,
			wantErrText: "got a part named",
		},
		{
			name:        "the request part must come first",
			parts:       []uploadPart{brief, request},
			maxBytes:    1 << 20,
			wantOpenErr: httpio.HasBadRequest,
			wantErrText: "the first part of an upload is request",
		},
		{
			name:        "a body that is not multipart is a 415",
			parts:       []uploadPart{request, brief},
			contentType: "application/json",
			maxBytes:    1 << 20,
			wantOpenErr: httpio.HasUnsupportedMediaType,
			wantErrText: "multipart/form-data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := multipartRequest(t, tt.parts...)
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}
			store := newMemoryStore()

			upload, err := OpenUpload(req, tt.maxBytes)
			if tt.wantOpenErr != nil {
				if err == nil || !tt.wantOpenErr(err) || !strings.Contains(err.Error(), tt.wantErrText) {
					t.Fatalf("OpenUpload() error = %v, want the classified refusal containing %q", err, tt.wantErrText)
				}

				return
			}
			if err != nil {
				t.Fatalf("OpenUpload() error = %v", err)
			}

			decoded, err := io.ReadAll(upload.Request().Body)
			if err != nil {
				t.Fatalf("reading the request part: %v", err)
			}
			if string(decoded) != tt.wantRequest {
				t.Errorf("request part = %q, want %q", decoded, tt.wantRequest)
			}
			if got := upload.Request().Header.Get("Content-Type"); got != "application/json" {
				t.Errorf("request part content type = %q, want application/json", got)
			}

			files, err := upload.Stream(t.Context(), store, tt.dryRun)
			if tt.wantStream != nil {
				if err == nil || !tt.wantStream(err) || !strings.Contains(err.Error(), tt.wantErrText) {
					t.Fatalf("Stream() error = %v, want the classified refusal containing %q", err, tt.wantErrText)
				}

				return
			}
			if err != nil {
				t.Fatalf("Stream() error = %v", err)
			}
			if len(files) != len(tt.wantFiles) {
				t.Fatalf("Stream() = %d files, want %d", len(files), len(tt.wantFiles))
			}
			for i, want := range tt.wantFiles {
				got := files[i]
				if got.Name != want.Name || got.ContentType != want.ContentType || got.Size != want.Size {
					t.Errorf("file %d = %+v, want name %q type %q size %d", i, got, want.Name, want.ContentType, want.Size)
				}
				if (got.Key == "") != tt.dryRun {
					t.Errorf("file %d key = %q, want minted = %v", i, got.Key, !tt.dryRun)
				}
				if !tt.dryRun {
					if string(store.objects[got.Key]) != tt.parts[i+1].content || store.types[got.Key] != want.ContentType {
						t.Errorf("stored %q = %q (%s), want the part's bytes and type", got.Key, store.objects[got.Key], store.types[got.Key])
					}
				}
			}
			if len(store.objects) != tt.wantStored {
				t.Errorf("stored %d objects, want %d", len(store.objects), tt.wantStored)
			}
			if got := files.Keys(); len(got) != tt.wantStored {
				t.Errorf("Keys() = %v, want %d keys", got, tt.wantStored)
			}
		})
	}
}

// docsStore is a named store as an application declares one, for the typed upload tests.
type docsStore struct{ Store }

func TestDiscardUpload(t *testing.T) {
	t.Parallel()

	cause := httpio.NewForbiddenMessage("no")
	unknown := errors.Wrap(&spanner.TransactionOutcomeUnknownError{}, "c.db.ReadWriteTransaction()")

	tests := []struct {
		name        string
		keys        []string
		cause       error
		noStore     bool
		deleteErr   error
		wantDeleted []string
		wantErrText string
	}{
		{name: "the minted keys are deleted and the cause is answered unchanged", keys: Files{{Key: "k1"}, {Key: ""}, {Key: "k2"}}.Keys(), cause: cause, wantDeleted: []string{"k1", "k2"}},
		{name: "a dry run's empty keys delete nothing", keys: Files{{Name: "dry"}}.Keys(), cause: cause},
		{name: "a typed upload's keys are deleted the same", keys: FilesIn[docsStore]{{Key: "d1"}, {Key: "d2"}}.Keys(), cause: cause, wantDeleted: []string{"d1", "d2"}},
		{name: "a commit whose outcome is unknown keeps the objects, since committed rows may hold them", keys: []string{"k1"}, cause: unknown},
		{name: "no store wired answers the cause and deletes nothing", keys: []string{"k1"}, cause: cause, noStore: true},
		{name: "a delete failure is noted on the cause and the cause's status stands", keys: []string{"k1"}, cause: cause, deleteErr: errors.New("store down"), wantDeleted: []string{"k1"}, wantErrText: "failed too, the objects are left to the orphaned-file cleanup"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := newMemoryStore()
			store.deleteErr = tt.deleteErr
			var target FileStore = store
			if tt.noStore {
				target = nil
			}
			// A canceled request: the discard runs detached from it.
			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			err := DiscardUpload(ctx, target, tt.keys, tt.cause)
			if !errors.Is(err, tt.cause) {
				t.Errorf("DiscardUpload() = %v, want it to carry the cause %v", err, tt.cause)
			}
			if httpio.HasForbidden(tt.cause) && !httpio.HasForbidden(err) {
				t.Errorf("DiscardUpload() = %v, want the cause's status kept", err)
			}
			if tt.wantErrText != "" && !strings.Contains(err.Error(), tt.wantErrText) {
				t.Errorf("DiscardUpload() = %v, want it to contain %q", err, tt.wantErrText)
			}
			if diff := cmp.Diff(tt.wantDeleted, store.deleted); diff != "" {
				t.Errorf("deleted mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestUpload_Stream_discards pins the two deletes the frame makes on its own: a later
// part failing deletes the parts stored before it, and a write the store refuses
// deletes its key, so a request that never reaches its transaction leaves no object.
func TestUpload_Stream_discards(t *testing.T) {
	t.Parallel()

	request := uploadPart{name: UploadRequestPart, contentType: "application/json", content: `{}`}
	brief := uploadPart{name: UploadFilePart, fileName: "brief.pdf", contentType: "application/pdf", content: "%PDF-1.7 brief"}
	chart := uploadPart{name: UploadFilePart, fileName: "chart.png", content: "PNG"}
	stray := uploadPart{name: "attachment", fileName: "x", content: "x"}

	tests := []struct {
		name     string
		parts    []uploadPart
		maxBytes int64
		putErr   error
		// wantPuts is how many keys the store was asked to write; every one is deleted
		// again, so wantDeleted is the same count and the store is left empty.
		wantPuts    int
		wantErr     func(error) bool
		wantErrText string
	}{
		{name: "a part under another name after a stored one deletes the stored one", parts: []uploadPart{request, brief, stray}, maxBytes: 1 << 20, wantPuts: 1, wantErr: httpio.HasBadRequest, wantErrText: "got a part named"},
		{name: "a body over the maximum after a stored part deletes the stored one", parts: []uploadPart{request, brief, chart, chart, chart}, maxBytes: 420, wantPuts: 1, wantErr: httpio.HasRequestEntityTooLarge, wantErrText: "exceeds the declared maximum"},
		{name: "a write the store refuses deletes its key", parts: []uploadPart{request, brief}, maxBytes: 1 << 20, putErr: errors.New("precondition failed"), wantPuts: 1, wantErr: func(err error) bool { return strings.Contains(err.Error(), "resource.FileStore.Put()") }, wantErrText: "precondition failed"},
		{name: "a refused second write deletes the first part and the refused key", parts: []uploadPart{request, brief, chart}, maxBytes: 1 << 20, putErr: errors.New("second write refused"), wantPuts: 1, wantErr: func(err error) bool { return strings.Contains(err.Error(), "resource.FileStore.Put()") }, wantErrText: "second write refused"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := newMemoryStore()
			if tt.putErr != nil {
				// The first write lands; the refusal comes on the last part.
				store.putErrAfter = len(tt.parts) - 2
				store.putErr = tt.putErr
			}
			upload, err := OpenUpload(multipartRequest(t, tt.parts...), tt.maxBytes)
			if err != nil {
				t.Fatalf("OpenUpload() error = %v", err)
			}
			files, err := upload.Stream(t.Context(), store, false)
			if err == nil || !tt.wantErr(err) || !strings.Contains(err.Error(), tt.wantErrText) {
				t.Fatalf("Stream() = %v, %v, want the refusal containing %q", files, err, tt.wantErrText)
			}
			if len(store.objects) != 0 {
				t.Errorf("the store keeps %d objects after the failure, want none: %v", len(store.objects), store.deleted)
			}
			if len(store.deleted) < tt.wantPuts || len(store.deleted) > tt.wantPuts+1 {
				t.Errorf("deleted = %v, want every stored key (%d) and at most the refused one", store.deleted, tt.wantPuts)
			}
		})
	}
}

// TestStreamInto pins the typed variant: the same parts, keyed by the store.
func TestStreamInto(t *testing.T) {
	t.Parallel()

	request := uploadPart{name: UploadRequestPart, contentType: "application/json", content: `{}`}
	brief := uploadPart{name: UploadFilePart, fileName: "brief.pdf", contentType: "application/pdf", content: "%PDF-1.7 brief"}

	tests := []struct {
		name    string
		dryRun  bool
		noStore bool
		wantKey bool
		wantErr string
	}{
		{name: "the parts stream and the keys are typed by the store", wantKey: true},
		{name: "a dry run types empty keys", dryRun: true},
		{name: "no store wired is refused naming the wiring", noStore: true, wantErr: "no file store is wired for the store this upload streams to"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var store FileStore = newMemoryStore()
			if tt.noStore {
				store = nil
			}
			upload, err := OpenUpload(multipartRequest(t, request, brief), 1<<20)
			if err != nil {
				t.Fatalf("OpenUpload() error = %v", err)
			}
			files, err := StreamInto[docsStore](t.Context(), upload, store, tt.dryRun)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("StreamInto() error = %v, want containing %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("StreamInto() error = %v", err)
			}
			if len(files) != 1 || files[0].Name != "brief.pdf" || files[0].ContentType != "application/pdf" || files[0].Size != 14 {
				t.Fatalf("StreamInto() = %+v, want the one typed part", files)
			}
			key := files[0].Key
			if (key != "") != tt.wantKey {
				t.Errorf("key = %q, want minted = %v", key, tt.wantKey)
			}
			if got := files.Keys(); (len(got) == 1) != tt.wantKey {
				t.Errorf("Keys() = %v, want one key = %v", got, tt.wantKey)
			}
		})
	}
}

func TestByteSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		text    string
		want    int64
		wantErr bool
		format  string
	}{
		{text: "5MB", want: 5 << 20, format: "5MB"},
		{text: "512KB", want: 512 << 10, format: "512KB"},
		{text: "1GB", want: 1 << 30, format: "1GB"},
		{text: "300", want: 300, format: "300 bytes"},
		{text: "300B", want: 300, format: "300 bytes"},
		{text: " 2 MB ", want: 2 << 20, format: "2MB"},
		{text: "0", wantErr: true},
		{text: "-1MB", wantErr: true},
		{text: "large", wantErr: true},
		{text: "5TB", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			t.Parallel()

			got, err := ParseByteSize(tt.text)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseByteSize(%q) = %d, want an error", tt.text, got)
				}

				return
			}
			if err != nil {
				t.Fatalf("ParseByteSize(%q) error = %v", tt.text, err)
			}
			if got != tt.want {
				t.Errorf("ParseByteSize(%q) = %d, want %d", tt.text, got, tt.want)
			}
			if formatted := FormatByteSize(got); formatted != tt.format {
				t.Errorf("FormatByteSize(%d) = %q, want %q", got, formatted, tt.format)
			}
		})
	}
}
