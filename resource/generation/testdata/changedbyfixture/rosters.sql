-- The roster: every berth with its dock's name. Comments name Visits, which the query
-- does not read, and the label below names Docks inside a string, which does not count
-- either; neither is a token in a table position.
SELECT
  b.DockId,
  b.Slot,
  b.Name,
  d.Name AS DockName,
  'Docks' AS Label, /* Visits */
  @minSlot AS MinSlot
FROM `Berths` b
JOIN Docks d ON d.Id = b.DockId
WHERE b.Slot >= @minSlot
