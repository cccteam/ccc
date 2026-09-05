package resources

type (
	// MissionStatus enumerates the mission workflow's seven states; the generated
	// constants are the vocabulary the RPC bodies and computed resources use, so a
	// state is always spelled in a checked constant, never a string.
	//
	// @enumerate(MissionStatuses)
	MissionStatus string

	// RefitStatus enumerates the refit workflow's six states — the hangar bays.
	//
	// @enumerate(RefitStatuses)
	RefitStatus string

	// MissionKind enumerates what a mission is: rescue, salvage, escort, courier.
	//
	// @enumerate(MissionKinds)
	MissionKind string

	// ShipRole enumerates a hull's role in the fleet.
	//
	// @enumerate(ShipRoles)
	ShipRole string

	// Certification enumerates the pilot certifications a mission may require.
	//
	// @enumerate(Certifications)
	Certification string

	// FailReason enumerates why a mission failed; FailMission validates its reason
	// against these constants in the body (enum tables are not referencable by
	// enumerated: today — finding 4).
	//
	// @enumerate(FailReasons)
	FailReason string
)
