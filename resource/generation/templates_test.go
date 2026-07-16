package generation

import (
	"strings"
	"testing"
)

func Test_fileTemplates_generationHeader(t *testing.T) {
	t.Parallel()

	fileTemplates := map[string]string{
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

	for name, tmpl := range fileTemplates {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if !strings.HasPrefix(tmpl, generationHeader+"\n") {
				t.Errorf("%s must start with generationHeader %q so removeGeneratedFileByHeaderComment can clean up its output; got %q", name, generationHeader, firstLine(tmpl))
			}
		})
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}

	return s
}
