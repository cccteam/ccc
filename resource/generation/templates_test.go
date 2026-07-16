package generation

import (
	"regexp"
	"strings"
	"testing"
)

// fileTemplates returns every file-level template by name: templates whose
// output is written as a complete generated file.
func fileTemplates() map[string]string {
	return map[string]string{
		"resourcesInterfaceTemplate":      resourcesInterfaceTemplate,
		"resourceFileTemplate":            resourceFileTemplate,
		"handlerHeaderTemplate":           handlerHeaderTemplate,
		"consolidatedPatchTemplate":       consolidatedPatchTemplate,
		"resourceEnumsTemplate":           resourceEnumsTemplate,
		"typescriptConstantsTemplate":     typescriptConstantsTemplate,
		"typescriptResourcesTemplate":     typescriptResourcesTemplate,
		"typescriptMethodsTemplate":       typescriptMethodsTemplate,
		"typescriptEnumsTemplate":         typescriptEnumsTemplate,
		"routesTemplate":                  routesTemplate,
		"routerTestTemplate":              routerTestTemplate,
		"rpcFileTemplate":                 rpcFileTemplate,
		"rpcHandlerTemplate":              rpcHandlerTemplate,
		"rpcInterfacesTemplate":           rpcInterfacesTemplate,
		"computedResourceHandlerTemplate": computedResourceHandlerTemplate,
	}
}

func Test_fileTemplates_generationHeader(t *testing.T) {
	t.Parallel()

	for name, tmpl := range fileTemplates() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if !strings.HasPrefix(tmpl, generationHeader+"\n") {
				t.Errorf("%s must start with generationHeader %q so removeGeneratedFileByHeaderComment can clean up its output; got %q", name, generationHeader, firstLine(tmpl))
			}
		})
	}
}

func firstLine(s string) string {
	if before, _, ok := strings.Cut(s, "\n"); ok {
		return before
	}

	return s
}

// declaredTemplateImports returns the import paths declared in a template's
// import block (both the parenthesized and single-import forms).
func declaredTemplateImports(tmpl string) []string {
	templateImportPath := regexp.MustCompile(`(?m)^\t(?:[\w.]+ )?"([^"]+)"$|^import (?:[\w.]+ )?"([^"]+)"$`)

	var paths []string
	for _, match := range templateImportPath.FindAllStringSubmatch(tmpl, -1) {
		if match[1] != "" {
			paths = append(paths, match[1])
		} else {
			paths = append(paths, match[2])
		}
	}

	return paths
}

// Test_stdlibImports_doesNotShadowTemplateImports pins the importFixer's stdlib
// seed exclusion rule: a qualifier that any template resolves to a third-party
// package (errors -> go-playground/errors, cmp -> go-cmp/cmp) must never appear
// in the seed. Otherwise a template that references the qualifier without
// declaring the third-party import would silently get the stdlib package
// instead of falling back to goimports resolution.
func Test_stdlibImports_doesNotShadowTemplateImports(t *testing.T) {
	t.Parallel()

	for name, tmpl := range fileTemplates() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			seed := stdlibImports()
			for _, path := range declaredTemplateImports(tmpl) {
				if root, _, _ := strings.Cut(path, "/"); !strings.Contains(root, ".") {
					continue // standard library path: cannot shadow itself
				}

				qualifier := assumedPackageName(path)
				if seedPath, ok := seed[qualifier]; ok {
					t.Errorf("stdlibImports() maps %q to %q, shadowing %q declared by %s: remove the seed entry — the seed must not contain qualifiers that generated code resolves to third-party packages", qualifier, seedPath, path, name)
				}
			}
		})
	}
}
