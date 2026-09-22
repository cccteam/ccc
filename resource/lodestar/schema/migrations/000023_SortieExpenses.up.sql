CREATE TABLE SortieExpenses (
  Id STRING(36) NOT NULL,
  SortieId STRING(36) NOT NULL,
  Category STRING(MAX) NOT NULL,
  Amount NUMERIC NOT NULL,
  Note STRING(MAX),

  CONSTRAINT CK_SortieExpenses_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_SortieExpenses_SortieId FOREIGN KEY (SortieId) REFERENCES Sorties(Id),
) PRIMARY KEY (Id);
