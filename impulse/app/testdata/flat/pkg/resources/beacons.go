package resources

type (
	// Beacon is a light on the coast.
	//
	// @resource
	// @permissionScope(domain)
	Beacon struct {
		ID string `spanner:"Id"`
	}

	// Lens is global: it names a lens design, not a beacon.
	//
	// @resource
	Lens struct {
		ID string `spanner:"Id"`
	}
)
