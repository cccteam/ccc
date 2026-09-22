WITH Booked AS (
  SELECT
    m.KindId,
    m.Fee
  FROM Missions m
  WHERE m.StatusId != 'stood_down'
    AND m.Fee >= @minFee
)
SELECT
  b.KindId AS KindId,
  COUNT(*) AS MissionCount,
  SUM(b.Fee) AS TotalFee,
  MAX(b.Fee) AS TopFee
FROM Booked b
GROUP BY b.KindId
