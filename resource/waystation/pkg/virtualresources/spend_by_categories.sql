WITH CountedLines AS (
  SELECT
    rl.CatalogItemId,
    rl.Quantity,
    rl.UnitCostSnapshot
  FROM RequisitionLines rl
  JOIN Requisitions r ON r.Id = rl.Id
  WHERE r.StatusId IN ('approved', 'fulfilled')
    AND rl.UnitCostSnapshot >= @minLineCost
)
SELECT
  ci.CategoryId AS Category,
  COUNT(*) AS LineCount,
  SUM(cl.Quantity) AS TotalQuantity,
  SUM(cl.UnitCostSnapshot * cl.Quantity) AS TotalSpend
FROM CountedLines cl
JOIN CatalogItems ci ON ci.Id = cl.CatalogItemId
GROUP BY ci.CategoryId
