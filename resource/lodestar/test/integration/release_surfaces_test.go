package integration

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/cccteam/ccc/resource"
)

// TestReleaseSurfaces pins what the release file tells the stack about the quiet
// surfaces, read through the reader the deploy uses: the beacon prefix, mounted by hand
// and declared on event with traces off in the generator program, and the ingest route,
// declared on event on its @rpc, each with only what it declares, so bedrock renders the
// stack's own log exclusion from the same words the router applies. Nothing else is
// listed: a surface that declares nothing writes its entry always and its spans follow
// the front end, and the file says nothing about it.
//
// Demonstrates: generation.request-log, generation.traces, @rpc.log.
func TestReleaseSurfaces(t *testing.T) {
	t.Parallel()

	release, err := resource.ReadReleaseFile(filepath.Join("..", "..", "pkg", "router"))
	if err != nil {
		t.Fatalf("resource.ReadReleaseFile() error = %v", err)
	}
	want := []resource.Surface{
		{Prefix: "/beacons/", Log: resource.RequestLogOnEvent, Traces: resource.TracesOff},
		{Prefix: "/droids/sectors/{sectorID}/ingest-droid-reports", Log: resource.RequestLogOnEvent},
	}
	if !slices.Equal(want, release.Surfaces) {
		t.Errorf("release file surfaces = %+v, want %+v", release.Surfaces, want)
	}
}
