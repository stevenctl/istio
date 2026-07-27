# krtlint

Static analysis for [`krt`](../../pkg/kube/krt) usage.

`krt` is generic, but several of its core requirements cannot be expressed as generic
constraints, so the compiler cannot enforce them. As the krt README puts it, violating
them "will result in undefined behavior (which would likely manifest as stale data)".
In practice the failure modes are a panic deep inside the framework, or — worse — change
detection that silently stops working. These analyzers recover those requirements
statically.

## Running

```bash
go run ./tools/krtlint ./pilot/... ./pkg/...
```

Individual checks can be selected the same way as any `go vet` tool:

```bash
go run ./tools/krtlint -krtfetch ./pilot/...
go run ./tools/krtlint -krtequalsfields=false ./pilot/...
```

## Checks

### `krtkey`

`krt.GetKey` derives an object's key by type-asserting the boxed value against a series of
interfaces, and panics if none match. Because the assertion is on the *value*, a
`ResourceName() string` declared on `*T` does not make `T` keyable — a mistake the compiler
happily accepts. This reports collection constructors whose element type would panic,
calling out the pointer-receiver case specifically.

It also catches the subtler case of a defined string type: `krt.GetKey` matches
`any(a).(string)`, which `type Host string` does not satisfy.

### `krtequal`

`krt.Equal` decides whether an object changed. It dispatches to an `Equals` method when the
value or its address satisfies `Equaler[T]` or `Equaler[*T]`, and otherwise falls back to
`proto.Equal` or `reflect.DeepEqual`. This reports element types where that fallback is
unsafe:

- An `Equals` method krt **cannot dispatch to**, because its parameter is neither `T` nor
  `*T`. This is the worst case: the author believes comparison is handled, and krt silently
  uses `reflect.DeepEqual` instead. Declaring `Equals(T)` on a collection of `*T` is the
  usual way in.
- Types reaching a **protobuf message** through their fields. `reflect.DeepEqual` compares
  the unexported bookkeeping state protobuf messages carry, so it is not a reliable answer;
  krt's own code notes that "DeepEqual on proto is broken".
- Types embedding a protobuf message, which krt detects at runtime and **panics** on.
- Types with **func** fields, which `reflect.DeepEqual` reports as unequal unless both are
  nil, so every object looks changed on every recomputation.
- Types with **synchronization primitives**, whose lock state is compared as data.

### `krtequalsfields`

When `Equals` returns true krt keeps the *old* object (`collection.go:515-518`), so a field
missing from `Equals` does not merely miss an event — it stays permanently stale in the
collection, and in any index built on it, until some compared field happens to change. This
reports three shapes:

- A field **never compared**.
- A field read from **only one operand**, so it is compared against itself.
- A loop that ranges over one operand's field and then indexes **that same field** rather
  than the other operand's, which is how a copy-paste error looks. The correct paired form,
  `for i := range a.F { a.F[i] == b.F[i] }`, is left alone.

Fields that `ResourceName` reads are exempt automatically. They are part of the key, so a
change to one produces a delete and an add rather than an update, and `Equals` is never asked
about it.

Fields genuinely derived from a compared field can be marked `+noKrtEquals`; known gaps can be
marked `+krtEqualsTodo` and revisited with `-krtequalsfields.todos`. Both markers are already
used in this repo.

```go
type AddressInfo struct {
    Name string
    // Marshaled is a cache of the fields above.
    // +noKrtEquals
    Marshaled []byte
}
```

Comparing a promoted field counts as comparing the embedded field it belongs to. The check
backs off entirely when the receiver or argument is used as a whole value (passed to
`reflect.DeepEqual`, to a helper, or to a method), since such an implementation cannot be
attributed to individual fields.

This check is the same idea as [kgateway's `krtequals`](https://github.com/kgateway-dev/krtequals),
adapted to istio's krt and extended with the two comparison-shape checks above.

### `krtfetch`

krt learns what a transformation depended on by intercepting `krt.Fetch`, and uses that to
decide what to recompute. Reading a collection any other way returns the data without
registering the dependency, so the transformation is never re-run when that data changes and
its output goes stale. This reports, inside any function taking a `krt.HandlerContext`:

- `Collection.List`, `Collection.GetKey`, `Singleton.Get` and `Index.Lookup`, each with the
  `Fetch` form to use instead.
- `Register` / `RegisterBatch`, which leak a handler per invocation since transformations run
  many times.
- Calls to `time.Now`, the `math/rand` generators, and `os.Getenv`. Transformations "may be
  called at any time, including many times for the same inputs", so a result derived from
  these cannot be reproduced.

Reads that only feed a log message are ignored, since they do not affect the output.
`krt.RecomputeProtected.Get(ctx)` is the sanctioned escape hatch for out-of-band state and
registers its own dependency, so it is not reported.

### `krtfilter`

Label and selector filters extract fields from the fetched object by type assertion and
reflection; the krt README notes that "failures to meet this requirement will result in a
`panic`". Worse, the panic only fires once a candidate object actually reaches the filter,
so it can lie dormant. This reports `krt.FilterLabel`, `krt.FilterSelects` and
`krt.FilterSelectsNonEmpty` applied to a collection whose element type provides neither the
accessor method nor the `Spec.Selector` field krt falls back to.

## Suppressing a diagnostic

`//krtlint:ignore` opts a single site out of one or more analyzers. It works for every check,
including the ones with no marker of their own.

```go
//krtlint:ignore                    // every analyzer
//krtlint:ignore krtequal           // one
//krtlint:ignore krtequal,krtfetch  // several
```

Text after `--` is a free-form reason, and the conventional form carries one:

```go
//krtlint:ignore krtequal -- placeholder collection, never written to
krt.NewStaticCollection[*securityclient.PeerAuthentication](nil, nil),
```

A directive covers the comment group holding it and the line immediately after that group, so
it can trail a statement or sit in the doc comment of the declaration it excuses. Keep the
reason on the directive's own line — gofmt moves directives to the end of a doc comment block,
stranding anything written below them — and put longer explanations in the prose above.

Prefer `+noKrtEquals` on the field itself where that fits: it survives the code moving around,
and it says which field is excused rather than silencing the whole method. Reach for
`//krtlint:ignore` when the diagnostic is not about a single field, as with an `Equals` that
compares by identity on purpose.

## Tests

`internal/krtlint/testdata` holds a stub of the krt API, so the analyzer tests run against
representative code without pulling in Kubernetes.

```bash
go test ./tools/krtlint/...
```
