INSERT INTO Tenants (Id, Name) VALUES ('north', 'North');
INSERT INTO Tenants (Id, Name) VALUES ('south', 'South');
-- The portal's members come through the Microsoft Entra directory the members auth
-- registers (APP_MEMBERS_DIRECTORY_TENANT_ID, the development placeholder): both tenants
-- declare it, so a member reaches the tenant they hold a role in.
INSERT INTO TenantDirectories (Id, TenantId, Method, Connection) VALUES ('10000000-0000-4000-8000-000000000001', 'north', 'azure', '00000000-0000-0000-0000-000000000000');
INSERT INTO TenantDirectories (Id, TenantId, Method, Connection) VALUES ('10000000-0000-4000-8000-000000000002', 'south', 'azure', '00000000-0000-0000-0000-000000000000');
