package cli

import (
	"fmt"
	"io/fs"
	"testing"

	"github.com/go-playground/errors/v5"
)

// TestMessage reads the line an error prints for the user: the outermost wrap's message
// and the cause, the chain's source positions and the runner's own wrap left out; an
// error wrapped nowhere as it is.
func TestMessage(t *testing.T) {
	t.Parallel()

	exited := exitStatus{code: 1, line: "exit status 1"}
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "a command the walk wrapped with the last line of its output, over the runner's wrap of the exit",
			err:  errors.Wrapf(errors.Wrapf(exited, "%s %s", "go", "get github.com/cccteam/ccc/resource@v0.2.0"), "go get %s: %s", "github.com/cccteam/ccc/resource@v0.2.0", "go: module lookup disabled by GOPROXY=off"),
			want: "go get github.com/cccteam/ccc/resource@v0.2.0: go: module lookup disabled by GOPROXY=off: exit status 1",
		},
		{
			name: "the runner's wrap alone",
			err:  errors.Wrapf(exited, "%s %s", "git", "status --porcelain"),
			want: "git status --porcelain: exit status 1",
		},
		{
			name: "a file operation wrapped with its name",
			err:  errors.Wrap(fs.ErrNotExist, "os.ReadFile()"),
			want: "os.ReadFile(): file does not exist",
		},
		{
			name: "a context over a message of the tool's own",
			err:  errors.Wrapf(errors.Newf("the marker is in neither form"), "recipe %s", "marker"),
			want: "recipe marker: the marker is in neither form",
		},
		{
			name: "a message of the tool's own, wrapped nowhere",
			err:  errors.Newf("the working tree is not clean (%d path(s))", 2),
			want: "the working tree is not clean (2 path(s))",
		},
		{
			name: "a plain error",
			err:  fmt.Errorf("unknown flag: --staged"),
			want: "unknown flag: --staged",
		},
		{
			name: "nil",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := message(tt.err); got != tt.want {
				t.Errorf("message() = %q, want %q", got, tt.want)
			}
		})
	}
}
