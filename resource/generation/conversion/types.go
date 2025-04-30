package conversion

// Used to generate Go functions converting types between databases
type (
	IntTo[T any]    = int
	StringTo[T any] = string
	Hidden[T any]   = T
	View[T any]     struct{}
)
