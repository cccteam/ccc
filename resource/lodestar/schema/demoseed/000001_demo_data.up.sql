INSERT INTO Sectors (Id, Name, Region, Established)
VALUES ('anvil', 'Anvil Reach', 'Inner frontier', DATE '2030-03-01');

INSERT INTO Sectors (Id, Name, Region, Established)
VALUES ('bastion', 'Bastion Gate', 'Trade lane', DATE '2032-07-15');

INSERT INTO Sectors (Id, Name, Region, Established)
VALUES ('cinder', 'Cinder Verge', 'Burned-out frontier', DATE '2034-11-02');

INSERT INTO Clients (Id, Name, ContactName, ContactEmail, Trusted)
VALUES ('10000000-0000-4000-8000-000000000001', 'Halvard Freight', 'Cleo Halvard', 'cleo@halvard.example', TRUE);

INSERT INTO Clients (Id, Name, ContactName, ContactEmail, Trusted)
VALUES ('10000000-0000-4000-8000-000000000002', 'Meridian Survey Office', 'Ines Marrow', 'ines@meridian.example', TRUE);

INSERT INTO Clients (Id, Name, ContactName, ContactEmail, Trusted)
VALUES ('10000000-0000-4000-8000-000000000003', 'Bastion Relay Station', 'Relay Desk', 'desk@bastion-relay.example', FALSE);

INSERT INTO Clients (Id, Name, ContactName, ContactEmail, Trusted)
VALUES ('10000000-0000-4000-8000-000000000004', 'Vellum Medical Cooperative', 'Dr. Sato Vellum', 'sato@vellum.example', TRUE);

INSERT INTO ShipClasses (Id, Designation, RoleId, Tonnage, Hardened)
VALUES ('20000000-0000-4000-8000-000000000001', 'Kestrel', 'cutter', 900, FALSE);

INSERT INTO ShipClasses (Id, Designation, RoleId, Tonnage, Hardened)
VALUES ('20000000-0000-4000-8000-000000000002', 'Ox', 'tug', 4200, TRUE);

INSERT INTO ShipClasses (Id, Designation, RoleId, Tonnage, Hardened)
VALUES ('20000000-0000-4000-8000-000000000003', 'Heron', 'tender', 2100, FALSE);

INSERT INTO ShipClasses (Id, Designation, RoleId, Tonnage, Hardened)
VALUES ('20000000-0000-4000-8000-000000000004', 'Mercy', 'medevac', 1500, TRUE);

INSERT INTO Pilots (Id, UserId, DisplayName, Clearance, FeeLimit)
VALUES ('30000000-0000-4000-8000-000000000001', 'governor', 'Governor Greer', 5, NUMERIC '1000000');

INSERT INTO Pilots (Id, UserId, DisplayName, Clearance, FeeLimit)
VALUES ('30000000-0000-4000-8000-000000000002', 'marshal', 'Marshal Maren Voss', 5, NUMERIC '500000');

INSERT INTO Pilots (Id, UserId, DisplayName, Clearance, FeeLimit)
VALUES ('30000000-0000-4000-8000-000000000003', 'cadet', 'Cadet Cass', 2, NUMERIC '0');

INSERT INTO Pilots (Id, UserId, DisplayName, Clearance, FeeLimit)
VALUES ('30000000-0000-4000-8000-000000000004', 'pilot', 'Pilot Pax', 3, NUMERIC '0');

INSERT INTO Pilots (Id, UserId, DisplayName, Clearance, FeeLimit)
VALUES ('30000000-0000-4000-8000-000000000005', 'veteran', 'Veteran Vela', 5, NUMERIC '0');

INSERT INTO Pilots (Id, UserId, DisplayName, Clearance, FeeLimit)
VALUES ('30000000-0000-4000-8000-000000000006', 'lead', 'Flight Lead Lior', 4, NUMERIC '0');

