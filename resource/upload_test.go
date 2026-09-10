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

	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
)

// memoryStore is an UploadStore that records what the frame asked of it.
type memoryStore struct {
	objects   map[string][]byte
	types     map[string]string
	promoted  []string
	discarded []string
	putErr    error
}

func newMemoryStore() *memoryStore {
	return &memoryStore{objects: map[string][]byte{}, types: map[string]string{}}
}

func (m *memoryStore) Put(_ context.Context, key, contentType string, r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return errors.Wrap(err, "io.ReadAll()")
	}
	if m.putErr != nil {
		return m.putErr
	}
	m.objects[key] = data
	m.types[key] = contentType

	return nil
}

func (m *memoryStore) Promote(_ context.Context, keys []string) error {
	m.promoted = append(m.promoted, keys...)

	return nil
}

func (m *memoryStore) Discard(_ context.Context, keys []string) error {
	m.discarded = append(m.discarded, keys...)

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

func TestDiscardUpload(t *testing.T) {
	t.Parallel()

	cause := httpio.NewForbiddenMessage("no")
	files := Files{{Key: "k1"}, {Key: ""}, {Key: "k2"}}

	store := newMemoryStore()
	if err := DiscardUpload(t.Context(), store, files, cause); !errors.Is(err, cause) || !httpio.HasForbidden(err) {
		t.Errorf("DiscardUpload() = %v, want the cause unchanged", err)
	}
	if len(store.discarded) != 2 || store.discarded[0] != "k1" || store.discarded[1] != "k2" {
		t.Errorf("discarded = %v, want the two minted keys", store.discarded)
	}

	if err := DiscardUpload(t.Context(), newMemoryStore(), Files{{Name: "dry"}}, cause); !errors.Is(err, cause) {
		t.Errorf("DiscardUpload() with no keys = %v, want the cause", err)
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
