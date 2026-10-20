REVOKE SELECT ON TABLE SchemaMigrations FROM ROLE migrations_reader;
DROP ROLE migrations_reader;
DROP TABLE DataChangeEvents;
