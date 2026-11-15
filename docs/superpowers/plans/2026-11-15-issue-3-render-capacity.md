# Issue #3 Render Capacity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Issue #3's candidate/finalization contract explicit in tests, then remove the CodeQL-reported integer-overflow risk while preserving rendered bytes.

**Architecture:** First add acceptance coverage for the existing internal transform/finalize split. Then change only the non-JSON branch of `render` in the internal mincer. Append the body and footer separately so Go manages slice growth; do not change JSON rendering or candidate acceptance.

**Tech Stack:** Go standard library; GitHub CodeQL and existing Go CI.

**Spec:** `docs/superpowers/specs/2026-11-15-issue-3-render-capacity-design.md`

## Global Constraints

- Keep the output bytes in the order: candidate body, then footer.
- Do not change the public API, footer text, JSON metadata, savings rule, or CCR storage behavior.
- Add no dependency.

## Review Focus

- Body and footer contents remain byte-for-byte unchanged: verify the existing mincer tests and the full Go test suite.
- Rejected candidates do not write to CCR, and accepted candidates write the exact original once: assert store puts and retrieve the original.
- Transform candidates carry no CCR pointer: verify the code-fold candidate before finalization contains no retrieval footer or ID.
- The manual `len(body) + len(footer)` calculation is gone: verify the CodeQL check on the PR.
- JSON output still takes the existing JSON rendering branch: verify the full Go test suite.

---

### Task 1: Prove the candidate and finalization boundary

**Files:**
- Modify: `internal/compression/mincer/mincer_test.go`.

**Interfaces:**
- Consumes: `mincer.Transform(context.Context, []byte, mincer.TransformOptions) mincer.Candidate`, `mincer.Finalize([]byte, mincer.Candidate, *ccr.Store) mincer.Result`, and `ccr.Store.Stats`/`Get`.
- Produces: tests proving candidates have no retrieval pointer, rejected candidates do not write, and accepted candidates store exact original bytes once.

- [x] **Step 1: Add `TestTransformProducesCandidateWithoutRetrievalMetadata`.** Use a code fixture with a folded body; assert `Changed`, `KindCode`, a fold marker, and no `ccr_` reference or retrieval footer.
- [x] **Step 2: Add `TestFinalizeRejectedCandidateDoesNotWriteCCR`.** Use a candidate below the 10% body-savings threshold; assert passthrough, empty `CCRID`, and zero store puts.
- [x] **Step 3: Add `TestFinalizeAcceptedCandidateStoresOriginalOnce`.** Use a long original and short candidate; assert shortened output, non-empty `CCRID`, exactly one store put, and byte-identical retrieval.
- [x] **Step 4: Run `go test ./internal/compression/mincer`.** These tests document and verify the Issue #3 contract already present in the PR.

### Task 2: Remove manual capacity addition in footer rendering

**Files:**
- Modify: `internal/compression/mincer/mincer.go` in `render`, at the `make` and append operations.

**Interfaces:**
- Consumes: existing `render(body []byte, kind router.Kind, id string, ratio pq.RationalPQ, original []byte) ([]byte, bool)` inputs.
- Produces: the same `[]byte` result and `bool`; output remains body followed by footer.

- [x] **Step 1: Record the failing static check.** The current PR's CodeQL check reports a possible overflow at the combined capacity expression `len(body)+len(footer)`.
- [x] **Step 2: Change slice construction.** Initialize output from the body and append the footer without computing a combined length or capacity.
- [x] **Step 3: Run focused Go tests.** Run `go test ./internal/compression/mincer` and confirm it passes.
- [x] **Step 4: Run the full Go suite.** Run `go test ./...`; mincer packages must pass. The known CCR mode assertion may fail on Windows because it is unchanged from `origin/main`.
- [x] **Step 5: Run static and formatting checks.** Run `gofmt` on the changed Go file, then `go vet ./...` and `git diff --check`.
- [x] **Step 6: Commit the implementation and plan artifacts to the existing PR branch.** Use a message that describes the candidate/finalization coverage and removal of unchecked buffer capacity arithmetic.
- [ ] **Step 7: Push the PR branch and inspect GitHub checks.** Confirm CodeQL no longer reports the allocation-size overflow and the Go checks pass.
