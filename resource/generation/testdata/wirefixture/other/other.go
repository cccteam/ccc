// Package other declares a struct named like one in wirefixture, so the walker's
// mirror-name collision rule has something to refuse.
package other

// Reading collides by name with wirefixture.Reading.
type Reading struct {
	Value int
}
