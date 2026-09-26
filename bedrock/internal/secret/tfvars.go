// tfvars.go reads and edits a layer's placement, terraform.tfvars: the pins it holds under
// secret_versions, per environment and per variable, and the one entry the command sets,
// keeping every other byte of the file.

package secret

import (
	"bytes"
	"os"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
)

// versionsKey is the placement attribute holding the pins: a map from environment to a
// map from variable to version.
const versionsKey = "secret_versions"

// placement is a parsed terraform.tfvars.
type placement struct {
	file string
	body *hclsyntax.Body
}

// parsePlacement parses the placement source.
func parsePlacement(src []byte, file string) (*placement, error) {
	f, diags := hclsyntax.ParseConfig(src, file, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, errors.Wrap(diags, "hclsyntax.ParseConfig()")
	}
	body, ok := f.Body.(*hclsyntax.Body)
	if !ok {
		return nil, errors.Newf("%s: not native HCL syntax", file)
	}

	return &placement{file: file, body: body}, nil
}

// at names an attribute of the placement for a message.
func (p *placement) at(name string) string {
	return name + " in " + p.file
}

// PlacementEnvironments lists the environments the placement at file pins secrets for:
// the keys of its secret_versions map, in the file's order. A missing file, one without
// the map, and one whose map is not written out are refused.
func PlacementEnvironments(file string) ([]string, error) {
	src, err := os.ReadFile(file)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errors.Newf("no placement at %s", file)
		}

		return nil, errors.Wrap(err, "os.ReadFile()")
	}
	p, err := parsePlacement(src, file)
	if err != nil {
		return nil, err
	}
	obj, err := p.versionsMap()
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(obj.Items))
	for _, item := range obj.Items {
		key, err := ObjectKey(item.KeyExpr)
		if err != nil {
			return nil, errors.Wrap(err, p.at(versionsKey))
		}
		keys = append(keys, key)
	}

	return keys, nil
}

// versionsMap is the secret_versions map as the placement writes it. A placement without
// the map and one whose map is not written out are refused.
func (p *placement) versionsMap() (*hclsyntax.ObjectConsExpr, error) {
	attr, ok := p.body.Attributes[versionsKey]
	if !ok {
		return nil, errors.Newf("no %s: the application layer's placement pins the version of each secret per environment there", p.at(versionsKey))
	}
	obj, ok := attr.Expr.(*hclsyntax.ObjectConsExpr)
	if !ok {
		return nil, errors.Newf("%s is not a map written out ({ ... })", p.at(versionsKey))
	}

	return obj, nil
}

// versions is the environment's map of pins as the placement writes it. A placement
// without the secret_versions map, one whose map is not written out, and one whose map
// lacks the environment are refused.
func (p *placement) versions(env string) (*hclsyntax.ObjectConsExpr, error) {
	obj, err := p.versionsMap()
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(obj.Items))
	for _, item := range obj.Items {
		key, err := ObjectKey(item.KeyExpr)
		if err != nil {
			return nil, errors.Wrap(err, p.at(versionsKey))
		}
		if key != env {
			keys = append(keys, key)

			continue
		}
		inner, ok := item.ValueExpr.(*hclsyntax.ObjectConsExpr)
		if !ok {
			return nil, errors.Newf("%s is not a map written out ({ ... })", p.at(versionsKey+"."+env))
		}

		return inner, nil
	}
	if len(keys) == 0 {
		return nil, errors.Newf("%s has no %s map: it is empty", p.at(versionsKey), env)
	}

	return nil, errors.Newf("%s has no %s map: its keys are %s", p.at(versionsKey), env, strings.Join(keys, ", "))
}

// pinned is the version the placement pins for the variable in the environment, and
// whether it pins one. A value that is not a string counts as a pin to something else.
func (p *placement) pinned(env, variable string) (version string, ok bool, err error) {
	inner, err := p.versions(env)
	if err != nil {
		return "", false, err
	}
	for _, item := range inner.Items {
		key, err := ObjectKey(item.KeyExpr)
		if err != nil {
			return "", false, errors.Wrap(err, p.at(versionsKey+"."+env))
		}
		if key != variable {
			continue
		}
		v, diags := item.ValueExpr.Value(nil)
		if diags.HasErrors() {
			return "", false, errors.Wrap(diags, p.at(versionsKey+"."+env+"."+variable))
		}
		if v.IsNull() || v.Type() != cty.String {
			return "", true, nil
		}

		return v.AsString(), true, nil
	}

	return "", false, nil
}

