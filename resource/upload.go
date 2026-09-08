package resource

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/cccteam/ccc"
	"github.com/cccteam/httpio"
	perrors "github.com/go-playground/errors/v5"
)

// Uploads (decided 2026-09-08) are their own endpoint form: an @rpc struct whose
// Execute takes resource.Files, declared with @upload(max: 5MB). The request
// travels as multipart/form-data — one part named request carrying the JSON the
// RPC decoder already understands, first, then one or more parts named file. The
// generated frame bounds the body before a byte is read, decodes and checks the
// request part exactly as a JSON RPC, streams each file to the application's
// UploadStore under a key it minted, and runs the body inside the transaction with
// the Files. The transaction claims the keys by recording them; after it commits
// the frame promotes them, and on any failure before commit it discards them. The
// store is the application's: resource never imports a cloud SDK, and reading a
// file back is the application's own route.

// The multipart part names an upload request carries.
const (
	// UploadRequestPart names the JSON part, which comes first.
	UploadRequestPart = "request"
	// UploadFilePart names each file part.
	UploadFilePart = "file"
)

// UploadStore is the application's object store as the frame drives it. Put
// writes one part under a key the frame minted, temporarily: the key is the
// permanent name the body records, and the store keeps the object pending until
// Promote. Promote makes pending keys permanent after the transaction committed;
// Discard removes pending keys whose transaction did not commit. A crash between
// commit and Promote leaves a pending object with a claiming row; the
// application's own sweep, outside the frame, promotes or deletes pending objects
// older than its window by checking its rows.
type UploadStore interface {
	Put(ctx context.Context, key string, contentType string, r io.Reader) error
	Promote(ctx context.Context, keys []string) error
	Discard(ctx context.Context, keys []string) error
}

// File describes one uploaded part as the body receives it.
type File struct {
	// Key is the store key the frame minted; the body records it wherever its
	// schema wants it, and the frame promotes it after commit. Empty on a dry
	// run, which streams nothing.
	Key string
	// Name is the part's filename as the client sent it.
	Name string
	// ContentType is the part's declared media type, application/octet-stream
	// when the client declared none.
	ContentType string
	// Size is the part's length in bytes.
	Size int64
}

// Files is the uploaded parts, in the order they were sent.
type Files []File

// Keys returns the keys the frame minted, skipping the empty keys of a dry run.
func (f Files) Keys() []string {
	keys := make([]string, 0, len(f))
	for _, file := range f {
		if file.Key != "" {
			keys = append(keys, file.Key)
		}
	}

	return keys
}

// Upload is one multipart upload request the frame opened: the request part,
// ready for the method's decoder, with the file parts still on the wire behind it.
type Upload struct {
	request  *http.Request
	reader   *multipart.Reader
	maxBytes int64
}

// OpenUpload bounds the request body to maxBytes, opens the multipart form, and
// positions it on the request part, which must come first. A body over the limit
// answers 413 naming the maximum, a body that is not multipart/form-data 415, and
// a form whose first part is not the request part 400.
func OpenUpload(r *http.Request, maxBytes int64) (*Upload, error) {
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		return nil, httpio.NewUnsupportedMediaTypeMessagef("an upload is sent as multipart/form-data with a %s part first and %s parts after it", UploadRequestPart, UploadFilePart)
	}
	boundary := params["boundary"]
	if boundary == "" {
		return nil, httpio.NewBadRequestMessage("multipart/form-data without a boundary")
	}

	upload := &Upload{
		request:  r,
		reader:   multipart.NewReader(http.MaxBytesReader(nil, r.Body, maxBytes), boundary),
		maxBytes: maxBytes,
	}
	part, err := upload.reader.NextPart()
	if err != nil {
		return nil, upload.readError(err, "reading the first part")
	}
	if part.FormName() != UploadRequestPart {
		return nil, httpio.NewBadRequestMessagef("the first part of an upload is %s, the method's JSON; got %q", UploadRequestPart, part.FormName())
	}

	request := r.Clone(r.Context())
	request.Body = part
	request.ContentLength = -1
	request.Header = r.Header.Clone()
	request.Header.Set("Content-Type", "application/json")
	upload.request = request

	return upload, nil
}

// Request returns the request the method's decoder reads: the upload request
// with the request part as its body, so field validation, the entry check, and
// the target's checks run exactly as for a JSON RPC.
func (u *Upload) Request() *http.Request {
	return u.request
}

