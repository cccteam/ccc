DROP INDEX CrewSessionUsersByNormalizedEmail;
ALTER TABLE CrewSessionUsers DROP COLUMN EmailVerifiedAt;
ALTER TABLE CrewSessionUsers DROP COLUMN NormalizedEmail;
ALTER TABLE CrewSessionUsers DROP COLUMN Email;
