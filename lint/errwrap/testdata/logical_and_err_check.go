// Package testdata is a test package for the errwrap analyzer.
// This was created to test and fix a bug with nested if statements.
package testdata

import (
	"github.com/go-playground/errors/v5"
)

type testStruct1 struct {
	s1 testStruct2
}

type testStruct2 struct {
	s2 testStruct3
}

func (t *testStruct2) method1() *testStruct3 { return &testStruct3{} }

type testStruct3 struct{}

func (t *testStruct3) method2() (interface{}, error) { return nil, nil }

func (t *testStruct1) test() (interface{}, error) {
	obj, err := t.s1.method1().method2()
	if err != nil && !errorCheck(err) { // Testing scenario such as err != nil && !httpio.HasNotFound(err)
		return nil, errors.Wrap(err, "t.s1.method1().incorrect()") // Making sure this incorrect error wrap is reported
	}

	return obj, nil
}

func (t *testStruct1) test2() (interface{}, error) {
	obj, err := t.s1.method1().method2()
	if errorCheck(err) && err != nil {
		return nil, errors.Wrap(err, "t.s1.method1().incorrect2()") // Making sure this incorrect error wrap is reported
	}

	return obj, nil
}

func errorCheck(err error) bool {
	return err != nil
}
