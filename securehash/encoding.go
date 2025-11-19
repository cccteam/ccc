package securehash

import (
	"encoding/base64"

	"github.com/go-playground/errors/v5"
)

const (
	dot = '.'
	eql = '='
	sep = '$'
)

func encodeBase64(dec []byte) []byte {
	enc := make([]byte, base64.StdEncoding.EncodedLen(len(dec)))
	base64.StdEncoding.Encode(enc, dec)

	return enc
}

func decodeBase64(enc []byte) ([]byte, error) {
	dec := make([]byte, base64.StdEncoding.DecodedLen(len(enc)))
	n, err := base64.StdEncoding.Decode(dec, enc)
	if err != nil {
		return nil, errors.Wrap(err, "base64.Encoding.Decode()")
	}

	return dec[:n], nil
}
