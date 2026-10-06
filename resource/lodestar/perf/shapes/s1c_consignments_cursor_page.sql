-- A keyset cursor page under bare-column tenancy: the second page of Consignments ordered by a nullable column.
SELECT Id, SectorId, ClientId, BondCode, Description, Mass, ExpiresOn, ReleasedAt FROM Consignments WHERE (`Consignments`.`SectorId` = 'anvil') AND ((`ReleasedAt` IS NULL AND `Id` > '60000000-0000-4000-8000-000000000004')) ORDER BY `ReleasedAt` DESC, `Id` ASC LIMIT 5
