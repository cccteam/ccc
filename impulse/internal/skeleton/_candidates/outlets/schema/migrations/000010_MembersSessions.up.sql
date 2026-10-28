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

-- The mail links: the single-use links the auth mails. A row is one link: an invitation
-- to an address, carrying the tenant and the roles its acceptance seats and who sent it;
-- the verification of an address a signed-in person adds (UserId names the account,
-- Email the address); or a sign-up a person starts by giving their address. The mail
-- carries an opaque token and the row keeps its hash; the row is accepted only while it
-- is live, not expired and not consumed, and is consumed by a conditional update so one
-- link completes once. A revoked invitation is consumed by hand. A verification link
-- goes with its user. CreatedAt and ConsumedAt take the commit time; ExpiresAt is the
-- library's deadline, written as given.
CREATE TABLE MembersSessionMailLinks (
    Id         STRING(36) NOT NULL,
    TokenHash  BYTES(32) NOT NULL,
    Kind       STRING(16) NOT NULL,
    Email      STRING(MAX) NOT NULL,
    UserId     STRING(36),
    Tenant     STRING(MAX),
    RoleNames  ARRAY<STRING(MAX)>,
    InvitedBy  STRING(36),
    CreatedAt  TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    ExpiresAt  TIMESTAMP NOT NULL,
    ConsumedAt TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    CONSTRAINT CK_MembersSessionMailLinksId CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
    CONSTRAINT CK_MembersSessionMailLinksKind CHECK (Kind IN ('invitation', 'verification', 'sign-up')),
    CONSTRAINT FK_MembersSessionMailLinks_MembersSessionUsers FOREIGN KEY (UserId) REFERENCES MembersSessionUsers (Id) ON DELETE CASCADE,
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX MembersSessionMailLinksByTokenHash ON MembersSessionMailLinks(TokenHash);

CREATE INDEX MembersSessionMailLinksByKind ON MembersSessionMailLinks(Kind, ConsumedAt, ExpiresAt);

-- The tokens: the bearer credentials of the machine outlets whose tokens the members auth
-- issues. A row is one credential: an application token a person issued to a program of
-- their own, or the access or refresh token of a device grant a command-line client
-- obtained with a person's approval; the client holds an opaque secret and the row keeps
-- its hash. A token acts as its user (UserId) on the one machine outlet it was issued
-- for (Outlet) and is live while it is not revoked and not expired. A device grant's
-- tokens share a Family, and every successor a refresh exchange issues joins it, so a
-- rotated refresh token presented again revokes the family at once (RevokedReason
-- 'reuse'). CreatedAt, LastUsedAt and RevokedAt take the commit time; ExpiresAt is the
-- library's deadline, written as given. A token goes with its user.
CREATE TABLE MembersSessionTokens (
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
    CONSTRAINT CK_MembersSessionTokensId CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
    CONSTRAINT CK_MembersSessionTokensKind CHECK (Kind IN ('application', 'access', 'refresh')),
    CONSTRAINT CK_MembersSessionTokensRevokedReason CHECK (RevokedReason IS NULL OR RevokedReason IN ('revoked', 'rotated', 'reuse', 'user')),
    CONSTRAINT FK_MembersSessionTokens_MembersSessionUsers FOREIGN KEY (UserId) REFERENCES MembersSessionUsers (Id) ON DELETE CASCADE,
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX MembersSessionTokensByTokenHash ON MembersSessionTokens(TokenHash);

CREATE INDEX MembersSessionTokensByUser ON MembersSessionTokens(UserId, RevokedAt, ExpiresAt);

CREATE INDEX MembersSessionTokensByFamily ON MembersSessionTokens(Family, RevokedAt);

-- The device codes: the device authorizations command-line clients start for a machine
-- outlet whose tokens the members auth issues (RFC 8628). The client holds the opaque
-- device code and the row keeps its hash; the person is shown the user code, normalized
-- here. A row is pending until a signed-in person approves it (ApprovedAt, UserId) or
-- denies it (DeniedAt), or it expires; an approved row is consumed once (ConsumedAt),
-- when the client's poll redeems it for its tokens. LastPolledAt holds the client to the
-- polling interval. The time columns but ExpiresAt take the commit time. An approved
-- code goes with its user.
CREATE TABLE MembersSessionDeviceCodes (
    Id             STRING(36) NOT NULL,
    DeviceCodeHash BYTES(32) NOT NULL,
    UserCode       STRING(16) NOT NULL,
    ClientId       STRING(MAX) NOT NULL,
    Outlet         STRING(MAX) NOT NULL,
    UserId         STRING(36),
    CreatedAt      TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    ExpiresAt      TIMESTAMP NOT NULL,
    ApprovedAt     TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    DeniedAt       TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    LastPolledAt   TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    ConsumedAt     TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    CONSTRAINT CK_MembersSessionDeviceCodesId CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
    CONSTRAINT FK_MembersSessionDeviceCodes_MembersSessionUsers FOREIGN KEY (UserId) REFERENCES MembersSessionUsers (Id) ON DELETE CASCADE,
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX MembersSessionDeviceCodesByHash ON MembersSessionDeviceCodes(DeviceCodeHash);

CREATE INDEX MembersSessionDeviceCodesByUserCode ON MembersSessionDeviceCodes(UserCode, ExpiresAt);

-- The second factor of the members auth's sessions: the sign-in codes and the trusted
-- devices. A sign-in code is one code mailed for a pending sign-in (PendingId): the mail
-- carries the code and the row keeps its hash; the row is live while it is not consumed
-- and not expired, Attempts counts the wrong codes entered against it, and a new code for
-- the same pending sign-in consumes the earlier one. A trusted device is a browser a
-- person asked to be remembered after a code (UserId): the browser holds an opaque token
-- in the auth's trusted-device cookie and the row keeps its hash; it stands in for the
-- factor while it is not revoked and not expired, and an administrator revokes every
-- browser of a person's from the users surface. A code goes with its pending sign-in and
-- a device with its user. CreatedAt, ConsumedAt, LastUsedAt and RevokedAt take the commit
-- time; ExpiresAt is the library's deadline, written as given.
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
