// Package store holds Lodestar's document store: a directory the upload frame streams
// files into and the generated file routes read them back from.
package store

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	perrors "github.com/go-playground/errors/v5"
)

// DirStore is a resource.FileStore over one directory, confined by an os.Root so no key
// can name a path outside it. A key is a UUID the frame minted; Put writes <key>, Delete
// removes it, Open reads it back. It is Lodestar's stand-in for an object store: a
// bucket store implements the same three methods.
//
// Demonstrates: rpc.upload-store, @file.stored.
type DirStore struct {
	root *os.Root
}

// NewDirStore opens dir as the store, creating it.
func NewDirStore(dir string) (*DirStore, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, perrors.Wrap(err, "os.MkdirAll()")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, perrors.Wrap(err, "os.OpenRoot()")
	}

	return &DirStore{root: root}, nil
}

// Put streams one part to <key>, permanently: the transaction the frame runs next is what
// claims the key, and the frame Deletes it when nothing commits.
func (s *DirStore) Put(_ context.Context, key, _ string, r io.Reader) error {
	if err := checkKey(key); err != nil {
		return err
	}
	f, err := s.root.Create(key)
	if err != nil {
		return perrors.Wrap(err, "os.Root.Create()")
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		_ = s.root.Remove(key)

		return perrors.Wrap(err, "io.Copy()")
	}
	if err := f.Close(); err != nil {
		return perrors.Wrap(err, "os.File.Close()")
	}

	return nil
}

// Delete removes objects; a key already gone is not an error.
func (s *DirStore) Delete(_ context.Context, keys []string) error {
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

// Open reads an object back for a @file route: its size and modification time from the
// file, the body seekable so the frame serves ranges, and resource.ErrFileNotFound where
// nothing is stored under the key. The store keeps no media type; the frame takes the
// row's type column, then the name's extension.
func (s *DirStore) Open(_ context.Context, key string) (*resource.Content, error) {
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

	return &resource.Content{Size: info.Size(), ModTime: info.ModTime(), Body: f}, nil
}

// Keys lists the stored objects' keys.
func (s *DirStore) Keys() ([]string, error) {
	entries, err := fs.ReadDir(s.root.FS(), ".")
	if err != nil {
		return nil, perrors.Wrap(err, "fs.ReadDir()")
	}
	keys := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			keys = append(keys, entry.Name())
		}
	}

	return keys, nil
}

// Sweep is the application's safety net, never the mechanism: an object no row claims,
// older than the window, is deleted. The frame leaves such an object only when the
// process dies between streaming a file and the transaction's commit, or between a
// failed commit and its delete, so the window is the application's decision: long enough
// that no upload still in flight is older than it. Lodestar exposes the sweep as a
// method and leaves scheduling to the operator; a service would run it on a timer.
func (s *DirStore) Sweep(ctx context.Context, olderThan time.Duration, claimed func(ctx context.Context, key string) (bool, error)) (deleted int, err error) {
	entries, err := fs.ReadDir(s.root.FS(), ".")
	if err != nil {
		return 0, perrors.Wrap(err, "fs.ReadDir()")
	}
	cutoff := time.Now().Add(-olderThan)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return deleted, perrors.Wrap(err, "fs.DirEntry.Info()")
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		key := entry.Name()
		ok, err := claimed(ctx, key)
		if err != nil {
			return deleted, err
		}
		if ok {
			continue
		}
		if err := s.Delete(ctx, []string{key}); err != nil {
			return deleted, err
		}
		deleted++
	}

	return deleted, nil
}

// Close releases the directory.
func (s *DirStore) Close() error {
	if err := s.root.Close(); err != nil {
		return perrors.Wrap(err, "os.Root.Close()")
	}

	return nil
}

// checkKey admits only the UUIDs the frame mints: the root already confines every
// path, and the check keeps a stray name from ever reaching it.
func checkKey(key string) error {
	if _, err := ccc.UUIDFromString(key); err != nil {
		return perrors.Wrapf(err, "store key %q is not a UUID", key)
	}

	return nil
}
