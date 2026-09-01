INSERT INTO Waystations (Id, Name, OrbitBand, Commissioned)
VALUES ('ws-alpha', 'Meridian Alpha', 'LEO-1', DATE '2031-04-12');

INSERT INTO Waystations (Id, Name, OrbitBand, Commissioned)
VALUES ('ws-beta', 'Beta Anchorage', 'LEO-2', DATE '2033-09-01');

INSERT INTO Waystations (Id, Name, OrbitBand, Commissioned)
VALUES ('ws-ceres', 'Ceres Relay', 'Belt', DATE '2035-01-20');

INSERT INTO Suppliers (Id, Name, ContactName, ContactEmail, Active)
VALUES ('10000000-0000-4000-8000-000000000001', 'Helion Supply Co', 'Vera Lin', 'procurement@helion.example', TRUE);

INSERT INTO Suppliers (Id, Name, ContactName, ContactEmail, Active)
VALUES ('10000000-0000-4000-8000-000000000002', 'Kuiper Provisioning', 'Marta Ito', 'sales@kuiper.example', TRUE);

INSERT INTO Suppliers (Id, Name, ContactName, ContactEmail, Active)
VALUES ('10000000-0000-4000-8000-000000000003', 'Redline Salvage', 'Dex Marlow', 'dex@redline.example', FALSE);

INSERT INTO CatalogItems (Id, Sku, Name, CategoryId, UnitCost, Hazardous)
VALUES ('20000000-0000-4000-8000-000000000001','CO2-CART','CO2 Scrubber Cartridge','consumable', NUMERIC '120.50',FALSE);

INSERT INTO CatalogItems (Id, Sku, Name, CategoryId, UnitCost, Hazardous)
VALUES ('20000000-0000-4000-8000-000000000002','CLNT-PUMP','Coolant Pump','spare_part', NUMERIC '890.00',FALSE);

INSERT INTO CatalogItems (Id, Sku, Name, CategoryId, UnitCost, Hazardous)
VALUES ('20000000-0000-4000-8000-000000000003','PLSM-TRCH','Plasma Torch','tool', NUMERIC '445.25',TRUE);

INSERT INTO CatalogItems (Id, Sku, Name, CategoryId, UnitCost, Hazardous)
VALUES ('20000000-0000-4000-8000-000000000004','HYDRZ-CAN','Hydrazine Canister','hazmat', NUMERIC '310.00',TRUE);

INSERT INTO StaffMembers (Id, UserId, DisplayName, ApprovalLimit, HomeWaystationId)
VALUES ('30000000-0000-4000-8000-000000000001','commander','Cmdr. A. Reyes', NUMERIC '100000',NULL);

INSERT INTO StaffMembers (Id, UserId, DisplayName, ApprovalLimit, HomeWaystationId)
VALUES ('30000000-0000-4000-8000-000000000002','chief-alpha','Chief O. Danner', NUMERIC '2500','ws-alpha');

INSERT INTO StaffMembers (Id, UserId, DisplayName, ApprovalLimit, HomeWaystationId)
VALUES ('30000000-0000-4000-8000-000000000003','procurement-chen','P. Chen', NUMERIC '5000','ws-alpha');

INSERT INTO StaffMembers (Id, UserId, DisplayName, ApprovalLimit, HomeWaystationId)
VALUES ('30000000-0000-4000-8000-000000000004','quartermaster-idris','Q. Idris', NUMERIC '500','ws-alpha');

INSERT INTO StaffMembers (Id, UserId, DisplayName, ApprovalLimit, HomeWaystationId)
VALUES ('30000000-0000-4000-8000-000000000005','tech-rivera','T. Rivera', NUMERIC '0','ws-alpha');

INSERT INTO StaffMembers (Id, UserId, DisplayName, ApprovalLimit, HomeWaystationId)
VALUES ('30000000-0000-4000-8000-000000000006','foreman-okafor','F. Okafor', NUMERIC '0','ws-alpha');

INSERT INTO StaffMembers (Id, UserId, DisplayName, ApprovalLimit, HomeWaystationId)
VALUES ('30000000-0000-4000-8000-000000000007','auditor-voss','A. Voss', NUMERIC '0',NULL);

INSERT INTO Modules (Id, WaystationId, Name, Zone, PressureRated)
VALUES ('40000000-0000-4000-8000-000000000001', 'ws-alpha', 'Habitat Ring', 'habitat', TRUE);