INSERT INTO Pilots (Id, UserId, DisplayName, Clearance, FeeLimit)
VALUES ('30000000-0000-4000-8000-000000000007', 'dispatcher', 'Dispatcher Dunn', 3, NUMERIC '0');

INSERT INTO Pilots (Id, UserId, DisplayName, Clearance, FeeLimit)
VALUES ('30000000-0000-4000-8000-000000000008', 'overseer', 'Overseer Orla', 3, NUMERIC '0');

INSERT INTO Pilots (Id, UserId, DisplayName, Clearance, FeeLimit)
VALUES ('30000000-0000-4000-8000-000000000009', 'booking', 'Booking Agent Bex', 0, NUMERIC '25000');

INSERT INTO Pilots (Id, UserId, DisplayName, Clearance, FeeLimit)
VALUES ('30000000-0000-4000-8000-000000000010', 'wingco', 'Wing Commander Wilde', 5, NUMERIC '0');

INSERT INTO Pilots (Id, UserId, DisplayName, Clearance, FeeLimit)
VALUES ('30000000-0000-4000-8000-000000000011', 'engineer', 'Engineer Ezra', 0, NUMERIC '0');

INSERT INTO Pilots (Id, UserId, DisplayName, Clearance, FeeLimit)
VALUES ('30000000-0000-4000-8000-000000000012', 'quartermaster', 'Quartermaster Quill', 0, NUMERIC '0');

INSERT INTO Pilots (Id, UserId, DisplayName, Clearance, FeeLimit)
VALUES ('30000000-0000-4000-8000-000000000013', 'supercargo', 'Supercargo Sol', 0, NUMERIC '0');

INSERT INTO Pilots (Id, UserId, DisplayName, Clearance, FeeLimit)
VALUES ('30000000-0000-4000-8000-000000000014', 'archivist', 'Archivist Ada', 0, NUMERIC '0');

INSERT INTO Pilots (Id, UserId, DisplayName, Clearance, FeeLimit)
VALUES ('30000000-0000-4000-8000-000000000015', 'hazards', 'Hazard Analyst Hale', 0, NUMERIC '0');

INSERT INTO Pilots (Id, UserId, DisplayName, Clearance, FeeLimit)
VALUES ('30000000-0000-4000-8000-000000000016', 'dock', 'Dockmaster Dara', 0, NUMERIC '0');

INSERT INTO Pilots (Id, UserId, DisplayName, Clearance, FeeLimit)
VALUES ('30000000-0000-4000-8000-000000000017', 'watch', 'Night Watch Nadia', 0, NUMERIC '0');

INSERT INTO ClientContacts (Id, UserId, ClientId, DisplayName)
VALUES ('31000000-0000-4000-8000-000000000001', 'client', '10000000-0000-4000-8000-000000000001', 'Client Cleo');

INSERT INTO PilotCertifications (UserId, CertificationId) VALUES ('pilot', 'deep_space');
INSERT INTO PilotCertifications (UserId, CertificationId) VALUES ('pilot', 'salvage');
INSERT INTO PilotCertifications (UserId, CertificationId) VALUES ('veteran', 'deep_space');
INSERT INTO PilotCertifications (UserId, CertificationId) VALUES ('veteran', 'hazmat');
INSERT INTO PilotCertifications (UserId, CertificationId) VALUES ('veteran', 'escort');
INSERT INTO PilotCertifications (UserId, CertificationId) VALUES ('veteran', 'salvage');
INSERT INTO PilotCertifications (UserId, CertificationId) VALUES ('lead', 'deep_space');
INSERT INTO PilotCertifications (UserId, CertificationId) VALUES ('lead', 'escort');
INSERT INTO PilotCertifications (UserId, CertificationId) VALUES ('marshal', 'deep_space');
INSERT INTO PilotCertifications (UserId, CertificationId) VALUES ('marshal', 'hazmat');
INSERT INTO PilotCertifications (UserId, CertificationId) VALUES ('marshal', 'escort');
INSERT INTO PilotCertifications (UserId, CertificationId) VALUES ('marshal', 'salvage');
INSERT INTO PilotCertifications (UserId, CertificationId) VALUES ('wingco', 'escort');

