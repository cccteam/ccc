CREATE TABLE IncidentReports (
  Id STRING(36) NOT NULL,
  WaystationId STRING(64) NOT NULL,
  Summary STRING(MAX) NOT NULL,
  Severity INT64 NOT NULL,
  ReporterContact STRING(MAX) NOT NULL,
  RawStatement STRING(MAX),
  CaseNumber STRING(MAX) NOT NULL,

  CONSTRAINT CK_IncidentReports_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_IncidentReports_WaystationId FOREIGN KEY (WaystationId) REFERENCES Waystations(Id),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX IncidentReportsByCaseNumber ON IncidentReports(CaseNumber);
