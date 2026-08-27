INSERT INTO DockingBays (Id, Name, DeckLevel, MaxTonnage)
VALUES ('1a1a1a1a-1111-4a1a-8a1a-1a1a1a1a1a1a', 'Bay Aurora', 1, 40000);

INSERT INTO DockingBays (Id, Name, DeckLevel, MaxTonnage)
VALUES ('2b2b2b2b-2222-4b2b-8b2b-2b2b2b2b2b2b', 'Bay Borealis', 2, 65000);

INSERT INTO DockingBays (Id, Name, DeckLevel, MaxTonnage)
VALUES ('3c3c3c3c-3333-4c3c-8c3c-3c3c3c3c3c3c', 'Bay Cinder', 5, 120000);

INSERT INTO Ships (Id, RegistryCode, Name, DockingBayId, CargoValue)
VALUES ('4d4d4d4d-4444-4d4d-8d4d-4d4d4d4d4d4d', 'SSV-3001', 'Meridian', '1a1a1a1a-1111-4a1a-8a1a-1a1a1a1a1a1a', 820000);

INSERT INTO Ships (Id, RegistryCode, Name, DockingBayId, CargoValue)
VALUES ('5e5e5e5e-5555-4e5e-8e5e-5e5e5e5e5e5e', 'SSV-3002', 'Longshore', '3c3c3c3c-3333-4c3c-8c3c-3c3c3c3c3c3c', 1450000);

INSERT INTO Ships (Id, RegistryCode, Name, DockingBayId, CargoValue)
VALUES ('6f6f6f6f-6666-4f6f-8f6f-6f6f6f6f6f6f', 'SSV-3003', 'Pale Harbor', NULL, 90000);

INSERT INTO CrewMembers (Id, ShipId, Name, Rank, ClearanceLevel, MedicalNotes)
VALUES ('7a7a7a7a-7777-4a7a-8a7a-7a7a7a7a7a7a', '4d4d4d4d-4444-4d4d-8d4d-4d4d4d4d4d4d', 'Sable Winters', 'Captain', 5, NULL);

INSERT INTO CrewMembers (Id, ShipId, Name, Rank, ClearanceLevel, MedicalNotes)
VALUES ('8b8b8b8b-8888-4b8b-8b8b-8b8b8b8b8b8b', '4d4d4d4d-4444-4d4d-8d4d-4d4d4d4d4d4d', 'Orrin Vale', 'Quartermaster', 2, 'Allergic to coolant fumes');

INSERT INTO CrewMembers (Id, ShipId, Name, Rank, ClearanceLevel, MedicalNotes)
VALUES ('9c9c9c9c-9999-4c9c-8c9c-9c9c9c9c9c9c', '5e5e5e5e-5555-4e5e-8e5e-5e5e5e5e5e5e', 'Petra Lune', 'Engineer', 3, NULL);

INSERT INTO CargoManifests (ShipId, LineNumber, Details, Quantity, DeclaredValue)
VALUES ('4d4d4d4d-4444-4d4d-8d4d-4d4d4d4d4d4d', 1, 'Reactor shielding', 40, 300000);

INSERT INTO CargoManifests (ShipId, LineNumber, Details, Quantity, DeclaredValue)
VALUES ('4d4d4d4d-4444-4d4d-8d4d-4d4d4d4d4d4d', 2, 'Hydroponic trays', 220, 66000);

INSERT INTO CargoManifests (ShipId, LineNumber, Details, Quantity, DeclaredValue)
VALUES ('5e5e5e5e-5555-4e5e-8e5e-5e5e5e5e5e5e', 1, 'Bulk regolith', 5000, 125000);

INSERT INTO SupplyCrates (Id, Label, Quantity, Priority, Status, Barcode, Notes, InspectorBadge, AssignedShipId)
VALUES ('a1b2c3d4-aaaa-4aaa-8aaa-a1b2c3d4aaaa', 'Coolant cells', 40, 1, 'provisioned', 'BC-4401', NULL, NULL, '4d4d4d4d-4444-4d4d-8d4d-4d4d4d4d4d4d');

INSERT INTO SupplyCrates (Id, Label, Quantity, Priority, Status, Barcode, Notes, InspectorBadge, AssignedShipId)
VALUES ('b2c3d4e5-bbbb-4bbb-8bbb-b2c3d4e5bbbb', 'Emergency rations', 300, 2, 'inspected', 'BC-4402', 'Rotate stock quarterly', 'INS-7', NULL);

INSERT INTO Berths (Id, Designation, SizeClass, Occupied)
VALUES ('c3d4e5f6-cccc-4ccc-8ccc-c3d4e5f6cccc', 'Berth A-1', 2, FALSE);

INSERT INTO Berths (Id, Designation, SizeClass, Occupied)
VALUES ('d4e5f6a7-dddd-4ddd-8ddd-d4e5f6a7dddd', 'Berth A-2', 3, TRUE);

INSERT INTO Berths (Id, Designation, SizeClass, Occupied)
VALUES ('e5f6a7b8-eeee-4eee-8eee-e5f6a7b8eeee', 'Berth C-9', 5, FALSE);
