package filestore

import (
	"context"
	"errors"
	"io"
	"iter"
	"mime"
	"net/http"
	"sync/atomic"
	"time"

	"cloud.google.com/go/storage"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/httpio"
	"github.com/cccteam/logger"
	perrors "github.com/go-playground/errors/v5"
	"github.com/googleapis/gax-go/v2"
	"golang.org/x/sync/errgroup"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/iterator"
)

// The bucket store's bounds.
const (
	// uploadChunkSize is the buffer each upload holds, 1 MiB in place of the client's
	// 16 MiB default: many uploads in flight at once fit a small instance.
	uploadChunkSize = 1 << 20
	// putTimeout bounds one object's write, retries included.
	putTimeout = 5 * time.Minute
	// callTimeout bounds one metadata call: attributes, a delete, a listing page, the
	// readiness probe.
	callTimeout = 30 * time.Second
	// maxAttempts bounds the retries of one call; the client's default retries until
	// the request's deadline.
	maxAttempts = 5
	// deletesInFlight bounds the deletes one Delete runs at once.
	deletesInFlight = 8
)

// Bucket is a file store over one Cloud Storage bucket: gs://<bucket>. Put is create
// only, so a key is written once and a retried write is safe; it records the part's
// media type, or application/octet-stream when the type does not parse, copies under a
// cancellable context and cancels before Close on any failure, so a failed write leaves
// no object. Open takes the type, size and modification time from the object's
// attributes, and its body seeks by reopening the object at the offset, so the frame
// serves ranges. A missing object is resource.ErrFileNotFound; a refused permission is
// an error of its own, answered 500 and never 404, since at the client's level a
// missing grant otherwise looks like a missing file. Delete takes several keys at once
// with a bounded number in flight, and an already-missing object counts as done.
//
// The readiness check lists one object, which roles/storage.objectUser allows. A bad
// URL or a missing bucket refuses to start. A refused permission at start does not: a
// new grant takes minutes to take effect, so refusing would fail a first deploy. The
// store starts, logs loudly, answers 503 on every file operation, and probes again on
// the next one, until the grant arrives.
type Bucket struct {
	name    string
	client  *storage.Client
	bucket  *storage.BucketHandle
	refused atomic.Bool
}

// openBucket opens the client over the named bucket. The client reads
// STORAGE_EMULATOR_HOST itself, which Open refuses on Cloud Run.
func openBucket(ctx context.Context, name string) (*Bucket, error) {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, perrors.Wrap(err, "storage.NewClient()")
	}
	client.SetRetry(
		storage.WithPolicy(storage.RetryAlways),
		storage.WithMaxAttempts(maxAttempts),
		storage.WithBackoff(gax.Backoff{Initial: 200 * time.Millisecond, Max: 5 * time.Second, Multiplier: 2}),
	)

	return &Bucket{name: name, client: client, bucket: client.Bucket(name)}, nil
}

// Location is the URL the store was opened at: gs://<bucket>.
func (s *Bucket) Location() string {
	return SchemeBucket + "://" + s.name
}

// Put writes one object under key, create only.
func (s *Bucket) Put(ctx context.Context, key, contentType string, r io.Reader) (err error) {
	if err := checkKey(key); err != nil {
		return err
	}
	if err := s.ready(ctx); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, putTimeout)
	defer cancel()

	w := s.bucket.Object(key).If(storage.Conditions{DoesNotExist: true}).NewWriter(ctx)
	w.ChunkSize = uploadChunkSize
	w.ContentType = mediaType(contentType)
	if _, err := io.Copy(w, r); err != nil {
		// Canceling before Close is what keeps a truncated object from being saved.
		cancel()
		_ = w.Close()

		return perrors.Wrap(err, "io.Copy()")
	}
	if err := w.Close(); err != nil {
		return perrors.Wrap(s.refusal(err), "storage.Writer.Close()")
	}

	return nil
}

// Delete removes objects, a bounded number at once; a key already gone is not an
// error, a refused permission is.
func (s *Bucket) Delete(ctx context.Context, keys []string) error {
	for _, key := range keys {
		if err := checkKey(key); err != nil {
			return err
		}
	}
	if err := s.ready(ctx); err != nil {
		return err
	}
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(deletesInFlight)
	for _, key := range keys {
		g.Go(func() error {
			ctx, cancel := context.WithTimeout(ctx, callTimeout)
			defer cancel()
			if err := s.bucket.Object(key).Delete(ctx); err != nil && !errors.Is(err, storage.ErrObjectNotExist) {
				return perrors.Wrapf(s.refusal(err), "storage.ObjectHandle.Delete(%s)", key)
			}

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return perrors.Wrap(err, "errgroup.Group.Wait()")
	}

	return nil
}

// Open reads an object back for a @file route: its attributes first, then a body that
// opens the object at the offset the frame reads from.
func (s *Bucket) Open(ctx context.Context, key string) (*resource.Content, error) {
	if err := checkKey(key); err != nil {
		return nil, err
	}
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	obj := s.bucket.Object(key)
	attrsCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	attrs, err := obj.Attrs(attrsCtx)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) {
			return nil, resource.ErrFileNotFound
		}

		return nil, perrors.Wrap(s.refusal(err), "storage.ObjectHandle.Attrs()")
	}

	return &resource.Content{
		ContentType: attrs.ContentType,
		Size:        attrs.Size,
		ModTime:     attrs.Updated,
		Body:        &bucketBody{ctx: ctx, obj: obj, size: attrs.Size},
	}, nil
}

