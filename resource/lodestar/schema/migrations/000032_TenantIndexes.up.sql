-- Tenant-leading indexes for the bare-tenant resources that list in a declared order:
-- each list reads its page off the index instead of sorting the sector's partition.
-- DroidReports keeps none on purpose; it is the registered demonstration of the
-- generator's index warning.
CREATE INDEX MissionsBySectorIdDeadline ON Missions(SectorId, Deadline);
CREATE INDEX HangarsBySectorIdName ON Hangars(SectorId, Name);
CREATE INDEX WingsBySectorIdName ON Wings(SectorId, Name);
CREATE INDEX ConsignmentsBySectorIdReleasedAt ON Consignments(SectorId, ReleasedAt DESC);
CREATE INDEX DistressCallsBySectorIdSeverity ON DistressCalls(SectorId, Severity DESC);
