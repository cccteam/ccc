SELECT
  c.Id AS Id,
  s.Id AS SectorId,
  c.Name AS Name,
  c.Trusted AS Trusted,
  (SELECT COUNT(*) FROM ClientContacts cc WHERE cc.ClientId = c.Id) AS ContactCount,
  (SELECT COUNT(*) FROM Missions m WHERE m.ClientId = c.Id AND m.SectorId = s.Id) AS SectorMissions
FROM Clients c
CROSS JOIN Sectors s
