CREATE TABLE SessionImpersonations (
    SessionId        STRING(36) NOT NULL,
    ActorUsername    STRING(MAX) NOT NULL,
    ActorRealm       STRING(MAX),
    SourceSessionId  STRING(36),
    PrincipalKind    STRING(16) NOT NULL,
    PrincipalUser    STRING(MAX),
    PrincipalRole    STRING(MAX),
    Mask             STRING(MAX),
    Reason           STRING(MAX),
    StartedAt        TIMESTAMP NOT NULL,
    ExpiresAt        TIMESTAMP NOT NULL,
    EndedAt          TIMESTAMP,
    EndReason        STRING(16),
    CONSTRAINT CK_SessionImpersonationsSessionId CHECK (REGEXP_CONTAINS(SessionId, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
    CONSTRAINT CK_SessionImpersonationsPrincipalKind CHECK (PrincipalKind IN ('User', 'Role')),
) PRIMARY KEY (SessionId);

CREATE INDEX SessionImpersonations_ActorUsername_idx
    ON SessionImpersonations
    (ActorUsername, StartedAt DESC);

CREATE INDEX SessionImpersonations_PrincipalUser_idx
    ON SessionImpersonations
    (PrincipalUser, StartedAt DESC);
