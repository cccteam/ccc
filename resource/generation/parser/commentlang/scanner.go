package commentlang

import (
	"fmt"

	"github.com/go-playground/errors/v5"
)

type (
	keyword string
	Keyword interface {
		isKeyword()
	}
)

func (keyword) isKeyword() {}

var keywords = []keyword{PrimaryKey, ForeignKey, Substring, UniqueIndex}

const (
	illegal     keyword = ""
	PrimaryKey  keyword = "primarykey"
	ForeignKey  keyword = "foreignkey"
	Substring   keyword = "substring"
	UniqueIndex keyword = "uniqueindex"
)

type scanner struct {
	src              []byte
	identifiers      map[string]struct{}
	keywordArguments map[Keyword][]string
	pos              int
}

func NewScanner(src []byte) *scanner {
	return &scanner{
		src:              src,
		identifiers:      make(map[string]struct{}),
		keywordArguments: make(map[Keyword][]string),
	}
}

// moves the position pointer forward and returns the current character
func (s *scanner) next() (byte, bool) {
	if s.pos >= len(s.src) {
		return 0, true
	}

	char := s.src[s.pos]
	s.pos += 1

	return char, false
}

func (s *scanner) consumeWhitespace() (byte, bool) {
	var (
		char byte
		eof  bool
	)
	for !eof && isWhitespace(char) {
		char, eof = s.next()
	}

	return char, eof
}

func (s *scanner) Scan() error {
	var (
		char byte
		eof  bool
	)

	char, eof = s.consumeWhitespace()

	for !eof {
		switch {
		case isWhitespace(char):
			break

		case char == byte('/'):
			if peek, ok := s.peekNext(); ok {
				switch peek {
				case byte('/'), byte('*'):
					char, eof = s.next()
				default:
					return errors.New(s.error("unexpected character %q", string(peek)))
				}
			}

		case char == byte('*'):
			if peek, ok := s.peekNext(); ok && peek == byte('/') {
				return nil
			}

		case char == byte('@'):
			kw, ok := s.matchKeyword()
			if !ok {
				if kw != illegal {
					return errors.New(s.errorPostscript("invalid keyword", "did you mean %s?", kw))
				}
				return errors.New(s.error("invalid keyword"))
			}

			if peek, ok := s.peekNext(); ok && peek == byte('(') {
				arg, err := s.scanArguments()
				if err != nil {
					return err
				}
				s.addKeywordArgument(kw, arg)
			}

		default:
			return errors.New(s.error("unexpected character %q", string(char)))
		}

		char, eof = s.next()
	}

	return nil
}

func (s *scanner) addKeywordArgument(kw Keyword, arg []byte) {
	if _, ok := s.keywordArguments[kw]; !ok {
		s.keywordArguments[kw] = make([]string, 0, 1)
	}

	s.keywordArguments[kw] = append(s.keywordArguments[kw], string(arg))
}

func (s *scanner) scanArguments() ([]byte, error) {
	var (
		opened          bool
		openParenthesis int
		buf             []byte
		char            byte
		eof             bool
	)

	currentPos := s.pos

	char, eof = s.next()
loop:
	for !eof {
		switch {
		case isWhitespace(char):
			if !opened {
				break
			} else if openParenthesis == 0 {
				break loop
			}
			buf = append(buf, char)

		case char == byte('('):
			openParenthesis += 1
			if opened {
				buf = append(buf, char)
			} else {
				opened = true
			}

		case char == byte(')'):
			openParenthesis -= 1
			if openParenthesis == 0 {
				break loop
			}
			buf = append(buf, char)

		default:
			buf = append(buf, char)
		}

		char, eof = s.next()
	}

	if openParenthesis > 0 {
		s.pos = currentPos
		return nil, errors.New(s.error("unclosed parenthesis"))
	}

	return buf, nil
}

func (s *scanner) Result() map[Keyword][]string {
	return s.keywordArguments
}

