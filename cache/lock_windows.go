//go:build windows

package cache

import (
	"os"

	"github.com/go-playground/errors/v5"
	"golang.org/x/sys/windows"
)

// allBytes locks the file's whole byte range, low and high halves.
const allBytes = ^uint32(0)

// tryLockFile takes the exclusive lock on the file through LockFileEx, the operating
// system's file lock, without waiting. It answers false, with no error, when another
// handle holds the lock. Windows releases the lock when the process holding it ends,
// however it ends.
func tryLockFile(f *os.File) (bool, error) {
	err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, allBytes, allBytes, new(windows.Overlapped))
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}
	if err != nil {
		return false, errors.Wrap(err, "windows.LockFileEx(LOCKFILE_FAIL_IMMEDIATELY)")
	}

	return true, nil
}

// lockFile takes the exclusive lock on the file, waiting until its holder releases it.
func lockFile(f *os.File) error {
	if err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, allBytes, allBytes, new(windows.Overlapped)); err != nil {
		return errors.Wrap(err, "windows.LockFileEx()")
	}

	return nil
}

// unlockFile releases the lock taken by tryLockFile or lockFile.
func unlockFile(f *os.File) error {
	if err := windows.UnlockFileEx(windows.Handle(f.Fd()), 0, allBytes, allBytes, new(windows.Overlapped)); err != nil {
		return errors.Wrap(err, "windows.UnlockFileEx()")
	}

	return nil
}
