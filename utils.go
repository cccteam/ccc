package ccc

// Must is a helper function to avoid the need to check for errors.
func Must[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}

	return value
}
