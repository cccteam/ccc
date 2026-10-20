// Package regen serializes the tests that regenerate the tree. go test runs each
// package's tests in its own process, in parallel with the other packages', and this
// module has two kinds of test that regenerate: cmd/generate's, which runs go generate
// ./... and compares the tree before and after, and each generate program's warnings
// test, which runs the generator in-process. Two generations over one module at once
// race in the generated files and in the generation cache, so each of those tests takes
// the module's generation lock first.
package regen

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-playground/errors/v5"
)

// Exclusive takes the module's generation lock for the test and releases it when the
// test ends. The lock is a file under the temporary directory named for this module's
// root, so every test process of this module waits on the same one.
func Exclusive(t *testing.T) {
	t.Helper()

	root, err := moduleRoot()
	if err != nil {
		t.Fatalf("regen.Exclusive: %v", err)
	}
	release, err := acquire(root)
	if err != nil {
		t.Fatalf("regen.Exclusive: %v", err)
	}
	t.Cleanup(func() {
		if err := release(); err != nil {
			t.Errorf("regen.Exclusive: releasing the generation lock: %v", err)
		}
	})
}

// acquire waits until the lock of the module rooted at root is held and returns the
// function that releases it.
func acquire(root string) (func() error, error) {
	sum := sha256.Sum256([]byte(root))
	name := filepath.Join(os.TempDir(), "generation-"+hex.EncodeToString(sum[:8])+".lock")
	f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, errors.Wrap(err, "os.OpenFile()")
	}
	if err := lock(f); err != nil {
		_ = f.Close()

		return nil, err
	}
	release := func() error {
		if err := unlock(f); err != nil {
			_ = f.Close()

			return err
		}
		if err := f.Close(); err != nil {
			return errors.Wrap(err, "os.File.Close()")
		}

		return nil
	}

	return release, nil
}

// moduleRoot is the directory holding go.mod at or above the working directory (go test
// runs a package's tests in that package's directory).
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", errors.Wrap(err, "os.Getwd()")
	}
	for {
		_, err := os.Stat(filepath.Join(dir, "go.mod"))
		if err == nil {
			return dir, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", errors.Wrap(err, "os.Stat()")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("no go.mod at or above the working directory")
		}
		dir = parent
	}
}
