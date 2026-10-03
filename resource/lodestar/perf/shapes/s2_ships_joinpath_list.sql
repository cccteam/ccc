-- Join-path tenancy, one hop: the Ships page in Anvil through Hangars.SectorId (mirrors the renderer's shape; the walkthrough does not list ships).
SELECT Id, HangarId, ClassId, Registry, Name, LastRefitAt, UpdatedAt FROM Ships WHERE (EXISTS (SELECT 1 FROM `Hangars` `ca1` WHERE `ca1`.`Id` = `Ships`.`HangarId` AND `ca1`.`SectorId` = 'anvil')) ORDER BY `Name` ASC, `Id` ASC LIMIT 11
