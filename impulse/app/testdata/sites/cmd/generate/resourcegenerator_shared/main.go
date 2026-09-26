// Package main is the shared resource generator (test fixture).
package main

import (
	"context"

	"github.com/cccteam/ccc/resource/generation"
)

func main() {
	generator, err := generation.NewResourceGenerator(
		context.Background(),
		"./pkg/sharedresources",
		[]string{"file://schema/migrations"},
		[]string{"example.com/harbor/pkg/sharedresources"},
		generation.WithSpannerEmulatorVersion("1.5.44"),
		generation.GenerateTypescript("apps/pilots/gui/src/app/core/service/shared-resources",
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
