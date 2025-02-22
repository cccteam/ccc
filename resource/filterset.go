package resource

type FilterSet struct {
	filterTyp FilterType
	filterKey FilterKey
	filterVal string
}

func NewFilterSet(filterTyp FilterType, filterKey FilterKey, filterVal string) *FilterSet {
	return &FilterSet{
		filterTyp: filterTyp,
		filterKey: filterKey,
		filterVal: filterVal,
	}
}
