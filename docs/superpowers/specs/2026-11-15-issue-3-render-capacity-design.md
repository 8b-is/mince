# Issue #3: Safe output buffer growth

## Intent

PR #19 renders a shortened candidate and its CCR footer. The current renderer
preallocates the output with `len(body) + len(footer)`. CodeQL flags that size
addition because a sufficiently large value could overflow an `int` before the
allocation.

## Goal

Build the same output without manually adding the two lengths, removing the
CodeQL finding while preserving existing rendering behavior.

## Non-goals

- Change the footer format, JSON metadata, candidate acceptance rules, or CCR
  behavior.
- Change the public API or the scope of Issue #3.

## Constraints

- Keep the implementation in the existing Go renderer.
- Preserve the exact bytes and order: candidate body followed by the footer.
- Use standard Go slice operations; add no dependency.

## Approaches considered

1. **Append body and footer separately (selected).** Let Go grow the slice as
   needed. This removes the unchecked length addition and keeps the renderer
   simple. The output bytes stay the same.
2. **Check the sum before allocating.** Add an integer-overflow guard and keep
   one preallocation. This can reject overflow explicitly, but adds custom size
   arithmetic for a capacity optimization that Go's append already manages.

The author selected approach 1 by replying “yes please” to the append-based
fix proposal.

## Implementation boundary

Update `internal/compression/mincer/mincer.go` in `render` to append the body
and footer without computing their combined capacity. Add or adjust focused
coverage only if it can meaningfully detect a regression; use the repository's
CodeQL and Go CI checks to verify the reported finding is gone and behavior is
unchanged.
