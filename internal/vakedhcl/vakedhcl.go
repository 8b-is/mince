// Package vakedhcl parses the mince.vkd.hcl capability contract: a small,
// strict HCL subset with blocks, labels, strings, integers, booleans, lists
// and FELIX heredocs. The parsed Config is the single source of truth for
// capability masks, budgets, wire agents, RALPH loops and salience engines.
package vakedhcl

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"mince/internal/litany"
	"mince/internal/pq"
)

// ValueKind identifies a parsed HCL literal.
type ValueKind uint8

const (
	ValueString ValueKind = iota + 1
	ValueInt
	ValueBool
	ValueList
)

// Value is a parsed HCL literal.
type Value struct {
	Kind ValueKind
	Str  string
	Int  int64
	Bool bool
	List []Value
}

// Block is a parsed HCL block.
type Block struct {
	Type   string
	Labels []string
	Attrs  map[string]Value
	Blocks []*Block
}

// File is a parsed HCL document.
type File struct {
	Blocks []*Block
}

// Config is the validated capability contract.
type Config struct {
	Version      string
	Capabilities map[string]Capability
	Salience     map[string]Salience
	WireAgents   map[string]WireAgent
	Ralph        map[string]Ralph
	Felix        map[string]FelixFunc
	Raw          *File
}

// Capability is one capability-scoped tool contract.
type Capability struct {
	Allow    []string
	MaxBytes int64
	Budget   pq.RationalPQ
	CapMask  uint64
}

// Salience describes a local salience engine and its verified weights.
type Salience struct {
	Model    string
	Tier     string
	SHA256   string
	Resident bool
}

// WireAgent describes one inline wire hook.
type WireAgent struct {
	Kind    string
	Budget  pq.RationalPQ
	Enabled bool
}

// Ralph describes one reconcile loop.
type Ralph struct {
	Every          int
	Replay         int
	MinImprovement pq.RationalPQ
	Policy         string
}

// FelixFunc is a compiled FELIX function contract.
type FelixFunc struct {
	Source string
	Budget pq.RationalPQ
}

// Parse parses HCL source into an unvalidated AST.
func Parse(src []byte) (*File, error) {
	p, err := newParser(src)
	if err != nil {
		return nil, err
	}
	f := &File{}
	for p.peek().kind != tokEOF {
		b, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		f.Blocks = append(f.Blocks, b)
	}
	return f, nil
}

// Load parses and validates a config file.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	f, err := Parse(b)
	if err != nil {
		return nil, err
	}
	return Decode(f)
}

// Decode validates a parsed file and projects it into Config.
func Decode(f *File) (*Config, error) {
	if f == nil {
		return nil, errors.New("vakedhcl: nil file")
	}
	c := &Config{
		Version:      "0.2.0",
		Capabilities: map[string]Capability{},
		Salience:     map[string]Salience{},
		WireAgents:   map[string]WireAgent{},
		Ralph:        map[string]Ralph{},
		Felix:        map[string]FelixFunc{},
		Raw:          f,
	}
	for _, b := range f.Blocks {
		switch b.Type {
		case "mince":
			if v, ok := b.Attrs["version"]; ok && v.Kind == ValueString {
				c.Version = v.Str
			}
		case "capability":
			if len(b.Labels) != 1 {
				return nil, errors.New("vakedhcl: capability needs exactly one label")
			}
			cap, err := decodeCapability(b)
			if err != nil {
				return nil, fmt.Errorf("capability %q: %w", b.Labels[0], err)
			}
			c.Capabilities[b.Labels[0]] = cap
		case "salience_engine":
			if len(b.Labels) != 1 {
				return nil, errors.New("vakedhcl: salience_engine needs exactly one label")
			}
			s, err := decodeSalience(b)
			if err != nil {
				return nil, fmt.Errorf("salience_engine %q: %w", b.Labels[0], err)
			}
			c.Salience[b.Labels[0]] = s
		case "wire_agent":
			if len(b.Labels) != 1 {
				return nil, errors.New("vakedhcl: wire_agent needs exactly one label")
			}
			w, err := decodeWireAgent(b)
			if err != nil {
				return nil, fmt.Errorf("wire_agent %q: %w", b.Labels[0], err)
			}
			c.WireAgents[b.Labels[0]] = w
		case "ralph_loop":
			if len(b.Labels) != 1 {
				return nil, errors.New("vakedhcl: ralph_loop needs exactly one label")
			}
			r, err := decodeRalph(b)
			if err != nil {
				return nil, fmt.Errorf("ralph_loop %q: %w", b.Labels[0], err)
			}
			c.Ralph[b.Labels[0]] = r
		case "felix":
			if len(b.Labels) != 1 {
				return nil, errors.New("vakedhcl: felix needs exactly one label")
			}
			fn, err := decodeFelix(b)
			if err != nil {
				return nil, fmt.Errorf("felix %q: %w", b.Labels[0], err)
			}
			c.Felix[b.Labels[0]] = fn
		}
	}
	if len(c.Capabilities) == 0 && len(c.Salience) == 0 && len(c.WireAgents) == 0 && len(c.Ralph) == 0 && len(c.Felix) == 0 {
		return nil, errors.New("vakedhcl: config declares no capabilities or engines")
	}
	return c, nil
}

