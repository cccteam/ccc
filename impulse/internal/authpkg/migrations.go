package authpkg

import (
	"fmt"
	"strings"
)

// Migration is one migration of an auth's session tables: its stem after the sequence
// number, and its up and down DDL.
type Migration struct {
	Stem string
	Up   string
	Down string
}

// guidCheck is the check every id column carries: a UUID in its canonical form.
const guidCheck = `r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'`

// Migrations renders the Auth session tables of an auth under its table prefix, for
// Spanner, in the order they apply: the users first, since the sessions reference them,
// then the sessions with the identities, auth events, pending sign-ins, sign-in attempts,
// mail links, tokens, device codes, sign-in codes and trusted devices in one migration,
// since a session set is one thing. The DDL is the
// session library's base and auth schema sets (schema/spanner/migrations,
// schema/spanner/auth/migrations) with the commit-timestamp options written inline. who
// names the population for the comments. usernameIsEmail renders the users of a password
// auth whose sign-in name is the address: the normalized address derives from the
// username rather than from an address column of its own.
func Migrations(prefix, who string, usernameIsEmail bool) []Migration {
	return []Migration{
		{Stem: prefix + "SessionUsers", Up: usersUp(prefix, who, usernameIsEmail), Down: usersDown(prefix)},
		{Stem: prefix + "Sessions", Up: sessionsUp(prefix, who), Down: sessionsDown(prefix)},
	}
}

func usersUp(p, who string, usernameIsEmail bool) string {
	return fmt.Sprintf(`-- The %[2]s auth's users: one row per person whichever way they sign in. PasswordHash is
-- set for a person with a password and null for one a directory alone admits.
CREATE TABLE %[1]sSessionUsers (
  Id                 STRING(36) NOT NULL,
  Username           STRING(MAX) NOT NULL,
  NormalizedUsername STRING(MAX) AS (NORMALIZE_AND_CASEFOLD(Username)) STORED,
  PasswordHash       STRING(MAX),
  Disabled           BOOL NOT NULL DEFAULT (FALSE),
  -- An operator set the password (a first password, a reset): the person changes it
  -- before anything else is served (the session library's forced change).
  PasswordChangeRequired BOOL NOT NULL DEFAULT (FALSE),
  -- The database's time at the insert and at the latest sign-in (null before the
  -- first); the session library writes both as commit timestamps.
  CreatedAt          TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
  LastSignInAt       TIMESTAMP OPTIONS (allow_commit_timestamp = true),
%[4]s  CONSTRAINT CK_%[1]sSessionUsersId CHECK (REGEXP_CONTAINS(Id, %[3]s)),
) PRIMARY KEY(Id);

CREATE UNIQUE INDEX %[1]sSessionUsersByNormalizedUsername ON %[1]sSessionUsers(NormalizedUsername);

CREATE UNIQUE NULL_FILTERED INDEX %[1]sSessionUsersByNormalizedEmail ON %[1]sSessionUsers(NormalizedEmail);
`, p, who, guidCheck, emailColumns(usernameIsEmail))
}

// emailColumns are the users table's address columns: the address in two states (the
// session library's SessionUserEmail migration), derived from the username where the
// sign-in name is the address.
func emailColumns(usernameIsEmail bool) string {
	if usernameIsEmail {
		return `  -- The person's email address is their sign-in name (session.UsernameIsEmail), so the
  -- normalized address derives from the username and uniqueness holds once, and
  -- EmailVerifiedAt is the database's time at which the person proved they read the
  -- mailbox by following a link sent to it; null while the address is unverified. An
  -- address is trusted for nothing until it is verified.
  NormalizedEmail    STRING(MAX) AS (NORMALIZE_AND_CASEFOLD(Username)) STORED,
  EmailVerifiedAt    TIMESTAMP OPTIONS (allow_commit_timestamp = true),
`
	}

	return `  -- The person's email address, in two states, held apart from the username: Email as
  -- the person gave it, NormalizedEmail the form uniqueness and lookups use, and
  -- EmailVerifiedAt the database's time at which the person proved they read the
  -- mailbox by following a link sent to it; null while the address is unverified. An
  -- address is trusted for nothing until it is verified; an operator may put one on a
  -- user by hand, unverified, which reserves it. No address is required.
  Email              STRING(MAX),
  NormalizedEmail    STRING(MAX) AS (NORMALIZE_AND_CASEFOLD(Email)) STORED,
  EmailVerifiedAt    TIMESTAMP OPTIONS (allow_commit_timestamp = true),
`
}

func usersDown(p string) string {
	return fmt.Sprintf("DROP INDEX %[1]sSessionUsersByNormalizedEmail;\nDROP INDEX %[1]sSessionUsersByNormalizedUsername;\nDROP TABLE %[1]sSessionUsers;\n", p)
}

