// Package names holds the naming rules the tool applies to an auth: how a name is
// substituted through a tree of code and prose, and what names an auth may take.
package names

import (
	"go/token"
	"regexp"
	"strings"

	"github.com/go-playground/errors/v5"
)

// authNameRE is the shape of an auth name: the population that signs in, lowercase, one
// word, letters and digits.
var authNameRE = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// Guidance is the one-line answer to "what is a good auth name".
const Guidance = "the population that signs in, plural, lowercase, one word: staff, members, partners, devices"

// Pascal is a name's PascalCase form: the table prefix and the accessor.
func Pascal(name string) string {
	if name == "" {
		return ""
	}

	return strings.ToUpper(name[:1]) + name[1:]
}

// Rename substitutes one auth name for another through text, in both cases. The lowercase
// name is renamed where it stands on its own or starts an identifier (members,
// membersAuth, pkg/auth/members) and not inside an English word that happens to begin
// with it (membership). The PascalCase form is renamed wherever it starts a word of an
// identifier (MembersSessions, CK_MembersSessionsId, MembersMembersUserRoles). Adjacent
// occurrences are each renamed: the boundary is read, never consumed.
func Rename(text, from, to string) string {
	text = replaceBounded(text, Pascal(from), Pascal(to), func(_, next byte) bool { return !isLower(next) })

	return replaceBounded(text, from, to, func(prev, next byte) bool { return !isLetter(prev) && !isLower(next) })
}

// replaceBounded replaces every occurrence of from whose neighbors satisfy ok; a neighbor
// beyond either end of the text is the zero byte.
func replaceBounded(text, from, to string, ok func(prev, next byte) bool) string {
	var b strings.Builder
	i := 0
	for {
		j := strings.Index(text[i:], from)
		if j < 0 {
			b.WriteString(text[i:])

			return b.String()
		}
		j += i
		var prev, next byte
		if j > 0 {
			prev = text[j-1]
		}
		if end := j + len(from); end < len(text) {
			next = text[end]
		}
		b.WriteString(text[i:j])
		if ok(prev, next) {
			b.WriteString(to)
		} else {
			b.WriteString(from)
		}
		i = j + len(from)
	}
}

func isLower(c byte) bool  { return c >= 'a' && c <= 'z' }
func isLetter(c byte) bool { return isLower(c) || (c >= 'A' && c <= 'Z') }

// predeclared are Go's predeclared identifiers an auth name would shadow.
var predeclared = map[string]bool{
	"any": true, "bool": true, "byte": true, "cap": true, "close": true, "complex": true, "copy": true,
	"delete": true, "error": true, "false": true, "float32": true, "float64": true, "imag": true, "int": true,
	"iota": true, "len": true, "make": true, "new": true, "nil": true, "panic": true, "print": true,
	"println": true, "real": true, "recover": true, "rune": true, "string": true, "true": true, "uint": true,
	"uintptr": true, "append": true, "clear": true, "max": true, "min": true, "comparable": true,
}

// ValidateAuth checks an auth name: its shape, that it is not a Go keyword or predeclared
// identifier (it becomes a package name and an identifier stem), and that it is not one of
// the reserved names: the packages the application already imports and the directories it
// already has, which the name would collide with.
func ValidateAuth(name string, reserved map[string]bool) error {
	if !authNameRE.MatchString(name) {
		return errors.Newf("auth name %q: %s", name, Guidance)
	}
	if token.IsKeyword(name) || predeclared[name] {
		return errors.Newf("auth name %q is a Go keyword or predeclared identifier; %s", name, Guidance)
	}
	if reserved[name] {
		return errors.Newf("auth name %q is already a package or directory the application uses; %s", name, Guidance)
	}

	return nil
}
