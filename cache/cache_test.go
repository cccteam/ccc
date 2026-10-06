package cache_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/cccteam/ccc/cache"
	"github.com/google/go-cmp/cmp"
)

type op int

const (
	load op = 1 << iota
	store
	remove
	del
)

func Test_Cache(t *testing.T) {
	t.Parallel()
	type foo struct {
		Int    int
		String string
		Bool   bool
	}
	type transaction struct {
		op      op
		subpath string
		key     string
		value   foo
	}
	type args struct {
		dir          string
		transactions []transaction
	}

	tests := []struct {
		name     string
		args     args
		wantErr  bool
		wantFail bool
	}{
		{
			name: "overwrite existing key and load correctly",
			args: args{t.TempDir(), []transaction{
				{store, "subpath1", "key1", foo{Int: 2}},
				{store, "subpath1", "key1", foo{Int: 3}},
				{load, "subpath1", "key1", foo{Int: 3}},
			}},
			wantFail: false,
		},
		{
			name: "fails without error when key does not exist",
			args: args{t.TempDir(), []transaction{
				{store, "subpath1", "key1", foo{Int: 2}},
				{load, "subpath1", "key2", foo{Int: -1}},
			}},
			wantFail: true,
		},
		{
			name: "removing subpath removes all keys",
			args: args{t.TempDir(), []transaction{
				{store, "subpath1", "key1", foo{Int: 1}},
				{store, "subpath1", "key2", foo{Int: 2}},
				{remove, "subpath1", "", foo{}},
				{load, "subpath1", "key1", foo{}},
				{load, "subpath1", "key2", foo{}},
			}},
			wantFail: true,
		},
		{
			name: "remove all removes all subpaths",
			args: args{t.TempDir(), []transaction{
				{store, "subpath", "key", foo{Int: 1}},
				{store, "subpath1", "key1", foo{Int: 2}},
				{op: del},
				{load, "subpath", "key", foo{}},
				{load, "subpath1", "key1", foo{}},
			}},
			wantFail: true,
		},
		{
			name: "a store after remove all loads",
			args: args{t.TempDir(), []transaction{
				{store, "subpath", "key", foo{Int: 1}},
				{op: del},
				{store, "subpath", "key", foo{Int: 2}},
				{load, "subpath", "key", foo{Int: 2}},
			}},
		},
		{
			name: "removing subpath does not remove other subpaths",
			args: args{t.TempDir(), []transaction{
				{store, "subpath", "key", foo{Int: 1}},
				{store, "subpath1", "key", foo{Int: 2}},
				{remove, "subpath1", "", foo{}},
				{load, "subpath", "key", foo{Int: 1}},
			}},
		},
		{
			name: "error when path is not a directory",
			args: args{t.TempDir(), []transaction{
				{store, "subpath1", "key2", foo{Int: -1}},
				{load, "subpath1/key2", "key", foo{Int: -1}},
			}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, err := cache.New(tt.args.dir)
			if err != nil {
				if !tt.wantErr {
					t.Errorf("cache.New() error = %v", err)
				}
				return
			}
			t.Cleanup(func() {
				if err := c.Close(); err != nil {
					t.Errorf("cache.Cache.Close() error = %v", err)
				}
			})
			for _, transaction := range tt.args.transactions {
				switch transaction.op {
				case store:
					if err := c.Store(transaction.subpath, transaction.key, &transaction.value); err != nil {
						t.Errorf("cache.Cache.Store() error = %v", err)
						return
					}
				case load:
					var got foo
					if ok, err := c.Load(transaction.subpath, transaction.key, &got); err != nil {
						if !tt.wantErr {
							t.Errorf("cache.Cache.Load() error = %v", err)
						}

						return
					} else if !ok {
						if !tt.wantFail {
							t.Errorf("cache.Cache.Load() did not find data for subpath=%q key=%q", transaction.subpath, transaction.key)
						}

						return
					}

					if diff := cmp.Diff(transaction.value, got); diff != "" {
						t.Errorf("cache.Cache.Load() mismatch (-want +got):\n%s", diff)
					}
				case remove:
					if err := c.DeleteSubpath(transaction.subpath); err != nil {
						t.Errorf("cache.Cache.DeleteSubpath() error = %v", err)
						return
					}
				case del:
					if err := c.DeleteAll(); err != nil {
						t.Errorf("cache.Cache.DeleteAll() error = %v", err)
						return
					}
				}
			}
		})
	}
}

