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
	"strings"
	"sync"

	"github.com/go-playground/errors/v5"
)

const cachePrefix string = ".ccc-cache"

type Option func(*Cache) *Cache

func WithPermission(perms uint32) Option {
	return func(c *Cache) *Cache {
		c.permissionBits = perms

		return c
	}
}

type Cache struct {
	permissionBits uint32
	mu             sync.RWMutex
	cacheFolder    string
	root           *os.Root
}

func New(path string, opts ...Option) (*Cache, error) {
	c := &Cache{
		permissionBits: 0o755,
		cacheFolder:    filepath.Join(path, cachePrefix),
	}

	for _, opt := range opts {
		opt(c)
	}

	// Require path exists so we don't need to make permission
	// assumptions on any parent directories.
	if _, err := os.Stat(path); err != nil {
		return nil, errors.Wrap(err, "os.Stat()")
	} else if os.IsNotExist(err) {
		return nil, errors.Newf("cache path %q does not exist", path)
	}

	if _, err := os.Stat(c.cacheFolder); err != nil && !os.IsNotExist(err) {
		return nil, errors.Wrap(err, "os.Stat()")
	} else if os.IsNotExist(err) {
		if err := os.Mkdir(c.cacheFolder, fs.FileMode(c.permissionBits)); err != nil {
			return c, errors.Wrap(err, "os.Mkdir()")
		}

		if err := os.Chmod(c.cacheFolder, fs.FileMode(c.permissionBits)); err != nil {
			return c, errors.Wrap(err, "os.Chmod()")
		}
	}

	root, err := os.OpenRoot(c.cacheFolder)
	if err != nil {
		return nil, errors.Wrap(err, "os.OpenRoot()")
	}
	c.root = root

	return c, nil
}

// Loads data from path/subpath and stores in dst
func (c *Cache) Load(subpath, key string, dst any) (bool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if exist, err := c.pathExists(subpath); err != nil {
		return false, err
	} else if !exist {
		return false, nil
	}

	fileName := filepath.Join(subpath, key)
	f, err := c.root.Open(fileName)
	if err != nil {
		if !os.IsNotExist(err) {
			return false, errors.Wrap(err, "os.Root.Open()")
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

	if exist, err := c.pathExists(subpath); err != nil {
		return nil, err
	} else if !exist {
		return empty, nil
	}

	dir, err := c.root.Open(subpath)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, errors.Wrap(err, "os.Root.Open()")
		}

		return empty, nil
	}
	defer dir.Close()

	dirEntries, err := dir.ReadDir(0)
	if err != nil {
		return nil, errors.Wrap(err, "os.File.ReadDir()")
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

	if exist, err := c.pathExists(subpath); err != nil {
		return err
	} else if !exist {
		var path string
		for part := range strings.SplitSeq(filepath.Clean(subpath), string(os.PathSeparator)) {
			path = filepath.Join(path, part)
			if _, err := c.root.Stat(path); os.IsNotExist(err) {
				if err := c.root.Mkdir(path, fs.FileMode(c.permissionBits)); err != nil {
					return errors.Wrapf(err, "os.Root.Mkdir(%q)", path)
				}

				if err := c.root.Chmod(path, fs.FileMode(c.permissionBits)); err != nil {
					return errors.Wrap(err, "os.Root.Chmod()")
				}
			}
		}
	}

	fileName := filepath.Join(subpath, key)
	f, err := c.root.OpenFile(fileName, os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_SYNC, fs.FileMode(c.permissionBits))
	if err != nil {
		return errors.Wrap(err, "os.Root.OpenFile()")
	}

	encoder := gob.NewEncoder(f)
	if err := encoder.Encode(data); err != nil {
		return errors.Wrap(err, "gob.Encoder.Encode()")
	}
	f.Close()

	// Files should not be executable, so drop execute bits
	if err := c.root.Chmod(fileName, fs.FileMode(c.permissionBits&^0o111)); err != nil {
		return errors.Wrap(err, "os.Root.Chmod()")
	}

	return nil
}

func (c *Cache) DeleteKey(subpath, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if exist, err := c.pathExists(subpath); err != nil {
		return err
	} else if !exist {
		return nil
	}

	if err := c.root.Remove(filepath.Join(subpath, key)); err != nil {
		return errors.Wrap(err, "os.Root.Remove()")
	}

	return nil
}

func (c *Cache) DeleteSubpath(subpath string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if exist, err := c.pathExists(subpath); err != nil {
		return err
	} else if !exist {
		return nil
	}

	if err := c.root.RemoveAll(subpath); err != nil {
		return errors.Wrap(err, "os.Root.RemoveAll()")
	}

	return nil
}

func (c *Cache) DeleteAll() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := os.RemoveAll(c.cacheFolder); err != nil {
		return errors.Wrap(err, "os.RemoveAll()")
	}

	if err := os.Mkdir(c.cacheFolder, fs.FileMode(c.permissionBits)); err != nil {
		return errors.Wrap(err, "os.Mkdir")
	}

	if err := os.Chmod(c.cacheFolder, fs.FileMode(c.permissionBits)); err != nil {
		return errors.Wrap(err, "os.Chmod()")
	}

	return nil
}

func (c *Cache) pathExists(subpath string) (bool, error) {
	stat, err := c.root.Stat(subpath)
	if err != nil {
		if !os.IsNotExist(err) {
			return false, errors.Wrap(err, "os.Root.Stat()")
		}

		return false, nil
	}

	if !stat.IsDir() {
		return false, errors.Newf("path %q is not a directory", subpath)
	}

	return true, nil
}
