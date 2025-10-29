package testdata

import (
	"context"
	"iter"
	"strconv"

	"github.com/go-playground/errors/v5"
)

type ForErrStruct struct{}

func (s *ForErrStruct) Method1() *ForErrQuery { return &ForErrQuery{} }

type ForErrQuery struct{}

func (q *ForErrQuery) Method2(ctx context.Context, obj interface{}) iter.Seq2[interface{}, error] {
	return func(yield func(interface{}, error) bool) {
		_ = ctx
		_ = obj
		yield(nil, nil)
	}
}

func forErrTest() (bool, error) {
	ret, err := strconv.Atoi("123")
	if err != nil {
		return false, errors.Wrap(err, "strconv.Atoi()")
	}

	_ = ret

	var s ForErrStruct

	for obj, err := range s.Method1().Method2(context.Background(), struct{}{}) {
		if err != nil {
			return false, errors.Wrap(err, "resource.Method2()")
		}

		_ = obj
	}

	return true, nil
}

func forErrTest2() (interface{}, error) {
	ret, err := strconv.Atoi("123")
	if err != nil {
		return nil, errors.Wrap(err, "strconv.Atoi()")
	}

	_ = ret

	var s ForErrStruct

	temp := s.Method1().Method2(context.Background(), struct{}{})
	for obj, err := range temp {
		if err != nil {
			return nil, errors.Wrap(err, "resource.QuerySet[resources.LoanPerson].Method2()")
		}

		_ = obj
	}

	return nil, nil
}
