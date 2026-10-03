-- Both auths' access tables take the shape the access library reads now. Where a row is
-- held is the (Kind, Axis, Domain) triple, Kind one of global, domain, or every, so a
-- membership or a custom role held in every sector is one row that reaches a sector
-- created later too. UserRoles stands alone rather than under Roles: a membership names a
-- release default role, which has no Roles row, and an index led by the user serves the
-- listing of everything one login holds. The statements are the library's own
-- (spannerstore.New(db, spannerstore.WithPrefix("Crew")).DDL(), likewise Members).
--
-- Role and grant rows are not carried over. The default roles now come from the role
-- file embedded in each auth's package and the bootstrap seeds the memberships again, so
-- the tables are dropped and recreated empty; a custom role or a membership held before
-- this migration is gone with them.
DROP INDEX CrewCrewUserRolesByScopeUser;

DROP TABLE CrewRoleGrants;

DROP TABLE CrewUserRoles;

DROP TABLE CrewRoles;

DROP INDEX MembersMembersUserRolesByScopeUser;

DROP TABLE MembersRoleGrants;

DROP TABLE MembersUserRoles;

DROP TABLE MembersRoles;

CREATE TABLE CrewRoles (
  Kind STRING(16) NOT NULL,
  Axis STRING(128) NOT NULL,
  Domain STRING(128) NOT NULL,
  Role STRING(128) NOT NULL,
  UpdatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
  CONSTRAINT CrewRolesKind CHECK (Kind IN ('global', 'domain', 'every')),
) PRIMARY KEY (Kind, Axis, Domain, Role);

CREATE TABLE CrewUserRoles (
  Kind STRING(16) NOT NULL,
  Axis STRING(128) NOT NULL,
  Domain STRING(128) NOT NULL,
  Role STRING(128) NOT NULL,
  User STRING(320) NOT NULL,
  CreatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
  CONSTRAINT CrewUserRolesKind CHECK (Kind IN ('global', 'domain', 'every')),
) PRIMARY KEY (Kind, Axis, Domain, Role, User);

CREATE INDEX CrewUserRolesByUser ON CrewUserRoles (User, Kind, Axis, Domain);

CREATE TABLE CrewRoleGrants (
  Kind STRING(16) NOT NULL,
  Axis STRING(128) NOT NULL,
  Domain STRING(128) NOT NULL,
  Role STRING(128) NOT NULL,
  Permission STRING(64) NOT NULL,
  Resource STRING(128) NOT NULL,
  Field STRING(128) NOT NULL,
  Condition STRING(MAX) NOT NULL,
  UpdatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
  CONSTRAINT CrewRoleGrantsKind CHECK (Kind IN ('global', 'domain', 'every')),
) PRIMARY KEY (Kind, Axis, Domain, Role, Permission, Resource, Field, Condition),
  INTERLEAVE IN PARENT CrewRoles ON DELETE CASCADE;

CREATE TABLE MembersRoles (
  Kind STRING(16) NOT NULL,
  Axis STRING(128) NOT NULL,
  Domain STRING(128) NOT NULL,
  Role STRING(128) NOT NULL,
  UpdatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
  CONSTRAINT MembersRolesKind CHECK (Kind IN ('global', 'domain', 'every')),
) PRIMARY KEY (Kind, Axis, Domain, Role);

CREATE TABLE MembersUserRoles (
  Kind STRING(16) NOT NULL,
  Axis STRING(128) NOT NULL,
  Domain STRING(128) NOT NULL,
  Role STRING(128) NOT NULL,
  User STRING(320) NOT NULL,
  CreatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
  CONSTRAINT MembersUserRolesKind CHECK (Kind IN ('global', 'domain', 'every')),
) PRIMARY KEY (Kind, Axis, Domain, Role, User);

CREATE INDEX MembersUserRolesByUser ON MembersUserRoles (User, Kind, Axis, Domain);

CREATE TABLE MembersRoleGrants (
  Kind STRING(16) NOT NULL,
  Axis STRING(128) NOT NULL,
  Domain STRING(128) NOT NULL,
  Role STRING(128) NOT NULL,
  Permission STRING(64) NOT NULL,
  Resource STRING(128) NOT NULL,
  Field STRING(128) NOT NULL,
  Condition STRING(MAX) NOT NULL,
  UpdatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
  CONSTRAINT MembersRoleGrantsKind CHECK (Kind IN ('global', 'domain', 'every')),
) PRIMARY KEY (Kind, Axis, Domain, Role, Permission, Resource, Field, Condition),
  INTERLEAVE IN PARENT MembersRoles ON DELETE CASCADE;