INSERT INTO Modules (Id, WaystationId, Name, Zone, PressureRated)
VALUES ('40000000-0000-4000-8000-000000000002', 'ws-alpha', 'Cargo Spine', 'cargo', FALSE);

INSERT INTO Modules (Id, WaystationId, Name, Zone, PressureRated)
VALUES ('40000000-0000-4000-8000-000000000003', 'ws-alpha', 'Reactor Core', 'reactor', TRUE);

INSERT INTO Modules (Id, WaystationId, Name, Zone, PressureRated)
VALUES ('40000000-0000-4000-8000-000000000004', 'ws-beta', 'Beta Habitat Loop', 'habitat', TRUE);

INSERT INTO Modules (Id, WaystationId, Name, Zone, PressureRated)
VALUES ('40000000-0000-4000-8000-000000000005', 'ws-beta', 'Beta Reactor Annex', 'reactor', TRUE);

INSERT INTO Modules (Id, WaystationId, Name, Zone, PressureRated)
VALUES ('40000000-0000-4000-8000-000000000006', 'ws-ceres', 'Relay Mast', 'cargo', FALSE);

INSERT INTO Facilities (Id, ModuleId, Name, Kind)
VALUES ('50000000-0000-4000-8000-000000000001', '40000000-0000-4000-8000-000000000001', 'Hydroponics Bay', 'lab');

INSERT INTO Facilities (Id, ModuleId, Name, Kind)
VALUES ('50000000-0000-4000-8000-000000000002', '40000000-0000-4000-8000-000000000001', 'Galley', 'crew');

INSERT INTO Facilities (Id, ModuleId, Name, Kind)
VALUES ('50000000-0000-4000-8000-000000000003', '40000000-0000-4000-8000-000000000002', 'Dock Control', 'ops');

INSERT INTO Facilities (Id, ModuleId, Name, Kind)
VALUES ('50000000-0000-4000-8000-000000000004', '40000000-0000-4000-8000-000000000002', 'Cargo Hold A', 'storage');

INSERT INTO Facilities (Id, ModuleId, Name, Kind)
VALUES ('50000000-0000-4000-8000-000000000005', '40000000-0000-4000-8000-000000000003', 'Reactor Control', 'ops');

INSERT INTO Facilities (Id, ModuleId, Name, Kind)
VALUES ('50000000-0000-4000-8000-000000000006', '40000000-0000-4000-8000-000000000004', 'Beta Commons', 'crew');

INSERT INTO Facilities (Id, ModuleId, Name, Kind)
VALUES ('50000000-0000-4000-8000-000000000007', '40000000-0000-4000-8000-000000000005', 'Beta Reactor Room', 'ops');

INSERT INTO Assets (Id, FacilityId, SerialNumber, Name, CommissionedOn, LastServicedAt)
VALUES ('60000000-0000-4000-8000-000000000001', '50000000-0000-4000-8000-000000000001', 'AR-9-0001', 'Atmos Recycler AR-9', DATE '2032-06-15', NULL);

INSERT INTO Assets (Id, FacilityId, SerialNumber, Name, CommissionedOn, LastServicedAt)
VALUES ('60000000-0000-4000-8000-000000000002', '50000000-0000-4000-8000-000000000002', 'GX-2-0044', 'Galley Oven GX-2', DATE '2032-08-01', NULL);

INSERT INTO Assets (Id, FacilityId, SerialNumber, Name, CommissionedOn, LastServicedAt)
VALUES ('60000000-0000-4000-8000-000000000003', '50000000-0000-4000-8000-000000000003', 'CC-7-0100', 'Crane Controller CC-7', DATE '2033-01-10', NULL);

INSERT INTO Assets (Id, FacilityId, SerialNumber, Name, CommissionedOn, LastServicedAt)
VALUES ('60000000-0000-4000-8000-000000000004', '50000000-0000-4000-8000-000000000004', 'PL-4-0007', 'Pallet Lift PL-4', DATE '2033-02-14', NULL);

INSERT INTO Assets (Id, FacilityId, SerialNumber, Name, CommissionedOn, LastServicedAt)
VALUES ('60000000-0000-4000-8000-000000000005', '50000000-0000-4000-8000-000000000005', 'CM-1-0002', 'Coolant Manifold CM-1', DATE '2031-12-25', NULL);

