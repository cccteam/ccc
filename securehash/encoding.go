package securehash

import (
	"bytes"
	"encoding/base64"
	"strconv"

	"github.com/go-playground/errors/v5"
)

const (
	eol = 0
	dot = '.'
	sep = '$'
)

func removeLeadingSeperator(s rune, b []byte) ([]byte, error) {
	if len(b) == 0 {
		return nil, errors.New("provided empty hash")
	}
	if !bytes.HasPrefix(b, []byte("$")) {
		return nil, errors.Newf("initial byte must be %s, found %s", string(s), string(b[0]))
	}
	b = b[1:]

	return b, nil
}

func parseUint32(s rune, b []byte) (u32 uint32, remaining []byte, err error) {
	i := len(b)
	if s != 0 {
		i = bytes.IndexRune(b, s)
		if i < 0 {
			return 0, nil, errors.New("failed to find separator")
		}
	}

	u, err := strconv.ParseUint(string(b[:i]), 10, 32)
	if err != nil {
		return 0, nil, errors.Wrapf(err, "strconv.ParseUint()")
	}

	return uint32(u), b[i+1:], nil
}

func parseUint8(s rune, b []byte) (u8 uint8, remaining []byte, err error) {
	i := len(b)
	if s != 0 {
		i = bytes.IndexRune(b, s)
		if i < 0 {
			return 0, nil, errors.New("failed to find separator")
		}
	}

	u, err := strconv.ParseUint(string(b[:i]), 10, 8)
	if err != nil {
		return 0, nil, errors.Wrapf(err, "strconv.ParseUint()")
	}

	return uint8(u), b[i+1:], nil
}

func parseBase64(s rune, b []byte) (val, remainder []byte, err error) {
	if len(b) == 0 {
		return nil, nil, errors.New("provided empty hash")
	}

	i := len(b)
	if s != 0 {
		i = bytes.IndexRune(b, s)
		if i < 0 {
			return nil, nil, errors.New("failed to find separator")
		}
	}

	salt, err := decodeBase64(b[:i])
	if err != nil {
		return nil, nil, err
	}

	if s != eol {
		i++
	}

	return salt, b[i:], nil
}

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
