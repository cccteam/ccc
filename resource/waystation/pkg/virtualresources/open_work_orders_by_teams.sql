SELECT
  t.Id AS TeamId,
  t.Name AS TeamName,
  wo.WaystationId AS WaystationId,
  COUNT(*) AS OpenOrders,
  MIN(wo.DueAt) AS NextDue
FROM WorkOrders wo
JOIN Teams t ON t.Id = wo.AssignedTeamId
WHERE wo.StatusId IN ('scheduled', 'in_progress', 'on_hold')
GROUP BY t.Id, t.Name, wo.WaystationId
