-- The crew's impersonation record: the view-as and act-as-role session the watch desk
-- lists, joined into every crew session read. StartedAt and EndedAt are written with the
-- database's commit time, which allow_commit_timestamp permits; ExpiresAt is the
-- mint route's deadline, written as given.
CREATE TABLE CrewSessionImpersonations (
    SessionId        STRING(36) NOT NULL,
    ActorUsername    STRING(MAX) NOT NULL,
    ActorRealm       STRING(MAX),
    SourceSessionId  STRING(36),
    PrincipalKind    STRING(16) NOT NULL,
    PrincipalUser    STRING(MAX),
    PrincipalRole    STRING(MAX),
    Mask             STRING(MAX),
    Reason           STRING(MAX),
    StartedAt        TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    ExpiresAt        TIMESTAMP NOT NULL,
    EndedAt          TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    EndReason        STRING(16),
    CONSTRAINT CK_CrewSessionImpersonationsSessionId CHECK (REGEXP_CONTAINS(SessionId, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
    CONSTRAINT CK_CrewSessionImpersonationsPrincipalKind CHECK (PrincipalKind IN ('User', 'Role')),
) PRIMARY KEY (SessionId);

CREATE INDEX CrewSessionImpersonations_ActorUsername_idx
    ON CrewSessionImpersonations
    (ActorUsername, StartedAt DESC);

CREATE INDEX CrewSessionImpersonations_PrincipalUser_idx
    ON CrewSessionImpersonations
    (PrincipalUser, StartedAt DESC);
