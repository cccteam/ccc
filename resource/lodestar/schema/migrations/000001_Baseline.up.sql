-- Lodestar's schema as one migration. It grew through sixty migrations until 2026-11-02;
-- on a real Cloud Spanner instance each was a schema operation of its own, so they were
-- folded into this file. Each table carries every column and constraint it had gained, and
-- what a later migration had dropped is gone. Schema changes from here append files after
-- this one.

CREATE TABLE DataChangeEvents (
  TableName STRING(MAX) NOT NULL,
  RowId STRING(MAX) NOT NULL,
  Sequence INT64 NOT NULL,
  EventTime TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp=true),
  EventSource STRING(MAX) NOT NULL,
  ChangeSet JSON,
) PRIMARY KEY (TableName, RowId, Sequence, EventTime);

-- The pipeline's migration guard reads this database's migration version through the
-- migrations_reader database role, which holds select on the migrations table alone,
-- and the environment layer grants the test environment's deploy identity the role.
-- Created once, by this migration, after the runner has made the table. The emulator
-- accepts the statements and keeps no role.
CREATE ROLE migrations_reader;

GRANT SELECT ON TABLE SchemaMigrations TO ROLE migrations_reader;

-- The crew's session users: the people of the Crew auth (pkg/auth/crew), one row per
-- person whichever way they sign in. PasswordHash is set for a person with a password
-- and null for one the directory alone admits.
CREATE TABLE CrewSessionUsers (
  Id                 STRING(36) NOT NULL,
  Username           STRING(MAX) NOT NULL,
  NormalizedUsername STRING(MAX) AS (NORMALIZE_AND_CASEFOLD(Username)) STORED,
  PasswordHash       STRING(MAX),
  Disabled           BOOL NOT NULL DEFAULT (FALSE),
  -- An operator set the password (a first password, a reset): the person changes it
  -- before anything else is served (the session library's forced change).
  PasswordChangeRequired BOOL NOT NULL DEFAULT (FALSE),
  -- The database's time at the insert and at the latest sign-in (null before the
  -- first); the session library writes both as commit timestamps.
  CreatedAt          TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
  LastSignInAt       TIMESTAMP OPTIONS (allow_commit_timestamp = true),
  CONSTRAINT CK_CrewSessionUsersId CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  -- The crew's email addresses, in two states. Email is the address as the person gave
  -- it, NormalizedEmail the form uniqueness and lookups use, and EmailVerifiedAt the
  -- database's time at which the person proved they read the mailbox by following a link
  -- sent to it; null while the address is unverified. An address is trusted for nothing
  -- until it is verified: code that sends a link may use an unverified address, code that
  -- relies on one requires the time. An operator may put an address on a user by hand,
  -- unverified, which reserves it: uniqueness holds among every address held, verified or
  -- not. No address is required, and the address is held apart from the username (the
  -- session library's SessionUserEmail migration, under the auth's prefix).
  Email STRING(MAX),
  NormalizedEmail STRING(MAX) AS (NORMALIZE_AND_CASEFOLD(Email)) STORED,
  EmailVerifiedAt TIMESTAMP OPTIONS (allow_commit_timestamp = true),
) PRIMARY KEY(Id);

CREATE UNIQUE INDEX CrewSessionUsersByNormalizedUsername ON CrewSessionUsers(NormalizedUsername);

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

