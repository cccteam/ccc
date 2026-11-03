package generation

import (
	"fmt"
	"slices"
	"strings"

	"github.com/cloudspannerecosystem/memefish"
	"github.com/cloudspannerecosystem/memefish/token"
	"github.com/go-playground/errors/v5"
)

// The tables a query reads are read off its token stream, not off a parse tree: the
// lexer knows every token shape Spanner takes, while a parser would refuse a feature it
// has not learned yet and stop generation with it. A table reference is an identifier
// path in a table position: right after FROM or JOIN, or after a comma of a FROM list,
// which runs from its FROM to the next clause keyword at the same parenthesis depth or
// to the parenthesis closing that depth (ON and USING end nothing). An identifier
// followed by an opening parenthesis is a function (UNNEST, a table-valued function,
// GRAPH_TABLE), a parenthesis in a table position opens a subquery or a parenthesized
// join, and a table hint, an alias, TABLESAMPLE and FOR SYSTEM_TIME follow a table
// without taking a table position themselves. A WITH clause's names are tracked by
// scope: each is visible from the end of its own body to the end of the query at that
// depth, and so inside the later bodies of the same WITH, which is how a CTE named like
// a table shadows the table after its body and not inside it. The walk refuses rather
// than guesses: a text the lexer refuses, a recursive WITH and a name in a table
// position that is neither a table nor a CTE in scope each stop generation, naming the
// place, and the declaration (@changedBy), which replaces the reading, is the way out,
// so a shape the walk did not anticipate surfaces instead of passing as a miss.

// lexerError says the lexer refused the text at Line and Column (1-origin), with its
// finding in Message.
type lexerError struct {
	Line    int
	Column  int
	Message string
}

func (e lexerError) Error() string {
	if e.Line == 0 {
		return fmt.Sprintf("cannot be read: the lexer refused it (%s)", e.Message)
	}

	return fmt.Sprintf("cannot be read: the lexer stopped at line %d, column %d (%s)", e.Line, e.Column, e.Message)
}

// tablePositionError says an identifier in a table position named neither a table nor
// a CTE in scope; Line and Column are 1-origin.
type tablePositionError struct {
	Name   string
	Line   int
	Column int
}

func (e tablePositionError) Error() string {
	return fmt.Sprintf("names %s at line %d, column %d in a table position (after FROM, JOIN or a comma of a FROM list), and that is neither a table in the schema nor a CTE in scope", e.Name, e.Line, e.Column)
}

// recursiveWithError says the query opens a recursive WITH, which the derivation does
// not follow.
type recursiveWithError struct{}

func (recursiveWithError) Error() string {
	return "opens WITH RECURSIVE, which the derivation does not follow"
}

// queryTables returns the schema tables the query reads, in the schema's spelling,
// sorted and unique; tables maps each table's lowercased name to its spelling. The
// error is a lexerError when the lexer refused the text, a recursiveWithError or a
// tablePositionError when the walk refuses the query; each reads on from "the query".
func queryTables(sql string, tables map[string]string) ([]string, error) {
	file := &token.File{Buffer: sql}
	toks, err := lexAll(file)
	if err != nil {
		return nil, err
	}
	walk := &tableWalk{file: file, toks: toks, tables: tables, fromLists: make(map[int]bool), calls: make(map[int]string)}
	if err := walk.run(); err != nil {
		return nil, err
	}
	slices.Sort(walk.named)

	return walk.named, nil
}

// lexAll reads every token of the text up to its end, so the walk can look ahead; a
// refusal comes back as a lexerError.
func lexAll(file *token.File) ([]token.Token, error) {
	lex := &memefish.Lexer{File: file}
	var toks []token.Token
	for {
		if err := lex.NextToken(); err != nil {
			var refusal *memefish.Error
			if errors.As(err, &refusal) && refusal.Position != nil {
				return nil, lexerError{Line: refusal.Position.Line + 1, Column: refusal.Position.Column + 1, Message: refusal.Message}
			}

			return nil, lexerError{Message: err.Error()}
		}
		if lex.Token.Kind == token.TokenEOF {
			return toks, nil
		}
		toks = append(toks, *lex.Token.Clone())
	}
}

// scopedName is a name (a WITH name or a table alias) in scope from its depth down.
type scopedName struct {
	name  string
	depth int
}

// tableWalk is the walk's state over the token slice.
type tableWalk struct {
	file   *token.File
	toks   []token.Token
	tables map[string]string
	named  []string

	depth int
	// fromLists says a FROM list is open at the depth, so a comma there introduces a
	// table item.
	fromLists map[int]bool
	// calls names the function whose argument list a parenthesis depth is, when it is
	// one: EXTRACT takes a FROM that introduces no table.
	calls map[int]string
	// ctes are the WITH names in scope, each until the depth drops below its own.
	ctes []scopedName
	// aliases are the aliases given to table items, each until the depth drops below
	// its own; a path in a table position leaving from one is a correlated array
	// reference (FROM Ships s, s.cargo), not a table.
	aliases []scopedName
	// pending are the WITH items whose body is open, by the depth the WITH sits at; a
	// name enters scope when its body closes.
	pending []scopedName
	// tablePos says the token at hand follows FROM, JOIN or a FROM list's comma, and
	// aliasPos that it follows a table item, so an identifier is the item's alias.
	tablePos, aliasPos bool
	// callInTablePos says the parenthesis at hand opens the argument list of a
	// function in a table position, whose result is a table item.
	callInTablePos bool
}

