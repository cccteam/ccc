package resources

type (
	/* @uniqueindex (Id, Description)
	@foreignkey (Type) (StatusTypes(Id))
	@foreignkey (Status) (Statuses(Id)) */
	foo struct {
		/* @primarykey
		@check (@self = 'N')
		@hidden
		@substring(@self) */
		ID string
	}
)
