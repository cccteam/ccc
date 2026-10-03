package cache_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

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

// Test_Cache_Store_concurrent pins that two caches over one directory (two processes
// storing the same content-addressed key at once) both succeed and leave the key whole:
// each writer renames its own temporary file over the key, so neither removes a file the
// other is writing.
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
			caches := make([]*cache.Cache, tt.writers)
			for i := range caches {
				c, err := cache.New(dir)
				if err != nil {
					t.Fatalf("cache.New() error = %v", err)
				}
				caches[i] = c
			}
			value := map[string]int{"a": 1, "b": 2}
			var wg sync.WaitGroup
			errs := make(chan error, tt.writers*tt.rounds)
			for _, c := range caches {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for range tt.rounds {
						if err := c.Store("sub", "key", value); err != nil {
							errs <- err
						}
					}
				}()
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				t.Errorf("Store() error = %v", err)
			}
			var got map[string]int
			if found, err := caches[0].Load("sub", "key", &got); err != nil || !found {
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
