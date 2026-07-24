package generation

import (
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"maps"
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
	collectionDataCache    string = "collectiondata" + genCacheSuffix
	typescriptMarkerCache  string = "typescriptmarker" + genCacheSuffix
)

// typescriptMarker records the TypeScript configurations the Resource Generator run
// emitted, one per GenerateTypescript target directory (empty when the run emitted no
// TypeScript). It is rewritten on every run so removing a GenerateTypescript call
// re-activates the deprecated TypeScript generator for that directory instead of
// silently generating nothing.
type typescriptMarker struct {
	Configs []typescriptRunConfig
}

// configFor returns the recorded configuration whose target directory matches targetDir,
// or nil when the Resource Generator run did not emit TypeScript there.
func (m *typescriptMarker) configFor(targetDir string) *typescriptRunConfig {
	dir := filepath.Clean(targetDir)
	for i := range m.Configs {
		if m.Configs[i].TargetDir == dir {
			return &m.Configs[i]
		}
	}

	return nil
}

// typescriptRunConfig captures every setting that shapes TypeScript output, so the
// deprecated TypeScript generator can verify the Resource Generator's configuration
// matches its own.
type typescriptRunConfig struct {
	TargetDir           string
	GenMetadata         bool
	GenPermission       bool
	GenEnums            bool
	TypescriptOverrides map[string]string
	VirtualDir          string
	ComputedDir         string
	RPCDir              string
	PluralOverrides     map[string]string
}

// typescriptRunConfigFrom assembles the comparable configuration from a resolved
// typescriptGenerator and its client.
func typescriptRunConfigFrom(t *typescriptGenerator, c *client, targetDir string) typescriptRunConfig {
	return typescriptRunConfig{
		TargetDir:           filepath.Clean(targetDir),
		GenMetadata:         t.genMetadata,
		GenPermission:       t.genPermission,
		GenEnums:            t.genEnums,
		TypescriptOverrides: t.typescriptOverrides,
		VirtualDir:          string(c.virtual),
		ComputedDir:         string(c.computed),
		RPCDir:              string(c.rpc),
		PluralOverrides:     c.pluralOverrides,
	}
}

// diff returns one line per setting that differs between the Resource Generator's
// configuration (m) and the deprecated TypeScript generator's configuration (other).
func (m *typescriptRunConfig) diff(other *typescriptRunConfig) []string {
	var diffs []string

	compare := func(setting, resourceGen, deprecatedGen string) {
		if resourceGen != deprecatedGen {
			diffs = append(diffs, fmt.Sprintf("%s: Resource Generator has %q, deprecated TypeScript generator has %q", setting, resourceGen, deprecatedGen))
		}
	}

	compare("TypeScript target directory", m.TargetDir, other.TargetDir)
	compare("GenerateMetadata", fmt.Sprint(m.GenMetadata), fmt.Sprint(other.GenMetadata))
	compare("GeneratePermissions", fmt.Sprint(m.GenPermission), fmt.Sprint(other.GenPermission))
	compare("GenerateEnums", fmt.Sprint(m.GenEnums), fmt.Sprint(other.GenEnums))
	compare("WithVirtualResources", m.VirtualDir, other.VirtualDir)
	compare("WithComputedResources", m.ComputedDir, other.ComputedDir)
	compare("WithRPC", m.RPCDir, other.RPCDir)

	if !maps.Equal(m.TypescriptOverrides, other.TypescriptOverrides) {
		diffs = append(diffs, "WithTypescriptOverrides: the effective TypeScript type override maps differ")
	}
	if !maps.Equal(m.PluralOverrides, other.PluralOverrides) {
		diffs = append(diffs, "WithPluralOverrides: the plural override maps differ")
	}

	return diffs
}

// Calculate the sha256 checksum of a file
func hashFile(filePath string) ([]byte, error) {
	stat, err := os.Stat(filePath)
	if err != nil {
		return nil, errors.Wrapf(err, "could not stat %q: os.Stat()", filePath)
	}
	if stat.IsDir() {
		return nil, errors.Newf("cannot compute sha256 of the directory %q. pass each of its files to this function individually", filePath)
	}

	f, err := os.Open(filePath)
	if err != nil {
		return nil, errors.Wrap(err, "os.Open()")
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, errors.Wrap(err, "io.Copy()")
	}

	return h.Sum(nil), nil
}

// Compute the sha256 checksum of each file in a directory
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

func hashString(s string) ([]byte, error) {
	h := sha256.New()
	if _, err := h.Write([]byte(s)); err != nil {
		return nil, errors.Wrap(err, "hash.Hash.Write()")
	}

	return h.Sum(nil), nil
}

