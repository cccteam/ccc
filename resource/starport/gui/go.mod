// This file exists solely as a Go module boundary: it stops Go tooling
// (go build/test/vet ./..., golangci-lint) from descending into the Angular
// application's tree, where npm dependencies can ship stray Go files
// (e.g. flatted's golang port). The gui is not a Go module in any real sense
// and contains no Go code.
module github.com/cccteam/ccc/resource/starport/gui

go 1.26.6
