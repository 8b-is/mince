// Package mincer implements the local, reversible content compressors.
package mincer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"unicode"

	"mince/internal/ccr"
	"mince/internal/pq"
	"mince/internal/router"
)

// Options controls one mince operation.
type Options struct {
	Kind        router.Kind
	MinBytes    int
	Store       *ccr.Store
	MaxOutliers int
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

// Mince compresses content when it is large enough and the result saves at
// least 10%. Any failure or weak saving returns the original bytes verbatim.
func Mince(ctx context.Context, content []byte, opts Options) Result {
	rawLen := len(content)
	res := Result{Minced: content, RawBytes: rawLen, MincedBytes: rawLen, Passthrough: true}
	minBytes := opts.MinBytes
	if minBytes <= 0 {
		minBytes = 1024
	}
	if rawLen < minBytes {
		return res
	}
	select {
	case <-ctx.Done():
		return res
	default:
	}

	kind := opts.Kind
	if kind == router.KindText {
		kind = router.Classify(content)
	}
	res.Kind = kind
	if kind == router.KindText {
		return res
	}

	store := storeFor(opts.Store)
	id, err := store.Put(content)
	if err != nil {
		return res
	}

	var body []byte
	var changed bool
	switch kind {
	case router.KindJSON:
		body, changed = minceJSON(content, opts.MaxOutliers)
	case router.KindLog:
		body, changed = minceLog(content)
	case router.KindCode:
		body, changed = minceCode(content, id)
	}
	if !changed || len(body) == 0 {
		return res
	}

	saved := int64(rawLen - len(body))
	ratio := pq.FromRatio(saved, int64(rawLen))
	if ratio.Cmp(pq.New(1, 10)) < 0 {
		return res
	}

	footer := fmt.Sprintf(
		"\n[mince:ccr id=ccr_%s saved=%s (%s) raw_lines=%d | use mince_retrieve(id, query, offset, limit) for exact raw content]",
		id, ratio.String(), ratio.Percent(), lineCount(content),
	)
	out := make([]byte, 0, len(body)+len(footer))
	if kind == router.KindJSON {
		// Keep JSON syntactically valid: carry the retrieval pointer in-band.
		var obj map[string]any
		if json.Unmarshal(body, &obj) == nil {
			obj["_ccr"] = "ccr_" + id
			obj["_saved"] = ratio.String()
			if encoded, err := json.Marshal(obj); err == nil {
				body = encoded
			}
		}
	} else {
		body = append(body, footer...)
	}
	out = append(out, body...)
	if len(out) >= rawLen {
		return res
	}

	res.Minced = out
	res.CCRID = "ccr_" + id
	res.MincedBytes = len(out)
	res.Saved = pq.FromRatio(int64(rawLen-len(out)), int64(rawLen))
	res.Passthrough = false
	return res
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
