-- The members's identity links: which provider subject is which person. A Members session
-- user holds one row per registration they have signed in through (the Google
-- registration's subject under its method and connection), keyed uniquely so a
-- subject links to one person, and deleting the person deletes the links. CreatedAt
-- and LastUsedAt are written with the database's commit time.
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
