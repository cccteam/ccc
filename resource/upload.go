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

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc"
	"github.com/cccteam/httpio"
	"github.com/cccteam/logger"
	perrors "github.com/go-playground/errors/v5"
)

// Uploads (decided 2026-09-08, the store contract narrowed 2026-09-18) are their own
// endpoint form: an @rpc struct whose Execute takes resource.Files, or FilesIn[S] for
// a named store, declared with @upload(max: 5MB) or @upload(max: 5MB, store: S). The
// request travels as multipart/form-data: one part named request carrying the JSON the
// RPC decoder already understands, first, then one or more parts named file. The
// generated frame bounds the body before a byte is read, decodes and checks the request
// part exactly as a JSON RPC, streams each file to the store the declaration names,
// read off the resource client, under a key it minted, and runs the body inside the
// transaction with the Files. The body records each key in a @file column of that
// store, and the transaction is what claims them: on any failure before commit the
// frame deletes the objects it streamed and answers with the failure, and after a
// commit nothing more happens. A key recorded anywhere but a @file column is invisible
// to the release and is deleted by the orphaned-file cleanup once it is older than the
// window. The frame deletes what it streamed when a later part fails, and a key whose
// write failed; the deletes after a failed transaction run detached from the request's
// cancellation, and only when nothing committed: a commit whose outcome Spanner cannot
// report keeps its objects, since the rows may hold them, and logs the keys for the
// orphaned-file cleanup. An object no row claims is otherwise left only by a crash
// between the stream and the commit, and the orphaned-file cleanup (resource/filestore)
// removes such objects once they are older than the application's window. The root
// package imports no object-store SDK; the framework's stores are resource/filestore.
// Reading a file back is the @file route (file.go).

// The multipart part names an upload request carries.
const (
	// UploadRequestPart names the JSON part, which comes first.
	UploadRequestPart = "request"
	// UploadFilePart names each file part.
	UploadFilePart = "file"
)

// File describes one uploaded part as the body receives it, in the default store.
type File struct {
	// Key is the store key the frame minted; the body records it in a @file column of
	// the default store, and the transaction's commit is what claims it. Empty on a
	// dry run, which streams nothing.
	Key string
	// Name is the part's filename as the client sent it.
	Name string
	// ContentType is the part's declared media type, application/octet-stream
	// (octetStream) when the client declared none. It is recorded as declared, the
	// uploader's word: the @file route sends it as the Content-Type under nosniff and
	// decides from it whether the file displays inline or downloads (file.go).
	ContentType string
	// Size is the part's length in bytes.
	Size int64
}

// Files is the uploaded parts, in the order they were sent, in the default store.
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

// FileIn describes one uploaded part as the body receives it, in the named store S:
// File with its Key typed Key[S], so the body records it in a column of that store and
// nowhere else, since the generated setter of any other column refuses the type.
type FileIn[S NamedStore] struct {
	// Key is the store key the frame minted, typed by the store. Empty on a dry run.
	Key Key[S]
	// Name is the part's filename as the client sent it.
	Name string
	// ContentType is the part's declared media type, application/octet-stream when
	// the client declared none; the uploader's word, as File.ContentType is.
	ContentType string
	// Size is the part's length in bytes.
	Size int64
}

// FilesIn is the uploaded parts, in the order they were sent, in the named store S: what
// an @upload(store: S) method's Execute takes third.
type FilesIn[S NamedStore] []FileIn[S]

// Keys returns the keys the frame minted, skipping the empty keys of a dry run.
func (f FilesIn[S]) Keys() []string {
	keys := make([]string, 0, len(f))
	for _, file := range f {
		if file.Key != "" {
			keys = append(keys, string(file.Key))
		}
	}

	return keys
}

