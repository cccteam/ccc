package sharedresources

type (
	// AnnouncementKind enumerates what an announcement is; the generated constants are
	// the vocabulary both sites and their tests use, so a kind is spelled in a checked
	// constant, never a string.
	//
	// @enumerate(AnnouncementKinds)
	AnnouncementKind string
)