func decodeCapability(b *Block) (Capability, error) {
	c := Capability{Budget: pq.New(0, 1)}
	if v, ok := b.Attrs["allow"]; ok {
		list, err := stringList(v)
		if err != nil {
			return c, fmt.Errorf("allow: %w", err)
		}
		c.Allow = list
	}
	if v, ok := b.Attrs["max_bytes"]; ok {
		if v.Kind != ValueInt || v.Int < 0 {
			return c, errors.New("max_bytes must be a non-negative integer")
		}
		c.MaxBytes = v.Int
	}
	if v, ok := b.Attrs["budget"]; ok {
		r, err := budgetValue(v)
		if err != nil {
			return c, err
		}
		if r.Sign() < 0 || r.Cmp(pq.New(1, 1)) > 0 {
			return c, errors.New("budget must be in [0,1]")
		}
		c.Budget = r
	}
	if v, ok := b.Attrs["caps"]; ok {
		list, err := stringList(v)
		if err != nil {
			return c, fmt.Errorf("caps: %w", err)
		}
		for _, name := range list {
			mask, ok := capBit(name)
			if !ok {
				return c, fmt.Errorf("unknown capability %q", name)
			}
			c.CapMask |= mask
		}
	}
	if c.CapMask == 0 {
		for _, tool := range c.Allow {
			c.CapMask |= inferCap(tool)
		}
	}
	if c.CapMask == 0 {
		return c, errors.New("capability grants no bits")
	}
	return c, nil
}

func decodeSalience(b *Block) (Salience, error) {
	s := Salience{Tier: "cpu", Resident: true}
	if v, ok := b.Attrs["model"]; ok && v.Kind == ValueString {
		s.Model = v.Str
	}
	if v, ok := b.Attrs["tier"]; ok && v.Kind == ValueString {
		s.Tier = v.Str
	}
	if v, ok := b.Attrs["sha256"]; ok && v.Kind == ValueString {
		s.SHA256 = strings.ToLower(v.Str)
	}
	if v, ok := b.Attrs["resident"]; ok && v.Kind == ValueBool {
		s.Resident = v.Bool
	}
	if s.Model == "" {
		return s, errors.New("model is required")
	}
	if s.SHA256 != "" && len(s.SHA256) != 64 {
		return s, errors.New("sha256 must be 64 hex characters")
	}
	return s, nil
}

