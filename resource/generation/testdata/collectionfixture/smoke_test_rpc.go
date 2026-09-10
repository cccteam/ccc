package collectionfixture

// SmokeTest lives in the file its name expects once the _rpc marker keeps it out of Go's
// _test.go files, pinning the validator's pass case for a name ending in Test.
type SmokeTest struct {
	Input string
}
