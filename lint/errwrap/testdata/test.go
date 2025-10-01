// Package main is a test file for the errwrap analyzer.
// This was created to test and fix a bug with nested if statements.
package main

import "github.com/go-playground/errors/v5"

func outerFunc() (string, error) { return "", nil }
func innerFunc() (string, error) { return "", nil }
func someCondition() bool        { return true }

type Struct1 struct{}

func (f *Struct1) Method1() *Struct2 { return &Struct2{} }

type Struct2 struct{}

func (f *Struct2) Method2() *Struct3 { return &Struct3{} }

type Struct3 struct{}

func (b *Struct3) Method3() (interface{}, error) {
	return nil, nil
}

var foo = &Struct1{}

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
