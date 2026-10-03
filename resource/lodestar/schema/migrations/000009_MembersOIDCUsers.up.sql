CREATE TABLE MembersOIDCUsers (
    Id         STRING(36) NOT NULL,
    Sub        STRING(MAX) NOT NULL,
    Hd         STRING(MAX) NOT NULL,
    Username   STRING(MAX) NOT NULL,
    CreatedAt  TIMESTAMP NOT NULL,
    UpdatedAt  TIMESTAMP NOT NULL,
    CONSTRAINT CK_MembersOIDCUsersId CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX MembersOIDCUsersBySub ON MembersOIDCUsers (Sub);
