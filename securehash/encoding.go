package securehash

import (
	"encoding/hex"

	"github.com/go-playground/errors/v5"
)

const (
	dot = '.'
	sep = '$'
)

type token struct {
	pre rune
	val []byte
}

// parse parses a hash into a map according to a template. The template must be in the form of a concatenated list of [<separator><key>, ...].
//
// Example: "$mem$salt.key"
func parse(template string, hash []byte) (map[string][]byte, error) {
	t, err := tokenize([]byte(template))
	if err != nil {
		return nil, errors.Wrap(err, "failed to tokenize template")
	}
	h, err := tokenize(hash)
	if err != nil {
		return nil, errors.Wrap(err, "failed to tokenize hash")
	}

	if len(t) != len(h) {
		return nil, errors.Newf("expected hash format of %s, got %s", template, string(hash))
	}

	params := make(map[string][]byte, len(t))

	for i := range t {
		tTok := t[i]
		hTok := h[i]

		if tTok.pre != hTok.pre {
			return nil, errors.Newf("parameter %d mismatch expected to find prefix %s, got %s", i, string(tTok.pre), string(hTok.pre))
		}

		name := string(tTok.val)
		_, ok := params[name]
		if ok {
			return nil, errors.Newf("found duplicated param %s", name)
		}
		params[name] = hTok.val
	}

	return params, nil
}

func tokenize(bytes []byte) ([]token, error) {
	if bytes == nil {
		return nil, nil
	}

	if initial := bytes[0]; initial != sep {
		return nil, errors.Newf("initial byte must be %s, found %s", string(sep), string(initial))
	}

	tokens := []token{{pre: sep}}

	for _, b := range bytes[1:] {
		if rune(b) == dot || rune(b) == sep {
			tokens = append(tokens, token{pre: rune(b)})

			continue
		}

		tokens[len(tokens)-1].val = append(tokens[len(tokens)-1].val, b)
	}

	return tokens, nil
}

func encodeHex(src []byte) []byte {
	enc := make([]byte, hex.EncodedLen(len(src)))
	hex.Encode(enc, src)

	return enc
}

func decodeHex(src []byte) ([]byte, error) {
	dec := make([]byte, hex.DecodedLen(len(src)))
	n, err := hex.Decode(dec, src)
	if err != nil {
		return nil, errors.Wrap(err, "hex.Decode()")
	}

	return dec[:n], nil
}
