// Package aligner rewrites only eligible tool-result turns, leaving system
// prompts, tool schemas and the protected tail byte-for-byte untouched so
// provider KV caches keep hitting.
package aligner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"mince/internal/compression/mincer"
	"mince/internal/storage/ccr"
)

// Options controls alignment.
type Options struct {
	MinBytes         int
	ProtectTailTurns int
	Store            *ccr.Store
}

// Stats reports what one alignment changed.
type Stats struct {
	Minced     int
	RawBytes   int
	OutBytes   int
	SavedBytes int
}

// Align auto-detects the provider envelope and dispatches accordingly.
func Align(ctx context.Context, body []byte, opts Options) ([]byte, Stats, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return body, Stats{}, err
	}
	if _, ok := top["system"]; ok {
		return AlignAnthropic(ctx, body, opts)
	}
	var msgs []json.RawMessage
	if raw, ok := top["messages"]; ok {
		if err := json.Unmarshal(raw, &msgs); err == nil {
			for _, m := range msgs {
				var probe struct {
					Role string `json:"role"`
				}
				_ = json.Unmarshal(m, &probe)
				if probe.Role == "tool" {
					return AlignOpenAI(ctx, body, opts)
				}
			}
		}
	}
	return AlignAnthropic(ctx, body, opts)
}

// AlignAnthropic compresses eligible tool_result blocks in /v1/messages.
func AlignAnthropic(ctx context.Context, body []byte, opts Options) ([]byte, Stats, error) {
	stats := Stats{RawBytes: len(body), OutBytes: len(body)}
	start, end, ok := findValueSpan(body, "messages")
	if !ok {
		return body, stats, nil
	}
	msgs, err := splitRawArray(body[start:end])
	if err != nil {
		return body, stats, err
	}
	protect := opts.ProtectTailTurns
	if protect < 0 {
		protect = 0
	}
	limit := len(msgs) - protect
	if limit < 0 {
		limit = 0
	}
	for i := 0; i < limit; i++ {
		next, changed := compressAnthropicMessage(ctx, msgs[i], opts)
		if changed {
			msgs[i] = next
			stats.Minced++
		}
	}
	if stats.Minced == 0 {
		return body, stats, nil
	}
	out := spliceArray(body, start, end, msgs)
	stats.OutBytes = len(out)
	stats.SavedBytes = len(body) - len(out)
	if stats.SavedBytes < 0 {
		return body, Stats{RawBytes: len(body), OutBytes: len(body)}, nil
	}
	return out, stats, nil
}

// AlignOpenAI compresses eligible role:"tool" messages in chat completions.
func AlignOpenAI(ctx context.Context, body []byte, opts Options) ([]byte, Stats, error) {
	stats := Stats{RawBytes: len(body), OutBytes: len(body)}
	start, end, ok := findValueSpan(body, "messages")
	if !ok {
		return body, stats, nil
	}
	msgs, err := splitRawArray(body[start:end])
	if err != nil {
		return body, stats, err
	}
	protect := opts.ProtectTailTurns
	if protect < 0 {
		protect = 0
	}
	limit := len(msgs) - protect
	if limit < 0 {
		limit = 0
	}
	for i := 0; i < limit; i++ {
		next, changed := compressOpenAIMessage(ctx, msgs[i], opts)
		if changed {
			msgs[i] = next
			stats.Minced++
		}
	}
	if stats.Minced == 0 {
		return body, stats, nil
	}
	out := spliceArray(body, start, end, msgs)
	stats.OutBytes = len(out)
	stats.SavedBytes = len(body) - len(out)
	if stats.SavedBytes < 0 {
		return body, Stats{RawBytes: len(body), OutBytes: len(body)}, nil
	}
	return out, stats, nil
}

func minceText(ctx context.Context, s string, opts Options) (string, bool) {
	res := mincer.Mince(ctx, []byte(s), mincer.Options{
		MinBytes: opts.MinBytes,
		Store:    opts.Store,
	})
	if res.Passthrough {
		return s, false
	}
	return string(res.Minced), true
}

func compressAnthropicMessage(ctx context.Context, raw json.RawMessage, opts Options) (json.RawMessage, bool) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return raw, false
	}
	content, ok := m["content"]
	if !ok || len(content) == 0 || content[0] != '[' {
		return raw, false
	}
	var blocks []json.RawMessage
	if err := json.Unmarshal(content, &blocks); err != nil {
		return raw, false
	}
	changed := false
	for i, br := range blocks {
		var bm map[string]json.RawMessage
		if err := json.Unmarshal(br, &bm); err != nil {
			continue
		}
		var typ string
		_ = json.Unmarshal(bm["type"], &typ)
		if typ != "tool_result" {
			continue
		}
		cRaw, ok := bm["content"]
		if !ok || len(cRaw) == 0 {
			continue
		}
		if cRaw[0] == '"' {
			var s string
			if err := json.Unmarshal(cRaw, &s); err != nil {
				continue
			}
			if out, ok := minceText(ctx, s, opts); ok {
				nb, _ := json.Marshal(out)
				bm["content"] = nb
				blocks[i] = marshalRawMap(bm)
				changed = true
			}
			continue
		}
		if cRaw[0] != '[' {
			continue
		}
		var inner []json.RawMessage
		if err := json.Unmarshal(cRaw, &inner); err != nil {
			continue
		}
		innerChanged := false
		for j, ir := range inner {
			var im map[string]json.RawMessage
			if err := json.Unmarshal(ir, &im); err != nil {
				continue
			}
			var ityp string
			_ = json.Unmarshal(im["type"], &ityp)
			if ityp != "text" {
				continue
			}
			var txt string
			if err := json.Unmarshal(im["text"], &txt); err != nil {
				continue
			}
			if out, ok := minceText(ctx, txt, opts); ok {
				nb, _ := json.Marshal(out)
				im["text"] = nb
				inner[j] = marshalRawMap(im)
				innerChanged = true
			}
		}
		if innerChanged {
			nb, _ := json.Marshal(inner)
			bm["content"] = nb
			blocks[i] = marshalRawMap(bm)
			changed = true
		}
	}
	if !changed {
		return raw, false
	}
	nb, _ := json.Marshal(blocks)
	m["content"] = nb
	return marshalRawMap(m), true
}

