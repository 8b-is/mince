# Go package API contract

This page describes the supported Go package at `github.com/8b-is/mince`.
It covers the API introduced in PR #17. Packages under `internal/` are
implementation details and are not part of this contract.

## Public surface

The root package exposes:

- `Mince(ctx, content, options) Result` for one deterministic local mincing
  attempt.
- `MinceWithPlan(ctx, content, options, plan, registry) (Result, error)` for an
  explicitly selected sequential plan with caller-provided stages.
- `Plan`, `StageName`, `StageRegistry`, `StageFunc`, and `Candidate` for defining
  ordered plans. `DefaultPlan()` returns the deterministic-only plan.
- `Options` and `Kind` to select automatic, JSON, log, or code handling, set a
  size threshold, and choose a store.
- `Result` to inspect the output, retrieval ID, selected kind, byte counts,
  savings, and whether the input passed through unchanged.
- `OpenStore`, `NewMemoryStore`, and `DefaultStore` to choose where originals
  are retained.
- `Store.Get` for exact original bytes and `Store.Retrieve` for selected text
  lines.

The storage backend is not an open interface in this version. Callers choose
one of mince's built-in stores; they cannot pass an arbitrary storage
implementation.

## `Mince` behavior

### Inputs and defaults

- `ctx` must be non-nil. A context already cancelled when a large input is
  processed causes passthrough. Current deterministic processing is
  synchronous; cancellation is checked before it starts and does not interrupt
  a summarizer midway.
- `content` is treated as bytes. Mincing does not intentionally modify the
  input. A passthrough result may refer to the same underlying byte slice, so
  callers should copy `Result.Minced` before modifying it.
- The zero `Options` value uses automatic classification and a 1 KiB minimum.
  `MinBytes <= 0` selects that same default.
- `KindAuto` and the zero `Kind` classify the content. The recognized explicit
  kinds are `KindJSON`, `KindLog`, and `KindCode`. An unknown kind is treated
  as automatic classification.
- `MaxOutliers` is used by JSON summarization; zero selects the existing
  summarizer default.
- `Options.Store == nil` uses `DefaultStore`, a process-wide memory store.

### Output and passthrough

- `Result.Minced` is the content to forward. For passthrough, it contains the
  input unchanged and `Result.Passthrough` is true.
- Small inputs, plain or unsupported content, cancelled requests, storage
  write failures, and candidates that do not qualify are returned unchanged.
  `Mince` reports these outcomes through `Result`; it does not return an error.
- The current heuristic requires the candidate body to save at least 10% before
  adding retrieval metadata. The final result is accepted only if it is
  smaller than the original. Since metadata adds bytes, final savings can be
  less than 10% even when a candidate passes the first threshold.
- A shortened result has a non-empty `CCRID`. Treat it as opaque and retrieve
  through the same `Store` used for the call. Do not parse its hex suffix or
  infer which store contains it.
- `RawBytes` is `len(content)`; `MincedBytes` is `len(Result.Minced)`.
  `SavedFraction` is the exact saved-byte fraction, such as `3/5`, and
  `SavedPercent` is its display form. For passthrough results they are `0/1`
  and `0.00%`.
- `Result.Kind` is the recognized content kind when classification ran. It is
  `KindAuto` when the input was returned before classification, such as an
  input below the size threshold.

## `MinceWithPlan` behavior

- A plan must contain at least one stage. Stage names must be non-blank, and
  every name other than the built-in `StageDeterministic` must exist in the
  supplied registry. Registering the reserved `StageDeterministic` name is an
  error. Invalid plans return an error before any stage runs.
- Stages run sequentially in the listed order with the same context. There is
  one current candidate and one final result; plans do not run stages in
  parallel.
- The built-in `StageDeterministic` runs the local JSON, log, or code
  summarizers. Caller-defined stages receive a copy of the current candidate's
  bytes and its classified kind. The initial candidate is the original input;
  after a reduction, later stages receive that reduction. Callbacks receive no
  CCR identifier or store through the API. Choosing a model-first plan therefore
  makes the original content available to that model callback.
