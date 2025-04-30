package generation_test

import (
	"testing"

	"github.com/cccteam/ccc/resource/generation"
)

func Test_schemageneration(t *testing.T) {
	generator, err := generation.NewSchemaGenerator("./testdata/schemagen/resources.go", t.TempDir())
	if err != nil {
		t.Error(err)

		return
	}

	if err := generator.Generate(); err != nil {
		t.Error(err)

		return
	}
}