func sessionsUp(p, who string) string {
	return fmt.Sprintf(`-- The %[2]s auth's session tables: the session library's Auth sessions (session.Auth), one
-- set of tables for every sign-in method registered on the auth. A session belongs to a
-- user, and deleting the user deletes its sessions; UserId is null only for a
-- role-principal impersonation. ProviderSessionId is the identity provider's session id
-- when the method reports one, for front-channel logout, and ProviderSessionIssuer the
-- issuer it belongs to. Every column that records when something happened is written
-- with the database's commit time, never the application's clock, which
-- allow_commit_timestamp permits.
CREATE TABLE %[1]sSessions (
    Id                    STRING(36) NOT NULL,
    Username              STRING(MAX) NOT NULL,
    Expired               BOOL NOT NULL,
    UserId                STRING(36),
    AuthenticatedAt       TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    ProviderSessionId     STRING(MAX),
    ProviderSessionIssuer STRING(MAX),
    CreatedAt             TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    UpdatedAt             TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    CONSTRAINT CK_%[1]sSessionsId CHECK (REGEXP_CONTAINS(Id, %[3]s)),
    CONSTRAINT FK_%[1]sSessions_%[1]sSessionUsers FOREIGN KEY (UserId) REFERENCES %[1]sSessionUsers (Id) ON DELETE CASCADE,
) PRIMARY KEY (Id);

CREATE INDEX %[1]sSessions_Expired_idx ON %[1]sSessions (Expired DESC);

CREATE INDEX %[1]sSessionsByUserId ON %[1]sSessions(UserId);

CREATE INDEX %[1]sSessionsByProviderSessionId ON %[1]sSessions(ProviderSessionId);

-- The identity links: which provider subject is which person. A user holds one row per
-- registration they have signed in through, keyed uniquely so a subject links to one
-- person, and deleting the person deletes the links.
CREATE TABLE %[1]sSessionIdentities (
    Id          STRING(36) NOT NULL,
    UserId      STRING(36) NOT NULL,
    Method      STRING(MAX) NOT NULL,
    Connection  STRING(MAX) NOT NULL,
    Subject     STRING(MAX) NOT NULL,
    Tenant      STRING(MAX),
    EmailAtLink STRING(MAX),
    CreatedAt   TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    LastUsedAt  TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    CONSTRAINT CK_%[1]sSessionIdentitiesId CHECK (REGEXP_CONTAINS(Id, %[3]s)),
    CONSTRAINT FK_%[1]sSessionIdentities_%[1]sSessionUsers FOREIGN KEY (UserId) REFERENCES %[1]sSessionUsers (Id) ON DELETE CASCADE,
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX %[1]sSessionIdentitiesByKey ON %[1]sSessionIdentities(Method, Connection, Subject);

CREATE INDEX %[1]sSessionIdentitiesByUserId ON %[1]sSessionIdentities(UserId);

-- The auth events: how each session was authenticated, one row per sign-in or step (the
-- method, its registration, and the provider's own amr claims). Not interleaved: an
-- interleaved child's key must begin with the parent's, so the events reference their
-- session through a cascading foreign key instead.
`, p, who, guidCheck) + eventsUp(p, who) + tokensUp(p, who) + factorsUp(p, who)
}

