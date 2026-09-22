CREATE TABLE MembersOIDCUsers (
    Id         STRING(36) NOT NULL,
    Tid        STRING(36) NOT NULL,
    Oid        STRING(36) NOT NULL,
    Username   STRING(MAX) NOT NULL,
    CreatedAt  TIMESTAMP NOT NULL,
    UpdatedAt  TIMESTAMP NOT NULL,
    CONSTRAINT CK_MembersOIDCUsersId CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX MembersOIDCUsersByTidOid ON MembersOIDCUsers (Tid, Oid);
