CREATE TABLE SensorReadings (
  Id STRING(36) NOT NULL,
  WaystationId STRING(64) NOT NULL,
  FacilityId STRING(36) NOT NULL,
  Metric STRING(MAX) NOT NULL,
  Reading FLOAT64 NOT NULL,
  RecordedAt TIMESTAMP NOT NULL,

  CONSTRAINT CK_SensorReadings_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_SensorReadings_WaystationId FOREIGN KEY (WaystationId) REFERENCES Waystations(Id),
  CONSTRAINT FK_SensorReadings_FacilityId FOREIGN KEY (FacilityId) REFERENCES Facilities(Id),
) PRIMARY KEY (Id);