func (s *scanner) consumeIdentifier() []byte {
	buf := make([]byte, 0)
	for {
		char, eof := s.next()
		// If buffer is longer than any known identifier we should return
		if isWhitespace(char) || len(buf) > 12 || eof {
			break
		}

		if char == byte('(') {
			// we want s.peek or s.next to pick this `(` up so we wind the position back by one
			s.pos -= 1
			break
		}

		buf = append(buf, char)
	}

	return buf
}

func (s *scanner) matchKeyword() (Keyword, bool) {
	currentPos := s.pos
	possibleMatch := illegal
	var matchSimilarity float64

	ident := s.consumeIdentifier()
	for _, kword := range keywords {
		if len(ident) == len(kword) && keyword(ident) == kword {
			return kword, true
		}

		// calculating a similarity score for identifiers is expensive
		// so we should only do it if they're nearly the same length
		v := len(ident) - len(kword)
		if -2 <= v && v <= 2 {
			if ss := similarity(string(ident), string(kword)); ss > matchSimilarity && ss > 0.65 {
				possibleMatch = kword
				matchSimilarity = ss
			}
		}
	}

	// rewind the position for accurate error messaging
	s.pos = currentPos

	return possibleMatch, false
}

// Calculates an edit distance between two strings using Jaro similarity:
// https://en.wikipedia.org/wiki/Jaro%E2%80%93Winkler_distance
func similarity(a, b string) float64 {
	var (
		short, long         string
		matches, outOfOrder float64
	)

	if len(a) > len(b) {
		short = b
		long = a
	} else {
		short = a
		long = b
	}

	windowSize := (len(long) / 2) - 1

	for i := range short {
		if short[i] == long[i] {
			matches += 1
			continue
		}

		var left, right int
		if i-windowSize > 0 {
			left = i - windowSize
		}

		if i+windowSize < len(long) {
			right = i + windowSize
		} else {
			right = len(long)
		}

		for j := left; j < right; j++ {
			if short[i] == long[j] {
				matches += 1
				outOfOrder += 1
			}
		}
	}

	if matches == 0 {
		return 0
	}

	transpositions := outOfOrder / 2
	shortLen := float64(len(short))
	longLen := float64(len(long))

	return ((matches / shortLen) + (matches / longLen) + ((matches - transpositions) / matches)) / 3
}

// returns the current char without moving the position pointer
func (s *scanner) peekNext() (byte, bool) {
	if s.pos >= len(s.src) {
		return byte('\x00'), false
	}

	return s.src[s.pos], true
}

func (s *scanner) error(msg string, a ...any) string {
	msg = fmt.Sprintf(msg, a...)

	return s.errorPostscript(msg, msg)
}

func (s *scanner) errorPostscript(msg, postscript string, a ...any) string {
	postscript = fmt.Sprintf(postscript, a...)

	// rewind the position back 1 character for error printing
	if s.pos > 0 {
		s.pos -= 1
	}

	buffer := make([]byte, 0, len(s.src)+len(postscript))
	offset := 0
srcLoop:
	for i := range s.src {
		switch {
		case s.src[i] == byte('\n') && i < s.pos:
			buffer = make([]byte, 0, len(s.src)+len(postscript)-offset)
			offset = 0
			continue
		case s.src[i] == byte('\n') && i >= s.pos:
			break srcLoop
		case s.src[i] == byte('\t') && i < s.pos:
			offset += 4
		case i < s.pos:
			offset += 1
		}
		buffer = append(buffer, s.src[i])
	}

	buffer = append(buffer, byte('\n'))

	for range offset {
		buffer = append(buffer, byte(' '))
	}

	buffer = append(buffer, byte('^'))
	buffer = append(buffer, []byte(postscript)...)

	return fmt.Sprintf("%s:\n%s", msg, string(buffer))
}

func isWhitespace(b byte) bool {
	switch b {
	case byte(' '), byte('\t'), byte('\n'), byte('\x00'):
		return true
	default:
		return false
	}
}
