CREATE TABLE Sessions (
    Id         STRING(36) NOT NULL,
    Username   STRING(MAX) NOT NULL,
    Expired    BOOL NOT NULL,
    CreatedAt  TIMESTAMP NOT NULL,
    UpdatedAt  TIMESTAMP NOT NULL,
    CONSTRAINT CK_SessionsId CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
) PRIMARY KEY (Id);

CREATE INDEX Sessions_Expired_idx
    ON Sessions
    (Expired DESC);