INSERT INTO Assets (Id, FacilityId, SerialNumber, Name, CommissionedOn, LastServicedAt)
VALUES ('60000000-0000-4000-8000-000000000006', '50000000-0000-4000-8000-000000000006', 'AH-3-0090', 'Beta Air Handler AH-3', DATE '2034-03-03', NULL);

INSERT INTO Assets (Id, FacilityId, SerialNumber, Name, CommissionedOn, LastServicedAt)
VALUES ('60000000-0000-4000-8000-000000000007', '50000000-0000-4000-8000-000000000007', 'CL-2-0011', 'Beta Coolant Loop CL-2', DATE '2034-04-04', NULL);

INSERT INTO Teams (Id, WaystationId, Name, Specialty)
VALUES ('70000000-0000-4000-8000-000000000001', 'ws-alpha', 'Alpha Mechanical', 'mechanical');

INSERT INTO Teams (Id, WaystationId, Name, Specialty)
VALUES ('70000000-0000-4000-8000-000000000002', 'ws-alpha', 'Alpha Life Support', 'environmental');

INSERT INTO Teams (Id, WaystationId, Name, Specialty)
VALUES ('70000000-0000-4000-8000-000000000003', 'ws-beta', 'Beta Maintenance', 'general');

INSERT INTO TeamMemberships (TeamId, UserId)
VALUES ('70000000-0000-4000-8000-000000000001', 'tech-rivera');

INSERT INTO TeamMemberships (TeamId, UserId)
VALUES ('70000000-0000-4000-8000-000000000002', 'chief-alpha');

INSERT INTO TeamMemberships (TeamId, UserId)
VALUES ('70000000-0000-4000-8000-000000000003', 'tech-rivera');

INSERT INTO WorkOrders (Id, WaystationId, AssetId, Title, Summary, Priority, StatusId, CreatedBy, AssignedTeamId, DueAt)
VALUES ('80000000-0000-4000-8000-000000000001', 'ws-alpha', '60000000-0000-4000-8000-000000000001', 'Replace scrubber cartridge bank', 'Cartridges past 80% saturation', 2, 'in_progress', 'chief-alpha', '70000000-0000-4000-8000-000000000001', TIMESTAMP '2026-09-05T12:00:00Z');

INSERT INTO WorkOrders (Id, WaystationId, AssetId, Title, Summary, Priority, StatusId, CreatedBy, AssignedTeamId, DueAt)
VALUES ('80000000-0000-4000-8000-000000000002', 'ws-alpha', '60000000-0000-4000-8000-000000000002', 'Oven thermal sensor drift', NULL, 1, 'scheduled', 'foreman-okafor', '70000000-0000-4000-8000-000000000002', TIMESTAMP '2026-09-10T09:00:00Z');

INSERT INTO WorkOrders (Id, WaystationId, AssetId, Title, Summary, Priority, StatusId, CreatedBy, AssignedTeamId, DueAt)
VALUES ('80000000-0000-4000-8000-000000000003', 'ws-alpha', '60000000-0000-4000-8000-000000000005', 'Coolant manifold inspection', 'Quarterly reactor loop check', 4, 'draft', 'foreman-okafor', NULL, NULL);

INSERT INTO WorkOrders (Id, WaystationId, AssetId, Title, Summary, Priority, StatusId, CreatedBy, AssignedTeamId, DueAt)
VALUES ('80000000-0000-4000-8000-000000000004', 'ws-alpha', '60000000-0000-4000-8000-000000000003', 'Crane controller firmware update', NULL, 3, 'completed', 'chief-alpha', '70000000-0000-4000-8000-000000000001', TIMESTAMP '2026-08-20T08:00:00Z');

INSERT INTO WorkOrders (Id, WaystationId, AssetId, Title, Summary, Priority, StatusId, CreatedBy, AssignedTeamId, DueAt)
VALUES ('80000000-0000-4000-8000-000000000005', 'ws-beta', '60000000-0000-4000-8000-000000000006', 'Beta air filter swap', NULL, 2, 'scheduled', 'commander', '70000000-0000-4000-8000-000000000003', TIMESTAMP '2026-09-08T10:00:00Z');

INSERT INTO WorkOrderTasks (Id, TaskNumber, Instructions, Done)
VALUES ('80000000-0000-4000-8000-000000000001', 1, 'Depressurize hydroponics bay', TRUE);

