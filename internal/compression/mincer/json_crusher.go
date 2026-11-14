package mincer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"mince/internal/compression/ternary"
)

const defaultOutliers = 3

// minceJSON collapses homogeneous object arrays to a schema plus head, tail
// and structural outliers. It returns changed=false when nothing was folded.
func minceJSON(content []byte, maxOutliers int) ([]byte, bool) {
	dec := json.NewDecoder(bytes.NewReader(content))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, false
	}
	if maxOutliers <= 0 {
		maxOutliers = defaultOutliers
	}

	switch t := v.(type) {
	case []any:
		if len(t) < 8 {
			return nil, false
		}
		summary := summarizeArray(t, maxOutliers)
		out, err := json.Marshal(summary)
		if err != nil {
			return nil, false
		}
		return out, true
	case map[string]any:
		changed := false
		for k, val := range t {
			arr, ok := val.([]any)
			if !ok || len(arr) < 8 {
				continue
			}
			encoded, err := json.Marshal(arr)
			if err != nil || len(encoded) < 2048 {
				continue
			}
			t[k] = summarizeArray(arr, maxOutliers)
			changed = true
		}
		if !changed {
			return nil, false
		}
		out, err := json.Marshal(t)
		if err != nil {
			return nil, false
		}
		return out, true
	default:
		return nil, false
	}
}

func summarizeArray(arr []any, maxOutliers int) map[string]any {
	schema := map[string]string{}
	sigs := make([]ternary.TritPlane64, len(arr))
	for i, item := range arr {
		enc, _ := json.Marshal(item)
		sigs[i] = ternary.SimHash64(tokens(enc))
		if m, ok := item.(map[string]any); ok {
			for k, val := range m {
				t := typeName(val)
				if prev, ok := schema[k]; ok && prev != t {
					schema[k] = "mixed"
				} else if !ok {
					schema[k] = t
				}
			}
		}
	}

	centroid := centroidOf(sigs)
	keep := map[int]bool{0: true, 1: true, len(arr) - 1: true}
	outliers := make([]map[string]any, 0, maxOutliers)
	// Semantic anomalies are never allowed to disappear, regardless of their
	// signature distance.
	for i, item := range arr {
		if keep[i] || !isAnomalyItem(item) {
			continue
		}
		if len(outliers) >= maxOutliers {
			break
		}
		outliers = append(outliers, map[string]any{"index": i, "item": item})
		keep[i] = true
	}
	type scored struct {
		idx  int
		dist int
	}
	var candidates []scored
	for i := range arr {
		if keep[i] {
			continue
		}
		candidates = append(candidates, scored{i, ternary.HammingDistance(centroid, sigs[i])})
	}
	sort.Slice(candidates, func(a, b int) bool {
		if candidates[a].dist == candidates[b].dist {
			return candidates[a].idx < candidates[b].idx
		}
		return candidates[a].dist > candidates[b].dist
	})
	for _, c := range candidates {
		if len(outliers) >= maxOutliers {
			break
		}
		outliers = append(outliers, map[string]any{"index": c.idx, "item": arr[c.idx]})
	}

	return map[string]any{
		"_mince":   "json_array",
		"count":    len(arr),
		"schema":   schema,
		"head":     []any{arr[0], arr[1]},
		"tail":     []any{arr[len(arr)-1]},
		"outliers": outliers,
		"note":     fmt.Sprintf("minced %d rows; use mince_retrieve for exact raw JSON", len(arr)),
	}
}

func centroidOf(sigs []ternary.TritPlane64) ternary.TritPlane64 {
	var c ternary.TritPlane64
	if len(sigs) == 0 {
		return c
	}
	for i := 0; i < 64; i++ {
		sum := 0
		for _, s := range sigs {
			sum += int(s.Get(i))
		}
		switch {
		case sum > 0:
			c.Set(i, ternary.Pos)
		case sum < 0:
			c.Set(i, ternary.Neg)
		}
	}
	return c
}

func isAnomalyItem(v any) bool {
	enc, err := json.Marshal(v)
	if err != nil {
		return false
	}
	s := strings.ToLower(string(enc))
	for _, k := range []string{`"error"`, `"fatal"`, "panic", "exception", "failed", "failure"} {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

func typeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "bool"
	case json.Number, float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return "unknown"
	}
}
