-- The members auth's session tables: the session library's Auth sessions (session.Auth), one
-- set of tables for every sign-in method registered on the auth. A session belongs to a
-- user, and deleting the user deletes its sessions; UserId is null only for a
-- role-principal impersonation. ProviderSessionId is the identity provider's session id
-- when the method reports one, for front-channel logout, and ProviderSessionIssuer the
-- issuer it belongs to. Every column that records when something happened is written
-- with the database's commit time, never the application's clock, which
-- allow_commit_timestamp permits.
CREATE TABLE MembersSessions (
    Id                    STRING(36) NOT NULL,
    Username              STRING(MAX) NOT NULL,
    Expired               BOOL NOT NULL,
    UserId                STRING(36),
    AuthenticatedAt       TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    ProviderSessionId     STRING(MAX),
    ProviderSessionIssuer STRING(MAX),
    CreatedAt             TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    UpdatedAt             TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    CONSTRAINT CK_MembersSessionsId CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
    CONSTRAINT FK_MembersSessions_MembersSessionUsers FOREIGN KEY (UserId) REFERENCES MembersSessionUsers (Id) ON DELETE CASCADE,
) PRIMARY KEY (Id);

CREATE INDEX MembersSessions_Expired_idx ON MembersSessions (Expired DESC);

CREATE INDEX MembersSessionsByUserId ON MembersSessions(UserId);

CREATE INDEX MembersSessionsByProviderSessionId ON MembersSessions(ProviderSessionId);

-- The identity links: which provider subject is which person. A user holds one row per
-- registration they have signed in through, keyed uniquely so a subject links to one
-- person, and deleting the person deletes the links.
CREATE TABLE MembersSessionIdentities (
    Id          STRING(36) NOT NULL,
    UserId      STRING(36) NOT NULL,
    Method      STRING(MAX) NOT NULL,
    Connection  STRING(MAX) NOT NULL,
    Subject     STRING(MAX) NOT NULL,
    Tenant      STRING(MAX),
    EmailAtLink STRING(MAX),
    CreatedAt   TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    LastUsedAt  TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    CONSTRAINT CK_MembersSessionIdentitiesId CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
    CONSTRAINT FK_MembersSessionIdentities_MembersSessionUsers FOREIGN KEY (UserId) REFERENCES MembersSessionUsers (Id) ON DELETE CASCADE,
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX MembersSessionIdentitiesByKey ON MembersSessionIdentities(Method, Connection, Subject);

CREATE INDEX MembersSessionIdentitiesByUserId ON MembersSessionIdentities(UserId);

-- The auth events: how each session was authenticated, one row per sign-in or step (the
-- method, its registration, and the provider's own amr claims). Not interleaved: an
-- interleaved child's key must begin with the parent's, so the events reference their
-- session through a cascading foreign key instead.
CREATE TABLE MembersSessionAuthEvents (
    SessionId  STRING(36) NOT NULL,
    Seq        INT64 NOT NULL,
    Method     STRING(MAX) NOT NULL,
    Connection STRING(MAX),
    IdpAmr     ARRAY<STRING(MAX)>,
    OccurredAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    CONSTRAINT FK_MembersSessionAuthEvents_MembersSessions FOREIGN KEY (SessionId) REFERENCES MembersSessions (Id) ON DELETE CASCADE,
) PRIMARY KEY (SessionId, Seq);

-- The pending sign-ins: a sign-in that waits for a password confirmation or the
-- application's MFA step, held server-side. The browser's pending cookie carries only an
-- opaque token; TokenHash is its SHA-256. A row is live until ExpiresAt or until a
-- completion, cancellation or replacement consumes it (ConsumedAt), once. ExpiresAt is
-- the library's deadline, written as given.
CREATE TABLE MembersSessionPendingSignIns (
    Id                    STRING(36) NOT NULL,
    TokenHash             BYTES(32) NOT NULL,
    Step                  STRING(16) NOT NULL,
    Registration          STRING(MAX) NOT NULL,
    Method                STRING(MAX) NOT NULL,
    Connection            STRING(MAX) NOT NULL,
    Subject               STRING(MAX) NOT NULL,
    Email                 STRING(MAX) NOT NULL,
    EmailVerified         BOOL NOT NULL,
    IdpAmr                ARRAY<STRING(MAX)>,
    Claims                BYTES(MAX),
    SignInName            STRING(MAX) NOT NULL,
    Guest                 BOOL NOT NULL,
    Organization          STRING(MAX) NOT NULL,
    UserId                STRING(36),
    Username              STRING(MAX) NOT NULL,
    Tenant                STRING(MAX) NOT NULL,
    Events                BYTES(MAX) NOT NULL,
    RoleNames             ARRAY<STRING(MAX)>,
    Factors               ARRAY<STRING(MAX)>,
    ReturnUrl             STRING(MAX) NOT NULL,
    ProviderSessionId     STRING(MAX),
    ProviderSessionIssuer STRING(MAX),
    CreatedAt             TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    ExpiresAt             TIMESTAMP NOT NULL,
    ConsumedAt            TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    CONSTRAINT CK_MembersSessionPendingSignInsId CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
    CONSTRAINT CK_MembersSessionPendingSignInsStep CHECK (Step IN ('confirmation', 'mfa')),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX MembersSessionPendingSignInsByTokenHash ON MembersSessionPendingSignIns(TokenHash);

CREATE INDEX MembersSessionPendingSignInsByExpiresAt ON MembersSessionPendingSignIns(ExpiresAt);

-- The sign-in attempts: the attempt limiter's count of failed password attempts per
-- sign-in name, known or not, within a window, and the lock those failures earned. The
-- row is keyed by the submitted name, never by the user row, so an unknown name is
-- counted exactly as a known one; a success, an MFA completion or a password change
-- deletes it. WindowEndsAt and LockedUntil are deadlines, written as given.
CREATE TABLE MembersSessionSignInAttempts (
    Name         STRING(MAX) NOT NULL,
    Failures     INT64 NOT NULL,
    WindowEndsAt TIMESTAMP NOT NULL,
    LockedUntil  TIMESTAMP,
    UpdatedAt    TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
) PRIMARY KEY (Name);