INSERT INTO Wings (Id, SectorId, Name)
VALUES ('40000000-0000-4000-8000-000000000001', 'anvil', 'Forge Wing');

INSERT INTO Wings (Id, SectorId, Name)
VALUES ('40000000-0000-4000-8000-000000000002', 'bastion', 'Rampart Wing');

INSERT INTO Wings (Id, SectorId, Name)
VALUES ('40000000-0000-4000-8000-000000000003', 'cinder', 'Ember Wing');

INSERT INTO Squadrons (Id, WingId, Name)
VALUES ('50000000-0000-4000-8000-000000000001', '40000000-0000-4000-8000-000000000001', 'Hammer');

INSERT INTO Squadrons (Id, WingId, Name)
VALUES ('50000000-0000-4000-8000-000000000002', '40000000-0000-4000-8000-000000000001', 'Tongs');

INSERT INTO Squadrons (Id, WingId, Name)
VALUES ('50000000-0000-4000-8000-000000000003', '40000000-0000-4000-8000-000000000002', 'Portcullis');

INSERT INTO Squadrons (Id, WingId, Name)
VALUES ('50000000-0000-4000-8000-000000000004', '40000000-0000-4000-8000-000000000003', 'Ashfall');

INSERT INTO SquadronMemberships (SquadronId, UserId) VALUES ('50000000-0000-4000-8000-000000000001', 'lead');
INSERT INTO SquadronMemberships (SquadronId, UserId) VALUES ('50000000-0000-4000-8000-000000000001', 'veteran');
INSERT INTO SquadronMemberships (SquadronId, UserId) VALUES ('50000000-0000-4000-8000-000000000001', 'wingco');
INSERT INTO SquadronMemberships (SquadronId, UserId) VALUES ('50000000-0000-4000-8000-000000000001', 'dispatcher');
INSERT INTO SquadronMemberships (SquadronId, UserId) VALUES ('50000000-0000-4000-8000-000000000002', 'dispatcher');
INSERT INTO SquadronMemberships (SquadronId, UserId) VALUES ('50000000-0000-4000-8000-000000000002', 'pilot');
INSERT INTO SquadronMemberships (SquadronId, UserId) VALUES ('50000000-0000-4000-8000-000000000003', 'dispatcher');
INSERT INTO SquadronMemberships (SquadronId, UserId) VALUES ('50000000-0000-4000-8000-000000000003', 'pilot');

INSERT INTO Hangars (Id, SectorId, Name, Zone)
VALUES ('60000000-0000-4000-8000-000000000001', 'anvil', 'Anvil Dock One', 'dock');

INSERT INTO Hangars (Id, SectorId, Name, Zone)
VALUES ('60000000-0000-4000-8000-000000000002', 'anvil', 'Quarantine Bay', 'quarantine');

INSERT INTO Hangars (Id, SectorId, Name, Zone)
VALUES ('60000000-0000-4000-8000-000000000003', 'bastion', 'Bastion Slip', 'dock');

INSERT INTO Hangars (Id, SectorId, Name, Zone)
VALUES ('60000000-0000-4000-8000-000000000004', 'cinder', 'Cinder Yard', 'salvage');

INSERT INTO Ships (Id, HangarId, ClassId, Registry, Name, LastRefitAt, UpdatedAt)
VALUES ('70000000-0000-4000-8000-000000000001', '60000000-0000-4000-8000-000000000001', '20000000-0000-4000-8000-000000000001', 'LS-101', 'Kingfisher', NULL, NULL);