// StreamInto is Upload.Stream for an @upload(store: S) method: the parts stream to the
// named store's FileStore, read off the resource client under StoreNameFor[S], and the
// body receives them typed by the store.
func StreamInto[S NamedStore](ctx context.Context, u *Upload, store FileStore, dryRun bool) (FilesIn[S], error) {
	files, err := u.Stream(ctx, store, dryRun)
	if err != nil {
		return nil, err
	}
	typed := make(FilesIn[S], 0, len(files))
	for _, file := range files {
		typed = append(typed, FileIn[S]{Key: Key[S](file.Key), Name: file.Name, ContentType: file.ContentType, Size: file.Size})
	}

	return typed, nil
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
// declared maximum is a 413. A failure after a part was stored (a later part
// malformed, over the limit, or refused by the store) deletes the parts already
// stored before the failure is returned, so no object is left behind by a request
// that never reaches its transaction. A nil store, the store the declaration names
// not wired on the resource client, is an error naming the wiring.
func (u *Upload) Stream(ctx context.Context, store FileStore, dryRun bool) (Files, error) {
	if store == nil && !dryRun {
		return nil, perrors.New("no file store is wired for the store this upload streams to; wire it on the resource client with resource.WithFileStore or resource.WithNamedFileStore")
	}
	var files Files
	for {
		part, err := u.reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, u.discardStreamed(ctx, store, files, u.readError(err, "reading a file part"))
		}
		if part.FormName() != UploadFilePart {
			return nil, u.discardStreamed(ctx, store, files, httpio.NewBadRequestMessagef("an upload carries %s parts after the request; got a part named %q", UploadFilePart, part.FormName()))
		}

		file, err := u.streamPart(ctx, store, part, dryRun)
		if err != nil {
			return nil, u.discardStreamed(ctx, store, files, err)
		}
		files = append(files, file)
	}
	if len(files) == 0 {
		return nil, httpio.NewBadRequestMessagef("an upload carries at least one %s part", UploadFilePart)
	}

	return files, nil
}

// discardStreamed deletes the parts stored before a later one failed and returns the
// failure, with a delete failure noted on it.
func (u *Upload) discardStreamed(ctx context.Context, store FileStore, streamed Files, cause error) error {
	keys := streamed.Keys()
	if len(keys) == 0 {
		return cause
	}

	return deleteDetached(ctx, store, keys, cause)
}

// streamPart stores one part, or measures it on a dry run. A write the store refuses
// leaves no object: the key is deleted before the failure is returned, which covers a
// retried create-only write that answered precondition failed and any partial object
// a provider keeps.
func (u *Upload) streamPart(ctx context.Context, store FileStore, part *multipart.Part, dryRun bool) (File, error) {
	contentType := part.Header.Get("Content-Type")
	if contentType == "" {
		contentType = octetStream
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
			err = u.readError(readErr, "streaming a file part")
		} else {
			err = perrors.Wrap(err, "resource.FileStore.Put()")
		}

		return File{}, deleteDetached(ctx, store, []string{file.Key}, err)
	}
	file.Size = counter.n

	return file, nil
}

// deleteDetached deletes keys from the store after a failure, detached from the
// request's cancellation (the client may have gone; the objects must still go) under a
// timeout of its own, and returns cause with a delete failure noted on it.
func deleteDetached(ctx context.Context, store FileStore, keys []string, cause error) error {
	ctx, cancel := detachedContext(ctx)
	defer cancel()
	if err := store.Delete(ctx, keys); err != nil {
		return perrors.Wrapf(cause, "resource.FileStore.Delete(%v) failed too, the objects are left to the orphaned-file cleanup: %v", keys, err)
	}

	return cause
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

// DiscardUpload deletes the streamed objects (keys, the Files' or the FilesIn's Keys)
// of an upload whose transaction did not commit and returns cause, the failure that
// ended it, so the frame answers with the original refusal. The delete runs detached
// from the request's cancellation under its own timeout, and only when nothing
// committed: when the client reports the commit's outcome as unknown
// (*spanner.TransactionOutcomeUnknownError, or the Postgres client's, a deadline or a cancel
// after the commit was sent), the rows may hold the keys, so the objects are kept and the keys logged for
// the orphaned-file cleanup, which removes them if no row claims them. A delete
// failure is noted on the cause: the objects it left are the cleanup's too.
func DiscardUpload(ctx context.Context, store FileStore, keys []string, cause error) error {
	if len(keys) == 0 || store == nil {
		return cause
	}
	if commitOutcomeUnknown(cause) {
		logger.FromCtx(ctx).Errorf("resource: an upload's commit ended with its outcome unknown; its objects %v are kept, since committed rows may hold them, and are the orphaned-file cleanup's if none does: %v", keys, cause)

		return cause
	}

	return deleteDetached(ctx, store, keys, cause)
}

// commitOutcomeUnknown reports a failure after which the transaction may have
// committed: the Spanner client's outcome-unknown error or the Postgres client's, wrapped
// at any depth.
func commitOutcomeUnknown(err error) bool {
	var spannerUnknown *spanner.TransactionOutcomeUnknownError
	var postgresUnknown *commitOutcomeUnknownError

	return errors.As(err, &spannerUnknown) || errors.As(err, &postgresUnknown)
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
