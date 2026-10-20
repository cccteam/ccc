package check

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/bedrock/internal/derive"
)

// lanternGolden is the committed golden of the lantern fixture, two sites behind one
// image.
const lanternGolden = "../render/testdata/lantern"

// multiStages is a sites-layout image up to its start: the build stage compiling both
// sites' binaries and the runtime stage copying them in.
const multiStages = "FROM golang AS build-env\nRUN go build -o /build/console ./apps/console && \\\n    go build -o /build/portal ./apps/portal\nFROM scratch\nCOPY --from=build-env /build /\n"

// The refusal's first line, under the report's label, for an image that keeps an
// entrypoint; and the lines after it, the ones to write, as the report indents them.
const (
	entrypointRefused = "  refused  Dockerfile sets ENTRYPOINT [\"/console\"]: an entrypoint runs with the container's arguments after it, so each site's service would start /console with its own binary as an argument, never its own binary. The stack's services pass each site's binary as the container's arguments (cloud-run.tf), which replace a default command and follow an entrypoint, so the image sets no ENTRYPOINT and names the first site's binary as its CMD; end the image with these lines in place of what it sets, as the seeded Dockerfile has them:\n"
	linesToWrite      = "           # No ENTRYPOINT: the default command is the first site's binary, and each site's Cloud Run\n           # service names its own binary as the container's arguments, which replace it (cloud-run.tf).\n           # The placeholder image the stack creates the services with keeps its own entrypoint, so\n           # the first apply's revisions start before the pipeline's first deploy.\n           CMD [\"/console\"]\n"
)

