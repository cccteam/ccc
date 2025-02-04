package resource

type SearchSet struct {
	searchTyp SearchType
	searchKey string
	searchVal string
}

func NewSearchSet(searchTyp SearchType, searchKey, searchVal string) *SearchSet {
	return &SearchSet{
		searchTyp: searchTyp,
		searchKey: searchKey,
		searchVal: searchVal,
	}
}
