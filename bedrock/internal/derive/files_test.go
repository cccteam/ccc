package derive

import "testing"

func TestCheckBrowserStages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		text    string
		wantErr bool
	}{
		{
			name: "a browser stage that declares the argument passes",
			text: "ARG VERSION=dev\nFROM node AS web-build-env\nARG VERSION\nRUN bun install && \\\n    bun run build\nFROM static\nARG VERSION\n",
		},
		{
			name:    "a browser stage without the argument is refused",
			text:    "ARG VERSION=dev\nFROM node AS web-build-env\nRUN bun install && \\\n    bun run build\nFROM static\nARG VERSION\n",
			wantErr: true,
		},
		{
			name:    "the global declaration before the first FROM does not count for the stage",
			text:    "ARG VERSION\nFROM node\nRUN bun run build\n",
			wantErr: true,
		},
		{
			name: "a comment naming the build is not a build",
			text: "FROM node AS web\n# RUN bun run build is documented here\nRUN echo nothing\nFROM static\n",
		},
		{
			name: "an image with no browser stage has nothing to declare",
			text: "FROM golang AS build-env\nRUN go build ./...\nFROM static\n",
		},
		{
			name: "two browser stages each declare it",
			text: "FROM node AS console\nARG VERSION\nRUN bun run build\nFROM node AS portal\nARG VERSION\nRUN bun run build\n",
		},
		{
			name:    "the second of two browser stages missing it is refused",
			text:    "FROM node AS console\nARG VERSION\nRUN bun run build\nFROM node AS portal\nRUN bun run build\n",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := checkBrowserStages(tt.text)
			if (err != nil) != tt.wantErr {
				t.Fatalf("checkBrowserStages() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
