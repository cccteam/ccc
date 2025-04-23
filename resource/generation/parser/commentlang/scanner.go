package commentlang

import (
	"fmt"
	"slices"

	"github.com/go-playground/errors/v5"
)

type (
	keyword            string
	fieldKeywordNoArgs = keyword
	Keyword            interface {
		isKeyword()
	}
	arglessFieldKeyword interface {
		isArglessFieldKeyword()
	}
)

func (keyword) isKeyword()                        {}
func (fieldKeywordNoArgs) isArglessFieldKeyword() {}

var keywords = []keyword{PrimaryKey, ForeignKey, Check, Default, Substring, UniqueIndex}

const (
	// remember to add new keywords to the slice above ^^^
	illegal     keyword            = ""
	PrimaryKey  fieldKeywordNoArgs = "primarykey"
	ForeignKey  keyword            = "foreignkey"
	Check       keyword            = "check"
	Default     keyword            = "default"
	Substring   keyword            = "substring"
	UniqueIndex keyword            = "uniqueindex"
)

type ScanMode interface {
	mode() scanMode
}

type scanMode int

func (s scanMode) mode() scanMode {
	return s
}

const (
	ScanStruct scanMode = iota << 1
	ScanField
)

type scanner struct {
	src              []byte
	mode             scanMode
	identifiers      map[string]struct{}
	keywordArguments map[Keyword][]string
	pos              int
}

func Scan(src []string, mode ScanMode) (map[Keyword][]string, error) {
	results := make(map[Keyword][]string)
	for i := range src {
		scanner := newScanner([]byte(src[i]), mode.mode())
		if err := scanner.scan(); err != nil {
			return nil, err
		}

		results = combineResults(results, scanner.result())
	}

	return results, nil
}

func combineResults(r1, r2 map[Keyword][]string) map[Keyword][]string {
	var result map[Keyword][]string

	keys := make([]Keyword, 0, len(r1)+len(r2))

	for k := range r1 {
		keys = append(keys, k)
	}
	for k := range r2 {
		keys = append(keys, k)
	}

	keys = slices.Compact(keys)
	result = make(map[Keyword][]string, len(keys))

	for _, k := range keys {
		v1, ok1 := r1[k]
		v2, ok2 := r2[k]

		switch {
		case ok1 && ok2:
			result[k] = v1
			result[k] = append(result[k], v2...)
			result[k] = slices.Compact(result[k])
		case ok1:
			result[k] = v1
		case ok2:
			result[k] = v2
		}
	}

	return result
}

func newScanner(src []byte, mode scanMode) *scanner {
	return &scanner{
		src:              src,
		mode:             mode,
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

func (s *scanner) scan() error {
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

			var (
				arg []byte
				err error
			)
			if peek, ok := s.peekNext(); ok && peek == byte('(') {
				if !s.canHaveArguments(kw) {
					s.pos += 1 // push error karat to start of arguments
					return errors.New(s.errorPostscript("unexpected argument", "%s keyword cannot take arguments on a field", kw))
				}
				arg, err = s.scanArguments()
				if err != nil {
					return err
				}
			}

			s.addKeywordArgument(kw, arg)

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

	if arg != nil {
		s.keywordArguments[kw] = append(s.keywordArguments[kw], string(arg))
	}
}

func (s scanner) canHaveArguments(kw Keyword) bool {
	if s.mode == ScanStruct {
		return true
	}

	if _, ok := kw.(arglessFieldKeyword); !ok {
		return true
	}

	return false
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

func (s *scanner) result() map[Keyword][]string {
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
