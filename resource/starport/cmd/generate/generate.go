// Package generate produces all generated code for the starport.
package generate

//go:generate go run ./resourcegenerator
//go:generate go run go.uber.org/mock/mockgen -source ../../pkg/router/router.go -destination ../../pkg/mock/mock_router/mock_handlers.go
