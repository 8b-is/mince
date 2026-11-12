package mincer

import (
	"regexp"
	"strings"
)

var keepLineRE = regexp.MustCompile(`^\s*(package|import|from|use|#include|module|namespace)\b|` +
	`^\s*(pub\s+)?(async\s+)?(fn|func|def|class|struct|interface|trait|impl|enum|type|const|var|let|static)\b|` +
	`^\s*(public|private|protected|internal|export|extern|inline)\b|` +
	`^\s*@|^\s*//|^\s*#|^\s*/\*|^\s*\*`)

// minceCode keeps declarations, imports and top-level structure while folding
// dense indented bodies into a reversible stub.
func minceCode(content []byte, id string) ([]byte, bool) {
	lines := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
	if len(lines) < 20 {
		return nil, false
	}
	var out []string
	changed := false
	for i := 0; i < len(lines); {
		ln := lines[i]
		trimmed := strings.TrimSpace(ln)
		indented := len(ln) > 0 && (ln[0] == ' ' || ln[0] == '\t')
		if trimmed == "" || !indented || keepLineRE.MatchString(ln) {
			out = append(out, ln)
			i++
			continue
		}
		j := i + 1
		for j < len(lines) {
			next := lines[j]
			nextTrim := strings.TrimSpace(next)
			nextIndented := len(next) > 0 && (next[0] == ' ' || next[0] == '\t')
			if nextTrim == "" || !nextIndented || keepLineRE.MatchString(next) {
				break
			}
			j++
		}
		run := j - i
		if run >= 4 {
			out = append(out, "[...minced L"+itoa(i+1)+"-L"+itoa(j)+" -> ccr_"+id+"...]")
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
