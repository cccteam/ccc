// Package main is the tugs site generator (test fixture). It reads its own migrations,
// which the multi-site check must flag, and the shared generator writes no TypeScript
// into its browser app.
package main

import (
	"context"

	"github.com/cccteam/ccc/resource/generation"
)

func main() {
	generator, err := generation.NewResourceGenerator(
		context.Background(),
		"./apps/tugs/pkg/resources",
		[]string{"file://apps/tugs/schema/migrations"},
		[]string{"example.com/harbor/apps/tugs/pkg/resources"},
		generation.GenerateHandlers("apps/tugs/app"),
		generation.GenerateRoutes("apps/tugs/pkg/router", "api"),
		generation.WithSpannerEmulatorVersion("1.5.44"),
		generation.GenerateTypescript("apps/tugs/gui/src/app/core/service",
			generation.GenerateEnums(),
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