// ObjectKey is the name an object item's key spells, bare or quoted.
func ObjectKey(expr hclsyntax.Expression) (string, error) {
	if name := hcl.ExprAsKeyword(expr); name != "" {
		return name, nil
	}
	v, diags := expr.Value(nil)
	if diags.HasErrors() {
		return "", errors.Wrap(diags, "hclsyntax.Expression.Value()")
	}
	if v.IsNull() || v.Type() != cty.String {
		return "", errors.New("a key that is not a string")
	}

	return v.AsString(), nil
}

// SetVersion pins the variable to the version in the environment's map of the placement
// source, adding the entry when absent and replacing its value when present, and returns
// the file formatted. Every other byte, comments included, is kept.
func SetVersion(src []byte, file, env, variable, version string) ([]byte, error) {
	p, err := parsePlacement(src, file)
	if err != nil {
		return nil, err
	}
	if _, err := p.versions(env); err != nil {
		return nil, err
	}
	f, diags := hclwrite.ParseConfig(src, file, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, errors.Wrap(diags, "hclwrite.ParseConfig()")
	}
	body := f.Body()
	tokens := body.GetAttribute(versionsKey).Expr().BuildTokens(nil)
	start, end, err := envObject(tokens, env)
	if err != nil {
		return nil, errors.Newf("%s: %s", p.at(versionsKey), err)
	}
	inner, err := withPin(tokens[start:end+1], variable, version)
	if err != nil {
		return nil, errors.Newf("%s: %s", p.at(versionsKey+"."+env), err)
	}
	body.SetAttributeRaw(versionsKey, splice(tokens, start, end+1, inner))

	return hclwrite.Format(f.Bytes()), nil
}

// item is one entry of an object's tokens: the name its key spells and where its value
// sits, as indexes into the object's tokens (the end exclusive).
type item struct {
	name       string
	valueStart int
	valueEnd   int
}

// objectItems lists the entries of an object's tokens, whose first token is its opening
// brace and last its closing brace.
func objectItems(obj hclwrite.Tokens) ([]item, error) {
	var items []item
	i := 1
	for i < len(obj)-1 {
		if separates(obj[i]) {
			i++

			continue
		}
		name, next, err := keyName(obj, i)
		if err != nil {
			return nil, err
		}
		if next >= len(obj)-1 || (obj[next].Type != hclsyntax.TokenEqual && obj[next].Type != hclsyntax.TokenColon) {
			return nil, errors.Newf("%s is not followed by =", name)
		}
		start := next + 1
		end := valueEnd(obj, start)
		items = append(items, item{name: name, valueStart: start, valueEnd: end})
		i = end
	}

	return items, nil
}

// keyName is the name the key at index i spells, bare or quoted, and the index after it.
func keyName(obj hclwrite.Tokens, i int) (name string, next int, err error) {
	t := obj[i]
	if t.Type == hclsyntax.TokenIdent {
		return string(t.Bytes), i + 1, nil
	}
	if t.Type == hclsyntax.TokenOQuote && i+2 < len(obj) && obj[i+1].Type == hclsyntax.TokenQuotedLit && obj[i+2].Type == hclsyntax.TokenCQuote {
		return string(obj[i+1].Bytes), i + 3, nil
	}

	return "", 0, errors.Newf("a key that is not a name, at %q", t.Bytes)
}

// valueEnd is the index after the value starting at start: the first token at the value's
// own depth that ends the entry (a newline, a comma, a comment, or the object's closing
// brace).
func valueEnd(obj hclwrite.Tokens, start int) int {
	depth := 0
	for i := start; i < len(obj); i++ {
		t := obj[i]
		switch {
		case opens(t):
			depth++
		case closes(t):
			if depth == 0 {
				return i
			}
			depth--
		case separates(t) && depth == 0:
			return i
		}
	}

	return len(obj)
}

// opens reports whether the token opens a nested object, list or parenthesis.
func opens(t *hclwrite.Token) bool {
	return slices.Contains([]hclsyntax.TokenType{hclsyntax.TokenOBrace, hclsyntax.TokenOBrack, hclsyntax.TokenOParen}, t.Type)
}

