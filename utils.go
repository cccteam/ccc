package ccc

const jsonNull = "null"

// Must is a helper function to avoid the need to check for errors.
func Must[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}

	return value
}

// Ptr returns a pointer to the given value t.
func Ptr[T any](t T) *T {
	return &t
}
