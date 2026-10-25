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
