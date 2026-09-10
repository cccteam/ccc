package staleoutput

// StaleWrapper stands in for generated code whose signature predates a library
// change: the declared return type no longer matches what the body produces.
func StaleWrapper(t Thing) int {
	return t.Name // in-package type error: string returned as int
}
