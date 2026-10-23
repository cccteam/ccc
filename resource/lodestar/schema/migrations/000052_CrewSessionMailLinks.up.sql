-- The crew's mail links: the single-use links the auth mails (the session library's
-- SessionMailLinks migration, under the auth's prefix). A row is one link: an invitation
-- to an address, carrying the sector and the roles its acceptance seats and who sent it;
-- the verification of an address a signed-in person adds (UserId names the account,
-- Email the address); or a sign-up a person starts by giving their address. The mail
-- carries an opaque token and the row keeps its hash; the row is accepted only while it
-- is live, not expired and not consumed, and is consumed by a conditional update so one
-- link completes once. A revoked invitation is consumed by hand. A verification link
-- goes with its user. CreatedAt and ConsumedAt take the commit time; ExpiresAt is the
-- library's deadline, written as given.
CREATE TABLE CrewSessionMailLinks (
    Id         STRING(36) NOT NULL,
    TokenHash  BYTES(32) NOT NULL,
    Kind       STRING(16) NOT NULL,
    Email      STRING(MAX) NOT NULL,
    UserId     STRING(36),
    Tenant     STRING(MAX),
    RoleNames  ARRAY<STRING(MAX)>,
    InvitedBy  STRING(36),
    CreatedAt  TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    ExpiresAt  TIMESTAMP NOT NULL,
    ConsumedAt TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    CONSTRAINT CK_CrewSessionMailLinksId CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
    CONSTRAINT CK_CrewSessionMailLinksKind CHECK (Kind IN ('invitation', 'verification', 'sign-up')),
    CONSTRAINT FK_CrewSessionMailLinks_CrewSessionUsers FOREIGN KEY (UserId) REFERENCES CrewSessionUsers (Id) ON DELETE CASCADE,
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX CrewSessionMailLinksByTokenHash ON CrewSessionMailLinks(TokenHash);

CREATE INDEX CrewSessionMailLinksByKind ON CrewSessionMailLinks(Kind, ConsumedAt, ExpiresAt);
