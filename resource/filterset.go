package resource

// TODO(bswaney): rename this file to filter

type Filter struct {
	typ FilterType
	// TODO(bswaney): get rid of these two
	key FilterKey
	val string

	// TODO(bswaney): rename to values
	filter map[FilterKey]string
}

func NewFilter(typ FilterType, values map[FilterKey]string) *Filter {
	return &Filter{
		typ:    typ,
		filter: values,
	}
}
