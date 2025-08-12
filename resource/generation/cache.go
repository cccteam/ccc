package generation

import (
	"crypto/sha1"
	"encoding/gob"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-playground/errors/v5"
)

const (
	genCacheDir    string = ".gencache"
	genCacheSuffix string = ".gen"
	tableMapCache  string = "tablemap" + genCacheSuffix
	enumValueCache string = "enumvalues" + genCacheSuffix
)

func readGenCache() (map[string]struct{}, error) {
	dir, err := os.Open(genCacheDir)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, errors.Wrap(err, "os.Open()")
		}

		if err := os.Mkdir(genCacheDir, fs.ModeDir|0o755); err != nil {
			return nil, errors.Wrap(err, "os.Mkdir()")
		}

		dir, err = os.Open(genCacheDir)
		if err != nil {
			return nil, errors.Wrap(err, "os.Open()")
		}
	}
	defer dir.Close()

	fileNames, err := dir.Readdirnames(0)
	if err != nil {
		return nil, errors.Wrap(err, "os.File.Readdirnames()")
	}

	genCacheMap := make(map[string]struct{}, len(fileNames))
	for _, fileName := range fileNames {
		genCacheMap[fileName] = struct{}{}
	}

	return genCacheMap, nil
}

func hashFile(fileName string) ([]byte, error) {
	f, err := os.Open(fileName)
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

func hashFilesInDir(path string) (map[string]struct{}, error) {
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

func cacheSchemaHashes(migrationPath string) error {
	migrationPath = strings.TrimPrefix(migrationPath, "file://")
	schemaMigrationHashes, err := hashFilesInDir(migrationPath)
	if err != nil {
		return err
	}

	for hash := range schemaMigrationHashes {
		fileName := filepath.Join(genCacheDir, fmt.Sprintf("%x", []byte(hash)))
		fd, err := os.Create(fileName)
		if err != nil {
			return errors.Wrap(err, "os.Create()")
		}
		fd.Close()
	}

	return nil
}

func isCacheValid(migrationPath string) (bool, error) {
	genCacheMap, err := readGenCache()
	if err != nil {
		return false, err
	}

	migrationPath = strings.TrimPrefix(migrationPath, "file://")
	dir, err := os.Open(migrationPath)
	if err != nil {
		return false, errors.Wrap(err, "os.Open()")
	}
	defer dir.Close()

	fileNames, err := dir.Readdirnames(0)
	if err != nil {
		return false, errors.Wrap(err, "os.File.Readdirnames()")
	}

	hashMap := make(map[string]struct{}, len(fileNames))
	for _, fileName := range fileNames {
		hash, err := hashFile(filepath.Join(migrationPath, fileName))
		if err != nil {
			return false, err
		}

		if _, ok := genCacheMap[fmt.Sprintf("%x", hash)]; !ok {
			return false, nil
		}

		hashMap[string(hash)] = struct{}{}
	}

	return true, nil
}

func cleanCache() error {
	if err := os.RemoveAll(genCacheDir); err != nil {
		return errors.Wrap(err, "os.RemoveAll")
	}

	if err := os.Mkdir(genCacheDir, fs.ModeDir|0o755); err != nil {
		return errors.Wrap(err, "os.Mkdir")
	}

	return nil
}

func cacheData(name string, data any) error {
	fileName := filepath.Join(genCacheDir, name)
	f, err := os.Create(fileName)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer f.Close()

	encoder := gob.NewEncoder(f)
	if err := encoder.Encode(data); err != nil {
		return errors.Wrap(err, "gob.Encoder.Encode()")
	}

	return nil
}

func loadData(name string, dst any) error {
	fileName := filepath.Join(genCacheDir, name)
	f, err := os.Open(fileName)
	if err != nil {
		return errors.Wrap(err, "os.Open")
	}
	defer f.Close()

	decoder := gob.NewDecoder(f)
	if err := decoder.Decode(dst); err != nil {
		return errors.Wrapf(err, "fileName=%q, gob.Decoder.Decode()", fileName)
	}

	return nil
}
