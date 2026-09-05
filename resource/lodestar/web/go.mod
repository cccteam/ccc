// This file exists solely as a Go module boundary: it stops Go tooling
// (go build/test/vet ./..., golangci-lint) from descending into the Angular
// workspace's tree, where npm dependencies can ship stray Go files
// (e.g. flatted's golang port). web/ is not a Go module in any real sense
// and contains no Go code. One fence covers both browser applications
// (console/ and portal/), which share this workspace's node_modules.
module github.com/cccteam/ccc/resource/lodestar/web

go 1.26.6