func decodeWireAgent(b *Block) (WireAgent, error) {
	w := WireAgent{Budget: pq.New(1, 8), Enabled: true}
	if v, ok := b.Attrs["kind"]; ok && v.Kind == ValueString {
		w.Kind = v.Str
	}
	if v, ok := b.Attrs["budget"]; ok {
		r, err := budgetValue(v)
		if err != nil {
			return w, err
		}
		w.Budget = r
	}
	if v, ok := b.Attrs["enabled"]; ok && v.Kind == ValueBool {
		w.Enabled = v.Bool
	}
	switch w.Kind {
	case "cache_guard", "tier_shunt", "panic_splice", "inline_ccr":
	default:
		return w, fmt.Errorf("unknown wire agent kind %q", w.Kind)
	}
	if w.Budget.Sign() < 0 || w.Budget.Cmp(pq.New(1, 1)) > 0 {
		return w, errors.New("budget must be in [0,1]")
	}
	return w, nil
}

func decodeRalph(b *Block) (Ralph, error) {
	r := Ralph{Every: 64, Replay: 64, MinImprovement: pq.New(1, 20), Policy: "bandit_pq"}
	if v, ok := b.Attrs["every"]; ok && v.Kind == ValueInt {
		r.Every = int(v.Int)
	}
	if v, ok := b.Attrs["replay"]; ok && v.Kind == ValueInt {
		r.Replay = int(v.Int)
	}
	if v, ok := b.Attrs["min_improvement"]; ok {
		x, err := budgetValue(v)
		if err != nil {
			return r, err
		}
		r.MinImprovement = x
	}
	if v, ok := b.Attrs["policy"]; ok && v.Kind == ValueString {
		r.Policy = v.Str
	}
	if r.Every <= 0 || r.Replay <= 0 {
		return r, errors.New("every and replay must be positive")
	}
	if r.MinImprovement.Sign() <= 0 {
		return r, errors.New("min_improvement must be positive")
	}
	if r.Policy != "bandit_pq" && r.Policy != "linucb_pq" {
		return r, fmt.Errorf("unknown policy %q", r.Policy)
	}
	return r, nil
}

func decodeFelix(b *Block) (FelixFunc, error) {
	f := FelixFunc{Budget: pq.New(1, 100)}
	if v, ok := b.Attrs["source"]; ok && v.Kind == ValueString {
		f.Source = v.Str
	}
	if v, ok := b.Attrs["budget"]; ok {
		r, err := budgetValue(v)
		if err != nil {
			return f, err
		}
		f.Budget = r
	}
	if strings.TrimSpace(f.Source) == "" {
		return f, errors.New("source is required")
	}
	return f, nil
}

func budgetValue(v Value) (pq.RationalPQ, error) {
	if v.Kind == ValueInt {
		return pq.FromInt(v.Int), nil
	}
	if v.Kind != ValueString {
		return pq.RationalPQ{}, errors.New("budget must be \"p/q\" or an integer")
	}
	parts := strings.Split(v.Str, "/")
	if len(parts) == 1 {
		n, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
		if err != nil {
			return pq.RationalPQ{}, fmt.Errorf("bad budget %q", v.Str)
		}
		return pq.FromInt(n), nil
	}
	if len(parts) != 2 {
		return pq.RationalPQ{}, fmt.Errorf("bad budget %q", v.Str)
	}
	n, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil {
		return pq.RationalPQ{}, fmt.Errorf("bad budget numerator %q", parts[0])
	}
	d, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64)
	if err != nil || d == 0 {
		return pq.RationalPQ{}, fmt.Errorf("bad budget denominator %q", parts[1])
	}
	return pq.New(n, d), nil
}

func stringList(v Value) ([]string, error) {
	if v.Kind != ValueList {
		return nil, errors.New("expected a list")
	}
	out := make([]string, 0, len(v.List))
	for _, x := range v.List {
		if x.Kind != ValueString {
			return nil, errors.New("list items must be strings")
		}
		out = append(out, x.Str)
	}
	return out, nil
}

