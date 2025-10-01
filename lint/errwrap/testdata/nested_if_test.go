package testdata

import (
	"context"

	"github.com/go-playground/errors/v5"
)

type Struct1 struct {
	s4 Struct4
}

func (s *Struct1) Method1() *Struct2 { return &Struct2{} }

type Struct2 struct {
	s4 Struct4
}

func (s *Struct2) Method2() *Struct3 { return &Struct3{} }

type Struct3 struct{}

func (s *Struct3) Method3() (interface{}, error) {
	return nil, nil
}

type Struct4 struct{}

func outerFunc() (string, error) { return "", nil }
func innerFunc() (string, error) { return "", nil }
func someCondition() bool        { return true }

func (s *Struct4) Method4() (t1, t2 interface{}, err error) {
	return nil, nil, nil
}

var foo = &Struct1{}

func main() {
	if err := test(); err != nil {
		panic(err)
	}
	if err := test2(); err != nil {
		panic(err)
	}

	s := &Struct1{}
	if _, err := s.Test(context.Background()); err != nil {
		panic(err)
	}
}

func test() error {
	// Outer assignment
	_, err := outerFunc()
	if err != nil {
		if someCondition() {
			// Inner assignment - this should be found for the inner if
			_, err := innerFunc()
			if err != nil {
				return errors.Wrap(err, "innerFunc()")
			}
		}
	}

	return nil
}

func test2() error {
	// Outer assignment
	_, err := outerFunc()
	if err != nil {
		if someCondition() {
			// Inner assignment with chained calls
			_, err := foo.Method1().Method2().Method3()
			if err != nil {
				return errors.Wrap(err, ".Method3()")
			}
		}
	}

	return nil
}

func (s *Struct1) Test(ctx context.Context) (interface{}, error) {
	// The generated openapi library uses the context to receive the App Key and Token
	_, err := s.prepareContext(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "prepareContext()")
	}

	{
		_, resp, err := s.s4.Method4()
		if resp != nil {
			return resp, nil
		}
		if err != nil {
			return nil, errors.Wrap(err, "s.s4.Method4()")
		}
	}

	return nil, nil
}

func (s *Struct1) prepareContext(ctx context.Context) (context.Context, error) {
	// Simulate some processing
	return ctx, nil
}
