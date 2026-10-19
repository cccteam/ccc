-- The staff auth's users: one row per person whichever way they sign in. PasswordHash is
-- set for a person with a password and null for one a directory alone admits.
CREATE TABLE StaffSessionUsers (
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
  CONSTRAINT CK_StaffSessionUsersId CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
) PRIMARY KEY(Id);

CREATE UNIQUE INDEX StaffSessionUsersByNormalizedUsername ON StaffSessionUsers(NormalizedUsername);
