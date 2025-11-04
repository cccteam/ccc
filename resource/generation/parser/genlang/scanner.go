// Package genlang provides parsing for godoc comment annotations
package genlang

import (
	"fmt"

	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/go-playground/errors/v5"
)

type (
	scanMode int
	// Scanner provides methods for parsing godoc annotations on various types from the generation/parser package.
	Scanner interface {
		ScanStruct(*parser.Struct) (StructAnnotations, error)
		ScanNamedType(*parser.NamedType) (NamedTypeAnnotations, error)
	}
	scanner struct {
		src              []byte
		mode             scanMode
		keywordArguments map[string]Arg
		keywords         map[string]KeywordOpts
		pos              int
		maxKeywordLength int
	}

	// StructAnnotations holds the MultiMaps of keywords and arguments for a parser.Struct and each of its fields.
	StructAnnotations struct {
		Struct ArgMap
		Fields []ArgMap
	}

	// NamedTypeAnnotations holds the MultiMap of keywords and arguments for a parser.NamedType
	NamedTypeAnnotations struct {
		Named ArgMap
	}
)

// Scanning modes configure the scanner for different generation/parser types.
const (
	ScanStruct scanMode = iota
	ScanField
	ScanNamedType
)

// NewScanner constructs a Scanner with the given keywords and keyword options.
func NewScanner(keywords map[string]KeywordOpts) Scanner {
	maxKeywordLength := 0
	for k := range keywords {
		if l := len(k); l > maxKeywordLength {
			maxKeywordLength = l
		}
	}

	return &scanner{
		keywordArguments: make(map[string]Arg),
		keywords:         keywords,
		maxKeywordLength: maxKeywordLength,
	}
}

// ScanStruct scans the godoc annotations of a parser.Struct using the keywords & flags provided to the scanner.
func (s *scanner) ScanStruct(pStruct *parser.Struct) (StructAnnotations, error) {
	s.src = []byte(pStruct.Comments())
	s.mode = ScanStruct
	if err := s.scan(); err != nil {
		return StructAnnotations{}, err
	}

	structResults := ArgMap{s.result()}

	s.mode = ScanField
	fieldResults := make([]ArgMap, 0, len(pStruct.Fields()))
	for _, f := range pStruct.Fields() {
		s.src = []byte(f.Comments())
		s.pos = 0
		s.keywordArguments = make(map[string]Arg)

		if err := s.scan(); err != nil {
			return StructAnnotations{}, err
		}

		fieldResults = append(fieldResults, ArgMap{s.result()})
	}

	return StructAnnotations{structResults, fieldResults}, nil
}

// ScanNamedType scans the godoc annotations of a parser.NamedType using the keywords & flags provided to the scanner.
func (s *scanner) ScanNamedType(namedType *parser.NamedType) (NamedTypeAnnotations, error) {
	s.src = []byte(namedType.Comments)
	s.mode = ScanNamedType
	if err := s.scan(); err != nil {
		return NamedTypeAnnotations{}, err
	}

	return NamedTypeAnnotations{ArgMap{s.result()}}, nil
}

// moves the position pointer forward and returns the current character
func (s *scanner) next() (byte, bool) {
	if s.pos >= len(s.src) {
		return 0, true
	}

	char := s.src[s.pos]
	s.pos++

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

func (s *scanner) skipUntil(b byte) {
	var (
		char byte
		eof  bool
	)
	for !eof && char != b {
		char, eof = s.next()
	}
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

		case char != byte('@'): // if a line doesn't start with @ we ignore the comment
			s.skipUntil('\n')

		case char == byte('@'):
			key, ok := s.matchKeyword()
			if !ok {
				if key != "" {
					return errors.New(s.errorPostscript("invalid keyword", "did you mean %s?", key))
				}

				return errors.New(s.error("invalid keyword"))
			}

			if _, ok := s.keywordArguments[key]; ok && s.isExclusive(key) {
				return errors.New(s.error("%s used twice here", key))
			}

			var (
				arg Arg
				err error
			)
			if peek, ok := s.peekNext(); ok && peek == byte('(') {
				if !s.canHaveArguments(key) {
					s.pos++ // push error caret to start of arguments

					return errors.New(s.errorPostscript("unexpected argument", "%s keyword cannot take arguments here", key))
				}

				arg, err = s.scanArguments()
				if err != nil {
					return err
				}
			} else if s.requiresArguments(key) {
				return errors.New(s.errorPostscript("expected argument", "%[1]s requires an argument. hint: `%[1]s(<your arg here>)", key))
			}

			s.addKeywordArgument(key, arg)

		default:
			return errors.New(s.error("unexpected character %q", string(char)))
		}

		char, eof = s.next()
	}

	return nil
}