// Stream reads the file parts behind the request part and streams each to the
// store under a fresh key, returning the Files the body receives. A dry run
// streams nothing: the parts are measured and described with empty keys. A form
// with no file part, or with a part under another name, is a 400; a body over the
// declared maximum is a 413.
func (u *Upload) Stream(ctx context.Context, store UploadStore, dryRun bool) (Files, error) {
	var files Files
	for {
		part, err := u.reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, u.readError(err, "reading a file part")
		}
		if part.FormName() != UploadFilePart {
			return nil, httpio.NewBadRequestMessagef("an upload carries %s parts after the request; got a part named %q", UploadFilePart, part.FormName())
		}

		file, err := u.streamPart(ctx, store, part, dryRun)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	if len(files) == 0 {
		return nil, httpio.NewBadRequestMessagef("an upload carries at least one %s part", UploadFilePart)
	}

	return files, nil
}

// streamPart stores one part, or measures it on a dry run.
func (u *Upload) streamPart(ctx context.Context, store UploadStore, part *multipart.Part, dryRun bool) (File, error) {
	contentType := part.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	file := File{Name: part.FileName(), ContentType: contentType}

	counter := &countingReader{r: part}
	if dryRun {
		if _, err := io.Copy(io.Discard, counter); err != nil {
			return File{}, u.readError(err, "measuring a file part")
		}
		file.Size = counter.n

		return file, nil
	}

	key, err := ccc.NewUUID()
	if err != nil {
		return File{}, perrors.Wrap(err, "ccc.NewUUID()")
	}
	file.Key = key.String()
	if err := store.Put(ctx, file.Key, contentType, counter); err != nil {
		if readErr := counter.err; readErr != nil {
			return File{}, u.readError(readErr, "streaming a file part")
		}

		return File{}, perrors.Wrap(err, "resource.UploadStore.Put()")
	}
	file.Size = counter.n

	return file, nil
}

// readError classifies a body read failure: the size limit is the 413 the
// declaration promised, anything else a malformed form.
func (u *Upload) readError(err error, during string) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return httpio.NewRequestEntityTooLargeMessagef("the upload exceeds the declared maximum of %s", FormatByteSize(u.maxBytes))
	}

	return httpio.NewBadRequestMessageWithError(err, "malformed multipart upload while "+during)
}

// DiscardUpload removes the streamed objects of an upload whose transaction did
// not commit and returns cause, the failure that ended it, so the frame answers
// with the original refusal. A discard failure is noted on the cause.
func DiscardUpload(ctx context.Context, store UploadStore, files Files, cause error) error {
	keys := files.Keys()
	if len(keys) == 0 {
		return cause
	}
	if err := store.Discard(ctx, keys); err != nil {
		return perrors.Wrapf(cause, "resource.UploadStore.Discard() failed too: %v", err)
	}

	return cause
}

// FormatByteSize renders a byte count the way @upload declares it: whole
// gigabytes, megabytes, or kilobytes (1024-based) when the count divides evenly,
// bytes otherwise.
func FormatByteSize(n int64) string {
	const (
		kb = int64(1) << 10
		mb = int64(1) << 20
		gb = int64(1) << 30
	)
	switch {
	case n >= gb && n%gb == 0:
		return fmt.Sprintf("%dGB", n/gb)
	case n >= mb && n%mb == 0:
		return fmt.Sprintf("%dMB", n/mb)
	case n >= kb && n%kb == 0:
		return fmt.Sprintf("%dKB", n/kb)
	default:
		return fmt.Sprintf("%d bytes", n)
	}
}

// ParseByteSize reads a size the way @upload declares it: a count of bytes, or a
// count suffixed KB, MB, or GB (1024-based).
func ParseByteSize(text string) (int64, error) {
	original := text
	text = strings.TrimSpace(text)
	multiplier := int64(1)
	for _, unit := range []struct {
		suffix string
		factor int64
	}{{"GB", 1 << 30}, {"MB", 1 << 20}, {"KB", 1 << 10}, {"B", 1}} {
		if strings.HasSuffix(strings.ToUpper(text), unit.suffix) {
			multiplier = unit.factor
			text = strings.TrimSpace(text[:len(text)-len(unit.suffix)])

			break
		}
	}
	count, err := strconv.ParseInt(text, 10, 64)
	if err != nil || count <= 0 {
		return 0, perrors.Newf("%q is not a size; write a positive count of bytes, or one suffixed KB, MB, or GB", original)
	}

	return count * multiplier, nil
}

// countingReader counts the bytes a store reads from a part and keeps the read
// error, so a size-limit failure surfaces as the 413 rather than the store's
// wrapping of it.
type countingReader struct {
	r   io.Reader
	n   int64
	err error
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	if err != nil && !errors.Is(err, io.EOF) {
		c.err = err
	}

	return n, err
}
