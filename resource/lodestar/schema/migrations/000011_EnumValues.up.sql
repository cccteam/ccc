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
