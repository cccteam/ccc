//go:build !unix

package regen

import "os"

// lock takes no lock on this platform, which has no advisory file lock here: the tests
// that regenerate may race when go test runs their packages in parallel, so run them
// one package at a time (go test -p 1 ./...).
func lock(_ *os.File) error {
	return nil
}

// unlock releases nothing; see lock.
func unlock(_ *os.File) error {
	return nil
}
