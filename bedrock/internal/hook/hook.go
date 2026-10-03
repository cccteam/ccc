// Package hook names the stages of the deploy sequence where an application's own work
// runs, and where that work is found. A hook is a script the application commits at
// infrastructure/hooks/<stage>.sh; the pipeline has a step for each stage the
// application has a script for, and bedrock deploy hook <stage> runs it.
package hook

import (
	"os"
	"path/filepath"

	"github.com/go-playground/errors/v5"
)

// Stage is one point in the deploy sequence where an application's hook runs.
type Stage string

// The stages, in the order the pipeline reaches them. after-down runs only on a
// teardown, and the others never do.
const (
	AfterDown     Stage = "after-down"
	BeforeBuild   Stage = "before-build"
	BeforeMigrate Stage = "before-migrate"
	AfterMigrate  Stage = "after-migrate"
	BeforeTraffic Stage = "before-traffic"
	AfterTraffic  Stage = "after-traffic"
)

// Stages are every stage, in the pipeline's order.
var Stages = []Stage{AfterDown, BeforeBuild, BeforeMigrate, AfterMigrate, BeforeTraffic, AfterTraffic}

// Dir is where the scripts are, relative to the application root: the root the
// pipeline's checkout is.
const Dir = "infrastructure/hooks"

// Script is the stage's script, relative to the application root.
func (s Stage) Script() string {
	return Dir + "/" + string(s) + ".sh"
}

// Valid reports whether s is one of the stages.
func (s Stage) Valid() bool {
	for _, known := range Stages {
		if s == known {
			return true
		}
	}

	return false
}

// Scripts are the stages the application at root has a script for, in the pipeline's
// order.
func Scripts(root string) ([]Stage, error) {
	var stages []Stage
	for _, s := range Stages {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(s.Script())))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}

			return nil, errors.Wrap(err, "os.Stat()")
		}
		if !info.IsDir() {
			stages = append(stages, s)
		}
	}

	return stages, nil
}
