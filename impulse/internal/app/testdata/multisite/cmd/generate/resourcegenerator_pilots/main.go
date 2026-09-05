// Package main is the pilots site generator (test fixture).
package main

import (
	"context"

	gen "github.com/cccteam/ccc/resource/generation"
)

func main() {
	generator, err := gen.NewResourceGenerator(
		context.Background(),
		"./apps/pilots/pkg/resources",
		[]string{"file://schema/migrations"},
		[]string{"example.com/harbor/apps/pilots/pkg/resources"},
		gen.GenerateHandlers("apps/pilots/app"),
		gen.GenerateRoutes("apps/pilots/pkg/router", "api"),
		gen.WithSpannerEmulatorVersion("1.5.44"),
		gen.GenerateTypescript("apps/pilots/gui/src/app/core/service",
			gen.GenerateMetadata(),
			gen.GenerateEnums(),
		),
	)
	if err != nil {
		panic(err)
	}
	defer generator.Close()

	if err := generator.Generate(); err != nil {
		panic(err)
	}
}