INSERT INTO Ships (Id, HangarId, ClassId, Registry, Name, LastRefitAt, UpdatedAt)
VALUES ('70000000-0000-4000-8000-000000000002', '60000000-0000-4000-8000-000000000001', '20000000-0000-4000-8000-000000000002', 'LS-202', 'Stubborn Mule', NULL, NULL);

INSERT INTO Ships (Id, HangarId, ClassId, Registry, Name, LastRefitAt, UpdatedAt)
VALUES ('70000000-0000-4000-8000-000000000003', '60000000-0000-4000-8000-000000000002', '20000000-0000-4000-8000-000000000003', 'LS-303', 'Lantern', NULL, NULL);

INSERT INTO Ships (Id, HangarId, ClassId, Registry, Name, LastRefitAt, UpdatedAt)
VALUES ('70000000-0000-4000-8000-000000000004', '60000000-0000-4000-8000-000000000001', '20000000-0000-4000-8000-000000000004', 'LS-404', 'Good Samaritan', TIMESTAMP '2026-06-01T09:00:00Z', NULL);

INSERT INTO Ships (Id, HangarId, ClassId, Registry, Name, LastRefitAt, UpdatedAt)
VALUES ('70000000-0000-4000-8000-000000000005', '60000000-0000-4000-8000-000000000001', '20000000-0000-4000-8000-000000000002', 'LS-505', 'Rusty Anchor', NULL, NULL);

INSERT INTO Ships (Id, HangarId, ClassId, Registry, Name, LastRefitAt, UpdatedAt)
VALUES ('70000000-0000-4000-8000-000000000006', '60000000-0000-4000-8000-000000000003', '20000000-0000-4000-8000-000000000001', 'LS-606', 'Bastion Watch', NULL, NULL);

INSERT INTO Ships (Id, HangarId, ClassId, Registry, Name, LastRefitAt, UpdatedAt)
VALUES ('70000000-0000-4000-8000-000000000007', '60000000-0000-4000-8000-000000000004', '20000000-0000-4000-8000-000000000002', 'LS-707', 'Cinder Moth', NULL, NULL);

-- Missions. Every §7 condition keeps at least one row on each side: hazard 1..5,
-- fees below and above 5000 and 10000, every state, missions with and without a
-- required certification, bookers on both sides of each ownership rule, and one
-- deadline written as bootstrap time plus three minutes so the overseer's
-- `deadline < now` grant flips during a live walkthrough.
INSERT INTO Missions (Id, SectorId, ClientId, KindId, Title, Brief, Hazard, Fee, Deadline, RequiredCertId, BookedBy, AssignedSquadronId, StatusId, Notes, Settlement)
VALUES ('80000000-0000-4000-8000-000000000001', 'anvil', '10000000-0000-4000-8000-000000000001', 'rescue', 'Stranded hauler off Anvil Reach', 'Halvard hauler lost main drive; crew of four aboard', 2, NUMERIC '8000', TIMESTAMP '2026-09-20T12:00:00Z', NULL, 'booking', NULL, 'open', NULL, NULL);

INSERT INTO Missions (Id, SectorId, ClientId, KindId, Title, Brief, Hazard, Fee, Deadline, RequiredCertId, BookedBy, AssignedSquadronId, StatusId, Notes, Settlement)
VALUES ('80000000-0000-4000-8000-000000000002', 'anvil', '10000000-0000-4000-8000-000000000002', 'salvage', 'Salvage the drifting Corvid wreck', 'Survey office wants the instrument bay recovered intact', 3, NUMERIC '24000', TIMESTAMP_ADD(CURRENT_TIMESTAMP(), INTERVAL 3 MINUTE), 'salvage', 'booking', '50000000-0000-4000-8000-000000000001', 'claimed', NULL, NULL);

