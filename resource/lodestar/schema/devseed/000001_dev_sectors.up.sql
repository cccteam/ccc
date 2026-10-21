-- The three sectors of the demo world (design plan §2): Anvil is home, Bastion is where a
-- role stops at a border, Cinder is the burned-out frontier most personas cannot see.
INSERT INTO Sectors (Id, Name, Region, Established) VALUES ('anvil', 'Anvil', 'Inner frontier', DATE '2201-03-14');
-- Bastion admits its people through its directories only: a password does not reach it.
INSERT INTO Sectors (Id, Name, Region, Established, DirectoryOnly) VALUES ('bastion', 'Bastion', 'Relay corridor', DATE '2207-09-01', TRUE);
INSERT INTO Sectors (Id, Name, Region, Established) VALUES ('cinder', 'Cinder', 'Outer frontier', DATE '2214-06-30');
-- Every sector declares the fleet's Google Workspace, named example by both auths'
-- registrations (APP_CREW_OIDC_CONNECTION, APP_MEMBERS_OIDC_CONNECTION): the navigator,
-- who signs in through it, and the client, whose company directory it stands in for,
-- reach the sectors they hold a role in.
INSERT INTO SectorDirectories (Id, SectorId, Method, Connection) VALUES ('e0000000-0000-4000-8000-000000000001', 'anvil', 'google', 'example');
INSERT INTO SectorDirectories (Id, SectorId, Method, Connection) VALUES ('e0000000-0000-4000-8000-000000000002', 'bastion', 'google', 'example');
INSERT INTO SectorDirectories (Id, SectorId, Method, Connection) VALUES ('e0000000-0000-4000-8000-000000000003', 'cinder', 'google', 'example');