- A caller-defined stage proposes a candidate by returning non-empty content
  that is strictly shorter than the current candidate and the same kind. An
  error, empty result, non-shortening result, or kind change declines that
  stage; the last safe candidate is retained and the next stage still runs.
  JSON proposals must be valid JSON objects so the finalizer can embed retrieval
  metadata; malformed JSON, arrays, and `null` are declined.
- Cancellation or deadline expiration stops later stages. The runner finalizes
  the last accepted candidate, or returns the original unchanged if no stage
  produced one. Stage errors are treated as declines; the returned error is
  reserved for invalid plans and a nil context. Callbacks must honor the shared
  context: a synchronous callback that ignores cancellation can delay return.
  A proposal returned after cancellation is discarded. Finalization may still
  store the last accepted candidate's original after cancellation.
- `Mince` remains the simple deterministic-only entry point. It has its
  existing signature and behavior; callers opt into `MinceWithPlan` to select
  another ordered path. A successful finalization calls CCR storage once, only
  after all stages have completed or stopped.

Example of a deterministic stage followed by an application-owned model stage:

```go
import (
	"context"

	mince "github.com/8b-is/mince"
)

func run(ctx context.Context, content []byte, opts mince.Options) error {
modelStage := mince.StageName("model")
plan := mince.Plan{Stages: []mince.StageName{
	mince.StageDeterministic,
	modelStage,
}}
registry := mince.StageRegistry{
	modelStage: func(ctx context.Context, in mince.Candidate) (mince.Candidate, error) {
		// Send only in.Content and in.Kind to the model implementation.
		// Return the same kind and a shorter candidate to accept its proposal.
		proposal := []byte("short model summary") // Replace with your model result.
		return mince.Candidate{Content: proposal, Kind: in.Kind}, nil
	},
}
result, err := mince.MinceWithPlan(ctx, content, opts, plan, registry)
_ = result
return err
}
```

The model implementation and its transport are application responsibilities;
this package does not select a provider or send model requests itself.

## Store behavior

### Choosing a store

- `OpenStore(dir)` creates or opens a disk-backed store. Passing an empty path
  creates a memory store instead.
- `NewMemoryStore()` creates an independent in-memory store.
- `DefaultStore()` returns the process-wide memory store used when `Options`
  has no store.
- Memory stores retain at most 64 MiB or 128 entries in their in-memory LRU.
  An evicted ID may no longer be retrievable.
- A disk-backed store keeps the original on disk across process restarts. The
  store currently has no retention period, quota, or public deletion method;
  disk records remain until removed outside this API. The store creates its
  directory with restrictive permissions and writes files with restrictive
  permissions where the operating system supports them.
- Candidate generation does not access the store. The finalizer checks the
  rendered output before writing the original, then writes it once only for an
  accepted shortened result. Rejected candidates and failed storage writes
  return the original without a `CCRID`.

### Retrieval

- `Store.Get(id)` returns the exact original bytes or an error if the ID is not
  available in that store. It accepts either the ID returned in
  `Result.CCRID` (`ccr_<suffix>`) or the suffix without `ccr_`.
- `Store.Retrieve(id, query, offset, limit)` is a text-oriented convenience.
  A valid regular expression filters lines; an invalid expression is treated
  as a literal substring. Negative offsets are treated as zero. A non-positive
  limit returns all remaining matches. CRLF line endings are normalized to LF
  in this returned string; use `Get` when byte-for-byte fidelity matters.
- IDs are scoped to a store. The same content may produce the same ID in two
  stores, but each store only retrieves its own record.

## Current limits and compatibility

This package exposes deterministic mincing and an opt-in caller-defined
sequential stage runner. It does not ship a local or hosted model provider, run
adaptive selection, or select a stage plan automatically; those remain future
extensions.

The module path is `github.com/8b-is/mince`, but this PR does not create a
versioned Go release or declare a v1 compatibility promise. The public API is
pre-v1 and may change before the project adopts a stable versioning policy.
