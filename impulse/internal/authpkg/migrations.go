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
// then the sessions with the identities, auth events, pending sign-ins and sign-in
// attempts in one migration, since a session set is one thing. The DDL is the session
// library's base and auth schema sets (schema/spanner/migrations,
// schema/spanner/auth/migrations) with the commit-timestamp options written inline. who
// names the population for the comments.
func Migrations(prefix, who string) []Migration {
	return []Migration{
		{Stem: prefix + "SessionUsers", Up: usersUp(prefix, who), Down: usersDown(prefix)},
		{Stem: prefix + "Sessions", Up: sessionsUp(prefix, who), Down: sessionsDown(prefix)},
	}
}

func usersUp(p, who string) string {
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
  CONSTRAINT CK_%[1]sSessionUsersId CHECK (REGEXP_CONTAINS(Id, %[3]s)),
) PRIMARY KEY(Id);

CREATE UNIQUE INDEX %[1]sSessionUsersByNormalizedUsername ON %[1]sSessionUsers(NormalizedUsername);
`, p, who, guidCheck)
}

func usersDown(p string) string {
	return fmt.Sprintf("DROP INDEX %[1]sSessionUsersByNormalizedUsername;\nDROP TABLE %[1]sSessionUsers;\n", p)
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
`, p, who, guidCheck) + eventsUp(p, who)
}

// eventsUp creates the auth events, pending sign-ins and sign-in attempts tables, the
// second half of the sessions migration.
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
`, p, who, guidCheck)
}

func sessionsDown(p string) string {
	lines := []string{
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