INSERT INTO Missions (Id, SectorId, ClientId, KindId, Title, Brief, Hazard, Fee, Deadline, RequiredCertId, BookedBy, AssignedSquadronId, StatusId, Notes, Settlement)
VALUES ('80000000-0000-4000-8000-000000000003', 'anvil', '10000000-0000-4000-8000-000000000002', 'escort', 'Escort the Meridian survey convoy', 'Three survey barges through the debris belt', 4, NUMERIC '15000', TIMESTAMP '2026-09-30T08:00:00Z', 'escort', 'marshal', '50000000-0000-4000-8000-000000000001', 'underway', NULL, NULL);

INSERT INTO Missions (Id, SectorId, ClientId, KindId, Title, Brief, Hazard, Fee, Deadline, RequiredCertId, BookedBy, AssignedSquadronId, StatusId, Notes, Settlement)
VALUES ('80000000-0000-4000-8000-000000000004', 'anvil', '10000000-0000-4000-8000-000000000004', 'courier', 'Medical courier run to Vellum station', 'Cold-chain plasma, time critical', 1, NUMERIC '3000', TIMESTAMP '2026-09-25T18:00:00Z', NULL, 'booking', '50000000-0000-4000-8000-000000000002', 'on_hold', 'Hold: solar weather advisory', NULL);

INSERT INTO Missions (Id, SectorId, ClientId, KindId, Title, Brief, Hazard, Fee, Deadline, RequiredCertId, BookedBy, AssignedSquadronId, StatusId, Notes, Settlement)
VALUES ('80000000-0000-4000-8000-000000000005', 'anvil', '10000000-0000-4000-8000-000000000001', 'salvage', 'Recover the Lantern cargo pod', 'Radioactive cargo pod adrift after the Lantern incident', 5, NUMERIC '40000', TIMESTAMP '2026-08-28T12:00:00Z', 'hazmat', 'governor', '50000000-0000-4000-8000-000000000001', 'completed', NULL, NUMERIC '38500');

INSERT INTO Missions (Id, SectorId, ClientId, KindId, Title, Brief, Hazard, Fee, Deadline, RequiredCertId, BookedBy, AssignedSquadronId, StatusId, Notes, Settlement)
VALUES ('80000000-0000-4000-8000-000000000006', 'anvil', '10000000-0000-4000-8000-000000000001', 'salvage', 'Tow the stalled tug Mule Two', 'Routine tow; drive failed short of the dock', 1, NUMERIC '2000', TIMESTAMP '2026-08-15T12:00:00Z', NULL, 'dispatcher', '50000000-0000-4000-8000-000000000002', 'failed', 'Failed: solar_weather', NULL);

INSERT INTO Missions (Id, SectorId, ClientId, KindId, Title, Brief, Hazard, Fee, Deadline, RequiredCertId, BookedBy, AssignedSquadronId, StatusId, Notes, Settlement)
VALUES ('80000000-0000-4000-8000-000000000007', 'anvil', '10000000-0000-4000-8000-000000000003', 'escort', 'Escort the bullion transfer', 'Relay station bullion run; client withdrew', 3, NUMERIC '12000', TIMESTAMP '2026-09-18T12:00:00Z', 'escort', 'booking', NULL, 'stood_down', NULL, NULL);

INSERT INTO Missions (Id, SectorId, ClientId, KindId, Title, Brief, Hazard, Fee, Deadline, RequiredCertId, BookedBy, AssignedSquadronId, StatusId, Notes, Settlement)
VALUES ('80000000-0000-4000-8000-000000000008', 'anvil', '10000000-0000-4000-8000-000000000001', 'courier', 'Quarantine relief courier', 'Supplies for the quarantine bay crew; nobody has claimed it', 2, NUMERIC '6000', TIMESTAMP '2026-08-20T12:00:00Z', NULL, 'booking', NULL, 'open', NULL, NULL);

