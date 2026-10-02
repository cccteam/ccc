// bedrock.go runs bedrock's generate-time step before the application's generators, so
// they read the migrations as the pipeline will: the migrations this branch added move to
// follow the default branch's highest index with no gap, up and down together, keeping
// their order (bedrock migration renumber; a committed migration is never touched). The
// file sorts before the application's own, which is what runs it first. The step runs the
// bedrock the placement pins, built by the Go toolchain from the module proxy the first
// time and cached after, so go generate needs no bedrock installed, here or in CI; it
// reads the default branch from origin's copy, so fetch first. Rendered by bedrock and
// owned by it: bedrock render rewrites this file and bedrock check compares it with the
// code, so a change belongs in bedrock, never here.

package generate

//go:generate go run github.com/cccteam/ccc/bedrock@v0.1.0 migration renumber
