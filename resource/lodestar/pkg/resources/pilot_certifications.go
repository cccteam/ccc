package resources

type (
	// PilotCertification is the GLOBAL subject-set anchor: `subject.certifications`
	// is the set of CertificationId values on rows whose UserId matches the requesting
	// user (`requiredCert IN subject.certifications`). A certification is earned once
	// and valid in every sector, so the anchor is deliberately global and its subject
	// subquery deliberately unfiltered — the shared pattern, the opposite of
	// SquadronMembership's per-sector one.
	//
	// Compound primary key (UserId, CertificationId); creates supply both parts.
	//
	// @resource
	PilotCertification struct {
		// @subjectSet(certifications, value: CertificationID)
		UserID          string `spanner:"UserId"`
		CertificationID string `spanner:"CertificationId"`
	}
)
