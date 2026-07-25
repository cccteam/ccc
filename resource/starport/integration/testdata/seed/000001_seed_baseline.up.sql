INSERT INTO DockingBays (Id, Name, DeckLevel, MaxTonnage)
VALUES ('5f2d1c3b-9a8e-4d7f-8b6a-1c2d3e4f5a6b', 'Bay Alpha', 3, 50000);

INSERT INTO Ships (Id, RegistryCode, Name, DockingBayId, CargoValue)
VALUES ('0b9e8d7c-6f5a-4b3c-9d2e-1f0a9b8c7d6e', 'SSV-1001', 'Vanta', '5f2d1c3b-9a8e-4d7f-8b6a-1c2d3e4f5a6b', 750000);

INSERT INTO Ships (Id, RegistryCode, Name, DockingBayId, CargoValue)
VALUES ('7a6b5c4d-3e2f-4a1b-8c9d-0e1f2a3b4c5d', 'SSV-1002', 'Comost', NULL, 125000);

INSERT INTO CrewMembers (Id, ShipId, Name, Rank, ClearanceLevel, MedicalNotes)
VALUES ('3c2b1a09-8d7e-4f6a-9b5c-4d3e2f1a0b9c', '0b9e8d7c-6f5a-4b3c-9d2e-1f0a9b8c7d6e', 'Ilyan Reeve', 'Navigator', 3, 'Cleared for extended duty');

INSERT INTO CargoManifests (ShipId, LineNumber, Details, Quantity, DeclaredValue)
VALUES ('0b9e8d7c-6f5a-4b3c-9d2e-1f0a9b8c7d6e', 1, 'Hull plating', 120, 90000);
