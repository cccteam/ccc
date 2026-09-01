CREATE TABLE WorkOrderStatuses (
  Id STRING(64) NOT NULL,
  Description STRING(MAX) NOT NULL,
) PRIMARY KEY (Id);

CREATE TABLE RequisitionStatuses (
  Id STRING(64) NOT NULL,
  Description STRING(MAX) NOT NULL,
) PRIMARY KEY (Id);

CREATE TABLE ItemCategories (
  Id STRING(64) NOT NULL,
  Description STRING(MAX) NOT NULL,
) PRIMARY KEY (Id);

CREATE TABLE DeclineReasons (
  Id STRING(64) NOT NULL,
  Description STRING(MAX) NOT NULL,
) PRIMARY KEY (Id);
