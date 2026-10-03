// tfvars.go reads and edits a layer's placement, terraform.tfvars: the values the command
// reads (the boot project, the registrant mailbox, the domains registered so far) and the
// one entry it adds, keeping every other byte of the file.

package domain

import (
	"bytes"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
)

const (
	// registrationsKey is the placement attribute holding the registrations: a map from
	// domain to its yearly price and notices.
	registrationsKey = "registrations"
	// registrantKey is the placement attribute holding the registrant contact, and
	// emailKey its mailbox.
	registrantKey = "registrant_contact"
	emailKey      = "email"
	// priceKey and noticesKey are the two attributes of a registration.
	priceKey   = "yearly_price_usd"
	noticesKey = "notices"
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

// stringValue is the named attribute's value, or "" when the placement lacks it.
func (p *placement) stringValue(name string) (string, error) {
	attr, ok := p.body.Attributes[name]
	if !ok {
		return "", nil
	}
	v, diags := attr.Expr.Value(nil)
	if diags.HasErrors() {
		return "", errors.Wrap(diags, p.at(name))
	}
	if v.IsNull() || v.Type() != cty.String {
		return "", errors.Newf("%s is not a string", p.at(name))
	}

	return v.AsString(), nil
}

// registrantEmail is the registrant contact's mailbox, or "" when the placement names
// none.
func (p *placement) registrantEmail() (string, error) {
	attr, ok := p.body.Attributes[registrantKey]
	if !ok {
		return "", nil
	}
	v, diags := attr.Expr.Value(nil)
	if diags.HasErrors() {
		return "", errors.Wrap(diags, p.at(registrantKey))
	}
	if !v.Type().IsObjectType() || !v.Type().HasAttribute(emailKey) {
		return "", nil
	}
	email := v.GetAttr(emailKey)
	if email.IsNull() || email.Type() != cty.String {
		return "", nil
	}

	return strings.TrimSpace(email.AsString()), nil
}

// checkRegistrant refuses a placement whose registrant contact names no mailbox: the
// registrar's verification mail goes there.
func (p *placement) checkRegistrant() error {
	email, err := p.registrantEmail()
	if err != nil {
		return err
	}
	if email == "" {
		return errors.Newf("%s is empty: the registrant contact must name a mailbox a person reads, since the registrar's verification mail goes there", p.at(registrantKey+"."+emailKey))
	}

	return nil
}

// registrations lists the domains the registrations map holds, in file order; a
// placement without the map holds none.
func (p *placement) registrations() ([]string, error) {
	attr, ok := p.body.Attributes[registrationsKey]
	if !ok {
		return nil, nil
	}
	obj, ok := attr.Expr.(*hclsyntax.ObjectConsExpr)
	if !ok {
		return nil, errors.Newf("%s is not a map written out ({ ... })", p.at(registrationsKey))
	}
	names := make([]string, 0, len(obj.Items))
	for _, item := range obj.Items {
		name, err := objectKey(item.KeyExpr)
		if err != nil {
			return nil, errors.Wrap(err, p.at(registrationsKey))
		}
		names = append(names, name)
	}

	return names, nil
}

// checkUnregistered refuses a domain the placement already lists.
func (p *placement) checkUnregistered(name string) error {
	names, err := p.registrations()
	if err != nil {
		return err
	}
	if slices.Contains(names, name) {
		return errors.Newf("%s is already in %s", name, p.at(registrationsKey))
	}

	return nil
}

// objectKey is the name an object item's key spells, bare or quoted.
func objectKey(expr hclsyntax.Expression) (string, error) {
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

// Registration is one entry of the registrations map: a domain with the yearly price
// Cloud Domains quoted and the notices it asked to acknowledge.
type Registration struct {
	Domain         string
	YearlyPriceUSD int64
	Notices        []string
}

// AddRegistration adds the registration to the placement source's registrations map,
// creating the map when absent, and returns the file formatted. Every other byte,
// comments included, is kept; a domain already listed is refused.
func AddRegistration(src []byte, file string, r Registration) ([]byte, error) {
	p, err := parsePlacement(src, file)
	if err != nil {
		return nil, err
	}
	if err := p.checkUnregistered(r.Domain); err != nil {
		return nil, err
	}
	f, diags := hclwrite.ParseConfig(src, file, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, errors.Wrap(diags, "hclwrite.ParseConfig()")
	}
	body := f.Body()
	var tokens hclwrite.Tokens
	if attr := body.GetAttribute(registrationsKey); attr != nil {
		tokens = attr.Expr().BuildTokens(nil)
	} else {
		if len(bytes.TrimSpace(src)) > 0 {
			body.AppendNewline()
		}
		tokens = hclwrite.Tokens{token(hclsyntax.TokenOBrace, "{"), newline(), token(hclsyntax.TokenCBrace, "}")}
	}
	tokens, err = withEntry(tokens, entryTokens(r))
	if err != nil {
		return nil, errors.Wrap(err, p.at(registrationsKey))
	}
	body.SetAttributeRaw(registrationsKey, tokens)

	return hclwrite.Format(f.Bytes()), nil
}

// withEntry inserts the entry's tokens before the closing brace of the map's tokens, on
// a line of its own.
func withEntry(object, entry hclwrite.Tokens) (hclwrite.Tokens, error) {
	end := -1
	for i, t := range object {
		if t.Type == hclsyntax.TokenCBrace {
			end = i
		}
	}
	if end < 0 {
		return nil, errors.New("not a map written out ({ ... })")
	}
	out := make(hclwrite.Tokens, 0, len(object)+len(entry)+1)
	out = append(out, object[:end]...)
	if !endsLine(out) {
		out = append(out, newline())
	}
	out = append(out, entry...)
	out = append(out, object[end:]...)

	return out, nil
}

// endsLine reports whether the tokens end on a line break: a newline, or a line comment,
// which carries its own.
func endsLine(tokens hclwrite.Tokens) bool {
	if len(tokens) == 0 {
		return false
	}
	last := tokens[len(tokens)-1]

	return last.Type == hclsyntax.TokenNewline || (last.Type == hclsyntax.TokenComment && bytes.HasSuffix(last.Bytes, []byte("\n")))
}

// entryTokens spells the registration as a map entry, one attribute per line: the price
// first, then the notices when there are any.
func entryTokens(r Registration) hclwrite.Tokens {
	tokens := hclwrite.TokensForValue(cty.StringVal(r.Domain))
	tokens = append(tokens, token(hclsyntax.TokenEqual, "="), token(hclsyntax.TokenOBrace, "{"), newline())
	tokens = append(tokens, hclwrite.TokensForIdentifier(priceKey)...)
	tokens = append(tokens, token(hclsyntax.TokenEqual, "="))
	tokens = append(tokens, hclwrite.TokensForValue(cty.NumberIntVal(r.YearlyPriceUSD))...)
	tokens = append(tokens, newline())
	if len(r.Notices) > 0 {
		notices := make([]cty.Value, 0, len(r.Notices))
		for _, n := range r.Notices {
			notices = append(notices, cty.StringVal(n))
		}
		tokens = append(tokens, hclwrite.TokensForIdentifier(noticesKey)...)
		tokens = append(tokens, token(hclsyntax.TokenEqual, "="))
		tokens = append(tokens, hclwrite.TokensForValue(cty.ListVal(notices))...)
		tokens = append(tokens, newline())
	}
	tokens = append(tokens, token(hclsyntax.TokenCBrace, "}"), newline())

	return tokens
}

// token is one token of the given type and text.
func token(typ hclsyntax.TokenType, text string) *hclwrite.Token {
	return &hclwrite.Token{Type: typ, Bytes: []byte(text)}
}

// newline is a line break token.
func newline() *hclwrite.Token {
	return token(hclsyntax.TokenNewline, "\n")
}
