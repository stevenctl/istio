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
`proto.Equal` or `reflect.DeepEqual`. Two kinds of thing are reported, and they differ in
where the fix belongs.

**Defects in the type declaration**, reported once per package because one fix serves every
collection built on the type:

- An `Equals` method krt **cannot dispatch to**, because its parameter is neither `T` nor
  `*T`. This is the worst case: the author believes comparison is handled, and krt silently
  uses `reflect.DeepEqual` instead. Declaring `Equals(T)` on a collection of `*T` is the
  usual way in.
- Types embedding a protobuf message, which krt detects at runtime and **panics** on.

**Fields the `reflect.DeepEqual` fallback cannot compare**, reported at every construction
site, because whether it costs anything depends on the collection rather than the type — a
transformation passing an object straight through compares the same pointer, one building a
fresh object every time does not, and a collection that is never written to compares nothing
at all:

- A reachable **protobuf message**. `reflect.DeepEqual` reads the unexported state protobuf
  messages carry, which marshaling writes in place, so two equal objects can compare unequal;
  krt's own code notes that "DeepEqual on proto is broken".
- **func** fields, which `reflect.DeepEqual` reports as unequal unless both are nil, so every
  object looks changed on every recomputation.
- **synchronization primitives**, whose lock state is compared as data.

These all err in the same direction: a genuine difference is still caught, so the cost is
recomputation that changes nothing rather than a change that never propagates. That is why a
site which provably never compares anything can reasonably carry a `//nokrtlint` — and
why it is still worth reporting, since only the author knows that.

Where the element type comes from another package, as Kubernetes CRDs do, the report says so:
Go will not let you declare `Equals` there, so the fix is a wrapper type or the opt-out.

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

A field that does not need comparing, because it is derived from one that is, can carry a
`//nokrtlint` directive.

```go
type AddressInfo struct {
    Name string
    // Marshaled is a cache of the fields above.
    //nokrtlint:krtequalsfields
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

`//nokrtlint` opts out of one or more analyzers, in the shape of `//nolint`.

```go
//nokrtlint                    // every analyzer
//nokrtlint:krtequal           // one
//nokrtlint:krtequal,krtfetch  // several
```

Text after `--` is a free-form reason, and the conventional form carries one:

```go
//nokrtlint:krtequal -- placeholder collection, never written to
krt.NewStaticCollection[*securityclient.PeerAuthentication](nil, nil),
```

What it covers depends on where it sits. On a **struct field** it exempts that field wherever
the diagnostic is reported from, which is what `krtequalsfields` needs, since that report lands
on the `Equals` method rather than on the field. **Anywhere else** it covers the comment group
holding it and the line immediately after, so it can trail a statement or sit in the doc comment
of the declaration it excuses.

Prefer the field form where it fits: it says which field is excused rather than silencing a
whole method, and it survives the code moving around.

Keep the reason on the directive's own line. gofmt moves directive comments to the end of a doc
comment block, which strands anything written below them; put longer explanations in the prose
above.

### Seeing what has been suppressed

A directive leaves no trace in the output it silences, so `-noignore` disables all of them at
once. This is the only way to audit what a tree has opted out of.

```bash
go run ./tools/krtlint ./pilot/...            # what CI enforces
go run ./tools/krtlint -noignore ./pilot/...  # every suppression revealed
```

Running it as a separate non-blocking job keeps `make lint-krt` green on the intentional set
while leaving the accepted debt visible.

## Tests

`internal/krtlint/testdata` holds a stub of the krt API, so the analyzer tests run against
representative code without pulling in Kubernetes.

```bash
go test ./tools/krtlint/...
```
