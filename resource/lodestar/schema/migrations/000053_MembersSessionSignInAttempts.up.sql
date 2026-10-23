-- The members' sign-in attempts: the password method's attempt limiter counts the failed
-- attempts on one name, known or not, within a window, and the lock those failures
-- earned (the session library's SessionSignInAttempts migration, under the auth's
-- prefix). The row is keyed by the submitted name as the users table normalizes
-- usernames, never by the user row, so an unknown name is counted exactly as a known
-- one. Failures counts in the window ending at WindowEndsAt; LockedUntil is set once
-- Failures reached the limit, and the name is refused until then. A success, an MFA
-- completion or a password change deletes the row. The mail the auth sends is counted
-- here too, under the address. UpdatedAt takes the commit time; WindowEndsAt and
-- LockedUntil are deadlines, written as given.
CREATE TABLE MembersSessionSignInAttempts (
    Name         STRING(MAX) NOT NULL,
    Failures     INT64 NOT NULL,
    WindowEndsAt TIMESTAMP NOT NULL,
    LockedUntil  TIMESTAMP,
    UpdatedAt    TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
) PRIMARY KEY (Name);
