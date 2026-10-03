package cli

import (
	"runtime/debug"
	"testing"
)

func TestBuildOf(t *testing.T) {
	t.Parallel()

	const (
		commit = "v0.0.0-lab.1.0.20260928222237-58b211dce544"
		sum    = "h1:bm90LWEtcmVhbC1zdW0tZm9yLXRoZS10ZXN0cyE="
	)
	checkout := []debug.BuildSetting{{Key: "vcs", Value: "git"}, {Key: "vcs.revision", Value: "58b211dce54409b23306e30f73f3444796ed561b"}}
	tests := []struct {
		name    string
		stamped string
		info    *debug.BuildInfo
		want    build
		// wantHeld is whether the build must be the pinned one to render.
		wantHeld bool
	}{
		{
			name: "a stamped release", stamped: "v0.4.0",
			info:     &debug.BuildInfo{Main: debug.Module{Path: "github.com/cccteam/ccc/bedrock", Version: "v0.4.0"}, Settings: checkout},
			want:     build{version: "v0.4.0", kind: buildStamped},
			wantHeld: true,
		},
		{
			name:     "an installed commit",
			info:     &debug.BuildInfo{Main: debug.Module{Path: "github.com/cccteam/ccc/bedrock", Version: commit, Sum: sum}},
			want:     build{version: commit, kind: buildInstalled},
			wantHeld: true,
		},
		{
			name:     "an installed release",
			info:     &debug.BuildInfo{Main: debug.Module{Path: "github.com/cccteam/ccc/bedrock", Version: "v0.4.0", Sum: sum}},
			want:     build{version: "v0.4.0", kind: buildInstalled},
			wantHeld: true,
		},
		{
			name: "a checkout at a commit",
			info: &debug.BuildInfo{Main: debug.Module{Path: "github.com/cccteam/ccc/bedrock", Version: commit}, Settings: checkout},
			want: build{version: commit, kind: buildCheckout},
		},
		{
			name: "a dirty checkout",
			info: &debug.BuildInfo{Main: debug.Module{Path: "github.com/cccteam/ccc/bedrock", Version: commit + "+dirty"}, Settings: checkout},
			want: build{version: commit + "+dirty", kind: buildCheckout},
		},
		{
			name:     "a clean checkout at a release tag",
			info:     &debug.BuildInfo{Main: debug.Module{Path: "github.com/cccteam/ccc/bedrock", Version: "v0.4.0"}, Settings: checkout},
			want:     build{version: "v0.4.0", kind: buildCheckout},
			wantHeld: true,
		},
		{
			name: "devel",
			info: &debug.BuildInfo{Main: debug.Module{Path: "github.com/cccteam/ccc/bedrock", Version: "(devel)"}},
			want: build{version: "(devel)", kind: buildDevel},
		},
		{
			name: "no build info",
			want: build{version: "(devel)", kind: buildDevel},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := buildOf(tt.stamped, tt.info)
			if got != tt.want {
				t.Errorf("buildOf() = %+v, want %+v", got, tt.want)
			}
			if held := got.heldToPin(); held != tt.wantHeld {
				t.Errorf("heldToPin() = %v, want %v", held, tt.wantHeld)
			}
		})
	}
}