INSERT INTO Missions (Id, SectorId, ClientId, KindId, Title, Brief, Hazard, Fee, Deadline, RequiredCertId, BookedBy, AssignedSquadronId, StatusId, Notes, Settlement)
VALUES ('80000000-0000-4000-8000-000000000009', 'bastion', '10000000-0000-4000-8000-000000000003', 'rescue', 'Bastion relay beacon repair', 'Beacon crew stranded on the relay mast', 2, NUMERIC '5000', TIMESTAMP '2026-09-22T12:00:00Z', NULL, 'dispatcher', NULL, 'open', NULL, NULL);

INSERT INTO Missions (Id, SectorId, ClientId, KindId, Title, Brief, Hazard, Fee, Deadline, RequiredCertId, BookedBy, AssignedSquadronId, StatusId, Notes, Settlement)
VALUES ('80000000-0000-4000-8000-000000000010', 'bastion', '10000000-0000-4000-8000-000000000003', 'escort', 'Portcullis patrol escort', 'Weekly patrol escort along the trade lane', 4, NUMERIC '20000', TIMESTAMP '2026-09-28T12:00:00Z', 'escort', 'governor', '50000000-0000-4000-8000-000000000003', 'claimed', NULL, NULL);

INSERT INTO Missions (Id, SectorId, ClientId, KindId, Title, Brief, Hazard, Fee, Deadline, RequiredCertId, BookedBy, AssignedSquadronId, StatusId, Notes, Settlement)
VALUES ('80000000-0000-4000-8000-000000000011', 'cinder', '10000000-0000-4000-8000-000000000002', 'salvage', 'Cinder Verge salvage sweep', 'Sweep of the burned-out yard for survey instruments', 5, NUMERIC '60000', TIMESTAMP '2026-08-01T12:00:00Z', 'hazmat', 'governor', '50000000-0000-4000-8000-000000000004', 'completed', NULL, NUMERIC '55000');

INSERT INTO Sorties (Id, MissionId, ShipId, PilotUserId, LaunchedAt, ReturnedAt, Debrief)
VALUES ('90000000-0000-4000-8000-000000000001', '80000000-0000-4000-8000-000000000003', '70000000-0000-4000-8000-000000000001', 'lead', TIMESTAMP '2026-09-02T06:00:00Z', NULL, NULL);

INSERT INTO Sorties (Id, MissionId, ShipId, PilotUserId, LaunchedAt, ReturnedAt, Debrief)
VALUES ('90000000-0000-4000-8000-000000000002', '80000000-0000-4000-8000-000000000005', '70000000-0000-4000-8000-000000000002', 'veteran', TIMESTAMP '2026-08-26T06:00:00Z', TIMESTAMP '2026-08-27T20:00:00Z', 'Pod recovered; hull dosimetry nominal');

INSERT INTO Sorties (Id, MissionId, ShipId, PilotUserId, LaunchedAt, ReturnedAt, Debrief)
VALUES ('90000000-0000-4000-8000-000000000003', '80000000-0000-4000-8000-000000000004', '70000000-0000-4000-8000-000000000004', 'pilot', TIMESTAMP '2026-09-03T10:00:00Z', NULL, NULL);

INSERT INTO Sorties (Id, MissionId, ShipId, PilotUserId, LaunchedAt, ReturnedAt, Debrief)
VALUES ('90000000-0000-4000-8000-000000000004', '80000000-0000-4000-8000-000000000006', '70000000-0000-4000-8000-000000000002', 'cadet', TIMESTAMP '2026-08-14T08:00:00Z', TIMESTAMP '2026-08-14T15:00:00Z', 'Solar weather forced a return');

INSERT INTO SortieExpenses (Id, SortieId, Category, Amount, Note)
VALUES ('91000000-0000-4000-8000-000000000001', '90000000-0000-4000-8000-000000000001', 'fuel', NUMERIC '1200', 'Reaction mass, outbound leg');

INSERT INTO SortieExpenses (Id, SortieId, Category, Amount, Note)
VALUES ('91000000-0000-4000-8000-000000000002', '90000000-0000-4000-8000-000000000001', 'medical', NUMERIC '300', NULL);

