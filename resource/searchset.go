package resource

type SearchSet struct {
	searchTyp SearchType
	searchKey SearchKey
	searchVal string
}

func NewSearchSet(searchTyp SearchType, searchKey SearchKey, searchVal string) *SearchSet {
	return &SearchSet{
		searchTyp: searchTyp,
		searchKey: searchKey,
		searchVal: searchVal,
	}
}
