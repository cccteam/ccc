// Package generate produces all generated code for harbor. The tugs generator is left
// out, which the options check reports.
package generate

//go:generate go run ./resourcegenerator_pilots
//go:generate go run ./resourcegenerator_shared
