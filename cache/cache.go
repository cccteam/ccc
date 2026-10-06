// Package cache provides a thread-safe key-value interface for persisting data to disk.
// Ideal use case is for caching data that is expensive to compute in devtools and unlikely to change.
// Do NOT use it to store sensitive information.
package cache

import (
	"fmt"
	"io"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fxamacker/cbor/v2"
	"github.com/go-playground/errors/v5"
)

const (
	cachePrefix string = ".ccc-cache"
	// lockFileName is the file at the cache root that New locks, so two Caches over one
	// root take turns.
	lockFileName string = ".lock"
)

// Option is a functional option for configuring the Cache.
type Option func(*Cache) *Cache

// WithPermission configures the unix permission bits on each
// file and directory within the Cache.
func WithPermission(perms uint32) Option {
	return func(c *Cache) *Cache {
		c.permissionBits = perms

		return c
	}
}

// Cache is an instance of persistence storage on disk. It provides methods
// for storing, loading, and removing encoded data on disk. It is safe to use concurrently.
// The Close method must be called when the cache is no longer needed: it releases the
// lock that other Caches over the same root wait on (see New).
type Cache struct {
	permissionBits uint32
	mu             sync.RWMutex
	cacheFolder    string
	root           *os.Root
	decoderOpts    cbor.DecOptions
	// lockFile is the open lock file at the cache root, locked from New until Close.
	lockFile *os.File
	// waitOutput receives the line New prints when it waits for another holder of the
	// lock: standard error.
	waitOutput io.Writer
}

// New creates a new Cache, with its storage located at `path“ concatenated with `.ccc-cache/`.
// Example: New("./foo") returns a Cache instance that stores data at `./foo/.ccc-cache/`.
//
// New takes an exclusive lock on the file `.lock` in the `.ccc-cache` folder (the cache
// root) and holds it until Close, so two Caches over one root, in two processes or in
// one, take turns: the second New waits until the first is closed, and then reads what
// the first stored. When another Cache holds the lock, New prints one line to standard
// error naming the root before it waits. The lock is advisory, which means only programs
// that take the same lock wait for it; it does not stop anything else from writing into
// the root. The operating system releases the lock when the process holding it ends,
// however it ends, so a process that is killed leaves no stale lock behind.
func New(path string, opts ...Option) (*Cache, error) {
	c := &Cache{
		permissionBits: 0o755,
		cacheFolder:    filepath.Join(path, cachePrefix),
		decoderOpts:    cbor.DecOptions{MaxMapPairs: 2147483647},
		waitOutput:     os.Stderr,
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
		// Another process may create the folder between the stat and the mkdir, before
		// either holds the lock; a folder that exists now is what was wanted.
		if err := os.Mkdir(c.cacheFolder, fs.FileMode(c.permissionBits)); err != nil && !errors.Is(err, fs.ErrExist) {
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

	if err := c.lock(); err != nil {
		_ = root.Close()

		return nil, err
	}

	return c, nil
}

// lock opens the lock file at the cache root and takes its exclusive lock. It tries
// without waiting first; when another Cache holds the lock, it prints one line naming
// the root and then waits until the lock is free.
func (c *Cache) lock() error {
	f, err := c.root.OpenFile(lockFileName, os.O_RDWR|os.O_CREATE, fs.FileMode(c.permissionBits&^0o111))
	if err != nil {
		return errors.Wrap(err, "os.Root.OpenFile()")
	}

	locked, err := tryLockFile(f)
	if err != nil {
		_ = f.Close()

		return err
	}
	if !locked {
		_, _ = fmt.Fprintf(c.waitOutput, "waiting for another process using the cache at %s\n", c.displayRoot())
		if err := lockFile(f); err != nil {
			_ = f.Close()

			return err
		}
	}
	c.lockFile = f

	return nil
}

// displayRoot is the cache root as the wait line names it: absolute where the working
// directory is known, so the line says which root it is wherever it is read.
func (c *Cache) displayRoot() string {
	abs, err := filepath.Abs(c.cacheFolder)
	if err != nil {
		return c.cacheFolder
	}

	return abs
}

// Close must be called when you are done using the cache. It releases the lock New
// took, so the next Cache waiting on the root goes ahead.
func (c *Cache) Close() error {
	rootErr := c.root.Close()
	lockErr := c.unlock()
	if rootErr != nil {
		return errors.Wrap(errors.Join(rootErr, lockErr), "os.Root.Close()")
	}

	return lockErr
}

// unlock releases the lock and closes the lock file. Closing the file alone would also
// release it; the explicit release reports a failure where it happens.
func (c *Cache) unlock() error {
	if err := unlockFile(c.lockFile); err != nil {
		_ = c.lockFile.Close()

		return err
	}
	if err := c.lockFile.Close(); err != nil {
		return errors.Wrap(err, "os.File.Close()")
	}

	return nil
}

// Load reads data from path/subpath and stores in dst
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

	decOpts, err := c.decoderOpts.DecMode()
	if err != nil {
		return false, errors.Wrap(err, "cbor.DecOptions.DecMode()")
	}

	decoder := decOpts.NewDecoder(f)
	if err := decoder.Decode(dst); err != nil {
		return false, errors.Wrap(err, "cbor.Decoder.Decode()")
	}

	return true, nil
}

// Keys returns an iterator over the file names in the given Cache subpath.
func (c *Cache) Keys(subpath string) (iter.Seq[string], error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	empty := func(func(string) bool) {}

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
	atRoot := filepath.Clean(subpath) == "."

	return func(yield func(string) bool) {
		for i := range dirEntries {
			if dirEntries[i].IsDir() || (atRoot && dirEntries[i].Name() == lockFileName) {
				continue
			}

			if !yield(dirEntries[i].Name()) {
				return
			}
		}
	}, nil
}

// Store encodes given data and writes it to file at "/path/subpath/key"
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
				// Another process may create the directory between the stat and the
				// mkdir; one that exists now is what was wanted.
				if err := c.root.Mkdir(path, fs.FileMode(c.permissionBits)); err != nil && !errors.Is(err, fs.ErrExist) {
					return errors.Wrapf(err, "os.Root.Mkdir(%q)", path)
				}

				if err := c.root.Chmod(path, fs.FileMode(c.permissionBits)); err != nil {
					return errors.Wrap(err, "os.Root.Chmod()")
				}
			}
		}
	}

	// The value is written to a temporary file beside the key and renamed over it, so a
	// reader never sees a partial file, even one a process stopped in the middle of a
	// write left behind: the rename replaces the key whole.
	fileName := filepath.Join(subpath, key)
	tmp, tmpName, err := c.createTemp(subpath, key)
	if err != nil {
		return err
	}
	if err := c.writeTemp(tmp, tmpName, data); err != nil {
		_ = c.root.Remove(tmpName)

		return err
	}
	if err := c.root.Rename(tmpName, fileName); err != nil {
		_ = c.root.Remove(tmpName)

		return errors.Wrap(err, "os.Root.Rename()")
	}

	return nil
}