INSERT INTO SortieExpenses (Id, SortieId, Category, Amount, Note)
VALUES ('91000000-0000-4000-8000-000000000003', '90000000-0000-4000-8000-000000000002', 'tow_gear', NUMERIC '1500', 'Grapple replacement');

INSERT INTO SortieExpenses (Id, SortieId, Category, Amount, Note)
VALUES ('91000000-0000-4000-8000-000000000004', '90000000-0000-4000-8000-000000000003', 'fuel', NUMERIC '400', NULL);

INSERT INTO Refits (Id, ShipId, StatusId, Estimate, InspectedAt, OpenedBy, Notes)
VALUES ('a0000000-0000-4000-8000-000000000001', '70000000-0000-4000-8000-000000000003', 'docked', NULL, NULL, 'engineer', 'Lantern towed in after the cargo pod incident');

INSERT INTO Refits (Id, ShipId, StatusId, Estimate, InspectedAt, OpenedBy, Notes)
VALUES ('a0000000-0000-4000-8000-000000000002', '70000000-0000-4000-8000-000000000002', 'inspected', NUMERIC '12000', TIMESTAMP '2026-09-01T09:00:00Z', 'engineer', NULL);

INSERT INTO Refits (Id, ShipId, StatusId, Estimate, InspectedAt, OpenedBy, Notes)
VALUES ('a0000000-0000-4000-8000-000000000003', '70000000-0000-4000-8000-000000000004', 'in_refit', NUMERIC '8000', TIMESTAMP '2026-08-20T09:00:00Z', 'dock', NULL);

INSERT INTO Refits (Id, ShipId, StatusId, Estimate, InspectedAt, OpenedBy, Notes)
VALUES ('a0000000-0000-4000-8000-000000000004', '70000000-0000-4000-8000-000000000005', 'scrapped', NUMERIC '90000', TIMESTAMP '2026-07-10T09:00:00Z', 'marshal', 'Hull fatigue beyond economic repair');

INSERT INTO Refits (Id, ShipId, StatusId, Estimate, InspectedAt, OpenedBy, Notes)
VALUES ('a0000000-0000-4000-8000-000000000005', '70000000-0000-4000-8000-000000000006', 'flight_test', NUMERIC '5500', TIMESTAMP '2026-08-25T09:00:00Z', 'governor', NULL);

INSERT INTO Refits (Id, ShipId, StatusId, Estimate, InspectedAt, OpenedBy, Notes)
VALUES ('a0000000-0000-4000-8000-000000000006', '70000000-0000-4000-8000-000000000007', 'cleared', NUMERIC '3000', TIMESTAMP '2026-07-01T09:00:00Z', 'governor', NULL);

INSERT INTO RefitTasks (Id, TaskNumber, Instructions, Done, Notes)
VALUES ('a0000000-0000-4000-8000-000000000002', 1, 'Replace coolant manifold', FALSE, NULL);

INSERT INTO RefitTasks (Id, TaskNumber, Instructions, Done, Notes)
VALUES ('a0000000-0000-4000-8000-000000000003', 1, 'Reline thruster bells', TRUE, NULL);

INSERT INTO RefitTasks (Id, TaskNumber, Instructions, Done, Notes)
VALUES ('a0000000-0000-4000-8000-000000000003', 2, 'Recertify hull seals', FALSE, NULL);

INSERT INTO RefitTasks (Id, TaskNumber, Instructions, Done, Notes)
VALUES ('a0000000-0000-4000-8000-000000000005', 1, 'Calibrate nav array', TRUE, NULL);

INSERT INTO Consignments (Id, SectorId, ClientId, BondCode, Description, Mass, ExpiresOn, ReleasedAt)
VALUES ('b0000000-0000-4000-8000-000000000001', 'anvil', '10000000-0000-4000-8000-000000000001', 'BND-ANV-0001', 'Sealed cargo pod, medical supplies', 420.5, DATE '2026-12-01', NULL);

