package handoff

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/internal/app"
)

// Snapshot is the state an agent must not move: the generator programs' option sets and
// the lint configurations. Everything else the agent may change is judged by the check.
type Snapshot struct {
	// Programs maps each generator program file to its program rendered as text: the
	// positional arguments and every option call with its literal arguments, in order.
	Programs map[string][]string
	// Configs maps each lint configuration file to its content hash, or "" when absent.
	Configs map[string]string
}

// Reader reads one root-relative file, returning os.ErrNotExist when it is absent.
type Reader func(rel string) ([]byte, error)

// FromTree reads files from the working tree.
func FromTree(a *app.App) Reader {
	return func(rel string) ([]byte, error) {
		data, err := os.ReadFile(a.Abs(rel))
		if err != nil {
			return nil, errors.Wrap(err, "os.ReadFile()")
		}

		return data, nil
	}
}

// GolangciConfig is the usual name of the Go lint configuration at the application root.
const GolangciConfig = ".golangci.yml"

// lintConfigs are the lint configuration files at the application root.
var lintConfigs = []string{GolangciConfig, ".golangci.yaml", ".golangci.toml", ".golangci.json"}

// eslintConfigs are the lint configuration files of a browser app.
var eslintConfigs = []string{
	"eslint.config.js", "eslint.config.mjs", "eslint.config.cjs",
	"eslint.config.ts", "eslint.config.mts", "eslint.config.cts", ".eslintrc.json", ".eslintrc.js", ".eslintrc.cjs", ".eslintignore",
}

// Take reads the guardrails with the reader: the generator programs the application has
// (those in the tree, so a program the agent adds or removes shows as a difference) and
// the lint configuration files at the root and in each browser app.
func Take(a *app.App, read Reader) (Snapshot, error) {
	s := Snapshot{Programs: map[string][]string{}, Configs: map[string]string{}}
	for _, g := range a.Generators {
		src, err := read(g.File)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return Snapshot{}, err
		}
		parsed, err := app.ParseGenerator(g.File, src)
		if err != nil {
			return Snapshot{}, err
		}
		if parsed == nil {
			continue
		}
		s.Programs[g.File] = renderProgram(parsed)
	}
	for _, rel := range configFiles(a) {
		data, err := read(rel)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return Snapshot{}, err
		}
		sum := sha256.Sum256(data)
		s.Configs[rel] = hex.EncodeToString(sum[:])
	}

	return s, nil
}

// Compare reports every guardrail that differs between two snapshots, as findings.
func Compare(before, after Snapshot) []string {
	var findings []string
	for _, file := range union(before.Programs, after.Programs) {
		b, inBefore := before.Programs[file]
		a, inAfter := after.Programs[file]
		switch {
		case !inBefore:
			findings = append(findings, fmt.Sprintf("%s: a generator program was added", file))
		case !inAfter:
			findings = append(findings, fmt.Sprintf("%s: the generator program was removed", file))
		default:
			findings = append(findings, programDiff(file, b, a)...)
		}
	}
	for _, file := range union(before.Configs, after.Configs) {
		b, inBefore := before.Configs[file]
		a, inAfter := after.Configs[file]
		switch {
		case !inBefore:
			findings = append(findings, fmt.Sprintf("%s: a lint configuration was added", file))
		case !inAfter:
			findings = append(findings, fmt.Sprintf("%s: the lint configuration was removed", file))
		case a != b:
			findings = append(findings, fmt.Sprintf("%s: the lint configuration was edited", file))
		}
	}

	return findings
}

// programDiff lists the option lines one program lost and gained.
func programDiff(file string, before, after []string) []string {
	var findings []string
	had := map[string]int{}
	for _, line := range before {
		had[line]++
	}
	for _, line := range after {
		if had[line] > 0 {
			had[line]--

			continue
		}
		findings = append(findings, fmt.Sprintf("%s: added %s", file, line))
	}
	lines := make([]string, 0, len(had))
	for line, n := range had {
		if n > 0 {
			lines = append(lines, line)
		}
	}
	sort.Strings(lines)
	for _, line := range lines {
		findings = append(findings, fmt.Sprintf("%s: removed %s", file, line))
	}

	return findings
}

func union[V any](a, b map[string]V) []string {
	seen := map[string]bool{}
	var keys []string
	for k := range a {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	for k := range b {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	return keys
}

// programFiles lists the snapshot's generator programs.
func (s Snapshot) programFiles() []string { return sortedKeys(s.Programs) }

// configFiles lists the snapshot's lint configurations.
func (s Snapshot) configFiles() []string { return sortedKeys(s.Configs) }

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	return keys
}

// renderProgram renders a program as one line per positional argument and option call,
// with literal arguments spelled out, so two programs compare by what they configure.
func renderProgram(g *app.Generator) []string {
	lines := make([]string, 0, 3+len(g.Options))
	lines = append(lines,
		"resources "+strconv.Quote(g.ResourcePackageDir),
		"migrations "+renderList(g.MigrationSources),
		"packages "+renderList(g.LocalPackages),
	)
	for i := range g.Options {
		lines = append(lines, renderCall(&g.Options[i]))
	}

	return lines
}

func renderCall(c *app.Call) string {
	args := make([]string, 0, len(c.Args))
	for i := range c.Args {
		args = append(args, renderArg(&c.Args[i]))
	}

	return c.Name + "(" + strings.Join(args, ", ") + ")"
}

func renderArg(a *app.Arg) string {
	switch a.Kind {
	case app.ArgString:
		return strconv.Quote(a.Str)
	case app.ArgBool:
		return strconv.FormatBool(a.Bool)
	case app.ArgStringList:
		return renderList(a.List)
	case app.ArgStringMap, app.ArgBoolMap:
		keys := sortedKeys(a.Map)
		pairs := make([]string, 0, len(keys))
		for _, k := range keys {
			v := a.Map[k]
			if a.Kind == app.ArgStringMap {
				v = strconv.Quote(v)
			}
			pairs = append(pairs, strconv.Quote(k)+": "+v)
		}

		return "{" + strings.Join(pairs, ", ") + "}"
	case app.ArgCall:
		return renderCall(a.Call)
	case app.ArgComposite, app.ArgOther:
		return a.Text
	default:
		return a.Text
	}
}

func renderList(items []string) string {
	quoted := make([]string, 0, len(items))
	for _, item := range items {
		quoted = append(quoted, strconv.Quote(item))
	}

	return "[" + strings.Join(quoted, ", ") + "]"
}

// configFiles lists the lint configuration files the application may have: the Go lint
// configuration at the root and the eslint configuration of each browser app. Absent
// files are listed too, so a file the agent adds shows as a difference.
func configFiles(a *app.App) []string {
	files := append([]string{}, lintConfigs...)
	for _, w := range a.WebApps {
		for _, name := range eslintConfigs {
			files = append(files, path.Join(w.Dir, name))
		}
	}

	return files
}
