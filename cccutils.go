// package cccutils contains utility types and functions
package cccutils

func ptr[T any](t T) *T {
	return &t
}
