package resource

type Query[T any] struct {
	Set *QuerySet
}

func (p *Query[T]) Row() *T {
	return new(T)
}

func (p *Query[T]) Rows() []*T {
	return make([]*T, 0)
}

func (p *Query[T]) QuerySet() *QuerySet {
	return p.Set
}
