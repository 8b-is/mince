// Package mincer implements the local, reversible content compressors.
package mincer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"unicode"

	"github.com/8b-is/mince/internal/compression/router"
	"github.com/8b-is/mince/internal/pq"
	"github.com/8b-is/mince/internal/storage/ccr"
)

// Options controls one mince operation.
type Options struct {
	Kind        router.Kind
	MinBytes    int
	Store       *ccr.Store
	MaxOutliers int
}

// TransformOptions controls deterministic candidate generation. It does not
// include a store because transformation must not persist or identify input.
type TransformOptions struct {
	Kind        router.Kind
	MinBytes    int
	MaxOutliers int
}

// Candidate is the output of a transformation stage before CCR finalization.
// Content may be a shortened proposal, but it is not accepted until Finalize
// adds retrieval metadata and confirms the final output is smaller.
type Candidate struct {
	Content []byte
	Kind    router.Kind
	Changed bool
}

// Result is the outcome of Mince.
type Result struct {
	Minced      []byte
	CCRID       string
	RawBytes    int
	MincedBytes int
	Kind        router.Kind
	Saved       pq.RationalPQ
	Passthrough bool
}

var (
	defaultStoreOnce sync.Once
	defaultStore     *ccr.Store
)

func storeFor(s *ccr.Store) *ccr.Store {
	if s != nil {
		return s
	}
	defaultStoreOnce.Do(func() { defaultStore = ccr.NewMemory() })
	return defaultStore
}

// Transform creates a deterministic candidate without reading or writing CCR.
// Small, cancelled, unsupported, or unchanged inputs return an unchanged
// candidate. The caller must pass the original and candidate to Finalize before
// forwarding a shortened result.
func Transform(ctx context.Context, content []byte, opts TransformOptions) Candidate {
	candidate := Candidate{Content: content, Kind: router.KindText}
	rawLen := len(content)
	minBytes := opts.MinBytes
	if minBytes <= 0 {
		minBytes = 1024
	}
	if rawLen < minBytes {
		return candidate
	}
	select {
	case <-ctx.Done():
		return candidate
	default:
	}

	kind := opts.Kind
	if kind == router.KindText {
		kind = router.Classify(content)
	}
	candidate.Kind = kind
	if kind == router.KindText {
		return candidate
	}

	var body []byte
	var changed bool
	switch kind {
	case router.KindJSON:
		body, changed = minceJSON(content, opts.MaxOutliers)
	case router.KindLog:
		body, changed = minceLog(content)
	case router.KindCode:
		body, changed = minceCode(content)
	}
	if !changed || len(body) == 0 {
		return candidate
	}

	candidate.Content = body
	candidate.Changed = true
	return candidate
}

// Finalize adds retrieval metadata and stores the exact original only if the
// fully rendered output is shorter than the input and meets the savings rule.
// Rejected candidates and storage failures pass through without creating CCR
// records.
func Finalize(original []byte, candidate Candidate, store *ccr.Store) Result {
	rawLen := len(original)
	result := Result{
		Minced:      original,
		RawBytes:    rawLen,
		MincedBytes: rawLen,
		Kind:        candidate.Kind,
		Passthrough: true,
	}
	if !candidate.Changed || rawLen == 0 || len(candidate.Content) == 0 {
		return result
	}

	saved := int64(rawLen - len(candidate.Content))
	ratio := pq.FromRatio(saved, int64(rawLen))
	if ratio.Cmp(pq.New(1, 10)) < 0 {
		return result
	}

	// CCR currently returns 12 hex characters, or 16 when it detects a
	// truncated-hash collision. Preflight with the longer ID so the real ID
	// cannot make an accepted result larger after storage.
	placeholderID := "ccr_0000000000000000"
	planned, ok := render(candidate.Content, candidate.Kind, placeholderID, ratio, original)
	if !ok || len(planned) >= rawLen {
		return result
	}

	id, err := storeFor(store).Put(original)
	if err != nil {
		return result
	}
	out, ok := render(candidate.Content, candidate.Kind, "ccr_"+id, ratio, original)
	if !ok {
		// The preflight used the same body and format, so this should be
		// unreachable unless rendering rules change. Preserve passthrough if so.
		return result
	}
	result.Minced = out
	result.CCRID = "ccr_" + id
	result.MincedBytes = len(out)
	result.Saved = pq.FromRatio(int64(rawLen-len(out)), int64(rawLen))
	result.Passthrough = false
	return result
}

func render(body []byte, kind router.Kind, id string, ratio pq.RationalPQ, original []byte) ([]byte, bool) {
	if kind == router.KindJSON {
		// Keep JSON syntactically valid: carry the retrieval pointer in-band.
		var obj map[string]any
		if err := json.Unmarshal(body, &obj); err != nil || obj == nil {
			return nil, false
		}
		obj["_ccr"] = id
		obj["_saved"] = ratio.String()
		encoded, err := json.Marshal(obj)
		if err != nil {
			return nil, false
		}
		return encoded, true
	}

	footer := fmt.Sprintf(
		"\n[mince:ccr id=%s saved=%s (%s) raw_lines=%d | use mince_retrieve(id, query, offset, limit) for exact raw content]",
		id, ratio.String(), ratio.Percent(), lineCount(original),
	)
	out := make([]byte, 0, len(body)+len(footer))
	out = append(out, body...)
	out = append(out, footer...)
	return out, true
}

// Mince preserves the legacy one-call flow by running Transform then Finalize.
func Mince(ctx context.Context, content []byte, opts Options) Result {
	candidate := Transform(ctx, content, TransformOptions{
		Kind:        opts.Kind,
		MinBytes:    opts.MinBytes,
		MaxOutliers: opts.MaxOutliers,
	})
	return Finalize(content, candidate, opts.Store)
}

func lineCount(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	n := 1
	for _, c := range b {
		if c == '\n' {
			n++
		}
	}
	return n
}

// tokens splits text into lowercase alphanumeric tokens for signatures.
func tokens(b []byte) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range string(b) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			cur.WriteRune(unicode.ToLower(r))
		} else {
			flush()
		}
	}
	flush()
	return out
}