CREATE TABLE Sectors (
  Id STRING(64) NOT NULL,
  Name STRING(MAX) NOT NULL,
  Region STRING(MAX) NOT NULL,
  Established DATE NOT NULL,
  -- The sector's people enter through its directories only: a password does not reach a
  -- sector whose flag is true. Half of what the sector declares about who may enter it;
  -- the other half is SectorDirectories.
  DirectoryOnly BOOL NOT NULL DEFAULT (FALSE),

  CONSTRAINT CK_Sectors_Id CHECK (REGEXP_CONTAINS(Id, r'^[a-z][a-z0-9-]{1,62}$')),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX SectorsByName ON Sectors(Name);

-- The directories a sector admits its people through, one row per declaration: the
-- sign-in method (google, azure, workos) and the connection the method's identities carry
-- (a Google Workspace by the name the registration gives it, a Microsoft Entra tenant by
-- its tenant id). One domain, one directory by default; a second directory is a second row.
-- DefaultSeat names the role a stranger the directory vouches for is seated with in the
-- sector at their first sign-in; NULL says the sector admits them by invitation only.
CREATE TABLE SectorDirectories (
  Id STRING(36) NOT NULL,
  SectorId STRING(64) NOT NULL,
  Method STRING(16) NOT NULL,
  Connection STRING(MAX) NOT NULL,
  DefaultSeat STRING(64),

  CONSTRAINT CK_SectorDirectories_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT CK_SectorDirectories_Method CHECK (Method IN ('google', 'azure', 'workos')),
  CONSTRAINT FK_SectorDirectories_SectorId FOREIGN KEY (SectorId) REFERENCES Sectors(Id),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX SectorDirectoriesBySectorIdMethodConnection ON SectorDirectories(SectorId, Method, Connection);

CREATE TABLE Wings (
  Id STRING(36) NOT NULL,
  SectorId STRING(64) NOT NULL,
  Name STRING(MAX) NOT NULL,

  CONSTRAINT CK_Wings_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_Wings_SectorId FOREIGN KEY (SectorId) REFERENCES Sectors(Id),
) PRIMARY KEY (Id);

CREATE INDEX WingsByName ON Wings(Name);

-- The members's session users: the people of the Members auth (pkg/auth/members), one row per
-- person whichever way they sign in. PasswordHash is set for a person with a password
-- and null for one the directory alone admits.
CREATE TABLE MembersSessionUsers (
  Id                 STRING(36) NOT NULL,
  Username           STRING(MAX) NOT NULL,
  NormalizedUsername STRING(MAX) AS (NORMALIZE_AND_CASEFOLD(Username)) STORED,
  PasswordHash       STRING(MAX),
  Disabled           BOOL NOT NULL DEFAULT (FALSE),
  -- An operator set the password (a first password, a reset): the person changes it
  -- before anything else is served (the session library's forced change).
  PasswordChangeRequired BOOL NOT NULL DEFAULT (FALSE),
  -- The database's time at the insert and at the latest sign-in (null before the
  -- first); the session library writes both as commit timestamps.
  CreatedAt          TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
  LastSignInAt       TIMESTAMP OPTIONS (allow_commit_timestamp = true),
  CONSTRAINT CK_MembersSessionUsersId CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  -- The members' email addresses, in two states. Email is the address as the person gave
  -- it, NormalizedEmail the form uniqueness and lookups use, and EmailVerifiedAt the
  -- database's time at which the person proved they read the mailbox by following a link
  -- sent to it; null while the address is unverified. An address is trusted for nothing
  -- until it is verified: code that sends a link may use an unverified address, code that
  -- relies on one requires the time. An operator may put an address on a user by hand,
  -- unverified, which reserves it: uniqueness holds among every address held, verified or
  -- not. No address is required, and the address is held apart from the username (the
  -- session library's SessionUserEmail migration, under the auth's prefix).
  Email STRING(MAX),
  NormalizedEmail STRING(MAX) AS (NORMALIZE_AND_CASEFOLD(Email)) STORED,
  EmailVerifiedAt TIMESTAMP OPTIONS (allow_commit_timestamp = true),
) PRIMARY KEY(Id);

CREATE UNIQUE INDEX MembersSessionUsersByNormalizedUsername ON MembersSessionUsers(NormalizedUsername);

-- The members's sessions: the session library's Auth sessions (session.Auth), one table for
-- every sign-in method registered on the Members auth. A session belongs to a user, and
-- deleting the user deletes its sessions; UserId is null only for a role-principal
-- impersonation. ProviderSessionId is the identity provider's session id when the
-- method reports one, for front-channel logout, and ProviderSessionIssuer the issuer it
-- belongs to: a provider's session id is unique only within its issuer. Every column that records when
-- something happened is written with the database's commit time, never the
-- application's clock, which allow_commit_timestamp permits.
CREATE TABLE MembersSessions (
    Id                STRING(36) NOT NULL,
    Username          STRING(MAX) NOT NULL,
    Expired           BOOL NOT NULL,
    UserId            STRING(36),
    AuthenticatedAt   TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    ProviderSessionId STRING(MAX),
    ProviderSessionIssuer STRING(MAX),
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

CREATE TABLE MissionStatuses (
  Id STRING(64) NOT NULL,
  Description STRING(MAX) NOT NULL,
) PRIMARY KEY (Id);

CREATE TABLE RefitStatuses (
  Id STRING(64) NOT NULL,
  Description STRING(MAX) NOT NULL,
) PRIMARY KEY (Id);

CREATE TABLE MissionKinds (
  Id STRING(64) NOT NULL,
  Description STRING(MAX) NOT NULL,
) PRIMARY KEY (Id);

CREATE TABLE ShipRoles (
  Id STRING(64) NOT NULL,
  Description STRING(MAX) NOT NULL,
) PRIMARY KEY (Id);

CREATE TABLE Certifications (
  Id STRING(64) NOT NULL,
  Description STRING(MAX) NOT NULL,
) PRIMARY KEY (Id);

CREATE TABLE FailReasons (
  Id STRING(64) NOT NULL,
  Description STRING(MAX) NOT NULL,
) PRIMARY KEY (Id);

CREATE TABLE Clients (
  Id STRING(36) NOT NULL,
  Name STRING(MAX) NOT NULL,
  ContactName STRING(MAX) NOT NULL,
  ContactEmail STRING(MAX) NOT NULL,
  Trusted BOOL NOT NULL,

  CONSTRAINT CK_Clients_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  -- Insured records whether the outfit carries salvage cover: unknown until the booking
  -- desk hears from the underwriter, then yes or no. The column is nullable on purpose:
  -- the third state is the story, and it is what makes the generator type the field
  -- NullBoolean for the browser (the demo's one nullable BOOL).
  Insured BOOL,
  -- Search over resources (resource README, section 4): the schema states the facts and
  -- the generator derives the vocabulary. A hidden TOKENLIST column generated by
  -- TOKENIZE_SUBSTRING over a column, carried by the table's one search index, gives the
  -- column's field its text matches and a place in the resource's search box; a stored
  -- LOWER(col) column second in the tenant's index gives a field a case-folded startswith
  -- and nothing else.

  -- Clients: the box searches the name, the contact's name and the contact's email. The
  -- name's token column is anchored, so Name answers startswith and endswith beside
  -- contains; the other two answer contains alone. ContactEmail is PII, so a filter
  -- carrying the box posts. Clients is global, so the index is unpartitioned.
  Name_Search TOKENLIST AS (TOKENIZE_SUBSTRING(Name, relative_search_types => ['value_prefix', 'value_suffix'])) HIDDEN,
  ContactName_Search TOKENLIST AS (TOKENIZE_SUBSTRING(ContactName)) HIDDEN,
  ContactEmail_Search TOKENLIST AS (TOKENIZE_SUBSTRING(ContactEmail)) HIDDEN,
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX ClientsByName ON Clients(Name);

CREATE TABLE ShipClasses (
  Id STRING(36) NOT NULL,
  Designation STRING(MAX) NOT NULL,
  RoleId STRING(64) NOT NULL,
  Tonnage INT64 NOT NULL,
  Hardened BOOL NOT NULL,

  CONSTRAINT CK_ShipClasses_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_ShipClasses_RoleId FOREIGN KEY (RoleId) REFERENCES ShipRoles(Id),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX ShipClassesByDesignation ON ShipClasses(Designation);

CREATE TABLE Pilots (
  Id STRING(36) NOT NULL,
  UserId STRING(320) NOT NULL,
  DisplayName STRING(MAX) NOT NULL,
  Clearance INT64 NOT NULL,
  FeeLimit NUMERIC NOT NULL,

  CONSTRAINT CK_Pilots_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX PilotsByUserId ON Pilots(UserId);

CREATE TABLE ClientContacts (
  Id STRING(36) NOT NULL,
  UserId STRING(320) NOT NULL,
  ClientId STRING(36) NOT NULL,
  DisplayName STRING(MAX) NOT NULL,

  CONSTRAINT CK_ClientContacts_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_ClientContacts_ClientId FOREIGN KEY (ClientId) REFERENCES Clients(Id),
  -- ClientContacts: the display name a client's box also searches (@searchAlso on Client).
  -- The index stores ClientId, the foreign key the branch joins the contact to its client
  -- through, so a matching contact yields its client's key from the index alone.
  DisplayName_Search TOKENLIST AS (TOKENIZE_SUBSTRING(DisplayName)) HIDDEN,
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX ClientContactsByUserId ON ClientContacts(UserId);

CREATE TABLE PilotCertifications (
  UserId STRING(320) NOT NULL,
  CertificationId STRING(64) NOT NULL,

  CONSTRAINT FK_PilotCertifications_CertificationId FOREIGN KEY (CertificationId) REFERENCES Certifications(Id),
) PRIMARY KEY (UserId, CertificationId);

CREATE TABLE Squadrons (
  Id STRING(36) NOT NULL,
  WingId STRING(36) NOT NULL,
  Name STRING(MAX) NOT NULL,

  CONSTRAINT CK_Squadrons_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_Squadrons_WingId FOREIGN KEY (WingId) REFERENCES Wings(Id),
  -- Callsigns are the handles a squadron answers to on the open channel, filed by the sector
  -- marshal: a nullable ARRAY<STRING(16)> column typed by the plain []string in the resource
  -- struct, where NULL (the squadron has not filed yet) and an empty array (it flies silent
  -- and says so) are different answers. Nullable so the two can be told apart; a Go slice
  -- has one form, so the struct's field takes its nullability from this column, the
  -- generated request struct carries nullable:"true", and a null in a PATCH writes the
  -- column back to NULL. Spanner cannot index an ARRAY column, so the field carries no
  -- allow_filter and no list filters or sorts by it.
  Callsigns ARRAY<STRING(16)>,
) PRIMARY KEY (Id);

CREATE INDEX SquadronsByName ON Squadrons(Name);

CREATE TABLE SquadronMemberships (
  SquadronId STRING(36) NOT NULL,
  UserId STRING(320) NOT NULL,

  CONSTRAINT FK_SquadronMemberships_SquadronId FOREIGN KEY (SquadronId) REFERENCES Squadrons(Id),
) PRIMARY KEY (SquadronId, UserId);

CREATE INDEX SquadronMembershipsByUserId ON SquadronMemberships(UserId);

CREATE TABLE Hangars (
  Id STRING(36) NOT NULL,
  SectorId STRING(64) NOT NULL,
  Name STRING(MAX) NOT NULL,
  Zone STRING(MAX) NOT NULL,

  CONSTRAINT CK_Hangars_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_Hangars_SectorId FOREIGN KEY (SectorId) REFERENCES Sectors(Id),
  -- Hangars: the prefix fact alone. The lowercased name sits after the sector in an
  -- index, so a startswith on Name seeks that index with the sector bound; a hangar's name
  -- joins no box, since the table has no search index.
  Name_Lower STRING(MAX) AS (LOWER(Name)) STORED,
) PRIMARY KEY (Id);

CREATE TABLE Ships (
  Id STRING(36) NOT NULL,
  HangarId STRING(36) NOT NULL,
  ClassId STRING(36) NOT NULL,
  Registry STRING(16) NOT NULL,
  Name STRING(MAX) NOT NULL,
  LastRefitAt TIMESTAMP OPTIONS (allow_commit_timestamp = true),
  UpdatedAt TIMESTAMP OPTIONS (allow_commit_timestamp = true),

  CONSTRAINT CK_Ships_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_Ships_HangarId FOREIGN KEY (HangarId) REFERENCES Hangars(Id),
  CONSTRAINT FK_Ships_ClassId FOREIGN KEY (ClassId) REFERENCES ShipClasses(Id),
  -- CargoBays is the tonnage each of a ship's cargo bays takes, in order: an ARRAY<INT64>
  -- column, which the generator types number[] in the console's interface and metadata, one
  -- member of the display-type vocabulary the client's union lists. NOT NULL with an empty
  -- array as the default: a ship with no bays carries [], never NULL, a create that says
  -- nothing about bays gets none, and the struct's []int64 matches the column as the
  -- nullability check reads it (pointers and Null wrappers only). Spanner has no array
  -- equality and cannot index an ARRAY column, so the field carries no allow_filter and no
  -- list filters or sorts by it.
  CargoBays ARRAY<INT64> NOT NULL DEFAULT (ARRAY<INT64>[]),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX ShipsByRegistry ON Ships(Registry);

CREATE TABLE Missions (
  Id STRING(36) NOT NULL,
  SectorId STRING(64) NOT NULL,
  ClientId STRING(36) NOT NULL,
  KindId STRING(64) NOT NULL,
  Title STRING(MAX) NOT NULL,
  Brief STRING(MAX),
  Hazard INT64 NOT NULL,
  Fee NUMERIC NOT NULL,
  Deadline TIMESTAMP NOT NULL,
  RequiredCertId STRING(64),
  BookedBy STRING(320) NOT NULL,
  AssignedSquadronId STRING(36),
  StatusId STRING(64) NOT NULL,
  Notes STRING(MAX),
  Settlement NUMERIC,

  CONSTRAINT CK_Missions_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_Missions_SectorId FOREIGN KEY (SectorId) REFERENCES Sectors(Id),
  CONSTRAINT FK_Missions_ClientId FOREIGN KEY (ClientId) REFERENCES Clients(Id),
  CONSTRAINT FK_Missions_KindId FOREIGN KEY (KindId) REFERENCES MissionKinds(Id),
  CONSTRAINT FK_Missions_RequiredCertId FOREIGN KEY (RequiredCertId) REFERENCES Certifications(Id),
  CONSTRAINT FK_Missions_AssignedSquadronId FOREIGN KEY (AssignedSquadronId) REFERENCES Squadrons(Id),
  CONSTRAINT FK_Missions_StatusId FOREIGN KEY (StatusId) REFERENCES MissionStatuses(Id),
  BriefingTemplateId STRING(64),
  -- The hazard scale is 1..5. The create validator (MissionCreateValidator) answers the rule
  -- as 400 naming the field before anything is buffered; the update path has no validator,
  -- so this constraint is what refuses an update to hazard 9, at commit, and the library
  -- answers the refusal as 400 in the resource's name. It is the demo's only CHECK a request
  -- can reach: the others guard server-minted ids.
  CONSTRAINT CK_Missions_Hazard CHECK (Hazard BETWEEN 1 AND 5),
  -- Missions: the title as text and the deadline as a date (YYYYMMDD in UTC, the zone the
  -- day is read in: a timestamp's token column names its zone, since the database's
  -- default is not UTC), partitioned by the sector, so a search reads one sector's tokens.
  Title_Search TOKENLIST AS (TOKENIZE_SUBSTRING(Title)) HIDDEN,
  Deadline_Search TOKENLIST AS (TOKENIZE_SUBSTRING(FORMAT_TIMESTAMP('%Y%m%d', Deadline, 'UTC'))) HIDDEN,
) PRIMARY KEY (Id);

CREATE TABLE Sorties (
  Id STRING(36) NOT NULL,
  MissionId STRING(36) NOT NULL,
  ShipId STRING(36) NOT NULL,
  PilotUserId STRING(320) NOT NULL,
  LaunchedAt TIMESTAMP NOT NULL,
  ReturnedAt TIMESTAMP,
  Debrief STRING(MAX),

  CONSTRAINT CK_Sorties_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_Sorties_MissionId FOREIGN KEY (MissionId) REFERENCES Missions(Id),
  CONSTRAINT FK_Sorties_ShipId FOREIGN KEY (ShipId) REFERENCES Ships(Id),
) PRIMARY KEY (Id);

CREATE TABLE SortieExpenses (
  Id STRING(36) NOT NULL,
  SortieId STRING(36) NOT NULL,
  Category STRING(MAX) NOT NULL,
  Amount NUMERIC NOT NULL,
  Note STRING(MAX),

  CONSTRAINT CK_SortieExpenses_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_SortieExpenses_SortieId FOREIGN KEY (SortieId) REFERENCES Sorties(Id),
) PRIMARY KEY (Id);

CREATE TABLE Refits (
  Id STRING(36) NOT NULL,
  ShipId STRING(36) NOT NULL,
  StatusId STRING(64) NOT NULL,
  Estimate NUMERIC,
  InspectedAt TIMESTAMP OPTIONS (allow_commit_timestamp = true),
  OpenedBy STRING(320) NOT NULL,
  Notes STRING(MAX),

  CONSTRAINT CK_Refits_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_Refits_ShipId FOREIGN KEY (ShipId) REFERENCES Ships(Id),
  CONSTRAINT FK_Refits_StatusId FOREIGN KEY (StatusId) REFERENCES RefitStatuses(Id),
) PRIMARY KEY (Id);

CREATE TABLE RefitTasks (
  Id STRING(36) NOT NULL,
  TaskNumber INT64 NOT NULL,
  Instructions STRING(MAX) NOT NULL,
  Done BOOL NOT NULL,
  Notes STRING(MAX),

  CONSTRAINT FK_RefitTasks_Id FOREIGN KEY (Id) REFERENCES Refits(Id),
  PhotoKey STRING(36),
) PRIMARY KEY (Id, TaskNumber),
  INTERLEAVE IN PARENT Refits ON DELETE CASCADE;

CREATE TABLE Consignments (
  Id STRING(36) NOT NULL,
  SectorId STRING(64) NOT NULL,
  ClientId STRING(36) NOT NULL,
  BondCode STRING(MAX) NOT NULL,
  Description STRING(MAX) NOT NULL,
  Mass FLOAT64 NOT NULL,
  ExpiresOn DATE NOT NULL,
  ReleasedAt TIMESTAMP,

  CONSTRAINT CK_Consignments_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_Consignments_SectorId FOREIGN KEY (SectorId) REFERENCES Sectors(Id),
  CONSTRAINT FK_Consignments_ClientId FOREIGN KEY (ClientId) REFERENCES Clients(Id),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX ConsignmentsByBondCode ON Consignments(BondCode);

CREATE TABLE DroidReports (
  Id STRING(36) NOT NULL,
  SectorId STRING(64) NOT NULL,
  ShipId STRING(36) NOT NULL,
  Subsystem STRING(MAX) NOT NULL,
  Reading FLOAT64 NOT NULL,
  RecordedAt TIMESTAMP NOT NULL,

  CONSTRAINT CK_DroidReports_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_DroidReports_SectorId FOREIGN KEY (SectorId) REFERENCES Sectors(Id),
  CONSTRAINT FK_DroidReports_ShipId FOREIGN KEY (ShipId) REFERENCES Ships(Id),
  -- Frame is the telemetry frame as the droid's firmware sent it, a JSON document the
  -- service does not model, stored verbatim: typed in Go by telemetry.Frame, a type declared
  -- over json.RawMessage in the droid link's own package, whose JSON and Spanner methods the
  -- generator writes there (WithTypes). Nullable: the seeded readings and a droid on old
  -- firmware send none.
  Frame JSON,
) PRIMARY KEY (Id);

CREATE TABLE DistressCalls (
  Id STRING(36) NOT NULL,
  SectorId STRING(64) NOT NULL,
  Summary STRING(MAX) NOT NULL,
  Severity INT64 NOT NULL,
  CallerContact STRING(MAX),
  Transcript STRING(MAX),
  CaseNumber STRING(MAX) NOT NULL,
  FiledBy STRING(320) NOT NULL,

  CONSTRAINT CK_DistressCalls_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_DistressCalls_SectorId FOREIGN KEY (SectorId) REFERENCES Sectors(Id),
  Position JSON,
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX DistressCallsByCaseNumber ON DistressCalls(CaseNumber);

CREATE TABLE MissionDocuments (
  Id STRING(36) NOT NULL,
  MissionId STRING(36) NOT NULL,
  Title STRING(MAX) NOT NULL,
  FileName STRING(MAX) NOT NULL,
  ContentType STRING(MAX) NOT NULL,
  Size INT64 NOT NULL,
  StoreKey STRING(36) NOT NULL,
  UploadedBy STRING(MAX) NOT NULL,
  UploadedAt TIMESTAMP NOT NULL,

  CONSTRAINT CK_MissionDocuments_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_MissionDocuments_MissionId FOREIGN KEY (MissionId) REFERENCES Missions(Id),
  -- Two JSON columns typed by application types: Provenance, a plain struct whose
  -- TypeScript interface the generator derives, and Position, a GeoJSON Point whose
  -- TypeScript type the Go type declares with @typescript. Both nullable: a document filed
  -- before the origin was recorded has none, and a call relayed by voice names no point.
  Provenance JSON,
  -- Digest is the SHA-256 of a mission document's stored bytes, recorded by the upload
  -- method from the file the frame streamed: a BYTES column, which the generator types as
  -- one string in both clients' interfaces (encoding/json carries it as base64) with
  -- display type bytes. NOT NULL: every document is digested as it is filed, and nothing
  -- else writes the table. Added nullable, then constrained, the two-step form Spanner
  -- accepts for a NOT NULL column on an existing table.
  Digest BYTES(32) NOT NULL,
) PRIMARY KEY (Id);

CREATE INDEX MissionDocumentsByMissionId ON MissionDocuments(MissionId);

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

-- Tenant-leading indexes for the bare-tenant resources that list in a declared order:
-- each list reads its page off the index instead of sorting the sector's partition.
-- DroidReports keeps none on purpose; it is the registered demonstration of the
-- generator's index warning.
CREATE INDEX MissionsBySectorIdDeadline ON Missions(SectorId, Deadline);

CREATE INDEX HangarsBySectorIdName ON Hangars(SectorId, Name);

CREATE INDEX WingsBySectorIdName ON Wings(SectorId, Name);

CREATE INDEX ConsignmentsBySectorIdReleasedAt ON Consignments(SectorId, ReleasedAt DESC);

CREATE INDEX DistressCallsBySectorIdSeverity ON DistressCalls(SectorId, Severity DESC);

-- Both auths' access tables take the shape the access library reads. Where a row is held is
-- the (Kind, Axis, Domain) triple, Kind one of global, domain, or every, so a membership or a
-- custom role held in every sector is one row that reaches a sector created later too.
-- UserRoles stands alone rather than under Roles: a membership names a release default role,
-- which has no Roles row, and an index led by the user serves the listing of everything one
-- login holds. The statements are the library's own
-- (spannerstore.New(db, spannerstore.WithPrefix("Crew")).DDL(), likewise Members).
CREATE TABLE CrewRoles (
  Kind STRING(16) NOT NULL,
  Axis STRING(128) NOT NULL,
  Domain STRING(128) NOT NULL,
  Role STRING(128) NOT NULL,
  Description STRING(MAX) NOT NULL,
  UpdatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
  CONSTRAINT CrewRolesKind CHECK (Kind IN ('global', 'domain', 'every')),
) PRIMARY KEY (Kind, Axis, Domain, Role);

CREATE TABLE CrewUserRoles (
  Kind STRING(16) NOT NULL,
  Axis STRING(128) NOT NULL,
  Domain STRING(128) NOT NULL,
  Role STRING(128) NOT NULL,
  User STRING(320) NOT NULL,
  CreatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
  CONSTRAINT CrewUserRolesKind CHECK (Kind IN ('global', 'domain', 'every')),
) PRIMARY KEY (Kind, Axis, Domain, Role, User);

CREATE INDEX CrewUserRolesByUser ON CrewUserRoles (User, Kind, Axis, Domain);

CREATE TABLE CrewRoleGrants (
  Kind STRING(16) NOT NULL,
  Axis STRING(128) NOT NULL,
  Domain STRING(128) NOT NULL,
  Role STRING(128) NOT NULL,
  Permission STRING(64) NOT NULL,
  Resource STRING(128) NOT NULL,
  Field STRING(128) NOT NULL,
  Condition STRING(MAX) NOT NULL,
  UpdatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
  CONSTRAINT CrewRoleGrantsKind CHECK (Kind IN ('global', 'domain', 'every')),
) PRIMARY KEY (Kind, Axis, Domain, Role, Permission, Resource, Field, Condition),
  INTERLEAVE IN PARENT CrewRoles ON DELETE CASCADE;

CREATE TABLE MembersRoles (
  Kind STRING(16) NOT NULL,
  Axis STRING(128) NOT NULL,
  Domain STRING(128) NOT NULL,
  Role STRING(128) NOT NULL,
  Description STRING(MAX) NOT NULL,
  UpdatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
  CONSTRAINT MembersRolesKind CHECK (Kind IN ('global', 'domain', 'every')),
) PRIMARY KEY (Kind, Axis, Domain, Role);

CREATE TABLE MembersUserRoles (
  Kind STRING(16) NOT NULL,
  Axis STRING(128) NOT NULL,
  Domain STRING(128) NOT NULL,
  Role STRING(128) NOT NULL,
  User STRING(320) NOT NULL,
  CreatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
  CONSTRAINT MembersUserRolesKind CHECK (Kind IN ('global', 'domain', 'every')),
) PRIMARY KEY (Kind, Axis, Domain, Role, User);

CREATE INDEX MembersUserRolesByUser ON MembersUserRoles (User, Kind, Axis, Domain);

CREATE TABLE MembersRoleGrants (
  Kind STRING(16) NOT NULL,
  Axis STRING(128) NOT NULL,
  Domain STRING(128) NOT NULL,
  Role STRING(128) NOT NULL,
  Permission STRING(64) NOT NULL,
  Resource STRING(128) NOT NULL,
  Field STRING(128) NOT NULL,
  Condition STRING(MAX) NOT NULL,
  UpdatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
  CONSTRAINT MembersRoleGrantsKind CHECK (Kind IN ('global', 'domain', 'every')),
) PRIMARY KEY (Kind, Axis, Domain, Role, Permission, Resource, Field, Condition),
  INTERLEAVE IN PARENT MembersRoles ON DELETE CASCADE;

-- The feature flags: one row per flag the resources package declares (a resource.Feature
-- constant), written by the deploy's resource.MigrateFeatures and flipped by SetFeature;
-- FeatureFlagChanges records every flip in the same commit. The statements are the
-- library's own (resource.FeatureFlagsDDL(resource.SpannerDBType)), which a test pins.
CREATE TABLE FeatureFlags (
  Name STRING(64) NOT NULL,
  Description STRING(MAX) NOT NULL,
  Enabled BOOL NOT NULL,
  UpdatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
  UpdatedBy STRING(MAX) NOT NULL,
) PRIMARY KEY (Name);

CREATE TABLE FeatureFlagChanges (
  Name STRING(64) NOT NULL,
  ChangedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
  Enabled BOOL NOT NULL,
  ChangedBy STRING(MAX) NOT NULL,
) PRIMARY KEY (Name, ChangedAt);

-- Commendations is the commendations desk: a citation on a pilot's record for a sortie
-- flown well, filed by headquarters. It is the one resource behind a feature flag
-- (pkg/resources/features.go, Commendations): the table and the grants ship with the
-- release whether or not the flag is on, and the flag decides whether the desk exists
-- for the API and the browser. AwardedAt is the commit timestamp of the create. PilotId
-- leads an index with the award time, so one pilot's citations list in award order off
-- the index.
CREATE TABLE Commendations (
  Id STRING(36) NOT NULL,
  PilotId STRING(36) NOT NULL,
  Citation STRING(MAX) NOT NULL,
  AwardedBy STRING(320) NOT NULL,
  AwardedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),

  CONSTRAINT CK_Commendations_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_Commendations_PilotId FOREIGN KEY (PilotId) REFERENCES Pilots(Id),
) PRIMARY KEY (Id);

CREATE INDEX CommendationsByPilotIdAwardedAt ON Commendations(PilotId, AwardedAt DESC);

-- The crew's identity links: which provider subject is which person. A Crew session
-- user holds one row per registration they have signed in through (the Google
-- registration's subject under its method and connection), keyed uniquely so a
-- subject links to one person, and deleting the person deletes the links. CreatedAt
-- and LastUsedAt are written with the database's commit time.
CREATE TABLE CrewSessionIdentities (
    Id          STRING(36) NOT NULL,
    UserId      STRING(36) NOT NULL,
    Method      STRING(MAX) NOT NULL,
    Connection  STRING(MAX) NOT NULL,
    Subject     STRING(MAX) NOT NULL,
    Tenant      STRING(MAX),
    EmailAtLink STRING(MAX),
    CreatedAt   TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    LastUsedAt  TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    CONSTRAINT CK_CrewSessionIdentitiesId CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
    CONSTRAINT FK_CrewSessionIdentities_CrewSessionUsers FOREIGN KEY (UserId) REFERENCES CrewSessionUsers (Id) ON DELETE CASCADE,
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX CrewSessionIdentitiesByKey ON CrewSessionIdentities(Method, Connection, Subject);

CREATE INDEX CrewSessionIdentitiesByUserId ON CrewSessionIdentities(UserId);

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

-- The crew's pending sign-ins: a sign-in that waits for a password confirmation or the
-- application's MFA step, held server-side. The browser's pending cookie carries only
-- an opaque token; TokenHash is its SHA-256. A row is live until ExpiresAt or until a
-- completion, cancellation or replacement consumes it (ConsumedAt), once. CreatedAt and
-- ConsumedAt are written with the database's commit time; ExpiresAt is the library's
-- deadline, written as given.
CREATE TABLE CrewSessionPendingSignIns (
    Id                STRING(36) NOT NULL,
    TokenHash         BYTES(32) NOT NULL,
    Step              STRING(16) NOT NULL,
    Registration      STRING(MAX) NOT NULL,
    Method            STRING(MAX) NOT NULL,
    Connection        STRING(MAX) NOT NULL,
    Subject           STRING(MAX) NOT NULL,
    Email             STRING(MAX) NOT NULL,
    EmailVerified     BOOL NOT NULL,
    IdpAmr            ARRAY<STRING(MAX)>,
    Claims            BYTES(MAX),
    SignInName        STRING(MAX) NOT NULL,
    Guest             BOOL NOT NULL,
    Organization      STRING(MAX) NOT NULL,
    UserId            STRING(36),
    Username          STRING(MAX) NOT NULL,
    Tenant            STRING(MAX) NOT NULL,
    Events            BYTES(MAX) NOT NULL,
    RoleNames         ARRAY<STRING(MAX)>,
    Factors           ARRAY<STRING(MAX)>,
    ReturnUrl         STRING(MAX) NOT NULL,
    ProviderSessionId STRING(MAX),
    ProviderSessionIssuer STRING(MAX),
    CreatedAt         TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    ExpiresAt         TIMESTAMP NOT NULL,
    ConsumedAt        TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    CONSTRAINT CK_CrewSessionPendingSignInsId CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
    CONSTRAINT CK_CrewSessionPendingSignInsStep CHECK (Step IN ('confirmation', 'mfa')),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX CrewSessionPendingSignInsByTokenHash ON CrewSessionPendingSignIns(TokenHash);

CREATE INDEX CrewSessionPendingSignInsByExpiresAt ON CrewSessionPendingSignIns(ExpiresAt);

-- The members's identity links: which provider subject is which person. A Members session
-- user holds one row per registration they have signed in through (the Google
-- registration's subject under its method and connection), keyed uniquely so a
-- subject links to one person, and deleting the person deletes the links. CreatedAt
-- and LastUsedAt are written with the database's commit time.
CREATE TABLE MembersSessionIdentities (
    Id          STRING(36) NOT NULL,
    UserId      STRING(36) NOT NULL,
    Method      STRING(MAX) NOT NULL,
    Connection  STRING(MAX) NOT NULL,
    Subject     STRING(MAX) NOT NULL,
    Tenant      STRING(MAX),
    EmailAtLink STRING(MAX),
    CreatedAt   TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    LastUsedAt  TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    CONSTRAINT CK_MembersSessionIdentitiesId CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
    CONSTRAINT FK_MembersSessionIdentities_MembersSessionUsers FOREIGN KEY (UserId) REFERENCES MembersSessionUsers (Id) ON DELETE CASCADE,
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX MembersSessionIdentitiesByKey ON MembersSessionIdentities(Method, Connection, Subject);

CREATE INDEX MembersSessionIdentitiesByUserId ON MembersSessionIdentities(UserId);

-- The members's auth events: how each session was authenticated, one row per sign-in or
-- step (the method, its registration, and the provider's own amr claims), which the
-- session read reports beside the session. Not interleaved: an interleaved child's
-- key must begin with the parent's, so the events reference their session through a
-- cascading foreign key instead. OccurredAt is written with the database's commit time.
CREATE TABLE MembersSessionAuthEvents (
    SessionId  STRING(36) NOT NULL,
    Seq        INT64 NOT NULL,
    Method     STRING(MAX) NOT NULL,
    Connection STRING(MAX),
    IdpAmr     ARRAY<STRING(MAX)>,
    OccurredAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    CONSTRAINT FK_MembersSessionAuthEvents_MembersSessions FOREIGN KEY (SessionId) REFERENCES MembersSessions (Id) ON DELETE CASCADE,
) PRIMARY KEY (SessionId, Seq);

-- The members's pending sign-ins: a sign-in that waits for a password confirmation or the
-- application's MFA step, held server-side. The browser's pending cookie carries only
-- an opaque token; TokenHash is its SHA-256. A row is live until ExpiresAt or until a
-- completion, cancellation or replacement consumes it (ConsumedAt), once. CreatedAt and
-- ConsumedAt are written with the database's commit time; ExpiresAt is the library's
-- deadline, written as given.
CREATE TABLE MembersSessionPendingSignIns (
    Id                STRING(36) NOT NULL,
    TokenHash         BYTES(32) NOT NULL,
    Step              STRING(16) NOT NULL,
    Registration      STRING(MAX) NOT NULL,
    Method            STRING(MAX) NOT NULL,
    Connection        STRING(MAX) NOT NULL,
    Subject           STRING(MAX) NOT NULL,
    Email             STRING(MAX) NOT NULL,
    EmailVerified     BOOL NOT NULL,
    IdpAmr            ARRAY<STRING(MAX)>,
    Claims            BYTES(MAX),
    SignInName        STRING(MAX) NOT NULL,
    Guest             BOOL NOT NULL,
    Organization      STRING(MAX) NOT NULL,
    UserId            STRING(36),
    Username          STRING(MAX) NOT NULL,
    Tenant            STRING(MAX) NOT NULL,
    Events            BYTES(MAX) NOT NULL,
    RoleNames         ARRAY<STRING(MAX)>,
    Factors           ARRAY<STRING(MAX)>,
    ReturnUrl         STRING(MAX) NOT NULL,
    ProviderSessionId STRING(MAX),
    ProviderSessionIssuer STRING(MAX),
    CreatedAt         TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    ExpiresAt         TIMESTAMP NOT NULL,
    ConsumedAt        TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    CONSTRAINT CK_MembersSessionPendingSignInsId CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
    CONSTRAINT CK_MembersSessionPendingSignInsStep CHECK (Step IN ('confirmation', 'mfa')),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX MembersSessionPendingSignInsByTokenHash ON MembersSessionPendingSignIns(TokenHash);

CREATE INDEX MembersSessionPendingSignInsByExpiresAt ON MembersSessionPendingSignIns(ExpiresAt);

-- The crew's sign-in attempts: the password method's attempt limiter counts the failed
-- attempts on one name, known or not, within a window, and the lock those failures
-- earned (the session library's SessionSignInAttempts migration, under the auth's
-- prefix). The row is keyed by the submitted name as the users table normalizes
-- usernames, never by the user row, so an unknown name is counted exactly as a known
-- one. Failures counts in the window ending at WindowEndsAt; LockedUntil is set once
-- Failures reached the limit, and the name is refused until then. A success, an MFA
-- completion or a password change deletes the row. The mail the auth sends is counted
-- here too, under the address. UpdatedAt takes the commit time; WindowEndsAt and
-- LockedUntil are deadlines, written as given.
CREATE TABLE CrewSessionSignInAttempts (
    Name         STRING(MAX) NOT NULL,
    Failures     INT64 NOT NULL,
    WindowEndsAt TIMESTAMP NOT NULL,
    LockedUntil  TIMESTAMP,
    UpdatedAt    TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
) PRIMARY KEY (Name);

CREATE UNIQUE NULL_FILTERED INDEX CrewSessionUsersByNormalizedEmail ON CrewSessionUsers(NormalizedEmail);

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

-- The members' sign-in attempts: the password method's attempt limiter counts the failed
-- attempts on one name, known or not, within a window, and the lock those failures
-- earned (the session library's SessionSignInAttempts migration, under the auth's
-- prefix). The row is keyed by the submitted name as the users table normalizes
-- usernames, never by the user row, so an unknown name is counted exactly as a known
-- one. Failures counts in the window ending at WindowEndsAt; LockedUntil is set once
-- Failures reached the limit, and the name is refused until then. A success, an MFA
-- completion or a password change deletes the row. The mail the auth sends is counted
-- here too, under the address. UpdatedAt takes the commit time; WindowEndsAt and
-- LockedUntil are deadlines, written as given.
CREATE TABLE MembersSessionSignInAttempts (
    Name         STRING(MAX) NOT NULL,
    Failures     INT64 NOT NULL,
    WindowEndsAt TIMESTAMP NOT NULL,
    LockedUntil  TIMESTAMP,
    UpdatedAt    TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
) PRIMARY KEY (Name);

CREATE UNIQUE NULL_FILTERED INDEX MembersSessionUsersByNormalizedEmail ON MembersSessionUsers(NormalizedEmail);

-- The members' mail links: the single-use links the auth mails (the session library's
-- SessionMailLinks migration, under the auth's prefix). A row is one link: an invitation
-- to an address, carrying the sector and the roles its acceptance seats and who sent it;
-- the verification of an address a signed-in person adds (UserId names the account,
-- Email the address); or a sign-up a person starts by giving their address. The mail
-- carries an opaque token and the row keeps its hash; the row is accepted only while it
-- is live, not expired and not consumed, and is consumed by a conditional update so one
-- link completes once. A revoked invitation is consumed by hand. A verification link
-- goes with its user. CreatedAt and ConsumedAt take the commit time; ExpiresAt is the
-- library's deadline, written as given.
CREATE TABLE MembersSessionMailLinks (
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
    CONSTRAINT CK_MembersSessionMailLinksId CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
    CONSTRAINT CK_MembersSessionMailLinksKind CHECK (Kind IN ('invitation', 'verification', 'sign-up')),
    CONSTRAINT FK_MembersSessionMailLinks_MembersSessionUsers FOREIGN KEY (UserId) REFERENCES MembersSessionUsers (Id) ON DELETE CASCADE,
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX MembersSessionMailLinksByTokenHash ON MembersSessionMailLinks(TokenHash);

CREATE INDEX MembersSessionMailLinksByKind ON MembersSessionMailLinks(Kind, ConsumedAt, ExpiresAt);

-- The crew's tokens: the bearer credentials of the droids outlet (the session library's
-- SessionTokens migration, under the auth's prefix). A row is one credential: an
-- application token a crew member issued to a program of their own, the droid service
-- account's configured key seeded as one, or the access or refresh token of a device
-- grant a command-line client obtained with a crew member's approval; the client holds
-- an opaque secret and the row keeps its hash. A token acts as its user (UserId) on the
-- one machine outlet it was issued for (Outlet) and is live while it is not revoked and
-- not expired. A device grant's tokens share a Family, and every successor a refresh
-- exchange issues joins it, so a rotated refresh token presented again revokes the
-- family at once (RevokedReason 'reuse'). CreatedAt, LastUsedAt and RevokedAt take the
-- commit time; ExpiresAt is the library's deadline, written as given. A token goes with
-- its user.
CREATE TABLE CrewSessionTokens (
    Id            STRING(36) NOT NULL,
    TokenHash     BYTES(32) NOT NULL,
    Kind          STRING(16) NOT NULL,
    UserId        STRING(36) NOT NULL,
    Outlet        STRING(MAX) NOT NULL,
    Name          STRING(MAX),
    Family        STRING(36),
    ClientId      STRING(MAX),
    CreatedAt     TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    ExpiresAt     TIMESTAMP NOT NULL,
    LastUsedAt    TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    RevokedAt     TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    RevokedReason STRING(16),
    CONSTRAINT CK_CrewSessionTokensId CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
    CONSTRAINT CK_CrewSessionTokensKind CHECK (Kind IN ('application', 'access', 'refresh')),
    CONSTRAINT CK_CrewSessionTokensRevokedReason CHECK (RevokedReason IS NULL OR RevokedReason IN ('revoked', 'rotated', 'reuse', 'user')),
    CONSTRAINT FK_CrewSessionTokens_CrewSessionUsers FOREIGN KEY (UserId) REFERENCES CrewSessionUsers (Id) ON DELETE CASCADE,
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX CrewSessionTokensByTokenHash ON CrewSessionTokens(TokenHash);

CREATE INDEX CrewSessionTokensByUser ON CrewSessionTokens(UserId, RevokedAt, ExpiresAt);

CREATE INDEX CrewSessionTokensByFamily ON CrewSessionTokens(Family, RevokedAt);

-- The crew's device codes: the device authorizations command-line clients start for
-- the droids outlet (RFC 8628; the session library's SessionDeviceCodes migration,
-- under the auth's prefix). The client holds the opaque device code and the row keeps
-- its hash; the crew member is shown the user code, normalized here. A row is pending
-- until a signed-in crew member approves it (ApprovedAt, UserId) or denies it
-- (DeniedAt), or it expires; an approved row is consumed once (ConsumedAt), when the
-- client's poll redeems it for its tokens. LastPolledAt holds the client to the polling
-- interval. The time columns but ExpiresAt take the commit time. An approved code goes
-- with its user.
CREATE TABLE CrewSessionDeviceCodes (
    Id             STRING(36) NOT NULL,
    DeviceCodeHash BYTES(32) NOT NULL,
    UserCode       STRING(16) NOT NULL,
    ClientId       STRING(MAX) NOT NULL,
    Outlet         STRING(MAX) NOT NULL,
    UserId         STRING(36),
    CreatedAt      TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    ExpiresAt      TIMESTAMP NOT NULL,
    ApprovedAt     TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    DeniedAt       TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    LastPolledAt   TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    ConsumedAt     TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    CONSTRAINT CK_CrewSessionDeviceCodesId CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
    CONSTRAINT FK_CrewSessionDeviceCodes_CrewSessionUsers FOREIGN KEY (UserId) REFERENCES CrewSessionUsers (Id) ON DELETE CASCADE,
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX CrewSessionDeviceCodesByHash ON CrewSessionDeviceCodes(DeviceCodeHash);

CREATE INDEX CrewSessionDeviceCodesByUserCode ON CrewSessionDeviceCodes(UserCode, ExpiresAt);

-- The crew's second factor (the session library's SessionFactors migration, under the
-- auth's prefix): the sign-in codes and the trusted devices. A factor code is one code
-- mailed for a pending sign-in (PendingId): the mail carries the code and the row keeps
-- its hash; the row is live while it is not consumed and not expired, Attempts counts the
-- wrong codes entered against it, and a new code for the same pending sign-in consumes
-- the earlier one. A trusted device is a browser a crew member asked to be remembered
-- after a code (UserId): the browser holds an opaque token in the crew-trusted cookie and
-- the row keeps its hash; it stands in for the factor while it is not revoked and not
-- expired, and headquarters revokes every browser of a person's from the user-management
-- surface. A code goes with its pending sign-in and a device with its user. CreatedAt,
-- ConsumedAt, LastUsedAt and RevokedAt take the commit time; ExpiresAt is the library's
-- deadline, written as given.
CREATE TABLE CrewSessionFactorCodes (
    Id         STRING(36) NOT NULL,
    PendingId  STRING(36) NOT NULL,
    Email      STRING(MAX) NOT NULL,
    CodeHash   BYTES(32) NOT NULL,
    Attempts   INT64 NOT NULL,
    CreatedAt  TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    ExpiresAt  TIMESTAMP NOT NULL,
    ConsumedAt TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    CONSTRAINT CK_CrewSessionFactorCodesId CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
    CONSTRAINT FK_CrewSessionFactorCodes_CrewSessionPendingSignIns FOREIGN KEY (PendingId) REFERENCES CrewSessionPendingSignIns (Id) ON DELETE CASCADE,
) PRIMARY KEY (Id);

CREATE INDEX CrewSessionFactorCodesByPending ON CrewSessionFactorCodes(PendingId, ConsumedAt, ExpiresAt);

CREATE TABLE CrewSessionTrustedDevices (
    Id         STRING(36) NOT NULL,
    UserId     STRING(36) NOT NULL,
    TokenHash  BYTES(32) NOT NULL,
    CreatedAt  TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    ExpiresAt  TIMESTAMP NOT NULL,
    LastUsedAt TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    RevokedAt  TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    CONSTRAINT CK_CrewSessionTrustedDevicesId CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
    CONSTRAINT FK_CrewSessionTrustedDevices_CrewSessionUsers FOREIGN KEY (UserId) REFERENCES CrewSessionUsers (Id) ON DELETE CASCADE,
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX CrewSessionTrustedDevicesByTokenHash ON CrewSessionTrustedDevices(TokenHash);

CREATE INDEX CrewSessionTrustedDevicesByUser ON CrewSessionTrustedDevices(UserId, RevokedAt);

-- The members' second factor (the session library's SessionFactors migration, under the
-- auth's prefix): the sign-in codes mailed for a pending sign-in and the browsers a
-- member asked to be remembered after one, shaped as the crew's
-- (000058_CrewSessionFactors). A member whose company directory attests for them is
-- never mailed a code, so these tables fill only where the fleet does not administer
-- the directory.
CREATE TABLE MembersSessionFactorCodes (
    Id         STRING(36) NOT NULL,
    PendingId  STRING(36) NOT NULL,
    Email      STRING(MAX) NOT NULL,
    CodeHash   BYTES(32) NOT NULL,
    Attempts   INT64 NOT NULL,
    CreatedAt  TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    ExpiresAt  TIMESTAMP NOT NULL,
    ConsumedAt TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    CONSTRAINT CK_MembersSessionFactorCodesId CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
    CONSTRAINT FK_MembersSessionFactorCodes_MembersSessionPendingSignIns FOREIGN KEY (PendingId) REFERENCES MembersSessionPendingSignIns (Id) ON DELETE CASCADE,
) PRIMARY KEY (Id);

CREATE INDEX MembersSessionFactorCodesByPending ON MembersSessionFactorCodes(PendingId, ConsumedAt, ExpiresAt);

CREATE TABLE MembersSessionTrustedDevices (
    Id         STRING(36) NOT NULL,
    UserId     STRING(36) NOT NULL,
    TokenHash  BYTES(32) NOT NULL,
    CreatedAt  TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
    ExpiresAt  TIMESTAMP NOT NULL,
    LastUsedAt TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    RevokedAt  TIMESTAMP OPTIONS (allow_commit_timestamp = true),
    CONSTRAINT CK_MembersSessionTrustedDevicesId CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
    CONSTRAINT FK_MembersSessionTrustedDevices_MembersSessionUsers FOREIGN KEY (UserId) REFERENCES MembersSessionUsers (Id) ON DELETE CASCADE,
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX MembersSessionTrustedDevicesByTokenHash ON MembersSessionTrustedDevices(TokenHash);

CREATE INDEX MembersSessionTrustedDevicesByUser ON MembersSessionTrustedDevices(UserId, RevokedAt);

CREATE SEARCH INDEX ClientsSearch ON Clients(Name_Search, ContactName_Search, ContactEmail_Search);

CREATE SEARCH INDEX ClientContactsSearch ON ClientContacts(DisplayName_Search) STORING (ClientId);

CREATE SEARCH INDEX MissionsSearch ON Missions(Title_Search, Deadline_Search) PARTITION BY SectorId;

-- The mission board's box also reaches Clients' index, and comes back to the missions
-- through ClientId: the key set costs the matches only with this index.
CREATE INDEX MissionsBySectorIdClientId ON Missions(SectorId, ClientId);

CREATE INDEX HangarsBySectorIdNameLower ON Hangars(SectorId, Name_Lower);

-- The rows the schema owns, after every table: the runner sends each run of DDL as one batch
-- and each run of DML as one transaction, so with these last the file is one of each.

INSERT INTO MissionStatuses (Id, Description) VALUES ('open', 'Open');

INSERT INTO MissionStatuses (Id, Description) VALUES ('claimed', 'Claimed');

INSERT INTO MissionStatuses (Id, Description) VALUES ('underway', 'Underway');

INSERT INTO MissionStatuses (Id, Description) VALUES ('on_hold', 'On hold');

INSERT INTO MissionStatuses (Id, Description) VALUES ('completed', 'Completed');

INSERT INTO MissionStatuses (Id, Description) VALUES ('failed', 'Failed');

INSERT INTO MissionStatuses (Id, Description) VALUES ('stood_down', 'Stood down');

INSERT INTO RefitStatuses (Id, Description) VALUES ('docked', 'Docked');

INSERT INTO RefitStatuses (Id, Description) VALUES ('inspected', 'Inspected');

INSERT INTO RefitStatuses (Id, Description) VALUES ('in_refit', 'In refit');

INSERT INTO RefitStatuses (Id, Description) VALUES ('flight_test', 'Flight test');

INSERT INTO RefitStatuses (Id, Description) VALUES ('cleared', 'Cleared');

INSERT INTO RefitStatuses (Id, Description) VALUES ('scrapped', 'Scrapped');

INSERT INTO MissionKinds (Id, Description) VALUES ('rescue', 'Rescue');

INSERT INTO MissionKinds (Id, Description) VALUES ('salvage', 'Salvage');

INSERT INTO MissionKinds (Id, Description) VALUES ('escort', 'Escort');

INSERT INTO MissionKinds (Id, Description) VALUES ('courier', 'Courier');

INSERT INTO ShipRoles (Id, Description) VALUES ('cutter', 'Cutter');

INSERT INTO ShipRoles (Id, Description) VALUES ('tender', 'Tender');

INSERT INTO ShipRoles (Id, Description) VALUES ('tug', 'Tug');

INSERT INTO ShipRoles (Id, Description) VALUES ('medevac', 'Medevac');

INSERT INTO Certifications (Id, Description) VALUES ('deep_space', 'Deep space');

INSERT INTO Certifications (Id, Description) VALUES ('hazmat', 'Hazardous materials');

INSERT INTO Certifications (Id, Description) VALUES ('escort', 'Escort');

INSERT INTO Certifications (Id, Description) VALUES ('salvage', 'Salvage');

INSERT INTO FailReasons (Id, Description) VALUES ('unrecoverable', 'Unrecoverable');

INSERT INTO FailReasons (Id, Description) VALUES ('solar_weather', 'Solar weather');

INSERT INTO FailReasons (Id, Description) VALUES ('aborted', 'Aborted');

INSERT INTO FailReasons (Id, Description) VALUES ('recalled', 'Recalled');
