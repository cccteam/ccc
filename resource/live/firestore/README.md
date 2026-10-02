# live/firestore

The Firestore implementation of the live service (`resource/live`): the subscription
record, the change sets, and the browser's identity.

## Layout

Two collections, one server-owned and one the browser reads.

`subscriptions/{id}` is the record. One document per subscription; the id is the
first 32 hex characters of `sha256("principal|tab|resource|key|domain")`, so the same
interest of the same tab is one document however often it is registered or renewed.
Fields: `principal`, `tab`, `resource`, `key` (empty for a list), `domain` (empty for
a row and for a global list), `expiry` (timestamp). No client reads it; the rules deny
everything outside the change sets.

`users/{uid}/changes/{id}` is a user's change set. The id is
`row|<resource>|<key>|<unix second>`, `list|<resource>|<domain>|<unix second>` or
`resource|<resource>|<unix second>` (a compound key's `/` is written `%2F`), set with
merge, so writes to one target within one second coalesce into one document. Fields:
`kind` (`row`, `list` or `resource`), `resource`, `key` and `deleted` on a row
document, `domain` on a list document, `at` (the server timestamp), `expires`
(timestamp, ten minutes after the write). The browser queries
`where at > lastSeen order by at` and may read only its own set.

`application/{topic}` is the application topic: one document per topic the server's
instances signal each other on, set with `at` (the server timestamp) and `by` (the
writing instance) on every `Broadcast`, and watched by every instance's `Watch`, which
skips the snapshot it starts from and runs its callback on each later one. The one topic
today is `features`: a feature flag flip signals every instance to reread its flags.
No client reads it; the rules deny everything outside the change sets.

## Indexes

`firestore.indexes.json` holds the three composite indexes the record's lookups use,
all on `subscriptions`: `(resource, key, expiry)` for a row's subscribers,
`(resource, domain, expiry)` for a list's, and `(resource, expiry)` for every
subscriber of a resource, the bulk path's one lookup. The unsubscribe queries are
equality-only (`principal`, `principal` and `tab`) and need no composite index. The
change sets query one field, `at`, which the default single-field index serves.

## Time-to-live policies

Two, named in `firestore.indexes.json` as `fieldOverrides` with `ttl: true`:

- `subscriptions.expiry`: a subscription that was not renewed is deleted after it
  expires. Every lookup also filters `expiry > now`, since a policy deletes within a
  day, not at the instant.
- `changes.expires` (the `changes` collection group): a change document is deleted
  ten minutes after it was written. The browser only reads what is newer than what it
  saw, so an expired document was already consumed or never needed.

The infrastructure applies the indexes and the policies (bedrock, a separate item);
nothing here creates them. Against the emulator no index is needed, and nothing
expires.

## Rules

`firestore.rules`: `match /users/{uid}/changes/{doc}` allows `read` when
`request.auth.uid == uid` and no `write`; everything else is denied. The server never
goes through the rules.

## Identity

In production the token route answers a Firebase custom token for the session
principal's id, minted by the Admin SDK and signed through the IAM credentials API with
the service account the metadata server names (no key file); the browser signs in with
`signInWithCustomToken`. A logout revokes the uid's refresh tokens. Against the
emulator (`EmulatorHost` set) the token is empty and the payload carries the emulator
host; the browser connects with the SDK's `mockUserToken: {sub: uid, user_id: uid}`.

## Configuration

`Config{ProjectID, DatabaseID, APIKey, EmulatorHost}`: the project and the database
id (`APP_FIRESTORE_DATABASE` is how bedrock hands the database to an application; empty
is `(default)`), the optional Firebase web API key (`APP_FIREBASE_API_KEY`) the
browser initializes the SDK with, and the emulator host (`FIRESTORE_EMULATOR_HOST`).

## Tests

The package's tests run against the Firestore emulator from the Cloud SDK emulators
image, started with podman beside the Spanner emulator the resource package's tests
use, and skip under `-short` and when podman is absent. They prove the record's
lookups and expiry, the publisher's documents, coalescing and the bulk threshold, the
emulator token, and, through the emulator's REST API with an unsigned token for a uid,
that a user reads their own change set and nothing else.
