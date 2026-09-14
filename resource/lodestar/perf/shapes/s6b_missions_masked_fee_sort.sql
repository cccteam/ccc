-- Visible projection: the archivist sorts by fee; masked cells fall to the NULL region, first ascending on Spanner.
SELECT Id, CASE WHEN `Missions`.`StatusId` = 'completed' THEN Fee ELSE NUMERIC '0' END AS Fee, Deadline, StatusId FROM Missions WHERE (`Missions`.`SectorId` = 'anvil') ORDER BY CASE WHEN `Missions`.`StatusId` = 'completed' THEN `Fee` END ASC, `Id` ASC LIMIT 26
