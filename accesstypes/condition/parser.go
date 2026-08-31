package condition

import (
	"fmt"
	"strings"
)

// The engine rejects expressions over these generous fixed limits at grant
// write; Parse enforces them so every caller gets the same fence.
const (
	// maxSourceBytes is the 4 KB source-size limit.
	maxSourceBytes = 4096

	// maxDepth is the nesting-depth limit: parenthesized groups and NOT
	// chains deepen the tree, and 32 levels is beyond any legible policy.
	maxDepth = 32
)

// Reserved words. They are matched case-sensitively — identifiers are
// case-sensitive throughout the language, and these live in identifier
// positions. A binding carrying any of these names is a generation error, so
// the parser owns their meaning outright.
const (
	reservedSubject = "subject"
	reservedNew     = "new"
)

// Parse compiles condition source into its vocabulary AST. It enforces the
// grammar and the fixed limits (4 KB source, nesting depth 32); everything a
// schema is needed for — binding names against the Collection, literal types
// against attribute types, image validity per permission — is the callers'
// validation, downstream of the tree this returns.
func Parse(source string) (Expr, error) {
	if len(source) > maxSourceBytes {
		return nil, fmt.Errorf("condition: source is %d bytes, over the %d-byte limit", len(source), maxSourceBytes)
	}

	p := &parser{lex: &lexer{src: source}}
	if err := p.advance(); err != nil {
		return nil, err
	}
	if p.tok.kind == tokenEOF {
		return nil, fmt.Errorf("condition: empty condition")
	}

	expr, err := p.parseOr(1)
	if err != nil {
		return nil, err
	}
	if p.tok.kind != tokenEOF {
		return nil, fmt.Errorf("condition: unexpected %q after the end of the condition at position %d", p.tok.text, p.tok.pos)
	}

	return expr, nil
}

type parser struct {
	lex *lexer
	tok token
}

func (p *parser) advance() error {
	tok, err := p.lex.next()
	if err != nil {
		return err
	}
	p.tok = tok

	return nil
}

// keywordIs matches the current token against a keyword, case-insensitively —
// keywords are case-insensitive while identifiers are exact.
func (p *parser) keywordIs(keyword string) bool {
	return p.tok.kind == tokenIdent && strings.EqualFold(p.tok.text, keyword)
}

func (p *parser) parseOr(depth int) (Expr, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("condition: nesting deeper than the %d-level limit", maxDepth)
	}

	first, err := p.parseAnd(depth)
	if err != nil {
		return nil, err
	}

	operands := []Expr{first}
	for p.keywordIs(kwOr) {
		if err := p.advance(); err != nil {
			return nil, err
		}
		next, err := p.parseAnd(depth)
		if err != nil {
			return nil, err
		}
		operands = append(operands, next)
	}
	if len(operands) == 1 {
		return first, nil
	}

	return Or{Operands: operands}, nil
}

func (p *parser) parseAnd(depth int) (Expr, error) {
	first, err := p.parseUnary(depth)
	if err != nil {
		return nil, err
	}

	operands := []Expr{first}
	for p.keywordIs(kwAnd) {
		if err := p.advance(); err != nil {
			return nil, err
		}
		next, err := p.parseUnary(depth)
		if err != nil {
			return nil, err
		}
		operands = append(operands, next)
	}
	if len(operands) == 1 {
		return first, nil
	}

	return And{Operands: operands}, nil
}

func (p *parser) parseUnary(depth int) (Expr, error) {
	if !p.keywordIs(kwNot) {
		return p.parsePrimary(depth)
	}

	if depth+1 > maxDepth {
		return nil, fmt.Errorf("condition: nesting deeper than the %d-level limit", maxDepth)
	}
	if err := p.advance(); err != nil {
		return nil, err
	}
	operand, err := p.parseUnary(depth + 1)
	if err != nil {
		return nil, err
	}

	return Not{Operand: operand}, nil
}

func (p *parser) parsePrimary(depth int) (Expr, error) {
	if p.tok.kind == tokenLParen {
		if err := p.advance(); err != nil {
			return nil, err
		}
		inner, err := p.parseOr(depth + 1)
		if err != nil {
			return nil, err
		}
		if p.tok.kind != tokenRParen {
			return nil, fmt.Errorf("condition: expected %q at position %d", ")", p.tok.pos)
		}
		if err := p.advance(); err != nil {
			return nil, err
		}

		return inner, nil
	}

	return p.parseComparison()
}