func (s *scanner) addKeywordArgument(key string, arg Arg) {
	if _, ok := s.keywordArguments[key]; !ok {
		s.keywordArguments[key] = arg

		return
	}

	s.keywordArguments[key] += Arg(fmt.Sprintf("\x00%s", arg)) // use a zero byte for separating multiple arguments
}

func (s scanner) canHaveArguments(key string) bool {
	opts, ok := s.keywords[key][s.mode]
	if !ok {
		return false
	}

	if !hasFlag(opts, NoArgs) {
		return true
	}

	return false
}

func (s scanner) requiresArguments(key string) bool {
	opts, ok := s.keywords[key][s.mode]
	if !ok {
		return false
	}

	if hasFlag(opts, ArgsRequired) {
		return true
	}

	return false
}

func (s scanner) isExclusive(key string) bool {
	opts, ok := s.keywords[key][s.mode]
	if !ok {
		return false
	}

	if hasFlag(opts, Exclusive) {
		return true
	}

	return false
}

func (s *scanner) scanArguments() (Arg, error) {
	var (
		opened          bool
		openParenthesis int

		buf  []byte
		char byte
		eof  bool
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
			openParenthesis++
			if opened {
				buf = append(buf, char)
			} else {
				opened = true
			}

		case char == byte(')'):
			openParenthesis--
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

		return Arg(""), errors.New(s.error("unclosed parenthesis"))
	}

	return Arg(buf), nil
}

func (s *scanner) result() map[string]Arg {
	return s.keywordArguments
}

func (s *scanner) consumeIdentifier() []byte {
	buf := make([]byte, 0)
	for {
		char, eof := s.next()
		if isWhitespace(char) || len(buf) > s.maxKeywordLength || eof {
			break
		}

		if char == byte('(') {
			// we want s.peek or s.next to pick this `(` up so we wind the position back by one
			s.pos--

			break
		}

		buf = append(buf, char)
	}

	return buf
}

func (s *scanner) matchKeyword() (string, bool) {
	currentPos := s.pos
	possibleMatch := ""
	var matchSimilarity float64

	ident := s.consumeIdentifier()
	for key := range s.keywords {
		if len(ident) == len(key) && string(ident) == key {
			return key, true
		}

		// calculating a similarity score for identifiers is expensive
		// so we should only do it if they're nearly the same length
		v := len(ident) - len(key)
		if -2 <= v && v <= 2 {
			if ss := similarity(string(ident), key); ss > matchSimilarity && ss > 0.65 {
				possibleMatch = key
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
			matches++

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
				matches++
				outOfOrder++
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
	counter := 0
	for {
		if s.pos+counter >= len(s.src) {
			return byte('\x00'), false
		}

		if !isWhitespace(s.src[s.pos+counter]) {
			break
		}

		counter++
	}

	return s.src[s.pos+counter], true
}

func (s *scanner) error(msg string, a ...any) string {
	msg = fmt.Sprintf(msg, a...)

	return s.errorPostscript(msg, msg)
}

func (s *scanner) errorPostscript(msg, postscript string, a ...any) string {
	postscript = fmt.Sprintf(postscript, a...)

	// rewind the position back 1 character for error printing
	if s.pos > 0 {
		s.pos--
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
			offset++
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

func hasFlag(option, flag keywordFlag) bool {
	return option&flag != 0
}
