package resource

type searchSet struct {
	searchType SearchType
	paramKey   string
	paramVal   string
}

func newSearchSet(typ SearchType, key, val string) *searchSet {
	return &searchSet{
		searchType: typ,
		paramKey:   key,
		paramVal:   val,
	}
}
