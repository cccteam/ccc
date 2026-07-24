package generation

import (
	"strings"
	"unicode/utf8"
)

// boxTextWidth is the wrap width for prose inside migration boxes; static lines may run
// slightly wider, and the box border sizes itself to the longest line.
const boxTextWidth = 128

// wrapText wraps text at word boundaries to boxTextWidth, preserving the text's leading
// indentation on the first line and prefixing continuation lines with indent. Words
// longer than the width are emitted unbroken.
func wrapText(text, indent string) []string {
	leading := text[:len(text)-len(strings.TrimLeft(text, " "))]
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}

	lines := make([]string, 0, len(words)/8+1)
	prefix := leading
	line := words[0]
	for _, word := range words[1:] {
		if utf8.RuneCountInString(prefix)+utf8.RuneCountInString(line)+1+utf8.RuneCountInString(word) > boxTextWidth {
			lines = append(lines, prefix+line)
			prefix = indent
			line = word

			continue
		}
		line += " " + word
	}

	return append(lines, prefix+line)
}

// banner formats migration guidance as a bordered box so it stands out in go:generate
// output. Lines render verbatim (pre-wrapped by the caller); empty strings become blank
// in-box lines. The leading newline pushes the box below the log prefix.
func banner(lines ...string) string {
	width := 0
	for _, line := range lines {
		width = max(width, utf8.RuneCountInString(line))
	}

	var b strings.Builder
	b.WriteString("\n╭")
	b.WriteString(strings.Repeat("─", width+2))
	b.WriteString("╮\n")
	for _, line := range lines {
		b.WriteString("│ ")
		b.WriteString(line)
		b.WriteString(strings.Repeat(" ", width-utf8.RuneCountInString(line)))
		b.WriteString(" │\n")
	}
	b.WriteString("╰")
	b.WriteString(strings.Repeat("─", width+2))
	b.WriteString("╯")

	return b.String()
}