func capBit(name string) (uint64, bool) {
	switch strings.ToLower(name) {
	case "retrieve", "mince_retrieve":
		return litany.CapRetrieve, true
	case "felix", "felix_eval":
		return litany.CapFelix, true
	case "model", "ollama":
		return litany.CapModel, true
	case "network":
		return litany.CapNetwork, true
	case "fs_read", "read":
		return litany.CapFSRead, true
	case "fs_write", "write":
		return litany.CapFSWrite, true
	case "exec":
		return litany.CapExec, true
	case "ralph":
		return litany.CapRalph, true
	case "telemetry":
		return litany.CapTelemetry, true
	default:
		return 0, false
	}
}

func inferCap(tool string) uint64 {
	switch tool {
	case "mince_retrieve", "mince_stats", "mince_compress":
		return litany.CapRetrieve
	case "felix_eval":
		return litany.CapFelix
	default:
		return 0
	}
}

// --- lexer ---

type tokenKind uint8

const (
	tokEOF tokenKind = iota
	tokIdent
	tokString
	tokNumber
	tokBool
	tokLBrace
	tokRBrace
	tokLBrack
	tokRBrack
	tokEq
	tokComma
)

type token struct {
	kind tokenKind
	text string
	line int
}

type lexer struct {
	src  []byte
	pos  int
	line int
}

func newLexer(src []byte) *lexer { return &lexer{src: src, line: 1} }

func (l *lexer) next() (token, error) {
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == '\n':
			l.line++
			l.pos++
		case c == ' ' || c == '\t' || c == '\r':
			l.pos++
		case c == '#':
			l.skipLine()
		case c == '/' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '/':
			l.skipLine()
		case c == '/' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '*':
			l.pos += 2
			for l.pos+1 < len(l.src) && !(l.src[l.pos] == '*' && l.src[l.pos+1] == '/') {
				if l.src[l.pos] == '\n' {
					l.line++
				}
				l.pos++
			}
			l.pos += 2
		default:
			return l.lexToken()
		}
	}
	return token{kind: tokEOF, line: l.line}, nil
}

func (l *lexer) skipLine() {
	for l.pos < len(l.src) && l.src[l.pos] != '\n' {
		l.pos++
	}
}

func (l *lexer) lexToken() (token, error) {
	start := l.pos
	line := l.line
	c := l.src[l.pos]
	switch c {
	case '{':
		l.pos++
		return token{tokLBrace, "{", line}, nil
	case '}':
		l.pos++
		return token{tokRBrace, "}", line}, nil
	case '[':
		l.pos++
		return token{tokLBrack, "[", line}, nil
	case ']':
		l.pos++
		return token{tokRBrack, "]", line}, nil
	case '=':
		l.pos++
		return token{tokEq, "=", line}, nil
	case ',':
		l.pos++
		return token{tokComma, ",", line}, nil
	case '"', '\'':
		return l.lexString(c)
	case '<':
		if l.pos+1 < len(l.src) && l.src[l.pos+1] == '<' {
			return l.lexHeredoc()
		}
	}
	if isIdentStart(c) {
		for l.pos < len(l.src) && isIdentPart(l.src[l.pos]) {
			l.pos++
		}
		text := string(l.src[start:l.pos])
		if text == "true" || text == "false" {
			return token{tokBool, text, line}, nil
		}
		return token{tokIdent, text, line}, nil
	}
	if c == '-' || (c >= '0' && c <= '9') {
		l.pos++
		for l.pos < len(l.src) && l.src[l.pos] >= '0' && l.src[l.pos] <= '9' {
			l.pos++
		}
		return token{tokNumber, string(l.src[start:l.pos]), line}, nil
	}
	return token{}, fmt.Errorf("vakedhcl: unexpected %q on line %d", c, line)
}

