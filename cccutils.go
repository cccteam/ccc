// package ccc contains utility types and functions
package ccc

func Ptr[T any](t T) *T {
	return &t
}
