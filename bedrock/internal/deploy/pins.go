// pins.go tests the secret pins a release carries: the secrets the code in the checkout
// declares against the versions the stack's placement (terraform.tfvars, secret_versions)
// pins for the environment. A secret with no pin has its container and is mounted nowhere,
// so the process starts without the variable: "not yet", which the first environment of the
// promotion order allows, since a value goes in there ahead of the release that first reads
// it. In every environment after it a release with an unpinned secret is refused at its
// plan, naming each unpinned variable and the pin to make; a pull request's plan says the
// same in the environment's summary, so the pin lands before the release reaches the
// environment. The declared secrets are read from the code as bedrock check and bedrock
// render read them, and the pins from terraform.tfvars as bedrock secret pin writes them,
// so neither reading depends on how the stack files were rendered.

package deploy

import (
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/secret"
)

// unpinnedSecrets are the secrets the code declares (the model's, in declaration order)
// that the stack's placement in the checkout pins no version for in env: what env's
// processes would start without. A generated secret is never among them: its version is
// derived at the plan (generated.go), and the placement carries the word for it.
func unpinnedSecrets(w Workspace, m *derive.Model, env string) ([]string, error) {
	pinned, err := secret.PinnedVariables(filepath.Join(string(w), stackDir), env)
	if err != nil {
		return nil, err
	}
	var unpinned []string
	for _, s := range m.Secrets {
		if !s.Generated && !slices.Contains(pinned, s.Variable.Name) {
			unpinned = append(unpinned, s.Variable.Name)
		}
	}

	return unpinned, nil
}

// pinnable counts the secrets the code declares that someone pins: every one but the
// generated.
func pinnable(m *derive.Model) int {
	n := 0
	for _, s := range m.Secrets {
		if !s.Generated {
			n++
		}
	}

	return n
}

// testPins is the release's own test in a tag build, run before the plan so that a refused
// release changes nothing (a restore run drops and restores the database before its plan):
// in the first environment an unpinned secret is said and allowed; in any other it refuses
// the release, naming each unpinned variable and the pin to make; every secret pinned is
// said. It answers the application in the checkout, which the plan reads on.
func testPins(w Workspace, env string, out io.Writer) (*derive.Model, error) {
	p, err := checkoutPlacement(w, "the promotion order is written")
	if err != nil {
		return nil, err
	}
	m, err := checkoutModel(w, p)
	if err != nil {
		return nil, err
	}
	unpinned, err := unpinnedSecrets(w, m, env)
	if err != nil {
		return nil, err
	}
	switch {
	case len(unpinned) == 0 && pinnable(m) > 0:
		fmt.Fprintf(out, "Secret pins: every secret the code declares has a version pinned in %s (%s in %s).\n", env, pinsKey(env), stackTfvars)
	case len(unpinned) == 0:
	case env == p.Integration():
		fmt.Fprintf(out, "%s has no pinned version for %s (%s in %s): the first environment runs without %s, \"not yet\"; a release to the environments after it is refused until %s is pinned there.\n", env, strings.Join(unpinned, ", "), pinsKey(env), stackTfvars, them(unpinned), each(unpinned))
	default:
		return nil, errors.Newf("%s%s", rejected, pinRefusal(env, p.Integration(), unpinned))
	}

	return m, nil
}

// pinSummary is what an environment's summary says of its secret pins on a pull request:
// the notice for an environment after the first that has no pinned version for a secret
// the code declares, said in the log too; empty otherwise, and always for the first
// environment, which runs without a pin.
func pinSummary(w Workspace, m *derive.Model, env string, out io.Writer) (string, error) {
	unpinned, err := unpinnedSecrets(w, m, env)
	if err != nil || len(unpinned) == 0 || env == m.Placement.Integration() {
		return "", err
	}
	notice := pinNotice(env, unpinned)
	fmt.Fprintf(out, "%s: %s\n", env, notice)

	return " " + notice, nil
}

// pinRefusal says why a tag build refuses the release in env: the secrets the code
// declares that env has no pinned version for, in declaration order, and the pin to make
// for each; first is the first environment, the one that runs without a pin.
func pinRefusal(env, first string, unpinned []string) string {
	noun := "secrets"
	if len(unpinned) == 1 {
		noun = "a secret"
	}

	return fmt.Sprintf("the code declares %s %s has no pinned version for (%s in %s): %s. %s's processes would start without %s, so the release is refused here; pin %s after an operator adds its value (%s): %s. The pin promotes as a release; %s, the first environment, alone runs without one (\"not yet\").", noun, env, pinsKey(env), stackTfvars, strings.Join(unpinned, ", "), env, them(unpinned), each(unpinned), addCommand(env, unpinned), pinCommands(env, unpinned), first)
}

// pinNotice is the pull request's line for env: the secrets the code declares that env has
// no pinned version for, in declaration order, that the release is refused there until each
// is pinned, and the pin to make for each.
func pinNotice(env string, unpinned []string) string {
	have := "have"
	if len(unpinned) == 1 {
		have = "has"
	}

	return fmt.Sprintf("%s %s no pinned version in %s (%s in %s); the release is refused there until %s is pinned (%s, after %s has put the value in place).", strings.Join(unpinned, ", "), have, env, pinsKey(env), stackTfvars, each(unpinned), pinCommands(env, unpinned), addCommand(env, unpinned))
}

// them is the pronoun for the unpinned secrets: "it" for one, "them" for several.
func them(unpinned []string) string {
	if len(unpinned) == 1 {
		return "it"
	}

	return "them"
}

// each is the word before a verb that applies to every unpinned secret: "it" for one,
// "each" for several ("pin it", "until each is pinned").
func each(unpinned []string) string {
	if len(unpinned) == 1 {
		return "it"
	}

	return "each"
}

// pinCommands are the pins to make, one bedrock secret pin per variable, comma-separated.
func pinCommands(env string, unpinned []string) string {
	commands := make([]string, 0, len(unpinned))
	for _, variable := range unpinned {
		commands = append(commands, "bedrock secret pin "+env+" "+variable+" <version>")
	}

	return strings.Join(commands, ", ")
}

// addCommand is the bedrock secret add that puts a value in place ahead of its pin: the one
// variable's, or with the placeholder when several are unpinned.
func addCommand(env string, unpinned []string) string {
	variable := "<VARIABLE>"
	if len(unpinned) == 1 {
		variable = unpinned[0]
	}

	return "bedrock secret add " + env + " " + variable
}

// pinsKey is the environment's map of pins in the placement, for messages.
func pinsKey(env string) string {
	return "secret_versions." + env
}
