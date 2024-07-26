// package ccc contains utility types and functions
package ccc

func Ptr[T any](t T) *T {
	return &t
}

func Deref[T any](value *T) T {
	var defaultValue T
	if value != nil {
		defaultValue = *value
	}

	return defaultValue
}
