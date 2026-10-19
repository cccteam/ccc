-- The crew's auth events: how each session was authenticated, one row per sign-in or
-- step (the method, its registration, and the provider's own amr claims), which the
-- session read reports beside the session. Not interleaved: an interleaved child's
-- key must begin with the parent's, so the events reference their session through a
-- cascading foreign key instead. OccurredAt is written with the database's commit time.
CREATE TABLE CrewSessionAuthEvents (
    SessionId  STRING(36) NOT NULL,
    Seq        INT64 NOT NULL,
    Method     STRING(MAX) NOT NULL,
    Connection STRING(MAX),
    IdpAmr     ARRAY<STRING(MAX)>,
    OccurredAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    CONSTRAINT FK_CrewSessionAuthEvents_CrewSessions FOREIGN KEY (SessionId) REFERENCES CrewSessions (Id) ON DELETE CASCADE,
) PRIMARY KEY (SessionId, Seq);
