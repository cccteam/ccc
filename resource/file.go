package resource

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
)

// A field or a computed struct declares that a file belongs to the row (@file), and the
// generator serves it: one GET route under the resource's read route, gated by Read on
// the resource and a grant on the route's own field, the segment (content), answering
// the bytes with their type, name, size, time, and validator, and refusing as every
// generated route does. The frame reads the row through the resource's own read path,
// with the caller's Read conditions and tenancy, so an absent, cross-tenant, or hidden
// row is 404 exactly as on the read route; the columns that deliver the file (the store
// key, the name, the type) are read for the frame itself, without field grants. A
// stored file is opened through the application's FileStore; a rendered file is a
// computed resource's content function. Bytes go through the application: the store
// is never exposed.

// octetStream is the media type sent when nothing names one.
const octetStream = "application/octet-stream"

// ErrFileNotFound is what a FileStore's Open returns for a key with no object behind
// it. The frame answers 404 with the row's identity and never the key.
var ErrFileNotFound = errors.New("file not found")

// FileStore is the application's object store as the frame drives it. Put writes one
// object under a key the frame minted, permanently: an @upload method's frame Puts each
// part before the body runs, and the transaction claims the keys by recording them; on
// any failure before commit the frame Deletes the keys it streamed and answers with the
// failure, so an object with no claiming row is left only by a crash between the two,
// and that one is the application's sweep's (an object no row claims, older than the
// application's window). Open reads one object back for a @file route, ErrFileNotFound
// when nothing is stored under the key. The store is the application's: resource never
// imports a cloud SDK.
type FileStore interface {
	Put(ctx context.Context, key string, contentType string, r io.Reader) error
	Delete(ctx context.Context, keys []string) error
	Open(ctx context.Context, key string) (*Content, error)
}

// Content is a file as the frame serves it: a stored object as the FileStore opened it,
// or the document a computed resource's content function rendered. Every field but the
// body is optional; the frame fills what it can from the row and the name.
type Content struct {
	// Name is the file's name, sent as the inline Content-Disposition's filename and,
	// with no ContentType, the source of the type by extension.
	Name string
	// ContentType is the media type sent; empty defers to the name's extension, then
	// to application/octet-stream.
	ContentType string
	// Size is the body's length in bytes, -1 when unknown.
	Size int64
	// ModTime is the file's last modification, the zero time when unknown.
	ModTime time.Time
	// Tag is the validator a rendered document sends as its ETag, quoted by the frame
	// when it is not; empty sends none, and the outlet's caching headers stand. A stored
	// file's validator is its key, set by the frame.
	Tag string
	// Body is the bytes. A body that also seeks is served with range support; any
	// other is copied. The frame closes it.
	Body io.ReadCloser
}

// StoredFile is what a @file row says about its stored file: the key column's value
// (empty for NULL), and the name and type columns where the declaration names them.
type StoredFile struct {
	Key         string
	Name        string
	ContentType string
}

// FileDecoder gates one @file route of a resource. It checks Read on the resource and
// on the route's own field, the segment, exactly as the read route checks its fields,
// and returns the QuerySet that locates the row through the resource's read path with
// the caller's Read conditions and tenancy, projecting only what the frame needs to
// deliver the file: the key, the store key, the name, and the type, every one read for
// the frame itself without a field grant. Request is that projection, a struct of the
// resource's fields the generated handler declares with perm:"-" throughout.
type FileDecoder[Resource Resourcer, Request any] struct {
	resourceSet *Set[Resource]
	fields      []accesstypes.Field
	jsonNames   map[accesstypes.Field]string
	segment     accesstypes.Tag
	collection  *GeneratedCollection
	// computed marks a computed resource, whose row the application's own function
	// locates: the gate is checked here, eagerly, and the QuerySet carries the scope
	// and identity to the function, as the computed decoder's does.
	computed bool
}

// NewFileDecoder builds the decoder for a table or view resource's @file route: the
// Set over the frame's projection, and the collection that renders a conditional Read
// grant into the row's statement.
func NewFileDecoder[Resource Resourcer, Request any](collection *GeneratedCollection, segment string) (*FileDecoder[Resource, Request], error) {
	decoder, err := newFileDecoder[Resource, Request](segment)
	if err != nil {
		return nil, err
	}
	decoder.collection = collection

	return decoder, nil
}

// MustNewFileDecoder is NewFileDecoder for the generated handlers, which construct
// their decoders at application startup: a construction error is a programming error
// (a request struct out of sync with its resource) and panics.
func MustNewFileDecoder[Resource Resourcer, Request any](collection *GeneratedCollection, segment string) *FileDecoder[Resource, Request] {
	decoder, err := NewFileDecoder[Resource, Request](collection, segment)
	if err != nil {
		panic(err)
	}

	return decoder
}

