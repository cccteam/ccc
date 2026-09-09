CREATE TABLE CrewRoles (
  IsGlobal BOOL NOT NULL,
  Domain STRING(128) NOT NULL,
  Role STRING(128) NOT NULL,
  UpdatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
) PRIMARY KEY (IsGlobal, Domain, Role);

CREATE TABLE CrewUserRoles (
  IsGlobal BOOL NOT NULL,
  Domain STRING(128) NOT NULL,
  Role STRING(128) NOT NULL,
  User STRING(320) NOT NULL,
  CreatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
) PRIMARY KEY (IsGlobal, Domain, Role, User),
  INTERLEAVE IN PARENT CrewRoles ON DELETE NO ACTION;

CREATE INDEX CrewCrewUserRolesByScopeUser ON CrewUserRoles (IsGlobal, Domain, User);

CREATE TABLE CrewRoleGrants (
  IsGlobal BOOL NOT NULL,
  Domain STRING(128) NOT NULL,
  Role STRING(128) NOT NULL,
  Permission STRING(64) NOT NULL,
  Resource STRING(128) NOT NULL,
  Field STRING(128) NOT NULL,
  Condition STRING(MAX) NOT NULL,
  UpdatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
) PRIMARY KEY (IsGlobal, Domain, Role, Permission, Resource, Field, Condition),
  INTERLEAVE IN PARENT CrewRoles ON DELETE CASCADE;
