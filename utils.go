package ccc

const jsonNull = "null"

// Must is a helper function to avoid the need to check for errors.
func Must[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}

	return value
}

func Ptr[T any](t T) *T {
	return &t
}