// NewComputedFileDecoder builds the decoder for a computed resource's @file route. A
// computed resource executes application code, so the gate is checked at decode, and a
// Conditional decision there is the invariant breach the computed decoder names:
// MigrateRoles refuses a row-referencing condition on a computed resource.
func NewComputedFileDecoder[Resource Resourcer, Request any](segment string) (*FileDecoder[Resource, Request], error) {
	decoder, err := newFileDecoder[Resource, Request](segment)
	if err != nil {
		return nil, err
	}
	decoder.computed = true

	return decoder, nil
}

// MustNewComputedFileDecoder is NewComputedFileDecoder for the generated handlers; a
// construction error panics at application startup.
func MustNewComputedFileDecoder[Resource Resourcer, Request any](segment string) *FileDecoder[Resource, Request] {
	decoder, err := NewComputedFileDecoder[Resource, Request](segment)
	if err != nil {
		panic(err)
	}

	return decoder
}

func newFileDecoder[Resource Resourcer, Request any](segment string) (*FileDecoder[Resource, Request], error) {
	if segment == "" {
		return nil, errors.New("resource.NewFileDecoder: the route needs a segment")
	}
	rSet, err := NewSet[Resource, Request](accesstypes.Read)
	if err != nil {
		return nil, errors.Wrap(err, "resource.NewSet()")
	}

	reqType := reflect.TypeFor[Request]()
	fields := make([]accesstypes.Field, 0, reqType.NumField())
	jsonNames := make(map[accesstypes.Field]string, reqType.NumField())
	for field := range reqType.Fields() {
		if field.Tag.Get(permTagKey) != permTagExempt {
			return nil, errors.Newf("resource.NewFileDecoder: field %s of the frame's projection must carry perm:%q; the frame reads its columns for itself, without field grants", field.Name, permTagExempt)
		}
		fields = append(fields, accesstypes.Field(field.Name))
		if name, _, _ := strings.Cut(field.Tag.Get(jsonTagKey), ","); name != "" && name != "-" {
			jsonNames[accesstypes.Field(field.Name)] = name
		}
	}

	return &FileDecoder[Resource, Request]{
		resourceSet: rSet,
		fields:      fields,
		jsonNames:   jsonNames,
		segment:     accesstypes.Tag(segment),
	}, nil
}

// Segment is the route's own field: the tag a Read grant names to open the route.
func (d *FileDecoder[Resource, Request]) Segment() accesstypes.Tag {
	return d.segment
}

// Decode checks the gate and returns the QuerySet that reads the row. Read on the
// resource and on the segment field are checked in one call, as the read route checks
// its base and its fields: a Denied decision on either is Forbidden in the read route's
// words, a Conditional one on the segment is the row condition the statement renders,
// so a row the condition does not admit is 404 as on the read route. The request's
// query string is not read: the route takes no parameters.
func (d *FileDecoder[Resource, Request]) Decode(request *http.Request, userPermissions UserPermissions, scope accesstypes.Scope) (*QuerySet[Resource], error) {
	ctx := request.Context()
	base := d.resourceSet.BaseResource()
	gate := base.ResourceWithTag(d.segment)

	qSet := NewQuerySet(d.resourceSet.ResourceMetadata())
	qSet.env = newRequestEnvironment()
	qSet.requestableFields = d.fields
	qSet.jsonNames = d.jsonNames
	qSet.collection = d.collection
	for _, field := range d.fields {
		qSet.AddField(field)
	}

	decisions, err := userPermissions.Check(ctx, qSet.env, scope, accesstypes.Read, base, gate)
	if err != nil {
		return nil, errors.Wrap(err, "resource.UserPermissions.Check()")
	}
	if denied := decisions.DeniedResources(); len(denied) > 0 {
		return nil, httpio.NewForbiddenMessagef("scope (%s), user (%s) does not have (%s) on %s", scope, userPermissions.User(), accesstypes.Read, denied)
	}

	if d.computed {
		if conditional := decisions.ConditionalResources(); len(conditional) > 0 {
			return nil, errConditionalAtDecode(accesstypes.Read, conditional)
		}
		qSet.scope = scope
		qSet.requiredPermission = accesstypes.Read
		qSet.userPermissions = userPermissions

		return qSet, nil
	}

	// The read path: enforcement rides the set, the tenancy predicate the scope, and
	// the segment's condition the row predicate (readConditionPlan).
	qSet.EnableUserPermissionEnforcement(d.resourceSet, userPermissions, scope, accesstypes.Read)
	qSet.fileGate = gate
	qSet.carryConditionalDecisions(decisions)

	return qSet, nil
}

