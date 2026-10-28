-- The members' second factor (the session library's SessionFactors migration, under the
-- auth's prefix): the sign-in codes mailed for a pending sign-in and the browsers a
-- member asked to be remembered after one, shaped as the crew's
-- (000058_CrewSessionFactors). A member whose company directory attests for them is
-- never mailed a code, so these tables fill only where the fleet does not administer
-- the directory.
CREATE TABLE MembersSessionFactorCodes (
    Id         STRING(36) NOT NULL,
    PendingId  STRING(36) NOT NULL,
    Email      STRING(MAX) NOT NULL,
    CodeHash   BYTES(32) NOT NULL,
    Attempts   INT64 NOT NULL,
    CreatedAt  TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    ExpiresAt  TIMESTAMP NOT NULL,
    ConsumedAt TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    CONSTRAINT CK_MembersSessionFactorCodesId CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
    CONSTRAINT FK_MembersSessionFactorCodes_MembersSessionPendingSignIns FOREIGN KEY (PendingId) REFERENCES MembersSessionPendingSignIns (Id) ON DELETE CASCADE,
) PRIMARY KEY (Id);

CREATE INDEX MembersSessionFactorCodesByPending ON MembersSessionFactorCodes(PendingId, ConsumedAt, ExpiresAt);

CREATE TABLE MembersSessionTrustedDevices (
    Id         STRING(36) NOT NULL,
    UserId     STRING(36) NOT NULL,
    TokenHash  BYTES(32) NOT NULL,
    CreatedAt  TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    ExpiresAt  TIMESTAMP NOT NULL,
    LastUsedAt TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    RevokedAt  TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    CONSTRAINT CK_MembersSessionTrustedDevicesId CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
    CONSTRAINT FK_MembersSessionTrustedDevices_MembersSessionUsers FOREIGN KEY (UserId) REFERENCES MembersSessionUsers (Id) ON DELETE CASCADE,
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX MembersSessionTrustedDevicesByTokenHash ON MembersSessionTrustedDevices(TokenHash);

CREATE INDEX MembersSessionTrustedDevicesByUser ON MembersSessionTrustedDevices(UserId, RevokedAt);
