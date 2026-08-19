# ccc

Utility types and functions created and maintained by the CCC team.

`UUID`, `NullUUID`, `Duration`, `NullDuration`, and `NullEnum` are **wire-format
types that round-trip across JSON, text, and Google Cloud Spanner**: they
implement `MarshalJSON`/`UnmarshalJSON`, `MarshalText`/`UnmarshalText`, and
`DecodeSpanner`/`EncodeSpanner` structurally, without importing the Spanner
client.

The nullable convention is uniform across the module: embed the value type plus a
`Valid bool`; `MarshalJSON` emits `null`, `MarshalText` returns `(nil, nil)`, and
`EncodeSpanner` returns `nil` when invalid.

Each subdirectory of this repository (`accesstypes`, `cache`, `middleware`, `pkg`,
`resource`, `securehash`, `sns`, `tracer`) is its **own Go module**, imported and
versioned independently — depending on the root module pulls in none of them.

## UUID / NullUUID

```go
id, err  := ccc.NewUUID()                     // v4
id2, err := ccc.UUIDFromString("4192bff0-e1e0-43ce-a4db-912808c32493")
ccc.NilUUID                                   // zero value

n, err := ccc.NewNullUUID()                   // v4, Valid=true
n2 := ccc.NullUUIDFromUUID(id)                // Valid=true
n2.IsNil()                                    // reports !Valid (shadows uuid.UUID.IsNil)
```

`UUID` embeds `gofrs/uuid.UUID`, so `String()`, `Bytes()`, `Version()` are all
promoted. Typical resource struct:

```go
type Ship struct {
    ID           ccc.UUID     `spanner:"Id"`
    DockingBayID ccc.NullUUID `spanner:"DockingBayId"`
}
```

## Duration / NullDuration

`Duration` embeds `time.Duration` and serializes in Go duration-string form
(`"1h0m0s"`), never as nanosecond integers. The string parsers strip spaces first,
so `ccc.NewDurationFromString("10h 3s")` works.

```go
d  := ccc.NewDuration(10 * time.Hour)
d2, err := ccc.NewDurationFromString("10h 3s")
nd := ccc.NewNullDuration(time.Minute)
nd2, err := ccc.NewNullDurationFromString("90s")
```

Note that `DecodeSpanner` does *not* strip spaces — a Spanner column holding
`"10h 3s"` fails to decode. Store canonical `String()` output only.

## NullEnum[T]

A generic nullable wrapper for named enum types, with constraint
`~string | ~int | ~int64 | ~float64` (no int32/uint/float32/bool). There is no
constructor — build a literal:

```go
type Status string
s := ccc.NullEnum[Status]{Value: "active", Valid: true}
var null ccc.NullEnum[Status]        // Valid=false → JSON null, Spanner nil
```

`UnmarshalText` on empty input means "null" (returns nil, leaves `Valid` false) —
it is not a parse error.

## JSONMap

`type JSONMap map[string]any` with an `UnmarshalJSON` that walks the whole tree
(maps and slices) converting each `json.Number` to `int` when it fits, otherwise
`float64` — fixing the "everything became float64" problem of `map[string]any`.
Integers land as `int`, not `int64`.

## Iterator adapters

```go
func ReadIter[T any](r ReadIterator[T]) iter.Seq2[T, error]   // adapts Read() (T, error), e.g. csv.Reader
func NextIter[T any](r NextIterator[T]) iter.Seq2[T, error]   // adapts Next()/Value()/Error(), e.g. Spanner RowIterator
func BatchIter2[T any](it iter.Seq2[T, error], size int) iter.Seq[iter.Seq2[T, error]]
```

```go
for batch := range ccc.BatchIter2(rows, 100) {
    // e.g. start a new db transaction per batch
    for item, err := range batch {
        if err != nil { return err }
        use(item)
    }
}
```

Rules to be aware of:

- Fully range each inner batch before advancing the outer loop, or `BatchIter2`
  panics. Breaking out of the inner loop early is fine — leftovers roll into the
  next batch. The outer iterator is single-use.
- `ReadIter` never terminates on its own; break on `io.EOF` yourself:
  `if errors.Is(err, io.EOF) { break }`.
- `size <= 0` yields one batch containing a single `(zero, error)` pair rather
  than returning an error from the function.

## Helpers

```go
func Must[T any](value T, err error) T   // panics on error — init/config paths only
```

Deprecated, avoid in new code: `ccc.Ptr` (use Go's builtin value-form `new(x)`)
and `ccc.StartTrace` (use `github.com/cccteam/ccc/tracer.Start`).

## Notes

- Unmarshalling JSON `null` into an already-populated `NullUUID`/`NullDuration`/
  `NullEnum` is a **no-op that leaves the old value in place** — decode into fresh
  zero values.
- `UUID.DecodeSpanner` accepts only `string`; the Null variants also accept
  `*string`/nil.
- Errors follow `go-playground/errors/v5` with messages naming the failed call;
  there are no exported sentinel errors. Some `duration.go` paths discard the
  cause (`errors.Newf("...: %s", err)`), so don't rely on unwrapping them.
