// Demonstrates: field.bytes, field.array, field.object, field.write-only.
package integration

// This suite pins the console's side of the four field shapes the browser library
// renders read-only or not at all: the generated metadata names each shape (bytes,
// object, an array, the write-only flag) exactly as the library dispatches on it, and the
// four config pages name the fields on their lists and forms, with no list column over the
// write-only field. The rendering itself is proven in the library's specs and in the
// browser against the running stack.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderShapes_console(t *testing.T) {
	t.Parallel()

	app := filepath.Join("..", "..", "web", "console", "src", "app")
	service := filepath.Join(app, "core", "service")

	tests := []struct {
		name   string
		file   string
		want   string
		absent bool
	}{
		{name: "a document's digest is bytes", file: filepath.Join(service, "zz_gen_resources.ts"), want: "{ fieldName: 'digest', displayType: 'bytes', required: true, isIndex: false }"},
		{name: "a document's provenance, a derived struct, is object", file: filepath.Join(service, "zz_gen_resources.ts"), want: "{ fieldName: 'provenance', displayType: 'object', required: false, isIndex: false }"},
		{name: "a call's position, an imported type, is object", file: filepath.Join(service, "zz_gen_resources.ts"), want: "{ fieldName: 'position', displayType: 'object', required: false, isIndex: false }"},
		{name: "a call's transcript is write-only", file: filepath.Join(service, "zz_gen_resources.ts"), want: "{ fieldName: 'transcript', displayType: 'string', required: false, isIndex: false, writeOnly: true }"},
		{name: "a ship's cargo bays are a number array", file: filepath.Join(service, "zz_gen_resources.ts"), want: "{ fieldName: 'cargoBays', displayType: 'number[]', required: false, isIndex: false }"},
		{name: "a squadron's callsigns are a string array with a per-element length", file: filepath.Join(service, "zz_gen_resources.ts"), want: "{ fieldName: 'callsigns', displayType: 'string[]', required: false, isIndex: false, maxLength: 16 }"},
		{name: "the Ships page lists and shows the cargo bays", file: filepath.Join(app, "configs", "ships.config.ts"), want: "Ships.fieldName.cargoBays"},
		{name: "the Squadrons page lists and shows the callsigns", file: filepath.Join(app, "configs", "squadrons.config.ts"), want: "Squadrons.fieldName.callsigns"},
		{name: "the Documents page lists and shows the digest", file: filepath.Join(app, "configs", "missionDocuments.config.ts"), want: "MissionDocuments.fieldName.digest"},
		{name: "the Documents page lists and shows the provenance", file: filepath.Join(app, "configs", "missionDocuments.config.ts"), want: "MissionDocuments.fieldName.provenance"},
		{name: "the Calls page lists and shows the position", file: filepath.Join(app, "configs", "distressCalls.config.ts"), want: "DistressCalls.fieldName.position"},
		{name: "the Calls page's form carries the transcript", file: filepath.Join(app, "configs", "distressCalls.config.ts"), want: "field({ name: DistressCalls.fieldName.transcript"},
		{name: "the Calls page lists no column over the transcript, which the page would refuse", file: filepath.Join(app, "configs", "distressCalls.config.ts"), want: "{ id: DistressCalls.fieldName.transcript", absent: true},
		{name: "the routes register the Documents page", file: filepath.Join(app, "app.routes.ts"), want: "resourceRoutes(missionDocumentsConfig, resourceMeta),"},
		{name: "the routes register the Calls page", file: filepath.Join(app, "app.routes.ts"), want: "resourceRoutes(distressCallsConfig, resourceMeta),"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			source, err := os.ReadFile(tt.file)
			if err != nil {
				t.Fatalf("os.ReadFile() error = %v", err)
			}
			if got := strings.Contains(string(source), tt.want); got == tt.absent {
				t.Errorf("%s contains %q = %v, want %v", filepath.Base(tt.file), tt.want, got, !tt.absent)
			}
		})
	}
}
