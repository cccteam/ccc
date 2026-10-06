// stages.go checks the Dockerfile's two reserved stages, go-modules and web-packages. The
// image build exports their layers alone to the registry's cache, where every
// environment's build of a later commit reads them, so a reserved stage holds nothing but
// its install: the lockfile copy and the one command whose result the lockfile pins. A
// stage holding more would put a compiled or copied file, or one of the pipeline's
// per-build files, into a layer shared across environments.

package check

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/go-playground/errors/v5"
)

// The reserved stages of the seeded Dockerfile: the Go module download and the browser
// package install, which the image build caches per commit.
const (
	GoModulesStage   = "go-modules"
	WebPackagesStage = "web-packages"
)

// ReservedStages lists the two, in the order the image build builds them.
var ReservedStages = []string{GoModulesStage, WebPackagesStage}

// The instruction keywords the scan reads, as Dockerfile writes them.
const (
	keywordFrom    = "FROM"
	keywordCopy    = "COPY"
	keywordAdd     = "ADD"
	keywordRun     = "RUN"
	keywordWorkdir = "WORKDIR"
	keywordArg     = "ARG"
)

// Stage is one build stage of a Dockerfile: its name (empty for a stage without AS), the
// line of its FROM, the image or earlier stage its FROM names (Base), and the
// instructions after it.
type Stage struct {
	Name         string
	Line         int
	Base         string
	Instructions []Instruction
}

// Instruction is one Dockerfile instruction: its line, its keyword in upper case, and
// its arguments with continuation lines joined.
type Instruction struct {
	Line    int
	Keyword string
	Args    string
}

// A reserved stage's allowed shape, by stage: the files its copy instructions may name
// (by base name) and the one command it runs.
var reserved = map[string]struct {
	files   []string
	command string
	what    string
}{
	GoModulesStage:   {files: []string{"go.mod", "go.sum"}, command: "go mod download", what: "the Go module download"},
	WebPackagesStage: {files: []string{"package.json", "bun.lock", "bunfig.toml", ".npmrc"}, command: "bun install --frozen-lockfile", what: "the browser package install"},
}

// StageFinding is an instruction a reserved stage holds that it must not.
type StageFinding struct {
	// Stage is the reserved stage, Line the instruction's line, Instruction the
	// instruction as written (its first line), and Problem what is wrong with it.
	Stage       string
	Line        int
	Instruction string
	Problem     string
}

// Stages reads the Dockerfile's stages: comment lines are dropped, a line ending in a
// backslash continues on the next, and a keyword is read whatever its case.
func Stages(src []byte) []Stage {
	var stages []Stage
	for _, in := range instructions(src) {
		if in.Keyword == keywordFrom {
			stages = append(stages, Stage{Name: stageName(in.Args), Line: in.Line, Base: stageBase(in.Args)})

			continue
		}
		if len(stages) == 0 {
			// ARG before the first FROM belongs to no stage.
			continue
		}
		last := &stages[len(stages)-1]
		last.Instructions = append(last.Instructions, in)
	}

	return stages
}

// Named is the stage of the name, or nil.
func Named(stages []Stage, name string) *Stage {
	for i := range stages {
		if stages[i].Name == name {
			return &stages[i]
		}
	}

	return nil
}