// Test_Cache_Store_concurrent pins that several caches over one directory, opened at once
// as several processes would open them, all store the same key and leave it whole with no
// temporary file behind: each New waits for the lock until the Cache before it is closed,
// so the writers take turns.
func Test_Cache_Store_concurrent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		writers int
		rounds  int
	}{
		{name: "two writers, many rounds", writers: 2, rounds: 200},
		{name: "eight writers", writers: 8, rounds: 50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			value := map[string]int{"a": 1, "b": 2}
			var wg sync.WaitGroup
			errs := make(chan error, tt.writers*(tt.rounds+2))
			for range tt.writers {
				wg.Go(func() {
					c, err := cache.New(dir, cache.WithWaitOutput(io.Discard))
					if err != nil {
						errs <- err

						return
					}
					for range tt.rounds {
						if err := c.Store("sub", "key", value); err != nil {
							errs <- err
						}
					}
					if err := c.Close(); err != nil {
						errs <- err
					}
				})
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				t.Errorf("concurrent writer error = %v", err)
			}
			c, err := cache.New(dir)
			if err != nil {
				t.Fatalf("cache.New() error = %v", err)
			}
			defer c.Close()
			var got map[string]int
			if found, err := c.Load("sub", "key", &got); err != nil || !found {
				t.Fatalf("Load() = %v, %v; want found", found, err)
			}
			if got["a"] != 1 || got["b"] != 2 {
				t.Errorf("Load() = %v, want %v", got, value)
			}
			entries, err := os.ReadDir(filepath.Join(dir, ".ccc-cache", "sub"))
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if e.Name() != "key" {
					t.Errorf("stray file %q left in the key's directory", e.Name())
				}
			}
		})
	}
}

// waitWriter records what New prints while it waits for the lock and closes printed on
// the first write, so a test knows New has reached the wait. It is safe to read while New
// writes.
type waitWriter struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	once    sync.Once
	printed chan struct{}
}

func newWaitWriter() *waitWriter {
	return &waitWriter{printed: make(chan struct{})}
}

func (w *waitWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	defer w.once.Do(func() {
		close(w.printed)
	})
	// A bytes.Buffer's Write always answers a nil error.
	n, _ := w.buf.Write(p)

	return n, nil
}

func (w *waitWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.buf.String()
}

// Test_New_lock pins the lock New takes on the cache root: a second New over a root
// another Cache holds prints one line naming the root and returns only after the holder's
// Close, a root nobody holds opens at once and prints nothing, and a holder that emptied
// the root with DeleteAll still holds the lock.
func Test_New_lock(t *testing.T) {
	t.Parallel()

	const timeout = 10 * time.Second

	tests := []struct {
		name      string
		held      bool
		deleteAll bool
	}{
		{name: "a root nobody holds opens at once and prints nothing"},
		{name: "a held root waits for the holder's Close and says so once", held: true},
		{name: "a root emptied by DeleteAll stays held", held: true, deleteAll: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			var holder *cache.Cache
			if tt.held {
				c, err := cache.New(dir)
				if err != nil {
					t.Fatalf("cache.New() for the holder error = %v", err)
				}
				holder = c
				if err := c.Store("sub", "key", "value"); err != nil {
					t.Fatalf("cache.Cache.Store() error = %v", err)
				}
				if tt.deleteAll {
					if err := c.DeleteAll(); err != nil {
						t.Fatalf("cache.Cache.DeleteAll() error = %v", err)
					}
				}
			}

			out := newWaitWriter()
			type opened struct {
				c   *cache.Cache
				err error
			}
			done := make(chan opened, 1)
			go func() {
				c, err := cache.New(dir, cache.WithWaitOutput(out))
				done <- opened{c, err}
			}()

			if holder != nil {
				select {
				case <-out.printed:
				case <-done:
					t.Fatal("cache.New() returned while another Cache held the root")
				case <-time.After(timeout):
					t.Fatal("cache.New() printed no wait line while another Cache held the root")
				}
				select {
				case <-done:
					t.Fatal("cache.New() returned before the holder's Close")
				case <-time.After(100 * time.Millisecond):
				}
				if err := holder.Close(); err != nil {
					t.Fatalf("cache.Cache.Close() for the holder error = %v", err)
				}
			}

			var second opened
			select {
			case second = <-done:
			case <-time.After(timeout):
				t.Fatal("cache.New() did not return")
			}
			if second.err != nil {
				t.Fatalf("cache.New() error = %v", second.err)
			}
			if err := second.c.Close(); err != nil {
				t.Errorf("cache.Cache.Close() error = %v", err)
			}

			var want string
			if tt.held {
				want = fmt.Sprintf("waiting for another process using the cache at %s\n", filepath.Join(dir, ".ccc-cache"))
			}
			if diff := cmp.Diff(want, out.String()); diff != "" {
				t.Errorf("cache.New() printed (-want +got):\n%s", diff)
			}
		})
	}
}

// Test_Cache_Keys pins that Keys lists the files at a subpath and leaves out the lock
// file at the cache root, which is not a key.
func Test_Cache_Keys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		stored  []string
		subpath string
		want    []string
	}{
		{name: "keys at the root leave out the lock file", stored: []string{"a", "b"}, subpath: ".", want: []string{"a", "b"}},
		{name: "an empty root lists nothing", subpath: "."},
		{name: "keys at a subpath", stored: []string{"a", "b"}, subpath: "sub", want: []string{"a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c, err := cache.New(t.TempDir())
			if err != nil {
				t.Fatalf("cache.New() error = %v", err)
			}
			defer c.Close()
			for _, key := range tt.stored {
				if err := c.Store(tt.subpath, key, key); err != nil {
					t.Fatalf("cache.Cache.Store() error = %v", err)
				}
			}
			keys, err := c.Keys(tt.subpath)
			if err != nil {
				t.Fatalf("cache.Cache.Keys() error = %v", err)
			}
			got := slices.Sorted(keys)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("cache.Cache.Keys() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
