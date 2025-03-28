package resource

import (
	"bytes"
	"io"
	"net/http"

	"github.com/go-playground/errors/v5"
)

func CloneRequest(r *http.Request) (*http.Request, error) {
	r2 := r.Clone(r.Context())

	switch t := r2.Body.(type) {
	case *cloneReader:
		if _, err := t.Seek(0, io.SeekStart); err != nil {
			return nil, errors.Wrap(err, "failed to seek to start of request body")
		}
	default:
		p, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, errors.Wrap(err, "failed to read request body")
		}
		if err := r.Body.Close(); err != nil {
			return nil, errors.Wrap(err, "failed to close request body")
		}
		r2.Body = &cloneReader{bytes.NewReader(p)}
		r.Body = r2.Body
	}

	return r2, nil
}

type cloneReader struct {
	*bytes.Reader
}

func (c *cloneReader) Close() error {
	return nil
}
