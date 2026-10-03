CREATE TABLE StaffRoles (
  Kind STRING(16) NOT NULL,
  Axis STRING(128) NOT NULL,
  Domain STRING(128) NOT NULL,
  Role STRING(128) NOT NULL,
  UpdatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
  CONSTRAINT StaffRolesKind CHECK (Kind IN ('global', 'domain', 'every')),
) PRIMARY KEY (Kind, Axis, Domain, Role);

CREATE TABLE StaffUserRoles (
  Kind STRING(16) NOT NULL,
  Axis STRING(128) NOT NULL,
  Domain STRING(128) NOT NULL,
  Role STRING(128) NOT NULL,
  User STRING(320) NOT NULL,
  CreatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
  CONSTRAINT StaffUserRolesKind CHECK (Kind IN ('global', 'domain', 'every')),
) PRIMARY KEY (Kind, Axis, Domain, Role, User);

CREATE INDEX StaffUserRolesByUser ON StaffUserRoles (User, Kind, Axis, Domain);

CREATE TABLE StaffRoleGrants (
  Kind STRING(16) NOT NULL,
  Axis STRING(128) NOT NULL,
  Domain STRING(128) NOT NULL,
  Role STRING(128) NOT NULL,
  Permission STRING(64) NOT NULL,
  Resource STRING(128) NOT NULL,
  Field STRING(128) NOT NULL,
  Condition STRING(MAX) NOT NULL,
  UpdatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
  CONSTRAINT StaffRoleGrantsKind CHECK (Kind IN ('global', 'domain', 'every')),
) PRIMARY KEY (Kind, Axis, Domain, Role, Permission, Resource, Field, Condition),
  INTERLEAVE IN PARENT StaffRoles ON DELETE CASCADE;
