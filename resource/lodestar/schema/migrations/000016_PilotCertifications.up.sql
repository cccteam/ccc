CREATE TABLE PilotCertifications (
  UserId STRING(320) NOT NULL,
  CertificationId STRING(64) NOT NULL,

  CONSTRAINT FK_PilotCertifications_CertificationId FOREIGN KEY (CertificationId) REFERENCES Certifications(Id),
) PRIMARY KEY (UserId, CertificationId);
