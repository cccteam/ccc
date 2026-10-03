SELECT
  sm.SquadronId AS SquadronId,
  sm.UserId AS UserId,
  w.SectorId AS SectorId,
  s.Name AS SquadronName,
  p.DisplayName AS PilotName,
  p.Id AS PilotId
FROM SquadronMemberships sm
JOIN Squadrons s ON s.Id = sm.SquadronId
JOIN Wings w ON w.Id = s.WingId
LEFT JOIN Pilots p ON p.UserId = sm.UserId
