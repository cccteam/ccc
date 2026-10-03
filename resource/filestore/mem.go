package filestore

import (
	"bytes"
	"context"
	"io"
	"iter"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cccteam/ccc/resource"
	"github.com/go-playground/errors/v5"
)

// Mem is a file store in memory: mem://, the test double. Each opening is a store of
// its own, so two memory stores never share a location. It follows the same rules as
// the others: create only, a failed Put leaves nothing, a missing key deletes fine.
type Mem struct {
	mu      sync.Mutex
	objects map[string]*memObject
	now     func() time.Time
}

// memObject is one stored object.
type memObject struct {
	data        []byte
	contentType string
	created     time.Time
}

// NewMem opens a memory store.
func NewMem() *Mem {
	return &Mem{objects: make(map[string]*memObject), now: time.Now}
}

// Put stores one object under key, create only.
func (s *Mem) Put(_ context.Context, key, contentType string, r io.Reader) error {
	if err := checkKey(key); err != nil {
		return err
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return errors.Wrap(err, "io.ReadAll()")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.objects[key]; exists {
		return errors.Newf("the file key %q is already stored; a key is written once", key)
	}
	s.objects[key] = &memObject{data: data, contentType: contentType, created: s.now()}

	return nil
}

// Delete removes objects; a key already gone is not an error.
func (s *Mem) Delete(_ context.Context, keys []string) error {
	for _, key := range keys {
		if err := checkKey(key); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, key := range keys {
		delete(s.objects, key)
	}

	return nil
}

// Open reads an object back, resource.ErrFileNotFound where nothing is stored under
// the key. The body seeks, so the frame serves ranges.
func (s *Mem) Open(_ context.Context, key string) (*resource.Content, error) {
	if err := checkKey(key); err != nil {
		return nil, err
	}
	s.mu.Lock()
	obj, ok := s.objects[key]
	s.mu.Unlock()
	if !ok {
		return nil, resource.ErrFileNotFound
	}

	return &resource.Content{
		ContentType: obj.contentType,
		Size:        int64(len(obj.data)),
		ModTime:     obj.created,
		Body:        memBody{Reader: bytes.NewReader(obj.data)},
	}, nil
}

// memBody is a stored object's body: a bytes reader that seeks, so the frame serves
// ranges, with the Close the frame calls.
type memBody struct {
	*bytes.Reader
}

// Close releases nothing.
func (memBody) Close() error {
	return nil
}

// Objects lists the store's objects, by key.
func (s *Mem) Objects(context.Context) iter.Seq2[Object, error] {
	return func(yield func(Object, error) bool) {
		s.mu.Lock()
		objects := make([]Object, 0, len(s.objects))
		for key, obj := range s.objects {
			objects = append(objects, Object{Key: key, Size: int64(len(obj.data)), Created: obj.created})
		}
		s.mu.Unlock()
		slices.SortFunc(objects, func(a, b Object) int {
			return strings.Compare(a.Key, b.Key)
		})
		for _, obj := range objects {
			if !yield(obj, nil) {
				return
			}
		}
	}
}

// Keys lists the stored keys, sorted: what a test asserts over.
func (s *Mem) Keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.objects))
	for key := range s.objects {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	return keys
}

// Age sets an object's creation time, so a test can make an object older than a
// cleanup's window; it reports whether the key is stored.
func (s *Mem) Age(key string, created time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	obj, ok := s.objects[key]
	if ok {
		obj.created = created
	}

	return ok
}

// Check answers at once: memory is always there.
func (s *Mem) Check(context.Context) error {
	return nil
}

// Close releases nothing.
func (s *Mem) Close() error {
	return nil
}
