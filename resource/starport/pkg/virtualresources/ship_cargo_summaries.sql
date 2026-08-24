SELECT
  s.Id AS ShipId,
  s.Name AS ShipName,
  b.Name AS DockingBayName,
  COUNT(m.LineNumber) AS ManifestLines,
  COALESCE(SUM(m.DeclaredValue), 0) AS TotalDeclaredValue
FROM Ships AS s
LEFT JOIN DockingBays AS b ON b.Id = s.DockingBayId
LEFT JOIN CargoManifests AS m ON m.ShipId = s.Id
GROUP BY s.Id, s.Name, b.Name