// Args are the build arguments the stage declares with ARG, in order: each name of an
// ARG instruction, with or without a default (ARG NAME, ARG NAME=value, several on one
// line). A build argument reaches a stage's instructions only once the stage declares it.
func (s *Stage) Args() []string {
	var names []string
	for _, in := range s.Instructions {
		if in.Keyword != keywordArg {
			continue
		}
		for _, field := range fields(in.Args) {
			name, _, _ := strings.Cut(field, "=")
			if name != "" && !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}

	return names
}

// Sees lists the build arguments among args that the named stage can see, in the order
// args gives them: those it declares with ARG, those its FROM line reads (an ARG before
// the first FROM reaches the FROM lines alone), and those any stage it is built on can
// see, the stage its FROM names and every stage it copies from (COPY --from), since what
// such a stage holds depends on their values. None for a stage the Dockerfile lacks.
func Sees(stages []Stage, name string, args []string) []string {
	visited := map[int]bool{}
	seen := map[string]bool{}
	var walk func(at int)
	walk = func(at int) {
		if at < 0 || visited[at] {
			return
		}
		visited[at] = true
		stage := &stages[at]
		declared := stage.Args()
		for _, arg := range args {
			if slices.Contains(declared, arg) || readsArg(stage.Base, arg) {
				seen[arg] = true
			}
		}
		walk(stageIndex(stages[:at], stage.Base))
		for _, in := range stage.Instructions {
			if from, _ := in.Copies(); from != "" {
				walk(stageIndex(stages[:at], from))
			}
		}
	}
	walk(stageIndex(stages, name))
	var names []string
	for _, arg := range args {
		if seen[arg] && !slices.Contains(names, arg) {
			names = append(names, arg)
		}
	}

	return names
}

// stageIndex is the index of the stage a FROM or a COPY --from names among stages: by its
// AS name, or by its number; -1 for an image.
func stageIndex(stages []Stage, ref string) int {
	if ref == "" {
		return -1
	}
	for i := range stages {
		if stages[i].Name != "" && strings.EqualFold(stages[i].Name, ref) {
			return i
		}
	}
	if n, err := strconv.Atoi(ref); err == nil && n >= 0 && n < len(stages) {
		return n
	}

	return -1
}

// readsArg reports whether the text reads the build argument: $NAME or ${NAME}, the
// braces' form with a modifier (${NAME:-default}, ${NAME#prefix}) included.
func readsArg(text, name string) bool {
	return regexp.MustCompile(`\$(?:` + regexp.QuoteMeta(name) + `\b|\{` + regexp.QuoteMeta(name) + `[}:+#%-])`).MatchString(text)
}

// Copies reads a COPY or ADD instruction: the stage or image it copies from (--from, empty
// for the build context) and its sources, the destination left off. Another instruction
// answers nothing.
func (in Instruction) Copies() (from string, sources []string) {
	if in.Keyword != keywordCopy && in.Keyword != keywordAdd {
		return "", nil
	}
	var operands []string
	for _, field := range fields(in.Args) {
		if value, ok := strings.CutPrefix(field, "--from="); ok {
			from = value

			continue
		}
		if strings.HasPrefix(field, "--") {
			continue
		}
		operands = append(operands, field)
	}
	if len(operands) < 2 {
		return from, nil
	}

	return from, operands[:len(operands)-1]
}

// fields splits an instruction's arguments, in the plain form or the JSON array form.
func fields(args string) []string {
	trimmed := strings.TrimSpace(args)
	if strings.HasPrefix(trimmed, "[") {
		var list []string
		if err := json.Unmarshal([]byte(trimmed), &list); err == nil {
			return list
		}
	}

	return strings.Fields(trimmed)
}

// stageBase is the image or earlier stage a FROM instruction's arguments name: the first
// operand after its flags (--platform).
func stageBase(args string) string {
	for _, field := range strings.Fields(args) {
		if !strings.HasPrefix(field, "--") {
			return field
		}
	}

	return ""
}

// stageName is the AS name of a FROM instruction's arguments, or empty.
func stageName(args string) string {
	parts := strings.Fields(args)
	for i := 0; i+1 < len(parts); i++ {
		if strings.EqualFold(parts[i], "AS") {
			return parts[i+1]
		}
	}

	return ""
}

// instructions reads the Dockerfile's instructions in order.
func instructions(src []byte) []Instruction {
	var out []Instruction
	var current *Instruction
	var continued strings.Builder
	line := 0
	scanner := bufio.NewScanner(bytes.NewReader(src))
	for scanner.Scan() {
		line++
		text := scanner.Text()
		trimmed := strings.TrimSpace(text)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			// A blank or comment line inside a continuation is skipped, as docker does.
			continue
		}
		more := strings.HasSuffix(trimmed, "\\")
		if more {
			trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, "\\"))
		}
		if current == nil {
			keyword, rest, _ := strings.Cut(trimmed, " ")
			current = &Instruction{Line: line, Keyword: strings.ToUpper(keyword)}
			continued.Reset()
			continued.WriteString(strings.TrimSpace(rest))
		} else {
			continued.WriteString(" ")
			continued.WriteString(trimmed)
		}
		if more {
			continue
		}
		current.Args = continued.String()
		out = append(out, *current)
		current = nil
	}
	if current != nil {
		current.Args = continued.String()
		out = append(out, *current)
	}

	return out
}

// scanReservedStages reads the Dockerfile at the application root for its reserved
// stages and reports every instruction one holds beyond its install, and the reserved
// stages it lacks. Without a Dockerfile there is nothing to scan: the unseeded finding
// covers it.
func scanReservedStages(appDir string) (findings []StageFinding, absent []string, err error) {
	src, err := os.ReadFile(filepath.Join(appDir, dockerfileName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil
		}

		return nil, nil, errors.Wrap(err, "os.ReadFile(): Dockerfile")
	}
	stages := Stages(src)
	for _, name := range ReservedStages {
		stage := Named(stages, name)
		if stage == nil {
			absent = append(absent, name)

			continue
		}
		for _, in := range stage.Instructions {
			if problem := reservedProblem(stages, name, in); problem != "" {
				findings = append(findings, StageFinding{Stage: name, Line: in.Line, Instruction: strings.TrimSpace(in.Keyword + " " + in.Args), Problem: problem})
			}
		}
	}

	return findings, absent, nil
}

// reservedProblem says what is wrong with an instruction of a reserved stage, or nothing
// for one the stage may hold: a WORKDIR, a copy of the stage's install files from the
// build context, a copy out of a stage that is an image alone (the bun binary from
// bun-binary), or the stage's one command.
func reservedProblem(stages []Stage, name string, in Instruction) string {
	shape := reserved[name]
	switch in.Keyword {
	case keywordWorkdir:
		return ""
	case keywordCopy:
		from, sources := in.Copies()
		if from != "" {
			if source := Named(stages, from); source != nil && len(source.Instructions) == 0 {
				return ""
			}

			return "copies out of " + from + ", which is not a stage that is an image alone; a reserved stage takes a binary from such a stage (the bun binary from bun-binary) and its files from the build context"
		}
		for _, s := range sources {
			if !slices.Contains(shape.files, path.Base(s)) {
				return "copies " + s + "; the stage copies only " + joinOr(shape.files)
			}
		}
		if len(sources) == 0 {
			return "copies nothing the stage may hold"
		}

		return ""
	case keywordRun:
		if strings.Join(fields(in.Args), " ") == shape.command {
			return ""
		}

		return "runs more than the stage's one command, " + shape.command
	default:
		return "is not an instruction the stage may hold (WORKDIR, the copy of its install files, its one command)"
	}
}

// joinOr lists names the way a sentence does: "go.mod or go.sum".
func joinOr(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}

	return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
}