// createTemp opens a new temporary file beside the key, named after it so a stray one is
// recognizable, exclusively created so two writers never share one, and answers it with
// its root-relative name.
func (c *Cache) createTemp(subpath, key string) (*os.File, string, error) {
	for attempt := range 100 {
		name := filepath.Join(subpath, fmt.Sprintf(".%s.tmp-%d-%d", key, os.Getpid(), attempt))
		f, err := c.root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL|os.O_SYNC, fs.FileMode(c.permissionBits&^0o111))
		if err == nil {
			return f, name, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, "", errors.Wrap(err, "os.Root.OpenFile()")
		}
	}

	return nil, "", errors.Newf("no free temporary name for %q after 100 attempts", key)
}

// writeTemp encodes the value into the temporary file, closes it, and sets its mode:
// files should not be executable, so the execute bits are dropped.
func (c *Cache) writeTemp(f *os.File, name string, data any) error {
	encoder := cbor.NewEncoder(f)
	if err := encoder.Encode(data); err != nil {
		_ = f.Close()

		return errors.Wrap(err, "cbor.Encoder.Encode()")
	}
	if err := f.Close(); err != nil {
		return errors.Wrap(err, "os.File.Close()")
	}
	if err := c.root.Chmod(name, fs.FileMode(c.permissionBits&^0o111)); err != nil {
		return errors.Wrap(err, "os.Root.Chmod()")
	}

	return nil
}

// DeleteKey deletes a file whose name matches the key at the given subpath.
// The subpath must exist. If the key does not exist, DeleteKey returns nil (no error).
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

// DeleteSubpath deletes a directory whose name matches the subpath.
// If the subpath does not exist, DeleteSubpath returns nil (no error).
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

// DeleteAll removes all directories and files in the Cache except the lock file, which
// stays because the Cache holds its lock: removing it would let another process lock a
// new file of the same name while this Cache still holds the old one.
// If the Cache is empty, DeleteAll returns nil (no error).
func (c *Cache) DeleteAll() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	entries, err := fs.ReadDir(c.root.FS(), ".")
	if err != nil {
		return errors.Wrap(err, "fs.ReadDir()")
	}
	for _, entry := range entries {
		if entry.Name() == lockFileName {
			continue
		}
		if err := c.root.RemoveAll(entry.Name()); err != nil {
			return errors.Wrap(err, "os.Root.RemoveAll()")
		}
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
