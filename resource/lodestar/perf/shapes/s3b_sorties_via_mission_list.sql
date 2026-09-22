-- Join-path tenancy through a state root: the Sorties page in Anvil through Missions.SectorId (mirrors the renderer's shape).
SELECT Id, MissionId, ShipId, PilotUserId, LaunchedAt, ReturnedAt, Debrief FROM Sorties WHERE (EXISTS (SELECT 1 FROM `Missions` `ca1` WHERE `ca1`.`Id` = `Sorties`.`MissionId` AND `ca1`.`SectorId` = 'anvil')) ORDER BY `LaunchedAt` DESC, `Id` ASC LIMIT 11
