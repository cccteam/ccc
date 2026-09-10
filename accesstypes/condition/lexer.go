package condition

import (
	"fmt"
	"strings"
)

// tokenKind is the closed set of token types the condition language lexes to.
type tokenKind int

const (
	tokenEOF tokenKind = iota
	tokenIdent
	tokenString
	tokenNumber
	tokenCompare // = != < <= > >=
	tokenLParen
	tokenRParen
	tokenComma
	tokenDot
)

type token struct {
	kind tokenKind
	text string // identifier text, operator spelling, number verbatim, or unescaped string content
	pos  int    // byte offset in the source, for error messages
}

// lexer tokenizes condition source. It is deliberately small: identifiers,
// quoted strings with quote doubling, signed numbers, six comparison operators,
// parentheses, commas, and dots. Keywords are not the lexer's concern — the
// parser matches identifier text case-insensitively against the keyword set.
type lexer struct {
	src string
	pos int
}

func (l *lexer) next() (token, error) {
	for l.pos < len(l.src) && isSpace(l.src[l.pos]) {
		l.pos++
	}
	if l.pos >= len(l.src) {
		return token{kind: tokenEOF, pos: l.pos}, nil
	}

	start := l.pos
	c := l.src[l.pos]

	switch {
	case c == '(':
		l.pos++

		return token{kind: tokenLParen, text: "(", pos: start}, nil
	case c == ')':
		l.pos++

		return token{kind: tokenRParen, text: ")", pos: start}, nil
	case c == ',':
		l.pos++

		return token{kind: tokenComma, text: ",", pos: start}, nil
	case c == '.':
		l.pos++

		return token{kind: tokenDot, text: ".", pos: start}, nil
	case c == '=' || c == '!' || c == '<' || c == '>':
		return l.lexCompare()
	case c == '\'':
		return l.lexString()
	case c == '+' || c == '-' || isDigit(c):
		return l.lexNumber()
	case isIdentStart(c):
		for l.pos < len(l.src) && isIdentPart(l.src[l.pos]) {
			l.pos++
		}

		return token{kind: tokenIdent, text: l.src[start:l.pos], pos: start}, nil
	default:
		return token{}, fmt.Errorf("condition: unexpected character %q at position %d", string(c), start)
	}
}

// lexCompare consumes one of the six comparison operators.
func (l *lexer) lexCompare() (token, error) {
	start := l.pos
	next := byte(0)
	if l.pos+1 < len(l.src) {
		next = l.src[l.pos+1]
	}

	switch l.src[l.pos] {
	case '=':
		l.pos++

		return token{kind: tokenCompare, text: "=", pos: start}, nil
	case '!':
		if next == '=' {
			l.pos += 2

			return token{kind: tokenCompare, text: "!=", pos: start}, nil
		}

		return token{}, fmt.Errorf("condition: unexpected %q at position %d", "!", start)
	case '<':
		switch next {
		case '=':
			l.pos += 2

			return token{kind: tokenCompare, text: "<=", pos: start}, nil
		case '>':
			return token{}, fmt.Errorf("condition: unsupported operator %q at position %d: the not-equal spelling is %q", "<>", start, "!=")
		}
		l.pos++

		return token{kind: tokenCompare, text: "<", pos: start}, nil
	default: // '>'
		if next == '=' {
			l.pos += 2

			return token{kind: tokenCompare, text: ">=", pos: start}, nil
		}
		l.pos++

		return token{kind: tokenCompare, text: ">", pos: start}, nil
	}
}

// lexString consumes a single-quoted string; a quote inside the string is
// escaped by writing it twice — single quotes because condition text lives
// inside RoleConfig JSON, which owns double quotes.
func (l *lexer) lexString() (token, error) {
	start := l.pos
	l.pos++ // opening quote

	var b strings.Builder
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c != '\'' {
			b.WriteByte(c)
			l.pos++

			continue
		}
		if l.pos+1 < len(l.src) && l.src[l.pos+1] == '\'' {
			b.WriteByte('\'')
			l.pos += 2

			continue
		}
		l.pos++ // closing quote

		return token{kind: tokenString, text: b.String(), pos: start}, nil
	}

	return token{}, fmt.Errorf("condition: unterminated string starting at position %d", start)
}

// lexNumber consumes a signed integer or decimal. There is no arithmetic in
// the language, so a leading sign always belongs to the number.
func (l *lexer) lexNumber() (token, error) {
	start := l.pos
	if c := l.src[l.pos]; c == '+' || c == '-' {
		l.pos++
	}

	digits := 0
	for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
		l.pos++
		digits++
	}
	if digits == 0 {
		return token{}, fmt.Errorf("condition: malformed number at position %d", start)
	}

	if l.pos < len(l.src) && l.src[l.pos] == '.' {
		l.pos++
		fraction := 0
		for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
			l.pos++
			fraction++
		}
		if fraction == 0 {
			return token{}, fmt.Errorf("condition: malformed number at position %d: a decimal needs digits after the point", start)
		}
	}

	return token{kind: tokenNumber, text: l.src[start:l.pos], pos: start}, nil
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

// isIdentStart and isIdentPart pin the identifier charset,
// [A-Za-z_][A-Za-z0-9_]* — agreed jointly with the binding annotation
// grammar, so a parseable attribute is always a declarable binding name.
func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || isDigit(c)
}