// ServeStoredFile answers a @file route from the application's store: 304 when the
// request's validator matches the key, before the store is opened, and the bytes
// otherwise, typed by the row's type column, then the object's, then the name's
// extension. A row carrying no key, or a key the store holds nothing under, is a 404
// returned for the handler to encode, in the row's words (label and key, "MissionDocument
// 0193…") and never the store key's; so is any other failure.
func ServeStoredFile(ctx context.Context, w http.ResponseWriter, r *http.Request, store FileStore, file StoredFile, segment, label string, key ...any) error {
	identity := rowIdentity(label, key)
	if file.Key == "" {
		return httpio.NewNotFoundMessagef("%s has no %s", identity, segment)
	}

	tag := strconv.Quote(file.Key)
	if writeNotModified(w, r, tag) {
		return nil
	}

	content, err := store.Open(ctx, file.Key)
	if err != nil {
		if errors.Is(err, ErrFileNotFound) {
			return httpio.NewNotFoundMessagef("the %s of %s was not found in the store", segment, identity)
		}

		return errors.Wrap(err, "resource.FileStore.Open()")
	}
	if file.Name != "" {
		content.Name = file.Name
	}
	if file.ContentType != "" {
		content.ContentType = file.ContentType
	}
	content.Tag = tag

	return serveContent(w, r, content)
}

// ServeRenderedFile answers a computed resource's @file route with what its content
// function rendered: 304 when the request's validator matches the content's Tag, the
// bytes otherwise, and, for a nil content, a 404 in the row's words returned for the
// handler to encode. The function never touches the response; the frame does.
func ServeRenderedFile(w http.ResponseWriter, r *http.Request, content *Content, segment, label string, key ...any) error {
	if content == nil {
		return httpio.NewNotFoundMessagef("%s has no %s", rowIdentity(label, key), segment)
	}
	if content.Tag != "" {
		content.Tag = quoteTag(content.Tag)
		if writeNotModified(w, r, content.Tag) {
			if content.Body != nil {
				_ = content.Body.Close()
			}

			return nil
		}
	}

	return serveContent(w, r, content)
}

// serveContent writes the content with its headers and closes the body. A seekable
// body goes through http.ServeContent, which brings range requests; any other body is
// copied whole.
func serveContent(w http.ResponseWriter, r *http.Request, content *Content) error {
	if content.Body == nil {
		return errors.New("resource.Content: a served file carries a body")
	}
	defer content.Body.Close()

	w.Header().Set("Content-Type", contentTypeOf(content))
	if content.Name != "" {
		w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": path.Base(content.Name)}))
	}
	if content.Tag != "" {
		w.Header().Set("ETag", content.Tag)
		// The browser may keep the file and must ask again: the outlet's no-store
		// header would forbid keeping it.
		w.Header().Set("Cache-Control", "private, no-cache")
	}

	if seeker, ok := content.Body.(io.ReadSeeker); ok {
		http.ServeContent(w, r, content.Name, content.ModTime, seeker)

		return nil
	}

	if !content.ModTime.IsZero() {
		w.Header().Set("Last-Modified", content.ModTime.UTC().Format(http.TimeFormat))
	}
	if content.Size >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(content.Size, 10))
	}
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, content.Body); err != nil {
		return errors.Wrap(err, "io.Copy()")
	}

	return nil
}

// contentTypeOf resolves the media type sent: the content's, then the name's
// extension, then application/octet-stream.
func contentTypeOf(content *Content) string {
	if content.ContentType != "" {
		return content.ContentType
	}
	if ext := path.Ext(content.Name); ext != "" {
		if byExt := mime.TypeByExtension(ext); byExt != "" {
			return byExt
		}
	}

	return octetStream
}

// writeNotModified answers 304 with the validator when the request's If-None-Match
// names it (or anything, with *), and reports whether it did. The comparison is weak:
// a W/ prefix on either side is ignored, which is what a GET's validator allows.
func writeNotModified(w http.ResponseWriter, r *http.Request, tag string) bool {
	header := r.Header.Get("If-None-Match")
	if header == "" {
		return false
	}
	want := strings.TrimPrefix(tag, "W/")
	matched := false
	for candidate := range strings.SplitSeq(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || strings.TrimPrefix(candidate, "W/") == want {
			matched = true

			break
		}
	}
	if !matched {
		return false
	}

	w.Header().Set("ETag", tag)
	w.Header().Set("Cache-Control", "private, no-cache")
	w.WriteHeader(http.StatusNotModified)

	return true
}

// quoteTag puts a validator in the quoted form the ETag header carries, leaving one
// already quoted, or weak, as it is.
func quoteTag(tag string) string {
	if strings.HasPrefix(tag, `"`) || strings.HasPrefix(tag, "W/") {
		return tag
	}

	return strconv.Quote(tag)
}

// rowIdentity renders the row a refusal names: the label and the key's parts.
func rowIdentity(label string, key []any) string {
	parts := make([]string, 0, len(key))
	for _, part := range key {
		parts = append(parts, fmt.Sprint(part))
	}

	return strings.TrimSpace(label + " " + strings.Join(parts, "/"))
}