// eventsUp creates the auth events, pending sign-ins, sign-in attempts and mail links tables, the
// second part of the sessions migration; tokensUp follows it.
func eventsUp(p, who string) string {
	return fmt.Sprintf(`CREATE TABLE %[1]sSessionAuthEvents (
    SessionId  STRING(36) NOT NULL,
    Seq        INT64 NOT NULL,
    Method     STRING(MAX) NOT NULL,
    Connection STRING(MAX),
    IdpAmr     ARRAY<STRING(MAX)>,
    OccurredAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    CONSTRAINT FK_%[1]sSessionAuthEvents_%[1]sSessions FOREIGN KEY (SessionId) REFERENCES %[1]sSessions (Id) ON DELETE CASCADE,
) PRIMARY KEY (SessionId, Seq);

-- The pending sign-ins: a sign-in that waits for a password confirmation or the
-- application's MFA step, held server-side. The browser's pending cookie carries only an
-- opaque token; TokenHash is its SHA-256. A row is live until ExpiresAt or until a
-- completion, cancellation or replacement consumes it (ConsumedAt), once. ExpiresAt is
-- the library's deadline, written as given.
CREATE TABLE %[1]sSessionPendingSignIns (
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
    CONSTRAINT CK_%[1]sSessionPendingSignInsId CHECK (REGEXP_CONTAINS(Id, %[3]s)),
    CONSTRAINT CK_%[1]sSessionPendingSignInsStep CHECK (Step IN ('confirmation', 'mfa')),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX %[1]sSessionPendingSignInsByTokenHash ON %[1]sSessionPendingSignIns(TokenHash);

CREATE INDEX %[1]sSessionPendingSignInsByExpiresAt ON %[1]sSessionPendingSignIns(ExpiresAt);

-- The sign-in attempts: the attempt limiter's count of failed password attempts per
-- sign-in name, known or not, within a window, and the lock those failures earned. The
-- row is keyed by the submitted name, never by the user row, so an unknown name is
-- counted exactly as a known one; a success, an MFA completion or a password change
-- deletes it. WindowEndsAt and LockedUntil are deadlines, written as given.
CREATE TABLE %[1]sSessionSignInAttempts (
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
CREATE TABLE %[1]sSessionMailLinks (
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
    CONSTRAINT CK_%[1]sSessionMailLinksId CHECK (REGEXP_CONTAINS(Id, %[3]s)),
    CONSTRAINT CK_%[1]sSessionMailLinksKind CHECK (Kind IN ('invitation', 'verification', 'sign-up')),
    CONSTRAINT FK_%[1]sSessionMailLinks_%[1]sSessionUsers FOREIGN KEY (UserId) REFERENCES %[1]sSessionUsers (Id) ON DELETE CASCADE,
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX %[1]sSessionMailLinksByTokenHash ON %[1]sSessionMailLinks(TokenHash);

CREATE INDEX %[1]sSessionMailLinksByKind ON %[1]sSessionMailLinks(Kind, ConsumedAt, ExpiresAt);

`, p, who, guidCheck)
}

// tokensUp creates the tokens and device codes tables, the third part of the sessions
// migration: the bearer credentials of the machine outlets whose tokens the auth issues
// and the device authorizations command-line clients wait on; factorsUp follows it.
func tokensUp(p, who string) string {
	return fmt.Sprintf(`-- The tokens: the bearer credentials of the machine outlets whose tokens the %[2]s auth
-- issues. A row is one credential: an application token a person issued to a program of
-- their own, or the access or refresh token of a device grant a command-line client
-- obtained with a person's approval; the client holds an opaque secret and the row keeps
-- its hash. A token acts as its user (UserId) on the one machine outlet it was issued
-- for (Outlet) and is live while it is not revoked and not expired. A device grant's
-- tokens share a Family, and every successor a refresh exchange issues joins it, so a
-- rotated refresh token presented again revokes the family at once (RevokedReason
-- 'reuse'). CreatedAt, LastUsedAt and RevokedAt take the commit time; ExpiresAt is the
-- library's deadline, written as given. A token goes with its user.
CREATE TABLE %[1]sSessionTokens (
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
    CONSTRAINT CK_%[1]sSessionTokensId CHECK (REGEXP_CONTAINS(Id, %[3]s)),
    CONSTRAINT CK_%[1]sSessionTokensKind CHECK (Kind IN ('application', 'access', 'refresh')),
    CONSTRAINT CK_%[1]sSessionTokensRevokedReason CHECK (RevokedReason IS NULL OR RevokedReason IN ('revoked', 'rotated', 'reuse', 'user')),
    CONSTRAINT FK_%[1]sSessionTokens_%[1]sSessionUsers FOREIGN KEY (UserId) REFERENCES %[1]sSessionUsers (Id) ON DELETE CASCADE,
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX %[1]sSessionTokensByTokenHash ON %[1]sSessionTokens(TokenHash);

CREATE INDEX %[1]sSessionTokensByUser ON %[1]sSessionTokens(UserId, RevokedAt, ExpiresAt);

CREATE INDEX %[1]sSessionTokensByFamily ON %[1]sSessionTokens(Family, RevokedAt);

-- The device codes: the device authorizations command-line clients start for a machine
-- outlet whose tokens the %[2]s auth issues (RFC 8628). The client holds the opaque
-- device code and the row keeps its hash; the person is shown the user code, normalized
-- here. A row is pending until a signed-in person approves it (ApprovedAt, UserId) or
-- denies it (DeniedAt), or it expires; an approved row is consumed once (ConsumedAt),
-- when the client's poll redeems it for its tokens. LastPolledAt holds the client to the
-- polling interval. The time columns but ExpiresAt take the commit time. An approved
-- code goes with its user.
CREATE TABLE %[1]sSessionDeviceCodes (
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
    CONSTRAINT CK_%[1]sSessionDeviceCodesId CHECK (REGEXP_CONTAINS(Id, %[3]s)),
    CONSTRAINT FK_%[1]sSessionDeviceCodes_%[1]sSessionUsers FOREIGN KEY (UserId) REFERENCES %[1]sSessionUsers (Id) ON DELETE CASCADE,
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX %[1]sSessionDeviceCodesByHash ON %[1]sSessionDeviceCodes(DeviceCodeHash);

CREATE INDEX %[1]sSessionDeviceCodesByUserCode ON %[1]sSessionDeviceCodes(UserCode, ExpiresAt);
`, p, who, guidCheck)
}