func (c *client) cacheSchemaHashes() error {
	for _, migrationSource := range c.migrationSourceURLs {
		migrationPath := strings.TrimPrefix(migrationSource, "file://")
		schemaMigrationHashes, err := hashFilesInDir(migrationPath)
		if err != nil {
			return err
		}

		hashedMigrationSourceURL, err := hashString(migrationSource)
		if err != nil {
			return err
		}
		migrationCachePath := filepath.Join("migrations", fmt.Sprintf("%x", hashedMigrationSourceURL))

		for hash := range schemaMigrationHashes {
			if err := c.genCache.Store(migrationCachePath, fmt.Sprintf("%x", []byte(hash)), ""); err != nil {
				return errors.Wrap(err, "could not store sha256 hash in gencache: cache.Cache.Store()")
			}
		}
	}

	return nil
}

// Loads previous schema migration checksums from gencache, if they exist.
// Returns false current schema migration checksums do not match cached checksums.
func (c *client) isSchemaClean() (bool, error) {
	for _, migrationSource := range c.migrationSourceURLs {
		hashedMigrationSourceURL, err := hashString(migrationSource)
		if err != nil {
			return false, err
		}

		keys, err := c.genCache.Keys(filepath.Join("migrations", fmt.Sprintf("%x", hashedMigrationSourceURL)))
		if err != nil {
			return false, errors.Wrap(err, "could not load migration hashes from genCache: cache.Cache.Keys()")
		}

		cachedHashes := make(map[string]struct{})
		for key := range keys {
			cachedHashes[key] = struct{}{}
		}

		// gather files from schema migration directory
		migrationPath := strings.TrimPrefix(migrationSource, "file://")
		dir, err := os.Open(migrationPath)
		if err != nil {
			return false, errors.Wrap(err, "os.Open()")
		}

		fileNames, err := dir.Readdirnames(0)
		if err != nil {
			return false, errors.Wrap(err, "os.File.Readdirnames()")
		}

		if len(fileNames) != len(cachedHashes) {
			log.Printf("\x1b[33mNumber of schema files (%d) does not match number of cached files (%d). Invalidating cache.\x1b[39m\n", len(fileNames), len(cachedHashes))

			return false, nil
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

		if err := dir.Close(); err != nil {
			log.Print(errors.Wrap(err, "os.File.Close()"))
		}
	}

	return true, nil
}

func (c *client) loadAllCachedData(genType generatorType) (bool, error) {
	spannerCachePath, err := c.spannerCachePath()
	if err != nil {
		return false, err
	}

	c.tableMap = make(map[string]*tableMetadata)
	if ok, err := c.genCache.Load(spannerCachePath, tableMapCache, &c.tableMap); err != nil {
		return false, errors.Wrapf(err, "cache.Cache.Load() for %q", tableMapCache)
	} else if !ok {
		return false, nil
	}

	c.enumValues = make(map[string][]*enumData)
	if ok, err := c.genCache.Load(spannerCachePath, enumValueCache, &c.enumValues); err != nil {
		return false, errors.Wrapf(err, "cache.Cache.Load() for %q", enumValueCache)
	} else if !ok {
		return false, nil
	}

	if genType == typeScriptGeneratorType {
		appCachePath, err := c.appCachePath()
		if err != nil {
			return false, err
		}

		if ok, err := c.genCache.Load(appCachePath, consolidatedRouteCache, &c.consolidateConfig); err != nil {
			return false, errors.Wrapf(err, "cache.Cache.Load() for %q", consolidatedRouteCache)
		} else if !ok {
			return false, nil
		}
	}

	return true, nil
}

func (c *client) spannerCachePath() (string, error) {
	var concatenatedPaths strings.Builder
	for _, migrationSource := range c.migrationSourceURLs {
		concatenatedPaths.WriteString(migrationSource)
	}

	hashedPaths, err := hashString(concatenatedPaths.String())
	if err != nil {
		return "", err
	}

	return filepath.Join("spanner", fmt.Sprintf("%x", hashedPaths)), nil
}

func (c *client) appCachePath() (string, error) {
	hashedPath, err := hashString(filepath.Clean(string(c.resource)))
	if err != nil {
		return "", err
	}

	return filepath.Join("app", fmt.Sprintf("%x", hashedPath)), nil
}

func (c *client) populateCache() error {
	var concatenatedPaths string
	for _, migrationSource := range c.migrationSourceURLs {
		concatenatedPaths += migrationSource
	}

	spannerCachePath, err := c.spannerCachePath()
	if err != nil {
		return err
	}

	if err := c.genCache.Store(spannerCachePath, tableMapCache, c.tableMap); err != nil {
		return errors.Wrap(err, "cache.Cache.Store()")
	}

	if err := c.cacheSchemaHashes(); err != nil {
		return err
	}

	if err := c.genCache.Store(spannerCachePath, enumValueCache, c.enumValues); err != nil {
		return errors.Wrap(err, "cache.Cache.Store()")
	}

	return nil
}
