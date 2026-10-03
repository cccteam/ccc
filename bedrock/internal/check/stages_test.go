package check

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// seededStages is the two reserved stages as the seeded Dockerfile writes them, with the
// bun binary's stage and a compile stage after them.
const seededStages = `ARG VERSION=dev
# the Go modules
FROM cgr.dev/chainguard/go@sha256:aa AS go-modules
WORKDIR /go/src/app
COPY go.mod go.sum ./
RUN go mod download

FROM docker.io/oven/bun@sha256:bb AS bun-binary

FROM docker.io/library/node@sha256:cc AS web-packages
COPY --from=bun-binary /usr/local/bin/bun /usr/local/bin/bun
WORKDIR /src/web
COPY web/package.json web/bun.lock web/bunfig.toml ./
RUN bun install --frozen-lockfile
WORKDIR /src/portal
COPY portal/package.json portal/bun.lock ./
RUN bun install \
    --frozen-lockfile

FROM cgr.dev/chainguard/go@sha256:aa AS build-env
COPY --from=go-modules /root/go/pkg/mod /root/go/pkg/mod
COPY . ./
RUN go build -o /build/app .
`

func TestScanReservedStages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		dockerfile string
		noFile     bool
		want       []StageFinding
		wantAbsent []string
	}{
		{name: "the seeded stages pass, a WORKDIR, the bun binary and a continued command included", dockerfile: seededStages},
		{name: "the committed golden passes", dockerfile: readGolden(t, "root/Dockerfile")},
		{
			name:       "a copy of the sources into go-modules is refused",
			dockerfile: strings.Replace(seededStages, "RUN go mod download\n", "COPY . ./\nRUN go mod download\n", 1),
			want:       []StageFinding{{Stage: "go-modules", Line: 6, Instruction: "COPY . ./", Problem: "copies .; the stage copies only go.mod or go.sum"}},
		},
		{
			name:       "an environment variable in a reserved stage is refused",
			dockerfile: strings.Replace(seededStages, "WORKDIR /go/src/app\n", "WORKDIR /go/src/app\nENV GOFLAGS=-mod=mod\n", 1),
			want:       []StageFinding{{Stage: "go-modules", Line: 5, Instruction: "ENV GOFLAGS=-mod=mod", Problem: "is not an instruction the stage may hold (WORKDIR, the copy of its install files, its one command)"}},
		},
		{
			name:       "another command is refused",
			dockerfile: strings.Replace(seededStages, "RUN bun install --frozen-lockfile\n", "RUN bun install --frozen-lockfile && bun run build\n", 1),
			want:       []StageFinding{{Stage: "web-packages", Line: 14, Instruction: "RUN bun install --frozen-lockfile && bun run build", Problem: "runs more than the stage's one command, bun install --frozen-lockfile"}},
		},
		{
			name:       "a copy out of a stage that builds something is refused",
			dockerfile: strings.Replace(seededStages, "WORKDIR /src/web\n", "WORKDIR /src/web\nCOPY --from=build-env /build/app ./app\n", 1),
			want:       []StageFinding{{Stage: "web-packages", Line: 13, Instruction: "COPY --from=build-env /build/app ./app", Problem: "copies out of build-env, which is not a stage that is an image alone; a reserved stage takes a binary from such a stage (the bun binary from bun-binary) and its files from the build context"}},
		},
		{
			name:       "a JSON-form copy is read like the plain one",
			dockerfile: strings.Replace(seededStages, "COPY go.mod go.sum ./\n", "COPY [\"go.mod\", \"go.sum\", \"main.go\", \"./\"]\n", 1),
			want:       []StageFinding{{Stage: "go-modules", Line: 5, Instruction: "COPY [\"go.mod\", \"go.sum\", \"main.go\", \"./\"]", Problem: "copies main.go; the stage copies only go.mod or go.sum"}},
		},
		{
			name:       "a Dockerfile without the stages names them absent",
			dockerfile: "FROM golang AS build-env\nRUN go build -o /build/app .\n",
			wantAbsent: []string{"go-modules", "web-packages"},
		},
		{name: "no Dockerfile, nothing to scan", noFile: true},
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
			got, absent, err := scanReservedStages(appDir)
			if err != nil {
				t.Fatalf("scanReservedStages() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("scanReservedStages() findings mismatch (-want +got):\n%s", diff)
			}
			if !slices.Equal(absent, tt.wantAbsent) {
				t.Errorf("absent = %v, want %v", absent, tt.wantAbsent)
			}
		})
	}
}

func TestStages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		dockerfile string
		want       []string
	}{
		{
			name:       "stages with their instructions, continuations joined and comments dropped",
			dockerfile: "ARG V=1\n# header\nFROM a AS one\n  run echo x \\\n    && echo y\nFROM b\nCOPY --from=one /x /y\n",
			want:       []string{"one@3: RUN echo x && echo y", "@6: COPY --from=one /x /y"},
		},
		{
			name:       "a lower-case as names the stage too",
			dockerfile: "from a as lower\nWORKDIR /w\n",
			want:       []string{"lower@1: WORKDIR /w"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got []string
			for _, s := range Stages([]byte(tt.dockerfile)) {
				var lines []string
				for _, in := range s.Instructions {
					lines = append(lines, in.Keyword+" "+in.Args)
				}
				got = append(got, s.Name+"@"+strconv.Itoa(s.Line)+": "+strings.Join(lines, "; "))
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Stages() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCopies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		instruction Instruction
		wantFrom    string
		wantSources []string
	}{
		{name: "sources from the context", instruction: Instruction{Keyword: "COPY", Args: "web/package.json web/bun.lock ./"}, wantSources: []string{"web/package.json", "web/bun.lock"}},
		{name: "a copy out of a stage", instruction: Instruction{Keyword: "COPY", Args: "--from=bun-binary --chown=1:1 /usr/local/bin/bun /usr/local/bin/bun"}, wantFrom: "bun-binary", wantSources: []string{"/usr/local/bin/bun"}},
		{name: "a JSON form", instruction: Instruction{Keyword: "ADD", Args: `["a b", "c", "/d"]`}, wantSources: []string{"a b", "c"}},
		{name: "another instruction copies nothing", instruction: Instruction{Keyword: "RUN", Args: "cp a b"}},
		{name: "a copy with one operand names no source", instruction: Instruction{Keyword: "COPY", Args: "./"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			from, sources := tt.instruction.Copies()
			if from != tt.wantFrom || !slices.Equal(sources, tt.wantSources) {
				t.Errorf("Copies() = %q, %v; want %q, %v", from, sources, tt.wantFrom, tt.wantSources)
			}
		})
	}
}

// readGolden reads one file of the committed harbor golden.
func readGolden(t *testing.T, rel string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(golden, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}
