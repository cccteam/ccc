CREATE TABLE AnnouncementKinds (
  Id STRING(64) NOT NULL,
  Description STRING(MAX) NOT NULL,
) PRIMARY KEY (Id);

INSERT INTO AnnouncementKinds (Id, Description) VALUES ('notice', 'Notice');
INSERT INTO AnnouncementKinds (Id, Description) VALUES ('alert', 'Alert');
