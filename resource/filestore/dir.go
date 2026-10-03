package filestore

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"iter"
	"os"
	"path"

	"github.com/cccteam/ccc/resource"
	perrors "github.com/go-playground/errors/v5"
)

// Dir is a file store over one directory, confined by an os.Root so no key can name a
// path outside it: the development store, file://<dir>. Put writes <key> once, create
// only, under any directories the key's segments name; Delete removes it; Open reads it
// back with its size and modification time, the body seekable so the frame serves
// ranges. The store keeps no media type: the frame takes the row's type column, then
// the name's extension.
type Dir struct {
	dir  string
	root *os.Root
}

// OpenDir opens dir as a store, creating the directory.
func OpenDir(dir string) (*Dir, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, perrors.Wrap(err, "os.MkdirAll()")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, perrors.Wrap(err, "os.OpenRoot()")
	}

	return &Dir{dir: dir, root: root}, nil
}

// Location is the URL the store was opened at: file://<dir>.
func (s *Dir) Location() string {
	return SchemeDir + "://" + s.dir
}

// Put writes one object under key, create only, and leaves nothing behind when the
// write fails.
func (s *Dir) Put(_ context.Context, key, _ string, r io.Reader) error {
	if err := checkKey(key); err != nil {
		return err
	}
	if parent := path.Dir(key); parent != "." {
		if err := s.root.MkdirAll(parent, 0o750); err != nil {
			return perrors.Wrap(err, "os.Root.MkdirAll()")
		}
	}
	f, err := s.root.OpenFile(key, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return perrors.Wrap(err, "os.Root.OpenFile()")
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		_ = s.root.Remove(key)

		return perrors.Wrap(err, "io.Copy()")
	}
	if err := f.Close(); err != nil {
		_ = s.root.Remove(key)

		return perrors.Wrap(err, "os.File.Close()")
	}

	return nil
}

// Delete removes objects; a key already gone is not an error.
func (s *Dir) Delete(_ context.Context, keys []string) error {
	for _, key := range keys {
		if err := checkKey(key); err != nil {
			return err
		}
		if err := s.root.Remove(key); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return perrors.Wrap(err, "os.Root.Remove()")
		}
	}

	return nil
}

// Open reads an object back for a @file route, resource.ErrFileNotFound where nothing
// is stored under the key.
func (s *Dir) Open(_ context.Context, key string) (*resource.Content, error) {
	if err := checkKey(key); err != nil {
		return nil, err
	}
	f, err := s.root.Open(key)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, resource.ErrFileNotFound
		}

		return nil, perrors.Wrap(err, "os.Root.Open()")
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()

		return nil, perrors.Wrap(err, "os.File.Stat()")
	}
	if info.IsDir() {
		_ = f.Close()

		return nil, resource.ErrFileNotFound
	}

	return &resource.Content{Size: info.Size(), ModTime: info.ModTime(), Body: f}, nil
}

// Objects walks the directory: every file, by its path from the root, with its size and
// modification time as its creation time.
func (s *Dir) Objects(ctx context.Context) iter.Seq2[Object, error] {
	return func(yield func(Object, error) bool) {
		err := fs.WalkDir(s.root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return perrors.Wrap(err, "context.Context.Err()")
			}
			if d.IsDir() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return perrors.Wrap(err, "fs.DirEntry.Info()")
			}
			if !yield(Object{Key: p, Size: info.Size(), Created: info.ModTime()}, nil) {
				return fs.SkipAll
			}

			return nil
		})
		if err != nil {
			yield(Object{}, perrors.Wrap(err, "fs.WalkDir()"))
		}
	}
}

// Check stats the directory.
func (s *Dir) Check(context.Context) error {
	if _, err := s.root.Stat("."); err != nil {
		return perrors.Wrap(err, "os.Root.Stat()")
	}

	return nil
}

// Close releases the directory.
func (s *Dir) Close() error {
	if err := s.root.Close(); err != nil {
		return perrors.Wrap(err, "os.Root.Close()")
	}

	return nil
}
