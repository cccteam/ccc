-- The crew's sessions: the session library's Auth sessions (session.Auth), one table for
-- every sign-in method registered on the Crew auth. A session belongs to a user, and
-- deleting the user deletes its sessions; UserId is null only for a role-principal
-- impersonation. ProviderSessionId is the identity provider's session id when the
-- method reports one, for front-channel logout, and ProviderSessionIssuer the issuer it
-- belongs to: a provider's session id is unique only within its issuer. Every column that records when
-- something happened is written with the database's commit time, never the
-- application's clock, which allow_commit_timestamp permits.
CREATE TABLE CrewSessions (
    Id                STRING(36) NOT NULL,
    Username          STRING(MAX) NOT NULL,
    Expired           BOOL NOT NULL,
    UserId            STRING(36),
    AuthenticatedAt   TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    ProviderSessionId STRING(MAX),
    ProviderSessionIssuer STRING(MAX),
    CreatedAt         TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    UpdatedAt         TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    CONSTRAINT CK_CrewSessionsId CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
    CONSTRAINT FK_CrewSessions_CrewSessionUsers FOREIGN KEY (UserId) REFERENCES CrewSessionUsers (Id) ON DELETE CASCADE,
) PRIMARY KEY (Id);

CREATE INDEX CrewSessions_Expired_idx
    ON CrewSessions
    (Expired DESC);

CREATE INDEX CrewSessionsByUserId ON CrewSessions(UserId);

CREATE INDEX CrewSessionsByProviderSessionId ON CrewSessions(ProviderSessionId);