// factorsUp creates the sign-in codes and trusted devices tables, the last part of the
// sessions migration: the second factor every session carries.
func factorsUp(p, who string) string {
	return fmt.Sprintf(`
-- The second factor of the %[2]s auth's sessions: the sign-in codes and the trusted
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
CREATE TABLE %[1]sSessionFactorCodes (
    Id         STRING(36) NOT NULL,
    PendingId  STRING(36) NOT NULL,
    Email      STRING(MAX) NOT NULL,
    CodeHash   BYTES(32) NOT NULL,
    Attempts   INT64 NOT NULL,
    CreatedAt  TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    ExpiresAt  TIMESTAMP NOT NULL,
    ConsumedAt TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    CONSTRAINT CK_%[1]sSessionFactorCodesId CHECK (REGEXP_CONTAINS(Id, %[3]s)),
    CONSTRAINT FK_%[1]sSessionFactorCodes_%[1]sSessionPendingSignIns FOREIGN KEY (PendingId) REFERENCES %[1]sSessionPendingSignIns (Id) ON DELETE CASCADE,
) PRIMARY KEY (Id);

CREATE INDEX %[1]sSessionFactorCodesByPending ON %[1]sSessionFactorCodes(PendingId, ConsumedAt, ExpiresAt);

CREATE TABLE %[1]sSessionTrustedDevices (
    Id         STRING(36) NOT NULL,
    UserId     STRING(36) NOT NULL,
    TokenHash  BYTES(32) NOT NULL,
    CreatedAt  TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    ExpiresAt  TIMESTAMP NOT NULL,
    LastUsedAt TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    RevokedAt  TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    CONSTRAINT CK_%[1]sSessionTrustedDevicesId CHECK (REGEXP_CONTAINS(Id, %[3]s)),
    CONSTRAINT FK_%[1]sSessionTrustedDevices_%[1]sSessionUsers FOREIGN KEY (UserId) REFERENCES %[1]sSessionUsers (Id) ON DELETE CASCADE,
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX %[1]sSessionTrustedDevicesByTokenHash ON %[1]sSessionTrustedDevices(TokenHash);

CREATE INDEX %[1]sSessionTrustedDevicesByUser ON %[1]sSessionTrustedDevices(UserId, RevokedAt);
`, p, who, guidCheck)
}

func sessionsDown(p string) string {
	lines := []string{
		"DROP INDEX %[1]sSessionTrustedDevicesByUser;",
		"DROP INDEX %[1]sSessionTrustedDevicesByTokenHash;",
		"DROP TABLE %[1]sSessionTrustedDevices;",
		"DROP INDEX %[1]sSessionFactorCodesByPending;",
		"DROP TABLE %[1]sSessionFactorCodes;",
		"DROP INDEX %[1]sSessionDeviceCodesByUserCode;",
		"DROP INDEX %[1]sSessionDeviceCodesByHash;",
		"DROP TABLE %[1]sSessionDeviceCodes;",
		"DROP INDEX %[1]sSessionTokensByFamily;",
		"DROP INDEX %[1]sSessionTokensByUser;",
		"DROP INDEX %[1]sSessionTokensByTokenHash;",
		"DROP TABLE %[1]sSessionTokens;",
		"DROP INDEX %[1]sSessionMailLinksByKind;",
		"DROP INDEX %[1]sSessionMailLinksByTokenHash;",
		"DROP TABLE %[1]sSessionMailLinks;",
		"DROP TABLE %[1]sSessionSignInAttempts;",
		"DROP INDEX %[1]sSessionPendingSignInsByExpiresAt;",
		"DROP INDEX %[1]sSessionPendingSignInsByTokenHash;",
		"DROP TABLE %[1]sSessionPendingSignIns;",
		"DROP TABLE %[1]sSessionAuthEvents;",
		"DROP INDEX %[1]sSessionIdentitiesByUserId;",
		"DROP INDEX %[1]sSessionIdentitiesByKey;",
		"DROP TABLE %[1]sSessionIdentities;",
		"DROP INDEX %[1]sSessionsByProviderSessionId;",
		"DROP INDEX %[1]sSessionsByUserId;",
		"DROP INDEX %[1]sSessions_Expired_idx;",
		"DROP TABLE %[1]sSessions;",
	}

	return fmt.Sprintf(strings.Join(lines, "\n")+"\n", p)
}
