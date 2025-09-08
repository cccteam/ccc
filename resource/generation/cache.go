package generation

import (
	"crypto/sha1"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-playground/errors/v5"
)

const (
	genCacheDir            string = "."
	genCacheSuffix         string = ".gen"
	tableMapCache          string = "tablemap" + genCacheSuffix
	enumValueCache         string = "enumvalues" + genCacheSuffix
	consolidatedRouteCache string = "consolidatedroutes" + genCacheSuffix
)

// Calculate the SHA1 checksum of a file
func hashFile(filePath string) ([]byte, error) {
	stat, err := os.Stat(filePath)
	if err != nil {
		return nil, errors.Wrapf(err, "could not stat %q: os.Stat()", filePath)
	}
	if stat.IsDir() {
		return nil, errors.Newf("cannot compute SHA1 of the directory %q. pass each of its files to this function individually", filePath)
	}

	f, err := os.Open(filePath)
	if err != nil {
		return nil, errors.Wrap(err, "os.Open()")
	}
	defer f.Close()

	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, errors.Wrap(err, "io.Copy()")
	}

	return h.Sum(nil), nil
}

// Compute the SHA1 checksum of each file in a directory
func hashFilesInDir(path string) (map[string]struct{}, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return nil, errors.Wrapf(err, "could not stat %q: os.Stat()", path)
	}
	if !stat.IsDir() {
		return nil, errors.Newf("%q is not a directory", path)
	}

	dir, err := os.Open(path)
	if err != nil {
		return nil, errors.Wrap(err, "os.Open()")
	}
	defer dir.Close()

	fileNames, err := dir.Readdirnames(0)
	if err != nil {
		return nil, errors.Wrap(err, "os.File.Readdirnames()")
	}

	hashMap := make(map[string]struct{}, len(fileNames))
	for _, fileName := range fileNames {
		hash, err := hashFile(filepath.Join(path, fileName))
		if err != nil {
			return nil, err
		}
		hashMap[string(hash)] = struct{}{}
	}

	return hashMap, nil
}

func (c *client) cacheSchemaHashes() error {
	migrationPath := strings.TrimPrefix(c.migrationSourceURL, "file://")
	schemaMigrationHashes, err := hashFilesInDir(migrationPath)
	if err != nil {
		return err
	}

	for hash := range schemaMigrationHashes {
		if err := c.genCache.Store("migrations", fmt.Sprintf("%x", []byte(hash)), ""); err != nil {
			return errors.Wrap(err, "could not store SHA1 hash in gencache: cache.Cache.Store()")
		}
	}

	return nil
}

// Loads previous schema migration checksums from gencache, if they exist.
// Returns false current schema migration checksums do not match cached checksums.
func (c *client) isSchemaClean() (bool, error) {
	keys, err := c.genCache.Keys("migrations")
	if err != nil {
		return false, errors.Wrap(err, "could not load migration hashes from genCache: cache.Cache.Keys()")
	}

	cachedHashes := make(map[string]struct{})
	for key := range keys {
		cachedHashes[key] = struct{}{}
	}

	// gather files from schema migration directory
	migrationPath := strings.TrimPrefix(c.migrationSourceURL, "file://")
	dir, err := os.Open(migrationPath)
	if err != nil {
		return false, errors.Wrap(err, "os.Open()")
	}
	defer dir.Close()

	fileNames, err := dir.Readdirnames(0)
	if err != nil {
		return false, errors.Wrap(err, "os.File.Readdirnames()")
	}

	// check cache for hash of each schema migration file
	for _, fileName := range fileNames {
		hash, err := hashFile(filepath.Join(migrationPath, fileName))
		if err != nil {
			return false, err
		}

		if _, ok := cachedHashes[fmt.Sprintf("%x", hash)]; !ok {
			return false, nil
		}
	}

	return true, nil
}

func (c *client) loadAllCachedData(genType generatorType) (bool, error) {
	c.tableMap = make(map[string]*tableMetadata)
	if ok, err := c.genCache.Load("spanner", tableMapCache, &c.tableMap); err != nil {
		return false, errors.Wrapf(err, "cache.Cache.Load() for %q", tableMapCache)
	} else if !ok {
		return false, nil
	}

	c.enumValues = make(map[string][]enumData)
	if ok, err := c.genCache.Load("spanner", enumValueCache, &c.enumValues); err != nil {
		return false, errors.Wrapf(err, "cache.Cache.Load() for %q", enumValueCache)
	} else if !ok {
		return false, nil
	}

	if genType == typeScriptGeneratorType {
		if ok, err := c.genCache.Load("app", consolidatedRouteCache, &c.consolidateConfig); err != nil {
			return false, errors.Wrapf(err, "cache.Cache.Load() for %q", consolidatedRouteCache)
		} else if !ok {
			return false, nil
		}
	}

	c.cleanup = func() {}

	return true, nil
}

func (c *client) populateCache() error {
	if err := c.genCache.Store("spanner", tableMapCache, c.tableMap); err != nil {
		return errors.Wrap(err, "cache.Cache.Store()")
	}

	if err := c.cacheSchemaHashes(); err != nil {
		return err
	}

	if err := c.genCache.Store("spanner", enumValueCache, c.enumValues); err != nil {
		return errors.Wrap(err, "cache.Cache.Store()")
	}

	return nil
}