func (p *parser) parseComparison() (Expr, error) {
	left, err := p.parseRef()
	if err != nil {
		return nil, err
	}

	switch {
	case p.tok.kind == tokenCompare:
		op := CompareOp(p.tok.text)
		if err := p.advance(); err != nil {
			return nil, err
		}
		right, err := p.parseOperand()
		if err != nil {
			return nil, err
		}

		return Comparison{Left: left, Op: op, Right: right}, nil

	case p.keywordIs(kwNot) || p.keywordIs(kwIn):
		negated := false
		if p.keywordIs(kwNot) {
			negated = true
			if err := p.advance(); err != nil {
				return nil, err
			}
			if !p.keywordIs(kwIn) {
				return nil, fmt.Errorf("condition: expected IN after NOT at position %d", p.tok.pos)
			}
		}
		if left.IsNow() {
			return nil, fmt.Errorf("condition: now supports only relational comparison, not IN")
		}
		if err := p.advance(); err != nil {
			return nil, err
		}

		return p.parseInBody(left, negated)

	case p.keywordIs(kwIs):
		if left.IsNow() {
			return nil, fmt.Errorf("condition: now supports only relational comparison, not IS NULL")
		}
		if err := p.advance(); err != nil {
			return nil, err
		}
		negated := false
		if p.keywordIs(kwNot) {
			negated = true
			if err := p.advance(); err != nil {
				return nil, err
			}
		}
		if !p.keywordIs(kwNull) {
			return nil, fmt.Errorf("condition: expected NULL at position %d", p.tok.pos)
		}
		if err := p.advance(); err != nil {
			return nil, err
		}

		return NullTest{Left: left, Negated: negated}, nil

	default:
		return nil, fmt.Errorf("condition: expected a comparison operator, IN, or IS after %q at position %d", left.String(), p.tok.pos)
	}
}

// parseRef parses a comparison's left side: `[new.] identifier`, or the
// reserved fact now. subject and subject attributes never stand on the left —
// pure subject classification belongs to roles, and subject attributes enter
// as operands (scalar) or sets (IN).
func (p *parser) parseRef() (Ref, error) {
	if p.tok.kind != tokenIdent {
		return Ref{}, fmt.Errorf("condition: expected an attribute at position %d", p.tok.pos)
	}

	name := p.tok.text
	pos := p.tok.pos

	if isKeywordText(name) {
		return Ref{}, fmt.Errorf("condition: keyword %q where an attribute is expected at position %d", name, pos)
	}

	switch name {
	case reservedSubject:
		return Ref{}, fmt.Errorf("condition: subject cannot stand on the left side of a comparison at position %d", pos)
	case reservedNew:
		if err := p.advance(); err != nil {
			return Ref{}, err
		}
		if p.tok.kind != tokenDot {
			return Ref{}, fmt.Errorf("condition: expected %q after new at position %d", ".", p.tok.pos)
		}
		if err := p.advance(); err != nil {
			return Ref{}, err
		}
		attr, err := p.identifierText("an attribute after new.")
		if err != nil {
			return Ref{}, err
		}
		if err := p.advance(); err != nil {
			return Ref{}, err
		}

		return Ref{Name: attr, PostImage: true}, nil
	default:
		if err := p.advance(); err != nil {
			return Ref{}, err
		}

		return Ref{Name: name}, nil
	}
}

func (p *parser) parseOperand() (Operand, error) {
	switch p.tok.kind {
	case tokenString:
		value := p.tok.text
		if err := p.advance(); err != nil {
			return nil, err
		}

		return StringLiteral{Value: value}, nil

	case tokenNumber:
		text := p.tok.text
		if err := p.advance(); err != nil {
			return nil, err
		}

		return NumberLiteral{Text: text}, nil

	case tokenIdent:
		switch {
		case p.keywordIs(kwTrue), p.keywordIs(kwFalse):
			value := strings.EqualFold(p.tok.text, kwTrue)
			if err := p.advance(); err != nil {
				return nil, err
			}

			return BoolLiteral{Value: value}, nil

		case p.tok.text == nowName:
			if err := p.advance(); err != nil {
				return nil, err
			}

			return Now{}, nil

		case p.tok.text == reservedSubject:
			if err := p.advance(); err != nil {
				return nil, err
			}
			if p.tok.kind != tokenDot {
				return Subject{}, nil
			}
			if err := p.advance(); err != nil {
				return nil, err
			}
			name, err := p.identifierText("a subject attribute after subject.")
			if err != nil {
				return nil, err
			}
			if err := p.advance(); err != nil {
				return nil, err
			}

			return SubjectValue{Name: name}, nil

		default:
			return nil, fmt.Errorf("condition: %q is not a valid operand at position %d — attribute-to-attribute comparison is not in the language", p.tok.text, p.tok.pos)
		}

	default:
		return nil, fmt.Errorf("condition: expected an operand at position %d", p.tok.pos)
	}
}

