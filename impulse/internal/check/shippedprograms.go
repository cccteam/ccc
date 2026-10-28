package check

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/cccteam/ccc/impulse/app"
)

// shippedPrograms verifies that every program a release ships (ShipsProgram on an outlet)
// is a main package on a machine outlet that builds for each platform it declares. The
// image build compiles the programs from the release file after the tag, so a program
// that fails here fails the version build, or ships a file no copy can run: the outlet
// declares Tokens, since a session outlet serves no programs; the directory holds a
// package main; and go build succeeds for each platform with CGO disabled, as the image
// build compiles it.
type shippedPrograms struct{}

func (shippedPrograms) Name() string { return "shipped-programs" }

func (shippedPrograms) Describe() string {
	return "every shipped program is a main package on a machine outlet that builds for each platform it declares"
}

func (shippedPrograms) Meaning() string {
	return "A machine outlet ships a program with the release: ShipsProgram(dir, Platforms(...)) beside Tokens names the program's main package and the platforms, the image build compiles it for each, the outlet serves the file with its checksum at GET /<prefix>/programs/<name>/<release>/<os>/<arch> behind its token check, and an installed copy upgrades itself from there through resource/shipped. A session outlet serves no programs, and a program that does not build for a platform cannot ship."
}

func (c shippedPrograms) Run(ctx context.Context, env *Env) Result {
	p := env.App.Profile()
	var details []string
	programs, failed := 0, 0
	for i := range p.Sites {
		outlets := p.Sites[i].AllOutlets()
		for j := range outlets {
			o := &outlets[j]
			for k := range o.Programs {
				programs++
				if problems := c.program(ctx, env, o.Name, o.Tokens != "", &o.Programs[k]); len(problems) > 0 {
					failed++
					details = append(details, problems...)
				}
			}
		}
	}
	switch {
	case programs == 0:
		return pass(c.Name(), "no outlet ships a program")
	case failed > 0:
		return fail(c.Name(), fmt.Sprintf("%d of %d shipped program(s) cannot ship as declared", failed, programs), details...)
	}

	return pass(c.Name(), fmt.Sprintf("%d shipped program(s) build for their platforms", programs))
}

// program verifies one declaration and reports what is wrong with it: the outlet is no
// machine outlet, no main package is at the directory, or a platform's build failed,
// with the compiler's output under it.
func (shippedPrograms) program(ctx context.Context, env *Env, outlet string, machine bool, program *app.Program) []string {
	switch {
	case !machine:
		return []string{fmt.Sprintf("%s: outlet %s ships %s and declares no Tokens: a session outlet serves no programs; a machine outlet, declared with Tokens(importPath), serves the programs the release ships behind its token check", program.Pos, outlet, program.Dir)}
	case !slices.Contains(env.App.MainPackages, program.Dir):
		return []string{fmt.Sprintf("%s: outlet %s ships %s, and no main package is there: ShipsProgram names the root-relative directory of the program's main package", program.Pos, outlet, program.Dir)}
	}

	var problems []string
	for _, platform := range program.Platforms {
		goos, goarch, _ := strings.Cut(platform, "/")
		env.notef("building %s for %s", program.Dir, platform)
		// The build's output is discarded: the check proves the program compiles, and the
		// image build makes the file that ships.
		out, err := env.Exec.Run(ctx, env.App.Root, []string{"GOOS=" + goos, "GOARCH=" + goarch, "CGO_ENABLED=0"}, "go", "build", "-o", os.DevNull, "./"+program.Dir)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %s does not build for %s:", program.Pos, program.Dir, platform))
			problems = append(problems, outputLines(out, 20)...)
		}
	}

	return problems
}