func (l *lexer) lexString(quote byte) (token, error) {
	line := l.line
	l.pos++
	var b strings.Builder
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == '\\' && l.pos+1 < len(l.src) {
			l.pos++
			switch l.src[l.pos] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '\\', '"', '\'':
				b.WriteByte(l.src[l.pos])
			default:
				b.WriteByte(l.src[l.pos])
			}
			l.pos++
			continue
		}
		if c == quote {
			l.pos++
			return token{tokString, b.String(), line}, nil
		}
		if c == '\n' {
			line++
		}
		b.WriteByte(c)
		l.pos++
	}
	return token{}, fmt.Errorf("vakedhcl: unterminated string on line %d", line)
}

func (l *lexer) lexHeredoc() (token, error) {
	line := l.line
	l.pos += 2
	start := l.pos
	for l.pos < len(l.src) && isIdentPart(l.src[l.pos]) {
		l.pos++
	}
	marker := string(l.src[start:l.pos])
	if marker == "" {
		return token{}, fmt.Errorf("vakedhcl: heredoc missing marker on line %d", line)
	}
	for l.pos < len(l.src) && l.src[l.pos] != '\n' {
		l.pos++
	}
	if l.pos < len(l.src) {
		l.pos++
		line++
	}
	var b strings.Builder
	for l.pos <= len(l.src) {
		lineStart := l.pos
		for l.pos < len(l.src) && l.src[l.pos] != '\n' {
			l.pos++
		}
		text := string(l.src[lineStart:l.pos])
		if strings.TrimSpace(text) == marker {
			if l.pos < len(l.src) {
				l.pos++
			}
			return token{tokString, b.String(), line}, nil
		}
		b.WriteString(text)
		b.WriteByte('\n')
		if l.pos < len(l.src) {
			l.pos++
			line++
		} else {
			break
		}
	}
	return token{}, fmt.Errorf("vakedhcl: unterminated heredoc %q", marker)
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9') || c == '-'
}

// --- parser ---

type parser struct {
	lx   *lexer
	cur  token
	next token
}

