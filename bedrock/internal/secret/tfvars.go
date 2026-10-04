// tfvars.go reads and edits a layer's placement, terraform.tfvars: the pins it holds under
// secret_versions, per environment and per variable, and the one entry the command sets,
// keeping every other byte of the file.

package secret

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
)

// versionsKey is the placement attribute holding the pins: a map from environment to a
// map from variable to version. buildKey is its twin for the build-time secrets the image
// build reads (bedrock's stack: var.build_secrets), the same shape.
const (
	versionsKey = "secret_versions"
	buildKey    = "build_secrets"
)

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
	obj, err := p.versionsMap(versionsKey)
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

// BuildSecretNames lists the build-time secrets the layer's placement (terraform.tfvars
// in layerDir) declares for the environment, the keys of build_secrets.<env>, sorted.
// No file, no build_secrets map and no map for the environment all declare none; a map
// that is not written out is refused.
func BuildSecretNames(layerDir, env string) ([]string, error) {
	file := filepath.Join(layerDir, tfvarsFile)
	src, err := os.ReadFile(file)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, errors.Wrap(err, "os.ReadFile()")
	}
	p, err := parsePlacement(src, file)
	if err != nil {
		return nil, err
	}
	if _, ok := p.body.Attributes[buildKey]; !ok {
		return nil, nil
	}
	inner, err := p.versions(buildKey, env)
	if err != nil {
		if errors.Is(err, errNoEnvironment) {
			return nil, nil
		}

		return nil, err
	}
	names := make([]string, 0, len(inner.Items))
	for _, item := range inner.Items {
		name, err := ObjectKey(item.KeyExpr)
		if err != nil {
			return nil, errors.Wrap(err, p.at(buildKey+"."+env))
		}
		names = append(names, name)
	}
	slices.Sort(names)

	return names, nil
}

// The placement's maps EnvironmentValues reads for the pipeline: the substitutions the
// application declares for its hooks and its image build (the stack's var.substitutions),
// and the build secrets' pins (var.build_secrets).
const (
	SubstitutionsKey = "substitutions"
	BuildSecretsKey  = buildKey
)

// EnvironmentValues is the environment's map of strings under key in the layer's
// placement (terraform.tfvars in layerDir), as the file writes it: what the stack takes
// for the environment with lookup(var.<key>, env, {}). No file, no map under key and no
// map for the environment all hold none. A map that is not written out, a key that is not
// a string and a value that is not a literal string are refused.
func EnvironmentValues(layerDir, key, env string) (map[string]string, error) {
	values := map[string]string{}
	file := filepath.Join(layerDir, tfvarsFile)
	src, err := os.ReadFile(file)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return values, nil
		}

		return nil, errors.Wrap(err, "os.ReadFile()")
	}
	p, err := parsePlacement(src, file)
	if err != nil {
		return nil, err
	}
	if _, ok := p.body.Attributes[key]; !ok {
		return values, nil
	}
	inner, err := p.versions(key, env)
	if err != nil {
		if errors.Is(err, errNoEnvironment) {
			return values, nil
		}

		return nil, err
	}
	for _, item := range inner.Items {
		name, err := ObjectKey(item.KeyExpr)
		if err != nil {
			return nil, errors.Wrap(err, p.at(key+"."+env))
		}
		v, diags := item.ValueExpr.Value(nil)
		if diags.HasErrors() {
			return nil, errors.Wrap(diags, p.at(key+"."+env+"."+name))
		}
		if v.IsNull() || !v.Type().Equals(cty.String) {
			return nil, errors.Newf("%s is not a string", p.at(key+"."+env+"."+name))
		}
		values[name] = v.AsString()
	}

	return values, nil
}

// errNoEnvironment marks a map that lacks the environment asked for.
var errNoEnvironment = errors.New("no map for the environment")

// versionsMap is the pins map (secret_versions, or build_secrets) as the placement
// writes it. A placement without the map and one whose map is not written out are
// refused.
func (p *placement) versionsMap(key string) (*hclsyntax.ObjectConsExpr, error) {
	attr, ok := p.body.Attributes[key]
	if !ok {
		return nil, errors.Newf("no %s: the application layer's placement pins the version of each secret per environment there", p.at(key))
	}
	obj, ok := attr.Expr.(*hclsyntax.ObjectConsExpr)
	if !ok {
		return nil, errors.Newf("%s is not a map written out ({ ... })", p.at(key))
	}

	return obj, nil
}

