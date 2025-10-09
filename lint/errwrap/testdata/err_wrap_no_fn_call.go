package testdata

import (
	"iter"
	"strconv"

	"github.com/go-playground/errors/v5"
)

func errWrapNoFnCall() {
	i, err := strconv.Atoi("123")
	if err != nil {
		return
	}

	_ = i
}

func errWrapNoFnCall2(err error) error {
	if err != nil {
		return errors.Wrap(err, "iter.Seq2[interface{}, error]")
	}

	return nil
}

func errWrapNoFnCall3(in iter.Seq2[interface{}, error]) (bool, error) {
	for r, err := range in {
		if err != nil {
			return false, errors.Wrap(err, "iter.Seq2[interface{}, error]")
		}

		_ = r
	}

	return true, nil
}
