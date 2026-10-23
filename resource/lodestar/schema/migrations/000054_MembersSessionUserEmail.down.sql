DROP INDEX MembersSessionUsersByNormalizedEmail;
ALTER TABLE MembersSessionUsers DROP COLUMN EmailVerifiedAt;
ALTER TABLE MembersSessionUsers DROP COLUMN NormalizedEmail;
ALTER TABLE MembersSessionUsers DROP COLUMN Email;
