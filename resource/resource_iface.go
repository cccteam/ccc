package resource

type Queryer[T any] interface {
	Query() *Query[T]
}

type InsertPatcher interface {
	InsertPatchSet() *PatchSet
}

type UpdatePatcher interface {
	UpdatePatchSet() *PatchSet
}

type DeletePatcher interface {
	DeletePatchSet() *PatchSet
}
