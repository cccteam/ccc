-- The crew's tokens: the bearer credentials of the droids outlet (the session library's
-- SessionTokens migration, under the auth's prefix). A row is one credential: an
-- application token a crew member issued to a program of their own, the droid service
-- account's configured key seeded as one, or the access or refresh token of a device
-- grant a command-line client obtained with a crew member's approval; the client holds
-- an opaque secret and the row keeps its hash. A token acts as its user (UserId) on the
-- one machine outlet it was issued for (Outlet) and is live while it is not revoked and
-- not expired. A device grant's tokens share a Family, and every successor a refresh
-- exchange issues joins it, so a rotated refresh token presented again revokes the
-- family at once (RevokedReason 'reuse'). CreatedAt, LastUsedAt and RevokedAt take the
-- commit time; ExpiresAt is the library's deadline, written as given. A token goes with
-- its user.
CREATE TABLE CrewSessionTokens (
    Id            STRING(36) NOT NULL,
    TokenHash     BYTES(32) NOT NULL,
    Kind          STRING(16) NOT NULL,
    UserId        STRING(36) NOT NULL,
    Outlet        STRING(MAX) NOT NULL,
    Name          STRING(MAX),
    Family        STRING(36),
    ClientId      STRING(MAX),
    CreatedAt     TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    ExpiresAt     TIMESTAMP NOT NULL,
    LastUsedAt    TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    RevokedAt     TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    RevokedReason STRING(16),
    CONSTRAINT CK_CrewSessionTokensId CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
    CONSTRAINT CK_CrewSessionTokensKind CHECK (Kind IN ('application', 'access', 'refresh')),
    CONSTRAINT CK_CrewSessionTokensRevokedReason CHECK (RevokedReason IS NULL OR RevokedReason IN ('revoked', 'rotated', 'reuse', 'user')),
    CONSTRAINT FK_CrewSessionTokens_CrewSessionUsers FOREIGN KEY (UserId) REFERENCES CrewSessionUsers (Id) ON DELETE CASCADE,
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX CrewSessionTokensByTokenHash ON CrewSessionTokens(TokenHash);

CREATE INDEX CrewSessionTokensByUser ON CrewSessionTokens(UserId, RevokedAt, ExpiresAt);

CREATE INDEX CrewSessionTokensByFamily ON CrewSessionTokens(Family, RevokedAt);