// The keyword kinds the walk acts on; punctuation is matched by its literal, as the
// module's other lexer loop does.
const (
	kindAs        token.TokenKind = "AS"
	kindWith      token.TokenKind = "WITH"
	kindFrom      token.TokenKind = "FROM"
	kindJoin      token.TokenKind = "JOIN"
	kindSelect    token.TokenKind = "SELECT"
	kindRecursive token.TokenKind = "RECURSIVE"
)

// The clause keywords that end a FROM list at their depth: the reserved ones by kind,
// the others by spelling, since the lexer hands them back as identifiers.
var (
	fromListEnders        = []token.TokenKind{"WHERE", "GROUP", "HAVING", "WINDOW", "ORDER", "LIMIT", "UNION", "INTERSECT", "EXCEPT"}
	fromListEnderSpelling = []string{"QUALIFY", "OFFSET"}
)

func (w *tableWalk) run() error {
	for i := 0; i < len(w.toks); i++ {
		tok := w.toks[i]
		if tok.Kind == "@" && w.next(i).Kind == "{" {
			i = w.matching(i+1, "{", "}")

			continue
		}
		wasTablePos, wasAliasPos := w.tablePos, w.aliasPos
		w.tablePos, w.aliasPos = false, false
		switch {
		case tok.Kind == "(":
			w.openParen(i, wasTablePos)
		case tok.Kind == ")":
			next, err := w.closeParen(i)
			if err != nil {
				return err
			}
			i = next
		case tok.Kind == "[":
			i = w.matching(i, "[", "]")
		case tok.Kind == kindWith:
			next, err := w.openWith(i)
			if err != nil {
				return err
			}
			i = next
		case tok.Kind == kindFrom:
			if !strings.EqualFold(w.calls[w.depth], "EXTRACT") {
				w.fromLists[w.depth] = true
				w.tablePos = true
			}
		case tok.Kind == kindJoin:
			w.tablePos = true
		case tok.Kind == ",":
			w.tablePos = w.fromLists[w.depth]
		case w.endsFromList(i):
			delete(w.fromLists, w.depth)
		case tok.Kind == kindAs && wasAliasPos:
			w.aliasPos = true
		case tok.Kind == token.TokenIdent && wasTablePos:
			next, err := w.tableReference(i)
			if err != nil {
				return err
			}
			i = next
		case tok.Kind == token.TokenIdent && wasAliasPos:
			w.aliases = append(w.aliases, scopedName{name: tok.AsString, depth: w.depth})
		case wasTablePos && w.next(i).Kind == "(":
			w.callInTablePos = true
		}
	}

	return nil
}

// next returns the token after i, or an empty token at the end.
func (w *tableWalk) next(i int) token.Token {
	if i+1 < len(w.toks) {
		return w.toks[i+1]
	}

	return token.Token{}
}

// matching returns the index of the closing token pairing the opening one at i, or the
// last index when the text ends first.
func (w *tableWalk) matching(i int, open, closing token.TokenKind) int {
	depth := 0
	for j := i; j < len(w.toks); j++ {
		switch w.toks[j].Kind {
		case open:
			depth++
		case closing:
			depth--
			if depth == 0 {
				return j
			}
		default:
		}
	}

	return len(w.toks) - 1
}

// endsFromList reports whether the token at i is a clause keyword that closes the FROM
// list at the current depth; a word after a dot is a column, whatever it spells.
func (w *tableWalk) endsFromList(i int) bool {
	tok := w.toks[i]
	if slices.Contains(fromListEnders, tok.Kind) {
		return true
	}
	if i > 0 && w.toks[i-1].Kind == "." {
		return false
	}
	for _, spelling := range fromListEnderSpelling {
		if tok.IsKeywordLike(spelling) {
			return true
		}
	}

	return false
}

// openParen opens a parenthesis. In a table position it opens a subquery when a query
// follows, and a parenthesized join otherwise, whose items are in table position in
// turn; after a function name it opens the function's argument list.
func (w *tableWalk) openParen(i int, wasTablePos bool) {
	w.depth++
	delete(w.calls, w.depth)
	switch {
	case w.callInTablePos:
		w.callInTablePos = false
		w.calls[w.depth] = callName(&w.toks[i-1])
		w.pending = append(w.pending, scopedName{name: "", depth: w.depth - 1})
	case wasTablePos:
		w.pending = append(w.pending, scopedName{name: "", depth: w.depth - 1})
		if next := w.next(i); next.Kind != kindSelect && next.Kind != kindWith && next.Kind != "(" {
			w.fromLists[w.depth] = true
			w.tablePos = true
		}
	case i > 0:
		w.calls[w.depth] = callName(&w.toks[i-1])
	}
}

