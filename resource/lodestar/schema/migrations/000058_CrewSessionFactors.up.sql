-- The crew's second factor (the session library's SessionFactors migration, under the
-- auth's prefix): the sign-in codes and the trusted devices. A factor code is one code
-- mailed for a pending sign-in (PendingId): the mail carries the code and the row keeps
-- its hash; the row is live while it is not consumed and not expired, Attempts counts the
-- wrong codes entered against it, and a new code for the same pending sign-in consumes
-- the earlier one. A trusted device is a browser a crew member asked to be remembered
-- after a code (UserId): the browser holds an opaque token in the crew-trusted cookie and
-- the row keeps its hash; it stands in for the factor while it is not revoked and not
-- expired, and headquarters revokes every browser of a person's from the user-management
-- surface. A code goes with its pending sign-in and a device with its user. CreatedAt,
-- ConsumedAt, LastUsedAt and RevokedAt take the commit time; ExpiresAt is the library's
-- deadline, written as given.
CREATE TABLE CrewSessionFactorCodes (
    Id         STRING(36) NOT NULL,
    PendingId  STRING(36) NOT NULL,
    Email      STRING(MAX) NOT NULL,
    CodeHash   BYTES(32) NOT NULL,
    Attempts   INT64 NOT NULL,
    CreatedAt  TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    ExpiresAt  TIMESTAMP NOT NULL,
    ConsumedAt TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    CONSTRAINT CK_CrewSessionFactorCodesId CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
    CONSTRAINT FK_CrewSessionFactorCodes_CrewSessionPendingSignIns FOREIGN KEY (PendingId) REFERENCES CrewSessionPendingSignIns (Id) ON DELETE CASCADE,
) PRIMARY KEY (Id);

CREATE INDEX CrewSessionFactorCodesByPending ON CrewSessionFactorCodes(PendingId, ConsumedAt, ExpiresAt);

CREATE TABLE CrewSessionTrustedDevices (
    Id         STRING(36) NOT NULL,
    UserId     STRING(36) NOT NULL,
    TokenHash  BYTES(32) NOT NULL,
    CreatedAt  TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    ExpiresAt  TIMESTAMP NOT NULL,
    LastUsedAt TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    RevokedAt  TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    CONSTRAINT CK_CrewSessionTrustedDevicesId CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
    CONSTRAINT FK_CrewSessionTrustedDevices_CrewSessionUsers FOREIGN KEY (UserId) REFERENCES CrewSessionUsers (Id) ON DELETE CASCADE,
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX CrewSessionTrustedDevicesByTokenHash ON CrewSessionTrustedDevices(TokenHash);

CREATE INDEX CrewSessionTrustedDevicesByUser ON CrewSessionTrustedDevices(UserId, RevokedAt);
