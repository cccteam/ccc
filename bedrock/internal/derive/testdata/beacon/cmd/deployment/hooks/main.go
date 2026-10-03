// Command hooks is beacon's hooks program, for the render and check tests: the stages it
// implements are read from its Hooks literal, the contract imported under a name of its own.
package main

import (
	"context"
	"fmt"

	hooks "github.com/cccteam/ccc/impulse/deployhook"
)

func main() {
	hooks.Main(hooks.Hooks{
		BeforeMigrate: announce,
		BeforeTraffic: nil,
		AfterTraffic:  announce,
	})
}

func announce(_ context.Context, f *hooks.Facts) error {
	fmt.Println(f.App, f.Release)

	return nil
}
