// Package store holds Lodestar's document store: a directory the upload frame
// streams files into, with pending objects kept apart until their transaction
// commits.
package store

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/cccteam/ccc"
	perrors "github.com/go-playground/errors/v5"
)

// pendingDir holds objects whose transaction has not committed. A key is a UUID the
// frame minted; Put writes pending/<key>, Promote renames it to <key>, Discard
// removes it. The key a row records is the permanent name.
const pendingDir = "pending"

// DirStore is a resource.UploadStore over one directory, confined by an os.Root so
// no key can name a path outside it. It is Lodestar's stand-in for an object
// store; the application reads a document back through Open.
//
// Demonstrates: rpc.upload-store.
type DirStore struct {
	root *os.Root
}

// NewDirStore opens dir as the store, creating it and its pending directory.
func NewDirStore(dir string) (*DirStore, error) {
	if err := os.MkdirAll(filepath.Join(dir, pendingDir), 0o750); err != nil {
		return nil, perrors.Wrap(err, "os.MkdirAll()")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, perrors.Wrap(err, "os.OpenRoot()")
	}

	return &DirStore{root: root}, nil
}

// Put streams one part to pending/<key>.
func (s *DirStore) Put(_ context.Context, key, _ string, r io.Reader) error {
	if err := checkKey(key); err != nil {
		return err
	}
	f, err := s.root.Create(pendingPath(key))
	if err != nil {
		return perrors.Wrap(err, "os.Root.Create()")
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		_ = s.root.Remove(pendingPath(key))

		return perrors.Wrap(err, "io.Copy()")
	}
	if err := f.Close(); err != nil {
		return perrors.Wrap(err, "os.File.Close()")
	}

	return nil
}

// Promote makes pending keys permanent: pending/<key> becomes <key>.
func (s *DirStore) Promote(_ context.Context, keys []string) error {
	for _, key := range keys {
		if err := checkKey(key); err != nil {
			return err
		}
		if err := s.root.Rename(pendingPath(key), key); err != nil {
			return perrors.Wrap(err, "os.Root.Rename()")
		}
	}

	return nil
}

// Discard removes pending keys whose transaction did not commit; a key already
// gone is not an error.
func (s *DirStore) Discard(_ context.Context, keys []string) error {
	for _, key := range keys {
		if err := checkKey(key); err != nil {
			return err
		}
		if err := s.root.Remove(pendingPath(key)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return perrors.Wrap(err, "os.Root.Remove()")
		}
	}

	return nil
}

// Open opens a promoted object for reading.
func (s *DirStore) Open(key string) (*os.File, error) {
	if err := checkKey(key); err != nil {
		return nil, err
	}
	f, err := s.root.Open(key)
	if err != nil {
		return nil, perrors.Wrap(err, "os.Root.Open()")
	}

	return f, nil
}

// Pending lists the keys still pending.
func (s *DirStore) Pending() ([]string, error) {
	entries, err := fs.ReadDir(s.root.FS(), pendingDir)
	if err != nil {
		return nil, perrors.Wrap(err, "fs.ReadDir()")
	}
	keys := make([]string, 0, len(entries))
	for _, entry := range entries {
		keys = append(keys, entry.Name())
	}

	return keys, nil
}

// Sweep is the application's answer to a crash between commit and promotion: a
// pending object older than the window is promoted when a row claims its key and
// deleted when none does. The window is the application's decision — long enough
// that no upload still in flight is older than it. Lodestar exposes the sweep as a
// method and leaves scheduling to the operator; a service would run it on a timer.
func (s *DirStore) Sweep(ctx context.Context, olderThan time.Duration, claimed func(ctx context.Context, key string) (bool, error)) (promoted, deleted int, err error) {
	entries, err := fs.ReadDir(s.root.FS(), pendingDir)
	if err != nil {
		return 0, 0, perrors.Wrap(err, "fs.ReadDir()")
	}
	cutoff := time.Now().Add(-olderThan)
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return promoted, deleted, perrors.Wrap(err, "fs.DirEntry.Info()")
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		key := entry.Name()
		ok, err := claimed(ctx, key)
		if err != nil {
			return promoted, deleted, err
		}
		if ok {
			if err := s.Promote(ctx, []string{key}); err != nil {
				return promoted, deleted, err
			}
			promoted++

			continue
		}
		if err := s.Discard(ctx, []string{key}); err != nil {
			return promoted, deleted, err
		}
		deleted++
	}

	return promoted, deleted, nil
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

func pendingPath(key string) string {
	return pendingDir + "/" + key
}
