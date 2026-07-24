package generation

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Test_banner pins the alignment property: every rendered line is the same rune width,
// so the box borders line up regardless of content.
func Test_banner(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		lines       []string
		wantContain string
	}{
		{
			name: "mixed-length content aligns to the widest line",
			lines: []string{
				"TITLE",
				"",
				"a longer line that sets the box width for everything",
				"  indented",
			},
			wantContain: "│ TITLE",
		},
		{
			name: "non-ASCII content is padded by rune width, not byte length",
			lines: []string{
				"TÍTLE",
				"naïve café — déjà vu",
			},
			wantContain: "│ TÍTLE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := banner(tt.lines...)

			lines := strings.Split(got, "\n")
			if lines[0] != "" {
				t.Errorf("banner() must start with a newline to clear the log prefix, got %q", lines[0])
			}

			width := utf8.RuneCountInString(lines[1])
			for i, line := range lines[1:] {
				if utf8.RuneCountInString(line) != width {
					t.Errorf("banner() line %d width = %d, want %d: %q", i+1, utf8.RuneCountInString(line), width, line)
				}
			}

			if !strings.Contains(got, tt.wantContain) {
				t.Errorf("banner() missing %q:\n%s", tt.wantContain, got)
			}
		})
	}
}
