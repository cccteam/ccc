-- The members's pending sign-ins: a sign-in that waits for a password confirmation or the
-- application's MFA step, held server-side. The browser's pending cookie carries only
-- an opaque token; TokenHash is its SHA-256. A row is live until ExpiresAt or until a
-- completion, cancellation or replacement consumes it (ConsumedAt), once. CreatedAt and
-- ConsumedAt are written with the database's commit time; ExpiresAt is the library's
-- deadline, written as given.
CREATE TABLE MembersSessionPendingSignIns (
    Id                STRING(36) NOT NULL,
    TokenHash         BYTES(32) NOT NULL,
    Step              STRING(16) NOT NULL,
    Registration      STRING(MAX) NOT NULL,
    Method            STRING(MAX) NOT NULL,
    Connection        STRING(MAX) NOT NULL,
    Subject           STRING(MAX) NOT NULL,
    Email             STRING(MAX) NOT NULL,
    EmailVerified     BOOL NOT NULL,
    IdpAmr            ARRAY<STRING(MAX)>,
    Claims            BYTES(MAX),
    UserId            STRING(36),
    Username          STRING(MAX) NOT NULL,
    Tenant            STRING(MAX) NOT NULL,
    Events            BYTES(MAX) NOT NULL,
    RoleNames         ARRAY<STRING(MAX)>,
    Factors           ARRAY<STRING(MAX)>,
    ReturnUrl         STRING(MAX) NOT NULL,
    ProviderSessionId STRING(MAX),
    ProviderSessionIssuer STRING(MAX),
    CreatedAt         TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    ExpiresAt         TIMESTAMP NOT NULL,
    ConsumedAt        TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    CONSTRAINT CK_MembersSessionPendingSignInsId CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
    CONSTRAINT CK_MembersSessionPendingSignInsStep CHECK (Step IN ('confirmation', 'mfa')),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX MembersSessionPendingSignInsByTokenHash ON MembersSessionPendingSignIns(TokenHash);

CREATE INDEX MembersSessionPendingSignInsByExpiresAt ON MembersSessionPendingSignIns(ExpiresAt);
