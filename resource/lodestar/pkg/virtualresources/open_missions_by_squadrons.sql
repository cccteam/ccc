SELECT
  s.Id AS SquadronId,
  s.Name AS SquadronName,
  m.SectorId AS SectorId,
  COUNT(*) AS OpenMissions,
  MIN(m.Deadline) AS NextDeadline
FROM Missions m
JOIN Squadrons s ON s.Id = m.AssignedSquadronId
WHERE m.StatusId IN ('claimed', 'underway', 'on_hold')
GROUP BY s.Id, s.Name, m.SectorId
