// Package router classifies bulky content deterministically before mincing.
package router

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
)

// Kind is the content family a mincer handles.
type Kind int

const (
	KindText Kind = iota
	KindJSON
	KindLog
	KindCode
)

func (k Kind) String() string {
	switch k {
	case KindJSON:
		return "json"
	case KindLog:
		return "log"
	case KindCode:
		return "code"
	default:
		return "text"
	}
}

// ParseKind maps a CLI name to a Kind. Unknown names yield KindText.
func ParseKind(s string) Kind {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "json":
		return KindJSON
	case "log":
		return KindLog
	case "code":
		return KindCode
	default:
		return KindText
	}
}

var (
	ansiRE  = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]")
	tsRE    = regexp.MustCompile(`(?m)^\s*\[?(\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}|\d{2}:\d{2}:\d{2})([.,]\d+)?\]?`)
	levelRE = regexp.MustCompile(`(?im)^\s*(\[?(trace|debug|info|warn|warning|error|fatal|panic)\]?)\b`)
)

// Classify inspects content and returns its dominant kind.
func Classify(content []byte) Kind {
	trimmed := bytes.TrimSpace(content)
	if len(trimmed) == 0 {
		return KindText
	}
	if (trimmed[0] == '{' || trimmed[0] == '[') && json.Valid(trimmed) {
		return KindJSON
	}
	lines := bytes.Count(content, []byte("\n")) + 1
	logHits := len(tsRE.FindAll(content, 4)) + len(levelRE.FindAll(content, 4))
	if ansiRE.Match(content) || (lines >= 3 && logHits >= 3) {
		return KindLog
	}
	if looksLikeCode(trimmed) {
		return KindCode
	}
	return KindText
}

func looksLikeCode(b []byte) bool {
	markers := [][]byte{
		[]byte("package "), []byte("import ("), []byte("import \""),
		[]byte("func "), []byte("pub fn "), []byte("fn "), []byte("def "),
		[]byte("#include"), []byte("use std::"), []byte("class "),
		[]byte("struct "), []byte("interface "), []byte("impl "),
		[]byte("function "), []byte("const "), []byte("let "),
	}
	hits := 0
	for _, m := range markers {
		if bytes.Contains(b, m) {
			hits++
			if hits >= 2 {
				return true
			}
		}
	}
	return false
}

// ApproxTokens is the cheap 4-bytes-per-token estimate used for thresholds.
func ApproxTokens(b []byte) int { return (len(b) + 3) / 4 }