func newParser(src []byte) (*parser, error) {
	p := &parser{lx: newLexer(src)}
	if err := p.advance(); err != nil {
		return nil, err
	}
	if err := p.advance(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *parser) peek() token { return p.cur }

func (p *parser) advance() error {
	p.cur = p.next
	t, err := p.lx.next()
	if err != nil {
		return err
	}
	p.next = t
	return nil
}

func (p *parser) expect(k tokenKind, what string) (token, error) {
	if p.cur.kind != k {
		return token{}, fmt.Errorf("vakedhcl: expected %s on line %d, got %q", what, p.cur.line, p.cur.text)
	}
	t := p.cur
	return t, p.advance()
}

func (p *parser) parseBlock() (*Block, error) {
	t, err := p.expect(tokIdent, "block type")
	if err != nil {
		return nil, err
	}
	b := &Block{Type: t.text, Attrs: map[string]Value{}}
	for p.cur.kind == tokString {
		b.Labels = append(b.Labels, p.cur.text)
		if err := p.advance(); err != nil {
			return nil, err
		}
	}
	if _, err := p.expect(tokLBrace, "{"); err != nil {
		return nil, err
	}
	for p.cur.kind != tokRBrace {
		if p.cur.kind == tokEOF {
			return nil, fmt.Errorf("vakedhcl: unterminated block %q", b.Type)
		}
		key, err := p.expect(tokIdent, "attribute or block name")
		if err != nil {
			return nil, err
		}
		if p.cur.kind == tokEq {
			if err := p.advance(); err != nil {
				return nil, err
			}
			v, err := p.parseValue()
			if err != nil {
				return nil, err
			}
			b.Attrs[key.text] = v
			if p.cur.kind == tokComma {
				if err := p.advance(); err != nil {
					return nil, err
				}
			}
			continue
		}
		// Nested block: rewind the consumed ident by parsing labels from cur.
		child, err := p.parseBlockAfter(key)
		if err != nil {
			return nil, err
		}
		b.Blocks = append(b.Blocks, child)
	}
	if err := p.advance(); err != nil {
		return nil, err
	}
	return b, nil
}

func (p *parser) parseBlockAfter(head token) (*Block, error) {
	b := &Block{Type: head.text, Attrs: map[string]Value{}}
	for p.cur.kind == tokString {
		b.Labels = append(b.Labels, p.cur.text)
		if err := p.advance(); err != nil {
			return nil, err
		}
	}
	if _, err := p.expect(tokLBrace, "{"); err != nil {
		return nil, err
	}
	for p.cur.kind != tokRBrace {
		if p.cur.kind == tokEOF {
			return nil, fmt.Errorf("vakedhcl: unterminated block %q", b.Type)
		}
		key, err := p.expect(tokIdent, "attribute or block name")
		if err != nil {
			return nil, err
		}
		if p.cur.kind == tokEq {
			if err := p.advance(); err != nil {
				return nil, err
			}
			v, err := p.parseValue()
			if err != nil {
				return nil, err
			}
			b.Attrs[key.text] = v
			if p.cur.kind == tokComma {
				if err := p.advance(); err != nil {
					return nil, err
				}
			}
			continue
		}
		child, err := p.parseBlockAfter(key)
		if err != nil {
			return nil, err
		}
		b.Blocks = append(b.Blocks, child)
	}
	if err := p.advance(); err != nil {
		return nil, err
	}
	return b, nil
}

func (p *parser) parseValue() (Value, error) {
	t := p.cur
	switch t.kind {
	case tokString:
		if err := p.advance(); err != nil {
			return Value{}, err
		}
		return Value{Kind: ValueString, Str: t.text}, nil
	case tokNumber:
		n, err := strconv.ParseInt(t.text, 10, 64)
		if err != nil {
			return Value{}, fmt.Errorf("vakedhcl: bad integer %q", t.text)
		}
		if err := p.advance(); err != nil {
			return Value{}, err
		}
		return Value{Kind: ValueInt, Int: n}, nil
	case tokBool:
		if err := p.advance(); err != nil {
			return Value{}, err
		}
		return Value{Kind: ValueBool, Bool: t.text == "true"}, nil
	case tokLBrack:
		if err := p.advance(); err != nil {
			return Value{}, err
		}
		var list []Value
		for p.cur.kind != tokRBrack {
			if p.cur.kind == tokEOF {
				return Value{}, errors.New("vakedhcl: unterminated list")
			}
			v, err := p.parseValue()
			if err != nil {
				return Value{}, err
			}
			list = append(list, v)
			if p.cur.kind == tokComma {
				if err := p.advance(); err != nil {
					return Value{}, err
				}
			}
		}
		if err := p.advance(); err != nil {
			return Value{}, err
		}
		return Value{Kind: ValueList, List: list}, nil
	default:
		return Value{}, fmt.Errorf("vakedhcl: expected value on line %d, got %q", t.line, t.text)
	}
}

// SampleConfig is written by `mince init`.
const SampleConfig = `# mince.vkd.hcl — capability-scoped local compression contract
mince {
  version = "0.2.0"
}

capability "cache_guard" {
  allow     = ["mince_retrieve", "mince_stats", "mince_compress"]
  max_bytes = 4096
  budget    = "3/4"
  caps      = ["retrieve", "telemetry"]
}

capability "felix_sandbox" {
  allow  = ["felix_eval"]
  budget = "1/8"
  caps   = ["felix", "retrieve"]
}

salience_engine "kompress_ultra" {
  model    = "PeetPedro/kompress-v17"
  tier     = "mlx_apple_silicon"
  resident = true
}

wire_agent "cache_guard" {
  kind    = "cache_guard"
  budget  = "1/8"
  enabled = true
}

wire_agent "tier_shunt" {
  kind    = "tier_shunt"
  budget  = "1/16"
  enabled = true
}

ralph_loop "default" {
  every           = 64
  replay          = 64
  min_improvement = "1/20"
  policy          = "bandit_pq"
}

felix "shrink" {
  source = <<FELIX
fn shrink(c: capsule<sym>) -> sym {
  consume(c)
}
FELIX
  budget = "1/100"
}
`