// Objects lists the bucket, page by page.
func (s *Bucket) Objects(ctx context.Context) iter.Seq2[Object, error] {
	return func(yield func(Object, error) bool) {
		if err := s.ready(ctx); err != nil {
			yield(Object{}, err)

			return
		}
		it := s.bucket.Objects(ctx, &storage.Query{Projection: storage.ProjectionNoACL})
		for {
			attrs, err := it.Next()
			if errors.Is(err, iterator.Done) {
				return
			}
			if err != nil {
				yield(Object{}, perrors.Wrap(s.refusal(err), "storage.ObjectIterator.Next()"))

				return
			}
			if !yield(Object{Key: attrs.Name, Size: attrs.Size, Created: attrs.Created}, nil) {
				return
			}
		}
	}
}

// Check probes the bucket by listing one object. A missing bucket or an unreachable
// service is an error; a refused permission is logged, puts the store in its refused
// state, and is not an error, so the process starts and serves everything but files.
func (s *Bucket) Check(ctx context.Context) error {
	err := s.probe(ctx)
	if err == nil {
		return nil
	}
	if !isRefused(err) {
		return err
	}
	s.refused.Store(true)
	logger.FromCtx(ctx).Errorf("filestore: the permission on %s is refused; the process starts, every file operation answers 503, and the store is probed again on the next one until the grant takes effect: %v", s.Location(), err)

	return nil
}

// probe lists one object under the call timeout.
func (s *Bucket) probe(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	it := s.bucket.Objects(ctx, &storage.Query{Projection: storage.ProjectionNoACL})
	it.PageInfo().MaxSize = 1
	if _, err := it.Next(); err != nil && !errors.Is(err, iterator.Done) {
		return perrors.Wrapf(err, "listing one object of %s", s.Location())
	}

	return nil
}

// ready answers nil while the store is usable. In the refused state it probes again
// first; while the permission stays refused every operation answers 503, so a file
// route says the store is unavailable rather than failing.
func (s *Bucket) ready(ctx context.Context) error {
	if !s.refused.Load() {
		return nil
	}
	if err := s.probe(ctx); err != nil {
		if !isRefused(err) {
			return err
		}

		return httpio.NewServiceUnavailableMessageWithError(err, "the file store is unavailable while its permission is refused; it is probed again on the next file request")
	}
	s.refused.Store(false)
	logger.FromCtx(ctx).Infof("filestore: the permission on %s is granted; file operations resume", s.Location())

	return nil
}

// refusal marks the refused state when an operation's error is a refused permission,
// so the next operation probes before it runs, and returns the error unchanged.
func (s *Bucket) refusal(err error) error {
	if isRefused(err) {
		s.refused.Store(true)
	}

	return err
}

// Close releases the client.
func (s *Bucket) Close() error {
	if err := s.client.Close(); err != nil {
		return perrors.Wrap(err, "storage.Client.Close()")
	}

	return nil
}

// isRefused reports a refused permission: the service answered 403.
func isRefused(err error) bool {
	var apiErr *googleapi.Error

	return errors.As(err, &apiErr) && apiErr.Code == http.StatusForbidden
}

// mediaType is the type recorded on an object: the part's declared type when it
// parses, application/octet-stream otherwise.
func mediaType(contentType string) string {
	if _, _, err := mime.ParseMediaType(contentType); err != nil {
		return "application/octet-stream"
	}

	return contentType
}

// bucketBody is a stored object's body: it reads from the current offset, opening the
// object there on the first read after a seek, so a range request costs one read of the
// range and nothing before it.
type bucketBody struct {
	ctx    context.Context
	obj    *storage.ObjectHandle
	size   int64
	offset int64
	reader *storage.Reader
}

// Read reads from the current offset, opening the object there when no reader is open.
func (b *bucketBody) Read(p []byte) (int, error) {
	if b.reader == nil {
		if b.offset >= b.size {
			return 0, io.EOF
		}
		reader, err := b.obj.NewRangeReader(b.ctx, b.offset, -1)
		if err != nil {
			return 0, perrors.Wrap(err, "storage.ObjectHandle.NewRangeReader()")
		}
		b.reader = reader
	}
	n, err := b.reader.Read(p)
	b.offset += int64(n)
	switch {
	case err == nil:
		return n, nil
	case errors.Is(err, io.EOF):
		return n, io.EOF
	default:
		return n, perrors.Wrap(err, "storage.Reader.Read()")
	}
}

// Seek moves the offset; a reader open elsewhere is closed and the object reopened on
// the next read.
func (b *bucketBody) Seek(offset int64, whence int) (int64, error) {
	var target int64
	switch whence {
	case io.SeekStart:
		target = offset
	case io.SeekCurrent:
		target = b.offset + offset
	case io.SeekEnd:
		target = b.size + offset
	default:
		return 0, perrors.Newf("invalid whence %d", whence)
	}
	if target < 0 {
		return 0, perrors.New("negative seek offset")
	}
	if target != b.offset && b.reader != nil {
		_ = b.reader.Close()
		b.reader = nil
	}
	b.offset = target

	return target, nil
}

// Close closes the open reader, if any.
func (b *bucketBody) Close() error {
	if b.reader == nil {
		return nil
	}
	err := b.reader.Close()
	b.reader = nil
	if err != nil {
		return perrors.Wrap(err, "storage.Reader.Close()")
	}

	return nil
}
