// Package main is a generator program the tool cannot read completely (test fixture).
package main

import (
	"context"
	"os"

	"github.com/cccteam/ccc/resource/generation"
)

func main() {
	handlersDir := os.Getenv("HANDLERS_DIR")
	generator, err := generation.NewResourceGenerator(
		context.Background(),
		"pkg/resources",
		[]string{"file://schema/migrations"},
		[]string{"example.com/badprogram/pkg/resources"},
		generation.GenerateHandlers(handlersDir),
		generation.GenerateRoutes("pkg/router"),
		generation.WithFrobnicator("x"),
		generation.GenerateEnums(),
		generation.WithConsolidatedHandlers("resources", "yes"),
		generation.GenerateTypescript("gui/src/app/core/service", generation.WithRPC("pkg/rpc")),
	)
	if err != nil {
		panic(err)
	}
	defer generator.Close()

	if err := generator.Generate(); err != nil {
		panic(err)
	}
}