INSERT INTO Consignments (Id, SectorId, ClientId, BondCode, Description, Mass, ExpiresOn, ReleasedAt)
VALUES ('b0000000-0000-4000-8000-000000000002', 'anvil', '10000000-0000-4000-8000-000000000002', 'BND-ANV-0002', 'Survey drones, crated', 85.0, DATE '2026-08-01', NULL);

INSERT INTO Consignments (Id, SectorId, ClientId, BondCode, Description, Mass, ExpiresOn, ReleasedAt)
VALUES ('b0000000-0000-4000-8000-000000000003', 'anvil', '10000000-0000-4000-8000-000000000001', 'BND-ANV-0003', 'Bullion crates', 1200.0, DATE '2027-01-15', TIMESTAMP '2026-08-30T14:00:00Z');

INSERT INTO Consignments (Id, SectorId, ClientId, BondCode, Description, Mass, ExpiresOn, ReleasedAt)
VALUES ('b0000000-0000-4000-8000-000000000004', 'bastion', '10000000-0000-4000-8000-000000000003', 'BND-BAS-0001', 'Relay spares', 60.0, DATE '2026-11-11', NULL);

INSERT INTO DroidReports (Id, SectorId, ShipId, Subsystem, Reading, RecordedAt)
VALUES ('c0000000-0000-4000-8000-000000000001', 'anvil', '70000000-0000-4000-8000-000000000001', 'hull', 0.42, TIMESTAMP '2026-09-01T10:00:00Z');

INSERT INTO DroidReports (Id, SectorId, ShipId, Subsystem, Reading, RecordedAt)
VALUES ('c0000000-0000-4000-8000-000000000002', 'anvil', '70000000-0000-4000-8000-000000000001', 'hull', 0.61, TIMESTAMP '2026-09-01T11:00:00Z');

INSERT INTO DroidReports (Id, SectorId, ShipId, Subsystem, Reading, RecordedAt)
VALUES ('c0000000-0000-4000-8000-000000000003', 'anvil', '70000000-0000-4000-8000-000000000001', 'reactor', 0.20, TIMESTAMP '2026-09-01T11:00:00Z');

INSERT INTO DroidReports (Id, SectorId, ShipId, Subsystem, Reading, RecordedAt)
VALUES ('c0000000-0000-4000-8000-000000000004', 'anvil', '70000000-0000-4000-8000-000000000002', 'hull', 0.90, TIMESTAMP '2026-09-01T11:30:00Z');

INSERT INTO DroidReports (Id, SectorId, ShipId, Subsystem, Reading, RecordedAt)
VALUES ('c0000000-0000-4000-8000-000000000005', 'bastion', '70000000-0000-4000-8000-000000000006', 'hull', 0.30, TIMESTAMP '2026-09-01T11:00:00Z');

INSERT INTO DistressCalls (Id, SectorId, Summary, Severity, CallerContact, Transcript, CaseNumber, FiledBy)
VALUES ('d0000000-0000-4000-8000-000000000001', 'anvil', 'Beacon lost near Anvil Reach', 3, 'cleo@halvard.example', NULL, 'DC-SEED-0001', 'client');

INSERT INTO DistressCalls (Id, SectorId, Summary, Severity, CallerContact, Transcript, CaseNumber, FiledBy)
VALUES ('d0000000-0000-4000-8000-000000000002', 'anvil', 'Debris field reported on the approach lane', 1, NULL, 'Cadet on watch logged a debris field', 'DC-SEED-0002', 'cadet');

INSERT INTO DistressCalls (Id, SectorId, Summary, Severity, CallerContact, Transcript, CaseNumber, FiledBy)
VALUES ('d0000000-0000-4000-8000-000000000003', 'bastion', 'Relay mast power fluctuation', 2, 'desk@bastion-relay.example', NULL, 'DC-SEED-0003', 'dispatcher');