// parseInBody parses what follows IN: a parenthesized literal list, or a
// @subjectSet reference (`subject.name`).
func (p *parser) parseInBody(left Ref, negated bool) (Expr, error) {
	if p.tok.kind == tokenIdent && p.tok.text == reservedSubject {
		if err := p.advance(); err != nil {
			return nil, err
		}
		if p.tok.kind != tokenDot {
			return nil, fmt.Errorf("condition: IN subject requires a set name — subject.name at position %d", p.tok.pos)
		}
		if err := p.advance(); err != nil {
			return nil, err
		}
		name, err := p.identifierText("a subject set after subject.")
		if err != nil {
			return nil, err
		}
		if err := p.advance(); err != nil {
			return nil, err
		}

		return In{Left: left, Negated: negated, SubjectSet: name}, nil
	}

	if p.tok.kind != tokenLParen {
		return nil, fmt.Errorf("condition: expected %q or subject.name after IN at position %d", "(", p.tok.pos)
	}
	if err := p.advance(); err != nil {
		return nil, err
	}

	var literals []Literal
	for {
		lit, err := p.parseLiteral()
		if err != nil {
			return nil, err
		}
		literals = append(literals, lit)

		if p.tok.kind == tokenComma {
			if err := p.advance(); err != nil {
				return nil, err
			}

			continue
		}

		break
	}
	if p.tok.kind != tokenRParen {
		return nil, fmt.Errorf("condition: expected %q at position %d", ")", p.tok.pos)
	}
	if err := p.advance(); err != nil {
		return nil, err
	}

	return In{Left: left, Negated: negated, Literals: literals}, nil
}

func (p *parser) parseLiteral() (Literal, error) {
	switch {
	case p.tok.kind == tokenString:
		value := p.tok.text
		if err := p.advance(); err != nil {
			return nil, err
		}

		return StringLiteral{Value: value}, nil
	case p.tok.kind == tokenNumber:
		text := p.tok.text
		if err := p.advance(); err != nil {
			return nil, err
		}

		return NumberLiteral{Text: text}, nil
	case p.keywordIs(kwTrue), p.keywordIs(kwFalse):
		value := strings.EqualFold(p.tok.text, kwTrue)
		if err := p.advance(); err != nil {
			return nil, err
		}

		return BoolLiteral{Value: value}, nil
	default:
		return nil, fmt.Errorf("condition: expected a literal at position %d", p.tok.pos)
	}
}

// identifierText validates that the current token is a plain identifier —
// not a keyword, not a reserved word — and returns its text.
func (p *parser) identifierText(what string) (string, error) {
	if p.tok.kind != tokenIdent {
		return "", fmt.Errorf("condition: expected %s at position %d", what, p.tok.pos)
	}
	name := p.tok.text
	if isKeywordText(name) || name == reservedSubject || name == nowName || name == reservedNew {
		return "", fmt.Errorf("condition: %q cannot be used as %s at position %d", name, what, p.tok.pos)
	}

	return name, nil
}

// The case-insensitive keywords. subject, now, and new are reserved words,
// not keywords: they live in identifier positions and match case-sensitively.
const (
	kwAnd   = "AND"
	kwOr    = "OR"
	kwNot   = "NOT"
	kwIn    = "IN"
	kwIs    = "IS"
	kwNull  = "NULL"
	kwTrue  = "TRUE"
	kwFalse = "FALSE"
)

var keywords = []string{kwAnd, kwOr, kwNot, kwIn, kwIs, kwNull, kwTrue, kwFalse}

func isKeywordText(text string) bool {
	for _, kw := range keywords {
		if strings.EqualFold(text, kw) {
			return true
		}
	}

	return false
}
