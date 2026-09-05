-- The access library's canonical DDL (spannerstore.Store.DDL) at access 0f95201: the
-- grant row's key carries the Condition, so one role may hold several conditional
-- grants on one permission and resource (§7's Dispatcher and Archivist pairs).
CREATE TABLE AccessRoles (
  IsGlobal BOOL NOT NULL,
  Domain STRING(128) NOT NULL,
  Role STRING(128) NOT NULL,
  UpdatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
) PRIMARY KEY (IsGlobal, Domain, Role);

CREATE TABLE AccessUserRoles (
  IsGlobal BOOL NOT NULL,
  Domain STRING(128) NOT NULL,
  Role STRING(128) NOT NULL,
  User STRING(320) NOT NULL,
  CreatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
) PRIMARY KEY (IsGlobal, Domain, Role, User),
  INTERLEAVE IN PARENT AccessRoles ON DELETE NO ACTION;

CREATE INDEX AccessUserRolesByScopeUser ON AccessUserRoles (IsGlobal, Domain, User);

CREATE TABLE AccessRoleGrants (
  IsGlobal BOOL NOT NULL,
  Domain STRING(128) NOT NULL,
  Role STRING(128) NOT NULL,
  Permission STRING(64) NOT NULL,
  Resource STRING(128) NOT NULL,
  Field STRING(128) NOT NULL,
  Condition STRING(MAX) NOT NULL,
  UpdatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
) PRIMARY KEY (IsGlobal, Domain, Role, Permission, Resource, Field, Condition),
  INTERLEAVE IN PARENT AccessRoles ON DELETE CASCADE;
