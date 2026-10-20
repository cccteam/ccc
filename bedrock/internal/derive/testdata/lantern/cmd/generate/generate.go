// Package generate produces all generated code for the application: one generator per site, then
// the shared generator whose TypeScript reaches every site.
package generate

//go:generate go run ./consolegenerator
//go:generate go run ./portalgenerator
//go:generate go run ./sharedgenerator
