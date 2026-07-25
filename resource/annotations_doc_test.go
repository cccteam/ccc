package resource

import (
	"os"
	"strings"
	"testing"
)

// TestAnnotationsDocCoversRuntimeVocabulary pins README.md against the runtime's
// vocabulary: every struct-tag key read from generated request structs and every
// reserved query parameter must be documented. Adding a tag or parameter without
// documenting it fails this test.
func TestAnnotationsDocCoversRuntimeVocabulary(t *testing.T) {
	t.Parallel()

	docBytes, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	doc := string(docBytes)

	type testCase struct {
		name   string
		needle string
	}

	tests := make([]testCase, 0, len(runtimeTagKeys)+len(reservedQueryParams))
	for _, key := range runtimeTagKeys {
		tests = append(tests, testCase{name: "runtime tag " + key, needle: "`" + key + ":"})
	}
	for _, param := range reservedQueryParams {
		tests = append(tests, testCase{name: "query parameter " + param, needle: "`" + param + "`"})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if !strings.Contains(doc, tt.needle) {
				t.Errorf("README.md does not document %s (expected to find %q)", tt.name, tt.needle)
			}
		})
	}
}
