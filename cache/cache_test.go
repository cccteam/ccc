package cache_test

import (
	"testing"

	"github.com/cccteam/ccc/cache"
	"github.com/google/go-cmp/cmp"
)

type op int

const (
	load op = 1 << iota
	store
	remove
	delete
)

func Test_Cache(t *testing.T) {
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
				{op: delete},
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
				case delete:
					if err := c.DeleteAll(); err != nil {
						t.Errorf("cache.Cache.DeleteAll() error = %v", err)
						return
					}
				}
			}
		})
	}
}
