-- The members's sessions: the session library's Auth sessions (session.Auth), one table for
-- every sign-in method registered on the Members auth. A session belongs to a user, and
-- deleting the user deletes its sessions; UserId is null only for a role-principal
-- impersonation. ProviderSessionId is the identity provider's session id when the
-- method reports one, for front-channel logout. Every column that records when
-- something happened is written with the database's commit time, never the
-- application's clock, which allow_commit_timestamp permits.
CREATE TABLE MembersSessions (
    Id                STRING(36) NOT NULL,
    Username          STRING(MAX) NOT NULL,
    Expired           BOOL NOT NULL,
    UserId            STRING(36),
    AuthenticatedAt   TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    ProviderSessionId STRING(MAX),
    CreatedAt         TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    UpdatedAt         TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    CONSTRAINT CK_MembersSessionsId CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
    CONSTRAINT FK_MembersSessions_MembersSessionUsers FOREIGN KEY (UserId) REFERENCES MembersSessionUsers (Id) ON DELETE CASCADE,
) PRIMARY KEY (Id);

CREATE INDEX MembersSessions_Expired_idx
    ON MembersSessions
    (Expired DESC);

CREATE INDEX MembersSessionsByUserId ON MembersSessions(UserId);

CREATE INDEX MembersSessionsByProviderSessionId ON MembersSessions(ProviderSessionId);
