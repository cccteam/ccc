---
name: securehash
description: Use the github.com/cccteam/ccc/securehash package for hashing and verifying secrets (passwords, API keys, tokens) anywhere in the CCC stack — including storing hashes in Spanner or database/sql columns and migrating stored hashes to stronger parameters or algorithms over time. Reach for it whenever code needs bcrypt or argon2, password verification, credential storage, or a "needs rehash" upgrade flow, even if the user just says "hash this password".
---

# securehash

Secure secret hashing with **built-in hash upgrading**: a stored `Hash` is
self-describing (it carries its own algorithm and parameters), so `Compare` can
verify a secret against the *old* parameters while telling you the hash should be
re-written with your *current* configuration — including across algorithms
(bcrypt → argon2 and back). `Hash` also implements Spanner and `database/sql`
codecs, so it drops straight into a column.

Never hand-roll bcrypt/argon2 calls in this codebase — use this package so hashes
stay upgradeable.

## Core workflow

```go
hasher := securehash.New(securehash.Argon2())   // or securehash.Bcrypt()

// Create
h, err := hasher.Hash("password")
stored, err := h.MarshalText()                  // persist this string

// Verify (e.g. at login)
loaded := &securehash.Hash{}
if err := loaded.UnmarshalText(stored); err != nil { ... }

needsUpgrade, err := hasher.Compare(loaded, "password")
if err != nil {
    // err != nil means the secret DID NOT MATCH — treat as auth failure
}
if needsUpgrade {
    // matched, but stored params/algorithm differ from current config:
    newHash, _ := hasher.Hash("password")       // re-hash and persist
}
```

**Read `Compare`'s results carefully**: the bool means *needs upgrade*, not
*matched*. A clean match is `(false, nil)`; a mismatch is `(false, err)`. Always
gate authentication on `err == nil`.

## API surface

```go
func New(algo HashAlgorithm) *SecureHasher
func Bcrypt() *BcryptOptions      // cost fixed at 15
func Argon2() *Argon2Options      // Argon2id, OWASP params: 12 MiB, times 3,
                                  // parallelism 1, salt 16B, key 32B

func (s *SecureHasher) Hash(plaintext string) (*Hash, error)
func (s *SecureHasher) Compare(hash *Hash, plaintext string) (needsUpgrade bool, err error)
func (s *SecureHasher) KeyType() string   // "Bcrypt" | "Argon2"
```

`Hash` implements `encoding.TextMarshaler`/`TextUnmarshaler`,
`spanner.Encoder`/`Decoder`, and `sql.Scanner`/`driver.Valuer`. There is no other
constructor: build one via `hasher.Hash(...)` or `UnmarshalText`.

**There are no tuning knobs.** Bcrypt cost and all argon2 parameters are fixed by
the package (an unexported `argon2WithOptions` exists for internal use only), and
`HashAlgorithm` is a sealed interface — no third-party algorithms. If a task calls
for different parameters, that requires changing this package, not the caller.

## Serialized format

- bcrypt: the raw bcrypt string (`$2a$15$...`) — its "version prefix" is empty.
- argon2: `1$<memory KiB>$<times>$<parallelism>$<b64 salt>.<b64 key>`
  (StdEncoding with padding).

Format changes break every stored hash; treat the encoding as frozen.

## Storing in a database

A `Hash` field works directly as a Spanner or `database/sql` column value. Use
`*Hash` for nullable columns: `Scan(nil)` succeeds but leaves the value unusable
(see gotchas), so nil-check before calling methods.

## Gotchas

- **Zero-value `Hash` panics** on `KeyType`/`MarshalText`/`Value`, and `Compare`
  panics on a `*Hash` that was never populated. Only use hashes that came from
  `Hash()` or a successful `UnmarshalText`/`Scan` of a non-NULL value.
- A failed `UnmarshalText` leaves the receiver's existing hash unchanged because
the replacement is assigned only after a complete successful decode.
- Store `*Hash` (or pass `&h`): the codec methods are split across pointer and
  value receivers.
- Any parameter or algorithm change is only actionable when you *have the
  plaintext* — the upgrade signal fires inside a successful verify flow, so wire
  re-hashing into login.
- bcrypt inherits x/crypto's 72-byte password limit (errors on longer input);
  argon2 has no such limit. Switching algorithms changes behavior for long secrets.
- Upgrade detection compares the *entire* options struct — changing any single
  parameter flags every stored hash for rehash.
- Treat every non-nil `Compare` error as an authentication failure. Bcrypt errors
  wrap x/crypto sentinels, but Argon2 mismatch errors expose no sentinel to compare.
