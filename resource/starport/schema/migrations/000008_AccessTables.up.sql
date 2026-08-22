CREATE TABLE AccessRoles (
  Domain STRING(128) NOT NULL,
  Role STRING(128) NOT NULL,
  UpdatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
) PRIMARY KEY (Domain, Role);

CREATE TABLE AccessUserRoles (
  Domain STRING(128) NOT NULL,
  Role STRING(128) NOT NULL,
  User STRING(320) NOT NULL,
  CreatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
) PRIMARY KEY (Domain, Role, User),
  INTERLEAVE IN PARENT AccessRoles ON DELETE NO ACTION;

CREATE INDEX AccessUserRolesByDomainUser ON AccessUserRoles (Domain, User);

CREATE TABLE AccessRoleGrants (
  Domain STRING(128) NOT NULL,
  Role STRING(128) NOT NULL,
  Permission STRING(64) NOT NULL,
  Resource STRING(128) NOT NULL,
  Field STRING(128) NOT NULL,
  UpdatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
) PRIMARY KEY (Domain, Role, Permission, Resource, Field),
  INTERLEAVE IN PARENT AccessRoles ON DELETE CASCADE;
