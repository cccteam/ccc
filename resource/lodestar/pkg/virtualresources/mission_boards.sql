SELECT
  m.Id AS Id,
  m.SectorId AS SectorId,
  m.Title AS Title,
  m.Title_Search AS Title_Search,
  c.Name AS ClientName,
  c.Name_Search AS Name_Search,
  s.Name AS SquadronName,
  m.KindId AS KindId,
  m.StatusId AS StatusId,
  m.Deadline AS Deadline,
  TIMESTAMP_DIFF(m.Deadline, CURRENT_TIMESTAMP(), DAY) AS DaysLeft
FROM Missions m
JOIN Clients c ON c.Id = m.ClientId
LEFT JOIN Squadrons s ON s.Id = m.AssignedSquadronId
