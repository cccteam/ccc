---
name: cache
description: Use the github.com/cccteam/ccc/cache package for a thread-safe, disk-persisted key-value cache in devtools and code generators — caching expensive-to-compute data (schema table maps, enum values, migration hashes) between runs. Reach for it whenever a CLI or generator needs to persist intermediate results locally, and never for sensitive data or cross-process coordination.
---

# cache

A thread-safe key-value store persisted to disk, intended for **devtools caching
data that is expensive to compute and unlikely to change** (its in-repo consumer is
the `resource/generation` code generator, which caches Spanner table maps and
migration hashes). Values are CBOR-encoded, one file per key, in a two-level
namespace (`subpath` → `key`), sandboxed under an `os.Root`.

**Do NOT use it to store sensitive information** — values are plaintext CBOR on
disk. It is also per-process only: two processes (or two `Cache` instances) on the
same directory are not coordinated.

## API

```go
func New(path string, opts ...Option) (*Cache, error)
func WithPermission(perms uint32) Option        // default 0o755; files get perms &^ 0o111

func (c *Cache) Store(subpath, key string, data any) error
func (c *Cache) Load(subpath, key string, dst any) (bool, error)   // (false, nil) = miss
func (c *Cache) Keys(subpath string) (iter.Seq[string], error)
func (c *Cache) DeleteKey(subpath, key string) error
func (c *Cache) DeleteSubpath(subpath string) error
func (c *Cache) DeleteAll() error
func (c *Cache) Close() error                   // required
```

## Usage

```go
gCache, err := cache.New(".")            // storage rooted at ./.ccc-cache/
if err != nil {
    return errors.Wrap(err, "cache.New()")
}
defer gCache.Close()                     // mandatory — closes the os.Root

type tableMap struct{ /* ... */ }
if err := gCache.Store("tables", schemaHash, &tm); err != nil { ... }

var tm tableMap
ok, err := gCache.Load("tables", schemaHash, &tm)
if err != nil { ... }
if !ok { /* cache miss — recompute */ }

keys, err := gCache.Keys("tables")
for k := range keys { ... }
```

Storage root is always `filepath.Join(path, ".ccc-cache")`, and **`path` must
already exist** — `New` stats it first and only creates the `.ccc-cache`
directory itself.

## Semantics worth knowing

- **Miss vs error:** `Load` returns `(false, nil)` for a missing subpath or key;
  only real I/O or decode failures error. `DeleteKey`/`DeleteSubpath` return nil
  when the subpath doesn't exist — but `DeleteKey` *errors* when the subpath
  exists and the key doesn't (despite its doc comment claiming otherwise).
- Writes are durable and clean: `O_SYNC` + truncate, with the prior file removed
  first, so overwriting a key is safe.
- `Keys` snapshots the directory under a read lock and yields file names lazily;
  directories are skipped.
- Prefer `DeleteSubpath` over `DeleteAll` when you intend to keep using the
  cache: `DeleteAll` replaces the cache directory out from under the held
  `os.Root`.
- A subpath that resolves to an existing *file* errors with
  `path %q is not a directory`.
- No environment variables, no network — pure local filesystem. Decode allows
  very large maps (`MaxMapPairs` maxed) on purpose for generator table maps.
