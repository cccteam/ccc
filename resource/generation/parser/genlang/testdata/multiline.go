package resources

type (
	/*
		Only lines starting with an @ symbol will be parsed for annotations
		@uniqueindex (Id, Description) comments can also go after an annotation
		@foreignkey (Type, StatusTypes(Id))
		@foreignkey (Status, Statuses(Id)) */
	foo struct {
		/* @primarykey
		@check (@self = 'N')
		@hidden
		@substring(@self) */
		ID string
	}
)
