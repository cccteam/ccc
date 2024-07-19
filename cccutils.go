// package cccutils contains utility types and functions
package cccutils

func Ptr[T any](t T) *T {
	return &t
}