// callName is the function a parenthesis right after the token would call: an
// identifier's name, a keyword's word (EXTRACT, UNNEST), empty for anything else.
func callName(tok *token.Token) string {
	switch {
	case tok.Kind == token.TokenIdent:
		return tok.AsString
	case token.IsKeyword(string(tok.Kind)):
		return string(tok.Kind)
	default:
		return ""
	}
}

// closeParen closes a parenthesis: the FROM list and the names scoped inside it end,
// a table item that opened it leaves the walk in alias position, and a WITH item whose
// body it closes enters scope; a comma right after such a body opens the next item of
// the same WITH.
func (w *tableWalk) closeParen(i int) (int, error) {
	inner := w.depth
	w.depth--
	delete(w.fromLists, inner)
	w.ctes = slices.DeleteFunc(w.ctes, func(c scopedName) bool {
		return c.depth > w.depth
	})
	w.aliases = slices.DeleteFunc(w.aliases, func(a scopedName) bool {
		return a.depth > w.depth
	})
	if len(w.pending) == 0 || w.pending[len(w.pending)-1].depth != w.depth {
		return i, nil
	}
	item := w.pending[len(w.pending)-1]
	w.pending = w.pending[:len(w.pending)-1]
	if item.name == "" {
		w.aliasPos = true

		return i, nil
	}
	w.ctes = append(w.ctes, item)
	if w.next(i).Kind == "," {
		return w.withItem(i + 2)
	}

	return i, nil
}

// openWith reads the first item of a WITH clause. WITH OFFSET after UNNEST names the
// array item's offset column and opens no CTE: the OFFSET is read with the WITH so it
// does not pass for the clause keyword, and the alias that may follow is skipped like
// any other.
func (w *tableWalk) openWith(i int) (int, error) {
	next := w.next(i)
	if next.Kind == kindRecursive {
		return i, recursiveWithError{}
	}
	if next.IsKeywordLike("OFFSET") && (w.next(i+1).Kind != kindAs || w.next(i+2).Kind != "(") {
		w.aliasPos = true

		return i + 1, nil
	}

	return w.withItem(i + 1)
}

// withItem reads one WITH item's head at index i: the name, an optional column list,
// AS and the opening parenthesis of the body, which the walk then enters as any
// parenthesis; the name waits in pending until the body closes. It returns the index
// before the body's parenthesis, or i-1 when no item starts there.
func (w *tableWalk) withItem(i int) (int, error) {
	if i >= len(w.toks) || w.toks[i].Kind != token.TokenIdent {
		return i - 1, nil
	}
	name := w.toks[i].AsString
	j := i + 1
	if j < len(w.toks) && w.toks[j].Kind == "(" {
		j = w.matching(j, "(", ")") + 1
	}
	if j >= len(w.toks) || w.toks[j].Kind != kindAs || w.next(j).Kind != "(" {
		return i - 1, nil
	}
	w.pending = append(w.pending, scopedName{name: name, depth: w.depth})

	return j, nil
}

// tableReference classifies the identifier path at i, in a table position: a function
// call opens a table item, a CTE in scope is the CTE, a path leaving from an alias is a
// correlated reference, a schema table joins the set, and anything else refuses. It
// returns the index of the path's last token.
func (w *tableWalk) tableReference(i int) (int, error) {
	last := i
	parts := []string{w.toks[i].AsString}
	for w.next(last).Kind == "." && last+2 < len(w.toks) && w.toks[last+2].Kind == token.TokenIdent {
		last += 2
		parts = append(parts, w.toks[last].AsString)
	}
	if w.next(last).Kind == "(" {
		w.callInTablePos = true

		return last, nil
	}
	w.aliasPos = true
	name := parts[len(parts)-1]
	if dot := strings.LastIndex(name, "."); dot >= 0 {
		name = name[dot+1:]
	}
	if w.inScope(w.ctes, name) || len(parts) > 1 && w.inScope(w.aliases, parts[0]) {
		return last, nil
	}
	if table, ok := w.tables[strings.ToLower(name)]; ok {
		if !slices.Contains(w.named, table) {
			w.named = append(w.named, table)
		}

		return last, nil
	}
	position := w.file.Position(w.toks[i].Pos, w.toks[last].End)

	return last, tablePositionError{Name: w.file.Buffer[w.toks[i].Pos:w.toks[last].End], Line: position.Line + 1, Column: position.Column + 1}
}

// inScope reports whether the name is in the scoped list at the current depth.
func (w *tableWalk) inScope(names []scopedName, name string) bool {
	for _, scoped := range names {
		if scoped.depth <= w.depth && strings.EqualFold(scoped.name, name) {
			return true
		}
	}

	return false
}
