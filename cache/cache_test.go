package cache_test

import (
	"testing"

	"github.com/cccteam/ccc/cache"
	"github.com/google/go-cmp/cmp"
)

func Test_Cache(t *testing.T) {
	type foo struct {
		Int    int
		String string
		Bool   bool
	}
	type args struct {
		dir     string
		subpath string
		key     string
		want    foo
	}
	tests := []struct {
		name     string
		args     args
		wantFail bool
	}{
		{
			name:     "store and load a struct correctly",
			args:     args{dir: t.TempDir(), subpath: "test", key: "some_data", want: foo{2, "2", true}},
			wantFail: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := cache.New(tt.args.dir)
			if err := c.Store(tt.args.subpath, tt.args.key, &tt.args.want); err != nil {
				t.Errorf("cache.Cache.Store() error = %v", err)
				return
			}

			var got foo

			if ok, err := c.Load(tt.args.subpath, tt.args.key, &got); err != nil {
				t.Errorf("cache.Cache.Load() error = %v", err)
			} else if !ok {
				t.Errorf("cache.Cache.Load() did not find data for subpath=%q key=%q", tt.args.subpath, tt.args.key)
				return
			}

			if diff := cmp.Diff(tt.args.want, got); diff != "" {
				t.Errorf("cache.Cache.Load() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