// versions is the environment's map of pins as the placement writes it, under key. A
// placement without the map, one whose map is not written out, and one whose map lacks
// the environment (errNoEnvironment) are refused.
func (p *placement) versions(key, env string) (*hclsyntax.ObjectConsExpr, error) {
	obj, err := p.versionsMap(key)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(obj.Items))
	for _, item := range obj.Items {
		k, err := ObjectKey(item.KeyExpr)
		if err != nil {
			return nil, errors.Wrap(err, p.at(key))
		}
		if k != env {
			keys = append(keys, k)

			continue
		}
		inner, ok := item.ValueExpr.(*hclsyntax.ObjectConsExpr)
		if !ok {
			return nil, errors.Newf("%s is not a map written out ({ ... })", p.at(key+"."+env))
		}

		return inner, nil
	}
	if len(keys) == 0 {
		return nil, errors.Wrapf(errNoEnvironment, "%s has no %s map: it is empty", p.at(key), env)
	}

	return nil, errors.Wrapf(errNoEnvironment, "%s has no %s map: its keys are %s", p.at(key), env, strings.Join(keys, ", "))
}

// pinned is the version the placement pins for the variable in the environment under
// key, and whether it pins one. A value that is not a string counts as a pin to
// something else.
func (p *placement) pinned(key, env, variable string) (version string, ok bool, err error) {
	inner, err := p.versions(key, env)
	if err != nil {
		return "", false, err
	}
	for _, item := range inner.Items {
		k, err := ObjectKey(item.KeyExpr)
		if err != nil {
			return "", false, errors.Wrap(err, p.at(key+"."+env))
		}
		if k != variable {
			continue
		}
		v, diags := item.ValueExpr.Value(nil)
		if diags.HasErrors() {
			return "", false, errors.Wrap(diags, p.at(key+"."+env+"."+variable))
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
// source (secret_versions), adding the entry when absent and replacing its value when
// present, and returns the file formatted. Every other byte, comments included, is kept.
func SetVersion(src []byte, file, env, variable, version string) ([]byte, error) {
	return setVersion(src, file, versionsKey, env, variable, version)
}

// SetBuildVersion is SetVersion for a build-time secret, whose pin lives under
// build_secrets.
func SetBuildVersion(src []byte, file, env, variable, version string) ([]byte, error) {
	return setVersion(src, file, buildKey, env, variable, version)
}

// setVersion pins under the map key names.
func setVersion(src []byte, file, key, env, variable, version string) ([]byte, error) {
	p, err := parsePlacement(src, file)
	if err != nil {
		return nil, err
	}
	if _, err := p.versions(key, env); err != nil {
		return nil, err
	}
	f, diags := hclwrite.ParseConfig(src, file, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, errors.Wrap(diags, "hclwrite.ParseConfig()")
	}
	body := f.Body()
	tokens := body.GetAttribute(key).Expr().BuildTokens(nil)
	start, end, err := envObject(tokens, env)
	if err != nil {
		return nil, errors.Newf("%s: %s", p.at(key), err)
	}
	inner, err := withPin(tokens[start:end+1], variable, version)
	if err != nil {
		return nil, errors.Newf("%s: %s", p.at(key+"."+env), err)
	}
	body.SetAttributeRaw(key, splice(tokens, start, end+1, inner))

	return hclwrite.Format(f.Bytes()), nil
}

// SetMapEntry sets the key to the value in the map a values file's attribute holds,
// adding the entry when absent and replacing its value when present, and returns the
// file formatted; a file without the attribute gains it at its end, as a map of that one
// entry. Every other byte, comments included, is kept. An attribute that is not a map
// written out ({ ... }) is refused.
func SetMapEntry(src []byte, file, attribute, key, value string) ([]byte, error) {
	if _, err := parsePlacement(src, file); err != nil {
		return nil, err
	}
	f, diags := hclwrite.ParseConfig(src, file, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, errors.Wrap(diags, "hclwrite.ParseConfig()")
	}
	body := f.Body()
	attr := body.GetAttribute(attribute)
	if attr == nil {
		body.SetAttributeValue(attribute, cty.ObjectVal(map[string]cty.Value{key: cty.StringVal(value)}))

		return hclwrite.Format(f.Bytes()), nil
	}
	tokens := attr.Expr().BuildTokens(nil)
	if len(tokens) < 2 || tokens[0].Type != hclsyntax.TokenOBrace || tokens[len(tokens)-1].Type != hclsyntax.TokenCBrace {
		return nil, errors.Newf("%s in %s is not a map written out ({ ... })", attribute, file)
	}
	set, err := withPin(tokens, key, value)
	if err != nil {
		return nil, errors.Newf("%s in %s: %s", attribute, file, err)
	}
	body.SetAttributeRaw(attribute, set)

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
