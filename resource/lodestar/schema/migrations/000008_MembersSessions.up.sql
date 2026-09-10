CREATE TABLE MembersSessions (
    Id         STRING(36) NOT NULL,
    Username   STRING(MAX) NOT NULL,
    Expired    BOOL NOT NULL,
    CreatedAt  TIMESTAMP NOT NULL,
    UpdatedAt  TIMESTAMP NOT NULL,
    CONSTRAINT CK_MembersSessionsId CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
) PRIMARY KEY (Id);

CREATE INDEX MembersSessionsByUsername ON MembersSessions (Username);

CREATE INDEX MembersSessionsByExpired ON MembersSessions (Expired DESC);