// closes reports whether the token closes one.
func closes(t *hclwrite.Token) bool {
	return slices.Contains([]hclsyntax.TokenType{hclsyntax.TokenCBrace, hclsyntax.TokenCBrack, hclsyntax.TokenCParen}, t.Type)
}

// separates reports whether the token separates entries: a newline, a comma, or a comment.
func separates(t *hclwrite.Token) bool {
	return slices.Contains([]hclsyntax.TokenType{hclsyntax.TokenNewline, hclsyntax.TokenComma, hclsyntax.TokenComment}, t.Type)
}

// envObject is where the environment's map sits within the secret_versions tokens: the
// indexes of its opening and closing braces.
func envObject(tokens hclwrite.Tokens, env string) (start, end int, err error) {
	if len(tokens) < 2 || tokens[0].Type != hclsyntax.TokenOBrace || tokens[len(tokens)-1].Type != hclsyntax.TokenCBrace {
		return 0, 0, errors.New("not a map written out ({ ... })")
	}
	items, err := objectItems(tokens)
	if err != nil {
		return 0, 0, err
	}
	for _, it := range items {
		if it.name != env {
			continue
		}
		if it.valueEnd-it.valueStart < 2 || tokens[it.valueStart].Type != hclsyntax.TokenOBrace || tokens[it.valueEnd-1].Type != hclsyntax.TokenCBrace {
			return 0, 0, errors.Newf("%s is not a map written out ({ ... })", env)
		}

		return it.valueStart, it.valueEnd - 1, nil
	}

	return 0, 0, errors.Newf("has no %s map", env)
}

// withPin sets the variable's entry in the environment map's tokens (its opening brace
// first, its closing brace last): the value is replaced when the entry exists, else the
// entry is added, after a comma when the map is written on one line and on a line of its
// own otherwise.
func withPin(obj hclwrite.Tokens, variable, version string) (hclwrite.Tokens, error) {
	items, err := objectItems(obj)
	if err != nil {
		return nil, err
	}
	value := hclwrite.TokensForValue(cty.StringVal(version))
	for _, it := range items {
		if it.name == variable {
			return splice(obj, it.valueStart, it.valueEnd, value), nil
		}
	}
	entry := hclwrite.TokensForIdentifier(variable)
	entry = append(entry, token(hclsyntax.TokenEqual, "="))
	entry = append(entry, value...)
	end := len(obj) - 1
	if len(items) > 0 && !hasNewline(obj) {
		repl := make(hclwrite.Tokens, 0, 1+len(entry))
		repl = append(repl, token(hclsyntax.TokenComma, ","))
		repl = append(repl, entry...)

		return splice(obj, end, end, repl), nil
	}
	var repl hclwrite.Tokens
	if !endsLine(obj[:end]) {
		repl = append(repl, newline())
	}
	repl = append(repl, entry...)
	repl = append(repl, newline())

	return splice(obj, end, end, repl), nil
}

// splice replaces tokens[start:end] with repl, in a new slice.
func splice(tokens hclwrite.Tokens, start, end int, repl hclwrite.Tokens) hclwrite.Tokens {
	out := make(hclwrite.Tokens, 0, len(tokens)-(end-start)+len(repl))
	out = append(out, tokens[:start]...)
	out = append(out, repl...)
	out = append(out, tokens[end:]...)

	return out
}

// hasNewline reports whether the tokens break a line anywhere: a newline, or a line
// comment, which carries its own.
func hasNewline(tokens hclwrite.Tokens) bool {
	for _, t := range tokens {
		if t.Type == hclsyntax.TokenNewline || (t.Type == hclsyntax.TokenComment && bytes.HasSuffix(t.Bytes, []byte("\n"))) {
			return true
		}
	}

	return false
}

// endsLine reports whether the tokens end on a line break: a newline, or a line comment,
// which carries its own.
func endsLine(tokens hclwrite.Tokens) bool {
	if len(tokens) == 0 {
		return false
	}

	return hasNewline(tokens[len(tokens)-1:])
}

// token is one token of the given type and text.
func token(typ hclsyntax.TokenType, text string) *hclwrite.Token {
	return &hclwrite.Token{Type: typ, Bytes: []byte(text)}
}

// newline is a line break token.
func newline() *hclwrite.Token {
	return token(hclsyntax.TokenNewline, "\n")
}
