package mincer

import (
	"regexp"
	"strings"

	"mince/internal/compression/ternary"
)

var (
	ansiStripRE = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]")
	anomalyRE   = regexp.MustCompile(`(?i)\b(error|fatal|panic|failed|failure|exception|traceback)\b|^panic:|\bpanic:`)
	stackRE     = regexp.MustCompile(`^\s+(at |goroutine \d+|[A-Za-z0-9_./*()\[\]-]+\(|/[A-Za-z0-9_./-]+)`)
)

// minceLog collapses repetitive non-anomaly runs while preserving every
// anomaly line and its stack context.
func minceLog(content []byte) ([]byte, bool) {
	clean := ansiStripRE.ReplaceAll(content, nil)
	lines := strings.Split(strings.ReplaceAll(string(clean), "\r\n", "\n"), "\n")
	if len(lines) < 10 {
		return nil, false
	}

	keep := make([]bool, len(lines))
	anomaly := make([]bool, len(lines))
	for i, ln := range lines {
		if anomalyRE.MatchString(ln) || stackRE.MatchString(ln) {
			anomaly[i] = true
			keep[i] = true
			for j := i + 1; j <= i+8 && j < len(lines); j++ {
				keep[j] = true
			}
		}
	}

	var out []string
	changed := false
	for i := 0; i < len(lines); {
		if keep[i] || lines[i] == "" {
			out = append(out, lines[i])
			i++
			continue
		}
		j := i + 1
		prev := ternary.SimHash64(tokens([]byte(lines[i])))
		for j < len(lines) && !keep[j] && lines[j] != "" {
			cur := ternary.SimHash64(tokens([]byte(lines[j])))
			if ternary.DotBitboard(prev, cur) < 48 {
				break
			}
			prev = cur
			j++
		}
		run := j - i
		if run >= 3 {
			out = append(out, lines[i], "[...repeated "+itoa(run-1)+"x...]")
			changed = true
		} else {
			out = append(out, lines[i:j]...)
		}
		i = j
	}
	if !changed {
		return nil, false
	}
	return []byte(strings.Join(out, "\n")), true
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
