//go:build unix

package cache

import (
	"os"

	"github.com/go-playground/errors/v5"
	"golang.org/x/sys/unix"
)

// tryLockFile takes the exclusive lock on the file through flock(2), the operating
// system's advisory file lock, without waiting. It answers false, with no error, when
// another open file holds the lock. The operating system releases the lock when the
// process holding it ends, however it ends.
func tryLockFile(f *os.File) (bool, error) {
	err := flock(f, unix.LOCK_EX|unix.LOCK_NB, "unix.Flock(LOCK_EX|LOCK_NB)")
	if errors.Is(err, unix.EWOULDBLOCK) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return true, nil
}

// lockFile takes the exclusive lock on the file, waiting until its holder releases it.
func lockFile(f *os.File) error {
	return flock(f, unix.LOCK_EX, "unix.Flock(LOCK_EX)")
}

// unlockFile releases the lock taken by tryLockFile or lockFile.
func unlockFile(f *os.File) error {
	return flock(f, unix.LOCK_UN, "unix.Flock(LOCK_UN)")
}

// flock calls flock(2) again when a signal interrupts it, as a wait for the lock may be,
// and wraps any other failure with the call it names.
func flock(f *os.File, how int, call string) error {
	for {
		err := unix.Flock(int(f.Fd()), how)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return errors.Wrap(err, call)
		}

		return nil
	}
}
