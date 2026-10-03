package resources

import "github.com/cccteam/ccc/resource"

// The feature flags this application declares. A flag is a resource.Feature constant:
// its value is the flag's name as the FeatureFlags table holds it, and its doc comment
// is the description the flag's administrator reads. The generator collects the
// constants into Features() (zz_gen_features.go), the deploy step writes them into the
// table off, and @feature(<Constant>) on a struct or a field puts it behind the flag.
const (
	// Commendations lets headquarters cite a pilot for a sortie flown well: the commendations desk lists and files the citations, and every crew member's card counts their own.
	Commendations resource.Feature = "commendations"
)