func TestScanStart(t *testing.T) {
	t.Parallel()

	multi := derive.Model{Sites: []derive.Site{
		{Name: "console", Dir: "apps/console", Process: derive.Process{Name: "console"}},
		{Name: "portal", Dir: "apps/portal", Process: derive.Process{Name: "portal"}},
	}}
	flat := derive.Model{Sites: []derive.Site{{Name: "harbor", Dir: ".", Process: derive.Process{Name: "app"}}}}
	lines := fmt.Sprintf(multiSiteStart, "/console")
	lantern, err := os.ReadFile(filepath.Join(lanternGolden, "root", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		dockerfile string
		noFile     bool
		model      derive.Model
		want       *ImageStart
		// wantOutput are the lines the report writes for the start, in full.
		wantOutput []string
	}{
		{
			name:       "a sites-layout image keeping an entrypoint is refused, with the lines to write",
			dockerfile: multiStages + "ENTRYPOINT [\"/console\"]\n",
			model:      multi,
			want:       &ImageStart{Found: `ENTRYPOINT ["/console"]`, Multi: true, Problem: "an entrypoint runs with the container's arguments after it, so each site's service would start /console with its own binary as an argument, never its own binary", Lines: lines},
			wantOutput: []string{entrypointRefused + linesToWrite},
		},
		{
			name:       "an entrypoint beside a default command is still an entrypoint",
			dockerfile: multiStages + "ENTRYPOINT [\"/console\"]\nCMD [\"--serve\"]\n",
			model:      multi,
			want:       &ImageStart{Found: `ENTRYPOINT ["/console"] and CMD ["--serve"]`, Multi: true, Problem: "an entrypoint runs with the container's arguments after it, so each site's service would start /console with its own binary as an argument, never its own binary", Lines: lines},
			wantOutput: []string{"  refused  Dockerfile sets ENTRYPOINT [\"/console\"] and CMD [\"--serve\"]: an entrypoint runs with"},
		},
		{
			name:       "a sites-layout image without a default command is refused",
			dockerfile: multiStages,
			model:      multi,
			want:       &ImageStart{Found: "neither an ENTRYPOINT nor a CMD", Multi: true, Problem: "the image has no default command, and runs no site on its own", Lines: lines},
			wantOutput: []string{"  refused  Dockerfile sets neither an ENTRYPOINT nor a CMD: the image has no default command, and runs no site on its own. The stack's services pass each site's binary as the container's arguments (cloud-run.tf), which replace a default command and follow an entrypoint, so the image sets no ENTRYPOINT and names the first site's binary as its CMD; end the image with these lines in place of what it sets, as the seeded Dockerfile has them:\n" + linesToWrite},
		},
		{
			name:       "a default command that is not a site's binary is refused",
			dockerfile: multiStages + "CMD [\"/app\"]\n",
			model:      multi,
			want:       &ImageStart{Found: `CMD ["/app"]`, Multi: true, Problem: "/app is not one of the sites' binaries (/console, /portal), so the image's default command runs no site", Lines: lines},
			wantOutput: []string{"  refused  Dockerfile sets CMD [\"/app\"]: /app is not one of the sites' binaries (/console, /portal), so the image's default command runs no site. The stack's services pass", linesToWrite},
		},
		{
			name:       "a site's binary with an argument is not the binary alone",
			dockerfile: multiStages + "CMD [\"/console\", \"--serve\"]\n",
			model:      multi,
			want:       &ImageStart{Found: `CMD ["/console", "--serve"]`, Multi: true, Problem: "/console --serve is not one of the sites' binaries (/console, /portal), so the image's default command runs no site", Lines: lines},
			wantOutput: []string{"  refused  Dockerfile sets CMD [\"/console\", \"--serve\"]: /console --serve is not one of the sites' binaries"},
		},
		{
			name:       "a default command in the shell form is refused",
			dockerfile: multiStages + "CMD /console\n",
			model:      multi,
			want:       &ImageStart{Found: "CMD /console", Multi: true, Problem: "a CMD in the shell form runs through /bin/sh, which the image does not hold; the exec form, a JSON array, names the binary alone", Lines: lines},
			wantOutput: []string{"  refused  Dockerfile sets CMD /console: a CMD in the shell form runs through /bin/sh, which the image does not hold; the exec form, a JSON array, names the binary alone. The stack's services pass"},
		},
		{
			name:       "no entrypoint and the first site's binary as the default command passes",
			dockerfile: multiStages + "CMD [\"/console\"]\n",
			model:      multi,
			want:       &ImageStart{Found: `CMD ["/console"]`, Multi: true, Binary: "/console", Site: "console", Lines: lines},
			wantOutput: []string{"  image    Dockerfile sets no ENTRYPOINT, and its default command is /console, the console site's binary: each site's service passes its own binary as the container's arguments (cloud-run.tf), which replace it\n"},
		},
		{
			name:       "another site's binary as the default command passes",
			dockerfile: multiStages + "CMD [\"/portal\"]\n",
			model:      multi,
			want:       &ImageStart{Found: `CMD ["/portal"]`, Multi: true, Binary: "/portal", Site: "portal", Lines: lines},
			wantOutput: []string{"  image    Dockerfile sets no ENTRYPOINT, and its default command is /portal, the portal site's binary"},
		},
		{
			name:       "an entrypoint of an earlier stage is not the image's",
			dockerfile: "FROM golang AS build-env\nENTRYPOINT [\"/usr/local/go/bin/go\"]\nRUN go build -o /build/console ./apps/console\nFROM scratch\nCOPY --from=build-env /build /\nCMD [\"/console\"]\n",
			model:      multi,
			want:       &ImageStart{Found: `CMD ["/console"]`, Multi: true, Binary: "/console", Site: "console", Lines: lines},
		},
		{
			name:       "the committed lantern golden passes",
			dockerfile: string(lantern),
			model:      multi,
			want:       &ImageStart{Found: `CMD ["/console"]`, Multi: true, Binary: "/console", Site: "console", Lines: lines},
		},
		{
			name:       "a flat image keeps /app as its entrypoint",
			dockerfile: "FROM golang AS build-env\nRUN go build -o /build/app .\nFROM scratch\nCOPY --from=build-env /build /\nENTRYPOINT [\"/app\"]\n",
			model:      flat,
			want:       &ImageStart{Found: `ENTRYPOINT ["/app"]`, Binary: "/app"},
			wantOutput: []string{"  image    Dockerfile starts the site with ENTRYPOINT [\"/app\"], which its service runs as written (no command or arguments of its own)\n"},
		},
		{
			name:       "a flat image is never refused for what it sets",
			dockerfile: "FROM scratch\nENTRYPOINT [\"/server\", \"--serve\"]\nCMD [\"--port\", \"8080\"]\n",
			model:      flat,
			want:       &ImageStart{Found: `ENTRYPOINT ["/server", "--serve"] and CMD ["--port", "8080"]`, Binary: "/server"},
		},
		{
			name:       "a flat image setting neither says nothing",
			dockerfile: "FROM scratch\nCOPY /build /\n",
			model:      flat,
		},
		{
			name:   "no Dockerfile, nothing to read",
			noFile: true,
			model:  multi,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			appDir := t.TempDir()
			if !tt.noFile {
				if err := os.WriteFile(filepath.Join(appDir, "Dockerfile"), []byte(tt.dockerfile), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := ScanStart(appDir, &tt.model)
			if err != nil {
				t.Fatalf("ScanStart() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ScanStart() mismatch (-want +got):\n%s", diff)
			}
			r := &Report{Start: got}
			if wantClean := got == nil || got.Problem == ""; r.Clean() != wantClean {
				t.Errorf("Clean() = %v, want %v", r.Clean(), wantClean)
			}
			var out bytes.Buffer
			r.Write(&out)
			for _, want := range tt.wantOutput {
				if !strings.Contains(out.String(), want) {
					t.Errorf("Write() output lacks %q:\n%s", want, out.String())
				}
			}
		})
	}
}

// TestMultiSiteStartLines pins the lines the check prints to the ones the seeded
// Dockerfile ends with: the committed lantern golden, rendered from Dockerfile.tmpl.
func TestMultiSiteStartLines(t *testing.T) {
	t.Parallel()

	golden, err := os.ReadFile(filepath.Join(lanternGolden, "root", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf(multiSiteStart, "/console") + "\n"
	if !strings.HasSuffix(string(golden), "\n\n"+want) {
		t.Errorf("the lantern golden's Dockerfile does not end with the lines the check prints:\n%s\n--- golden ends with ---\n%s", want, lastLines(string(golden), 6))
	}
}

// lastLines is the text's last n lines.
func lastLines(text string, n int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}

	return strings.Join(lines, "\n")
}
