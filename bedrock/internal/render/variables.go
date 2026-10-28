// variables.go reads the stack's input variables as its .tf files use them: every
// var.<name> a file reads must be declared by a variable block in the stack, or tofu
// refuses the plan at its first step, before anything else in the stack is read.

package render

import (
	"regexp"
	"sort"
	"strings"
)

// UndeclaredVariable is a reference to an input variable (var.<name>) in one of a
// stack's .tf files that no variable "<name>" block among them declares. tofu refuses a
// plan over it ("Reference to undeclared input variable"), so the render tests hold every
// template to the declarations and bedrock check refuses one in a committed stack, a
// person's files included.
type UndeclaredVariable struct {
	// Path is the referencing file, as the caller named it.
	Path string
	// Line is the reference's line (1-based).
	Line int
	// Name is the variable's name, without the var. prefix.
	Name string
}

var (
	// variableDeclaration opens a variable block: variable "<name>" {.
	variableDeclaration = regexp.MustCompile(`^\s*variable\s+"([A-Za-z_][A-Za-z0-9_-]*)"\s*\{`)
	// variableReference reads an input variable: var.<name>.
	variableReference = regexp.MustCompile(`\bvar\.([A-Za-z_][A-Za-z0-9_-]*)`)
)

// UndeclaredVariables finds every input variable the .tf files reference that none of
// them declares, by file and then by line, over the files' contents by path. A line that
// is a comment whole is not read; a reference anywhere else is, a string or a heredoc
// included, since a template's prose names only variables the stack has. One name
// referenced twice on a line is one finding.
func UndeclaredVariables(files map[string][]byte) []UndeclaredVariable {
	declared := map[string]bool{}
	for _, content := range files {
		for _, line := range strings.Split(string(content), "\n") {
			if m := variableDeclaration.FindStringSubmatch(line); m != nil {
				declared[m[1]] = true
			}
		}
	}
	var undeclared []UndeclaredVariable
	for path, content := range files {
		for i, line := range strings.Split(string(content), "\n") {
			if isComment(line) {
				continue
			}
			seen := map[string]bool{}
			for _, m := range variableReference.FindAllStringSubmatch(line, -1) {
				if declared[m[1]] || seen[m[1]] {
					continue
				}
				seen[m[1]] = true
				undeclared = append(undeclared, UndeclaredVariable{Path: path, Line: i + 1, Name: m[1]})
			}
		}
	}
	sort.Slice(undeclared, func(i, j int) bool {
		if undeclared[i].Path != undeclared[j].Path {
			return undeclared[i].Path < undeclared[j].Path
		}
		if undeclared[i].Line != undeclared[j].Line {
			return undeclared[i].Line < undeclared[j].Line
		}

		return undeclared[i].Name < undeclared[j].Name
	})

	return undeclared
}

// isComment reports a line that is a comment whole, in either of HCL's line comment
// forms.
func isComment(line string) bool {
	trimmed := strings.TrimSpace(line)

	return strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//")
}