func compressOpenAIMessage(ctx context.Context, raw json.RawMessage, opts Options) (json.RawMessage, bool) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return raw, false
	}
	var role string
	_ = json.Unmarshal(m["role"], &role)
	if role != "tool" {
		return raw, false
	}
	cRaw, ok := m["content"]
	if !ok || len(cRaw) == 0 {
		return raw, false
	}
	if cRaw[0] == '"' {
		var s string
		if err := json.Unmarshal(cRaw, &s); err != nil {
			return raw, false
		}
		out, ok := minceText(ctx, s, opts)
		if !ok {
			return raw, false
		}
		nb, _ := json.Marshal(out)
		m["content"] = nb
		return marshalRawMap(m), true
	}
	if cRaw[0] != '[' {
		return raw, false
	}
	var parts []json.RawMessage
	if err := json.Unmarshal(cRaw, &parts); err != nil {
		return raw, false
	}
	changed := false
	for i, pr := range parts {
		var pm map[string]json.RawMessage
		if err := json.Unmarshal(pr, &pm); err != nil {
			continue
		}
		var typ string
		_ = json.Unmarshal(pm["type"], &typ)
		if typ != "text" {
			continue
		}
		var txt string
		if err := json.Unmarshal(pm["text"], &txt); err != nil {
			continue
		}
		if out, ok := minceText(ctx, txt, opts); ok {
			nb, _ := json.Marshal(out)
			pm["text"] = nb
			parts[i] = marshalRawMap(pm)
			changed = true
		}
	}
	if !changed {
		return raw, false
	}
	nb, _ := json.Marshal(parts)
	m["content"] = nb
	return marshalRawMap(m), true
}

func marshalRawMap(m map[string]json.RawMessage) json.RawMessage {
	b, _ := json.Marshal(m)
	return b
}

// findValueSpan locates the exact byte span of a top-level key's value.
func findValueSpan(body []byte, key string) (int, int, bool) {
	dec := json.NewDecoder(bytes.NewReader(body))
	tok, err := dec.Token()
	if err != nil {
		return 0, 0, false
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return 0, 0, false
	}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return 0, 0, false
		}
		k, _ := kt.(string)
		pos := int(dec.InputOffset())
		for pos < len(body) && isSpace(body[pos]) {
			pos++
		}
		if pos < len(body) && body[pos] == ':' {
			pos++
		}
		for pos < len(body) && isSpace(body[pos]) {
			pos++
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return 0, 0, false
		}
		end := int(dec.InputOffset())
		if k == key {
			return pos, end, true
		}
	}
	return 0, 0, false
}

// splitRawArray splits a JSON array without re-encoding its elements.
func splitRawArray(b []byte) ([]json.RawMessage, error) {
	i := 0
	for i < len(b) && isSpace(b[i]) {
		i++
	}
	if i >= len(b) || b[i] != '[' {
		return nil, errors.New("aligner: messages is not an array")
	}
	i++
	var out []json.RawMessage
	for {
		for i < len(b) && (isSpace(b[i]) || b[i] == ',') {
			i++
		}
		if i >= len(b) {
			return nil, errors.New("aligner: unterminated array")
		}
		if b[i] == ']' {
			return out, nil
		}
		start := i
		depth := 0
		inStr := false
		esc := false
		for i < len(b) {
			c := b[i]
			if inStr {
				if esc {
					esc = false
				} else if c == '\\' {
					esc = true
				} else if c == '"' {
					inStr = false
				}
				i++
				continue
			}
			switch c {
			case '"':
				inStr = true
			case '{', '[':
				depth++
			case '}', ']':
				if depth == 0 {
					// end of enclosing array
					out = append(out, json.RawMessage(b[start:i]))
					return out, nil
				}
				depth--
			case ',':
				if depth == 0 {
					out = append(out, json.RawMessage(b[start:i]))
					i++
					goto next
				}
			}
			i++
		}
		return nil, errors.New("aligner: unterminated element")
	next:
	}
}

func spliceArray(body []byte, start, end int, items []json.RawMessage) []byte {
	var arr bytes.Buffer
	arr.WriteByte('[')
	for i, it := range items {
		if i > 0 {
			arr.WriteByte(',')
		}
		arr.Write(it)
	}
	arr.WriteByte(']')
	out := make([]byte, 0, len(body)-end+start+arr.Len())
	out = append(out, body[:start]...)
	out = append(out, arr.Bytes()...)
	out = append(out, body[end:]...)
	return out
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}