INSERT INTO WorkOrderTasks (Id, TaskNumber, Instructions, Done)
VALUES ('80000000-0000-4000-8000-000000000001', 2, 'Swap cartridge bank', FALSE);

INSERT INTO WorkOrderTasks (Id, TaskNumber, Instructions, Done)
VALUES ('80000000-0000-4000-8000-000000000002', 1, 'Order replacement sensor', FALSE);

INSERT INTO WorkOrderTasks (Id, TaskNumber, Instructions, Done)
VALUES ('80000000-0000-4000-8000-000000000004', 1, 'Flash firmware image', TRUE);

INSERT INTO WorkOrderTasks (Id, TaskNumber, Instructions, Done)
VALUES ('80000000-0000-4000-8000-000000000004', 2, 'Verify crane telemetry', TRUE);

INSERT INTO Requisitions (Id, WaystationId, RequestedBy, Justification, NeededBy, TotalCost, StatusId)
VALUES ('90000000-0000-4000-8000-000000000001','ws-alpha','foreman-okafor','Scrubber cartridge refresh',DATE '2026-09-15', NUMERIC '361.50','submitted');

INSERT INTO Requisitions (Id, WaystationId, RequestedBy, Justification, NeededBy, TotalCost, StatusId)
VALUES ('90000000-0000-4000-8000-000000000002','ws-alpha','foreman-okafor','Backup coolant pump',DATE '2026-09-20', NUMERIC '0','draft');

INSERT INTO Requisitions (Id, WaystationId, RequestedBy, Justification, NeededBy, TotalCost, StatusId)
VALUES ('90000000-0000-4000-8000-000000000003','ws-alpha','quartermaster-idris','Coolant pump fleet overhaul',DATE '2026-10-01', NUMERIC '7120.00','submitted');

INSERT INTO Requisitions (Id, WaystationId, RequestedBy, Justification, NeededBy, TotalCost, StatusId)
VALUES ('90000000-0000-4000-8000-000000000004','ws-alpha','foreman-okafor','Plasma torch for hull work',DATE '2026-09-01', NUMERIC '445.25','approved');

INSERT INTO Requisitions (Id, WaystationId, RequestedBy, Justification, NeededBy, TotalCost, StatusId)
VALUES ('90000000-0000-4000-8000-000000000005','ws-beta','commander','Beta scrubber topup',DATE '2026-09-30', NUMERIC '241.00','submitted');

INSERT INTO RequisitionLines (Id, LineNumber, CatalogItemId, Quantity, UnitCostSnapshot)
VALUES ('90000000-0000-4000-8000-000000000001',1,'20000000-0000-4000-8000-000000000001',3, NUMERIC '120.50');

INSERT INTO RequisitionLines (Id, LineNumber, CatalogItemId, Quantity, UnitCostSnapshot)
VALUES ('90000000-0000-4000-8000-000000000002',1,'20000000-0000-4000-8000-000000000002',1, NUMERIC '890.00');

INSERT INTO RequisitionLines (Id, LineNumber, CatalogItemId, Quantity, UnitCostSnapshot)
VALUES ('90000000-0000-4000-8000-000000000003',1,'20000000-0000-4000-8000-000000000002',8, NUMERIC '890.00');

INSERT INTO RequisitionLines (Id, LineNumber, CatalogItemId, Quantity, UnitCostSnapshot)
VALUES ('90000000-0000-4000-8000-000000000004',1,'20000000-0000-4000-8000-000000000003',1, NUMERIC '445.25');

INSERT INTO RequisitionLines (Id, LineNumber, CatalogItemId, Quantity, UnitCostSnapshot)
VALUES ('90000000-0000-4000-8000-000000000005',1,'20000000-0000-4000-8000-000000000001',2, NUMERIC '120.50');

INSERT INTO InventoryLots (Id, WaystationId, CatalogItemId, Quantity, ExpiresOn, BinLocation)
VALUES ('a0000000-0000-4000-8000-000000000001', 'ws-alpha', '20000000-0000-4000-8000-000000000001', 42, DATE '2027-03-01', 'A-01');

INSERT INTO InventoryLots (Id, WaystationId, CatalogItemId, Quantity, ExpiresOn, BinLocation)
VALUES ('a0000000-0000-4000-8000-000000000002', 'ws-alpha', '20000000-0000-4000-8000-000000000004', 6, DATE '2026-05-01', 'H-03');

