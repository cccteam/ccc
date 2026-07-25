package generation

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestAnnotationsDocCoversGeneratorVocabulary pins README.md against the
// generator's annotation vocabulary: every comment-annotation keyword registered in
// resourceKeywords(), every author-written struct-tag key, and every recognized
// conditions value must be documented. Adding a keyword or tag without documenting it
// fails this test.
func TestAnnotationsDocCoversGeneratorVocabulary(t *testing.T) {
	t.Parallel()

	// Resolve the doc relative to this source file, not the working directory:
	// NewClient chdirs the test process, so tests running after it cannot rely on
	// relative paths.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	docBytes, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "..", "README.md"))
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	doc := string(docBytes)

	type testCase struct {
		name   string
		needle string
	}

	tests := make([]testCase, 0, len(resourceKeywords())+len(sourceStructTagKeys)+len(conditionValues))
	for keyword := range resourceKeywords() {
		tests = append(tests, testCase{name: "keyword @" + keyword, needle: "`@" + keyword + "`"})
	}
	for _, key := range sourceStructTagKeys {
		tests = append(tests, testCase{name: "source tag " + key, needle: "`" + key + ":"})
	}
	for _, value := range conditionValues {
		tests = append(tests, testCase{name: "condition " + value, needle: "`" + value + "`"})
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
