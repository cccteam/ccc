// package cache provides a thread-safe key-value interface for persisting data to disk.
// Ideal use case is for caching data that is expensive to compute in devtools and unlikely to change.
// Do NOT use it to store sensitive information.
package cache

import (
	"encoding/gob"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"sync"

	"github.com/go-playground/errors/v5"
)

const cachePrefix string = ".ccc-cache"

func New(path string) *Cache {
	c := &Cache{
		permissionBits: 0o755,
		path:           filepath.Join(path, cachePrefix),
	}

	return c
}

type Cache struct {
	permissionBits uint32
	mu             sync.RWMutex
	path           string
}

func (c *Cache) SetPermissions(perms uint32) {
	c.permissionBits = perms
}

// Loads data from path/subpath and stores in dst
func (c *Cache) Load(subpath, key string, dst any) (bool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if ok, err := c.pathExists(subpath); err != nil {
		return false, err
	} else if !ok {
		return false, nil
	}

	fileName := filepath.Join(c.path, subpath, key)
	f, err := os.Open(fileName)
	if err != nil {
		if !os.IsNotExist(err) {
			return false, errors.Wrap(err, "os.Open()")
		}

		return false, nil
	}
	defer f.Close()

	decoder := gob.NewDecoder(f)
	if err := decoder.Decode(dst); err != nil {
		return false, errors.Wrap(err, "gob.Decoder.Decode()")
	}

	return true, nil
}

func (c *Cache) Keys(subpath string) (iter.Seq[string], error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	empty := func(yield func(string) bool) {}

	if ok, err := c.pathExists(subpath); err != nil {
		return nil, err
	} else if !ok {
		return empty, nil
	}

	dir, err := os.Open(filepath.Join(c.path, subpath))
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, errors.Wrap(err, "os.Open()")
		}

		return empty, nil
	}
	defer dir.Close()

	dirEntries, err := dir.ReadDir(0)
	if err != nil {
		return nil, errors.Wrap(err, "os.Open()")
	}

	return func(yield func(string) bool) {
		for i := range dirEntries {
			if dirEntries[i].IsDir() {
				continue
			}

			if !yield(dirEntries[i].Name()) {
				return
			}
		}
	}, nil
}

func (c *Cache) Store(subpath, key string, data any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if ok, err := c.pathExists(subpath); err != nil {
		return err
	} else if !ok {
		path := filepath.Join(c.path, subpath)
		if err := os.MkdirAll(path, fs.ModeDir|fs.FileMode(c.permissionBits)); err != nil {
			return errors.Wrap(err, "os.MkdirAll()")
		}

		if err := os.Chmod(path, fs.FileMode(c.permissionBits)); err != nil {
			return errors.Wrap(err, "os.Chmod()")
		}
	}

	fileName := filepath.Join(c.path, subpath, key)
	f, err := os.OpenFile(fileName, os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_SYNC, fs.FileMode(c.permissionBits))
	if err != nil {
		return errors.Wrap(err, "os.OpenFile()")
	}

	encoder := gob.NewEncoder(f)
	if err := encoder.Encode(data); err != nil {
		return errors.Wrap(err, "gob.Encoder.Encode()")
	}
	f.Close()

	if err := os.Chmod(fileName, fs.FileMode(c.permissionBits)); err != nil {
		return errors.Wrap(err, "os.Chmod()")
	}

	return nil
}

func (c *Cache) DeleteKey(subpath, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if ok, err := c.pathExists(subpath); err != nil {
		return err
	} else if !ok {
		return nil
	}

	if err := os.Remove(filepath.Join(c.path, subpath, key)); err != nil {
		return errors.Wrap(err, "os.Remove()")
	}

	return nil
}

func (c *Cache) DeleteSubpath(subpath string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if ok, err := c.pathExists(subpath); err != nil {
		return err
	} else if !ok {
		return nil
	}

	return deletePath(c.path)
}

func (c *Cache) DeleteAll() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := deletePath(c.path); err != nil {
		return err
	}

	if err := os.Mkdir(c.path, fs.ModeDir|fs.FileMode(c.permissionBits)); err != nil {
		return errors.Wrap(err, "os.Mkdir")
	}

	if err := os.Chmod(c.path, fs.FileMode(c.permissionBits)); err != nil {
		return errors.Wrap(err, "os.Chmod()")
	}

	return nil
}

func deletePath(path string) error {
	if err := os.RemoveAll(path); err != nil {
		return errors.Wrap(err, "os.RemoveAll()")
	}

	return nil
}

func (c *Cache) pathExists(subpath string) (bool, error) {
	path := filepath.Join(c.path, subpath)
	stat, err := os.Stat(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return false, errors.Wrap(err, "os.Stat()")
		}

		return false, nil
	}

	if !stat.IsDir() {
		return false, errors.Newf("path %q is not a directory", path)
	}

	return true, nil
}
