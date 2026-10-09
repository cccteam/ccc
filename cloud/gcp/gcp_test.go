package gcp

import (
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/logger"
)

// TestOpenWithoutProject: no logging project means console logs and no trace provider, and
// Close has nothing to release.
func TestOpenWithoutProject(t *testing.T) {
	t.Parallel()

	d, err := Open(t.Context(), Settings{}, "harbor")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if _, ok := d.LogExporter.(*logger.ConsoleExporter); !ok {
		t.Errorf("LogExporter = %T, want the console exporter", d.LogExporter)
	}
	if d.TraceProvider != nil {
		t.Errorf("TraceProvider = %v, want nil", d.TraceProvider)
	}
	if err := d.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

// TestOpenRefusesBadSampling: the sampling is read before anything is opened.
func TestOpenRefusesBadSampling(t *testing.T) {
	t.Parallel()

	_, err := Open(t.Context(), Settings{TraceSampling: "half"}, "harbor")
	if err == nil || !strings.Contains(err.Error(), `"half" is not a sampling setting (edge, all)`) {
		t.Fatalf("Open() error = %v, want the sampling refused", err)
	}
}

// TestSettingsVariables holds the declared variables to their names: what the stack and
// the development environment render for an application that embeds Settings, and what
// bedrock's derivation lists for the embedded struct.
func TestSettingsVariables(t *testing.T) {
	t.Parallel()

	st := reflect.TypeFor[Settings]()
	tags := make([]string, 0, st.NumField())
	for i := range st.NumField() {
		tags = append(tags, st.Field(i).Tag.Get("env"))
	}
	want := []string{"GOOGLE_CLOUD_LOGGING_PROJECT", "APP_TRACE_SAMPLING,default=edge"}
	if diff := cmp.Diff(want, tags); diff != "" {
		t.Errorf("Settings env tags mismatch (-want +got):\n%s", diff)
	}
}
