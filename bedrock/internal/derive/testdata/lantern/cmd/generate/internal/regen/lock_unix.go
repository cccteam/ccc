//go:build unix

package regen

import (
	"os"
	"syscall"

	"github.com/go-playground/errors/v5"
)

// lock takes the exclusive advisory lock on the file, waiting for its holder to release
// it. The kernel releases it when the holding process ends, however it ends, so a test
// stopped short never leaves the lock behind.
func lock(f *os.File) error {
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return errors.Wrap(err, "syscall.Flock(LOCK_EX)")
	}

	return nil
}

// unlock releases the lock taken by lock.
func unlock(f *os.File) error {
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
		return errors.Wrap(err, "syscall.Flock(LOCK_UN)")
	}

	return nil
}