INSERT INTO InventoryLots (Id, WaystationId, CatalogItemId, Quantity, ExpiresOn, BinLocation)
VALUES ('a0000000-0000-4000-8000-000000000003', 'ws-alpha', '20000000-0000-4000-8000-000000000002', 3, NULL, 'B-02');

INSERT INTO InventoryLots (Id, WaystationId, CatalogItemId, Quantity, ExpiresOn, BinLocation)
VALUES ('a0000000-0000-4000-8000-000000000004', 'ws-beta', '20000000-0000-4000-8000-000000000001', 10, DATE '2026-12-01', 'BA-1');

INSERT INTO Shipments (Id, WaystationId, SupplierId, ManifestCode, ArrivedAt)
VALUES ('b0000000-0000-4000-8000-000000000001', 'ws-alpha', '10000000-0000-4000-8000-000000000001', 'MC-1001', NULL);

INSERT INTO Shipments (Id, WaystationId, SupplierId, ManifestCode, ArrivedAt)
VALUES ('b0000000-0000-4000-8000-000000000002', 'ws-alpha', '10000000-0000-4000-8000-000000000002', 'MC-1002', TIMESTAMP '2026-08-15T14:30:00Z');

INSERT INTO Shipments (Id, WaystationId, SupplierId, ManifestCode, ArrivedAt)
VALUES ('b0000000-0000-4000-8000-000000000003', 'ws-beta', '10000000-0000-4000-8000-000000000001', 'MC-2001', NULL);

INSERT INTO IncidentReports (Id, WaystationId, Summary, Severity, ReporterContact, RawStatement, CaseNumber)
VALUES ('c0000000-0000-4000-8000-000000000001', 'ws-alpha', 'Coolant drip in reactor control', 3, 'rivera@ws-alpha.demo', NULL, 'IR-SEED-0001');

INSERT INTO IncidentReports (Id, WaystationId, Summary, Severity, ReporterContact, RawStatement, CaseNumber)
VALUES ('c0000000-0000-4000-8000-000000000002', 'ws-alpha', 'Galley oven tripped breaker', 1, 'okafor@ws-alpha.demo', 'Breaker reset twice before holding', 'IR-SEED-0002');

INSERT INTO IncidentReports (Id, WaystationId, Summary, Severity, ReporterContact, RawStatement, CaseNumber)
VALUES ('c0000000-0000-4000-8000-000000000003', 'ws-beta', 'Loose handrail in commons', 2, 'crew@ws-beta.demo', NULL, 'IR-SEED-0003');

INSERT INTO SensorReadings (Id, WaystationId, FacilityId, Metric, Reading, RecordedAt)
VALUES ('d0000000-0000-4000-8000-000000000001', 'ws-alpha', '50000000-0000-4000-8000-000000000001', 'o2_ppm', 209.5, TIMESTAMP '2026-08-31T10:00:00Z');

INSERT INTO SensorReadings (Id, WaystationId, FacilityId, Metric, Reading, RecordedAt)
VALUES ('d0000000-0000-4000-8000-000000000002', 'ws-alpha', '50000000-0000-4000-8000-000000000001', 'o2_ppm', 210.1, TIMESTAMP '2026-08-31T11:00:00Z');

INSERT INTO SensorReadings (Id, WaystationId, FacilityId, Metric, Reading, RecordedAt)
VALUES ('d0000000-0000-4000-8000-000000000003', 'ws-alpha', '50000000-0000-4000-8000-000000000005', 'coolant_temp', 88.4, TIMESTAMP '2026-08-31T10:30:00Z');

INSERT INTO SensorReadings (Id, WaystationId, FacilityId, Metric, Reading, RecordedAt)
VALUES ('d0000000-0000-4000-8000-000000000004', 'ws-alpha', '50000000-0000-4000-8000-000000000005', 'coolant_temp', 91.2, TIMESTAMP '2026-08-31T11:30:00Z');

INSERT INTO SensorReadings (Id, WaystationId, FacilityId, Metric, Reading, RecordedAt)
VALUES ('d0000000-0000-4000-8000-000000000005', 'ws-beta', '50000000-0000-4000-8000-000000000006', 'o2_ppm', 207.9, TIMESTAMP '2026-08-31T11:00:00Z');
