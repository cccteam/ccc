-- The crew's email addresses, in two states. Email is the address as the person gave
-- it, NormalizedEmail the form uniqueness and lookups use, and EmailVerifiedAt the
-- database's time at which the person proved they read the mailbox by following a link
-- sent to it; null while the address is unverified. An address is trusted for nothing
-- until it is verified: code that sends a link may use an unverified address, code that
-- relies on one requires the time. An operator may put an address on a user by hand,
-- unverified, which reserves it: uniqueness holds among every address held, verified or
-- not. No address is required, and the address is held apart from the username (the
-- session library's SessionUserEmail migration, under the auth's prefix).
ALTER TABLE CrewSessionUsers ADD COLUMN Email STRING(MAX);
ALTER TABLE CrewSessionUsers ADD COLUMN NormalizedEmail STRING(MAX) AS (NORMALIZE_AND_CASEFOLD(Email)) STORED;
ALTER TABLE CrewSessionUsers ADD COLUMN EmailVerifiedAt TIMESTAMP OPTIONS (allow_commit_timestamp = true);
CREATE UNIQUE NULL_FILTERED INDEX CrewSessionUsersByNormalizedEmail ON CrewSessionUsers(NormalizedEmail);
