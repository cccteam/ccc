//go:build unix

package regen

import (
	"testing"
	"time"
)

func TestAcquire(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(t *testing.T, root string)
	}{
		{
			name: "the second holder waits for the first to release",
			run: func(t *testing.T, root string) {
				t.Helper()

				releaseFirst, err := acquire(root)
				if err != nil {
					t.Fatalf("acquire() error = %v", err)
				}
				held := make(chan error, 1)
				go func() {
					releaseSecond, err := acquire(root)
					if err == nil {
						err = releaseSecond()
					}
					held <- err
				}()
				select {
				case err := <-held:
					t.Fatalf("the second acquire() returned (%v) while the first held the lock", err)
				case <-time.After(300 * time.Millisecond):
				}
				if err := releaseFirst(); err != nil {
					t.Fatalf("release() error = %v", err)
				}
				select {
				case err := <-held:
					if err != nil {
						t.Fatalf("the second acquire() error = %v", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("the second acquire() never returned after the first release")
				}
			},
		},
		{
			name: "a released lock is taken again at once",
			run: func(t *testing.T, root string) {
				t.Helper()

				release, err := acquire(root)
				if err != nil {
					t.Fatalf("acquire() error = %v", err)
				}
				if err := release(); err != nil {
					t.Fatalf("release() error = %v", err)
				}
				again := make(chan error, 1)
				go func() {
					release, err := acquire(root)
					if err == nil {
						err = release()
					}
					again <- err
				}()
				select {
				case err := <-again:
					if err != nil {
						t.Fatalf("acquire() again error = %v", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("acquire() after a release never returned")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// The lock is named for the root, so each case has its own.
			tt.run(t, t.TempDir())
		})
	}
}
