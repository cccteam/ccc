SELECT
  m.ShipId,
  m.LineNumber,
  s.Name AS ShipName,
  m.Details,
  m.Quantity,
  m.DeclaredValue
FROM CargoManifests AS m
JOIN Ships AS s ON s.Id = m.ShipId
