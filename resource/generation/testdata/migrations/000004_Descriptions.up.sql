-- Three tables with a Description column, of which one is an enumeration: WidgetKinds
-- carries Id and Description, the enumeration shape; Flags is keyed by name and Roles by
-- scope and name, as the library's feature flags and an auth's roles are.
CREATE TABLE WidgetKinds (
  Id STRING(36) NOT NULL,
  Description STRING(MAX) NOT NULL,
) PRIMARY KEY (Id);

CREATE TABLE Flags (
  Name STRING(64) NOT NULL,
  Description STRING(MAX) NOT NULL,
  Enabled BOOL NOT NULL,
) PRIMARY KEY (Name);

CREATE TABLE Roles (
  Kind STRING(16) NOT NULL,
  Axis STRING(128) NOT NULL,
  Domain STRING(128) NOT NULL,
  Role STRING(128) NOT NULL,
  Description STRING(MAX) NOT NULL,
) PRIMARY KEY (Kind, Axis, Domain, Role);

INSERT INTO WidgetKinds (Id, Description) VALUES ('gear', 'Gear');
INSERT INTO WidgetKinds (Id, Description) VALUES ('lever', 'Lever');
INSERT INTO Flags (Name, Description, Enabled) VALUES ('Debriefs', 'Debriefs', true);
INSERT INTO Roles (Kind, Axis, Domain, Role, Description) VALUES ('global', '', '', 'Auditor', 'Reads the books.');
