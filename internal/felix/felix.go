// Package felix is a tiny, linear-typed, one-shot compiler and stack VM for
// capability-scoped local decisions. Programs are compiled once and executed
// with bounded gas; capsule values are consumed at most once.
package felix

import (
	"fmt"
	"strconv"
	"strings"
)

// Type is a Felix type: int, pq, trit, bool, sym, or capsule<T>.
type Type struct {
	Name string
	Elem *Type
}

func (t Type) String() string {
	if t.Name == "capsule" && t.Elem != nil {
		return "capsule<" + t.Elem.String() + ">"
	}
	return t.Name
}

// Equal reports structural type equality.
func (t Type) Equal(o Type) bool {
	if t.Name != o.Name {
		return false
	}
	if t.Elem == nil || o.Elem == nil {
		return t.Elem == nil && o.Elem == nil
	}
	return t.Elem.Equal(*o.Elem)
}

// Param is a function parameter.
type Param struct {
	Name string
	Type Type
}

// FuncDecl is a parsed function.
type FuncDecl struct {
	Name   string
	Params []Param
	Ret    Type
	Body   []Stmt
}

// Stmt is a statement node.
type Stmt interface{ stmtNode() }

// LetStmt binds an expression to a local slot.
type LetStmt struct {
	Name string
	Expr Expr
}

// IfStmt is a conditional block.
type IfStmt struct {
	Cond Expr
	Then []Stmt
	Else []Stmt
}

// ReturnStmt returns an expression.
type ReturnStmt struct{ Expr Expr }

// ExprStmt evaluates an expression for its effect.
type ExprStmt struct{ Expr Expr }

func (*LetStmt) stmtNode()    {}
func (*IfStmt) stmtNode()     {}
func (*ReturnStmt) stmtNode() {}
func (*ExprStmt) stmtNode()   {}

// Expr is an expression node.
type Expr interface{ exprNode() }

// IntLit is an integer literal.
type IntLit struct{ Value int64 }

// RatLit is an exact rational literal.
type RatLit struct {
	Num int64
	Den uint64
}

// TritLit is a balanced-ternary literal.
type TritLit struct{ Value int8 }

// BoolLit is a boolean literal.
type BoolLit struct{ Value bool }

// StringLit is a symbol literal.
type StringLit struct{ Value string }

// Var reads a local.
type Var struct{ Name string }

// Unary is a prefix operation.
type Unary struct {
	Op string
	X  Expr
}

// Binary is an infix operation.
type Binary struct {
	Op   string
	L, R Expr
}

// Call invokes a user function or builtin.
type Call struct {
	Name string
	Args []Expr
}

func (*IntLit) exprNode()    {}
func (*RatLit) exprNode()    {}
func (*TritLit) exprNode()   {}
func (*BoolLit) exprNode()   {}
func (*StringLit) exprNode() {}
func (*Var) exprNode()       {}
func (*Unary) exprNode()     {}
func (*Binary) exprNode()    {}
func (*Call) exprNode()      {}

// Parse parses a Felix source file into function declarations.
func Parse(src string) ([]*FuncDecl, error) {
	l := newLexer(src)
	p := &parser{lx: l}
	if err := p.advance(); err != nil {
		return nil, err
	}
	if err := p.advance(); err != nil {
		return nil, err
	}
	var out []*FuncDecl
	for p.cur.kind != tokEOF {
		fn, err := p.parseFunc()
		if err != nil {
			return nil, err
		}
		out = append(out, fn)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("felix: no functions")
	}
	return out, nil
}

// --- lexer ---

type tokenKind uint8

const (
	tokEOF tokenKind = iota
	tokIdent
	tokInt
	tokRat
	tokTrit
	tokString
	tokBool
	tokLParen
	tokRParen
	tokLBrace
	tokRBrace
	tokComma
	tokColon
	tokArrow
	tokSemi
	tokEq
	tokPlus
	tokMinus
	tokStar
	tokSlash
	tokEqEq
	tokNe
	tokLt
	tokLe
	tokGt
	tokGe
	tokAndAnd
	tokOrOr
	tokBang
)

type token struct {
	kind tokenKind
	text string
	line int
}

type lexer struct {
	src  string
	pos  int
	line int
}

func newLexer(src string) *lexer { return &lexer{src: src, line: 1} }

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
	line := l.line
	c := l.src[l.pos]
	two := ""
	if l.pos+1 < len(l.src) {
		two = l.src[l.pos : l.pos+2]
	}
	switch two {
	case "->":
		l.pos += 2
		return token{tokArrow, "->", line}, nil
	case "==":
		l.pos += 2
		return token{tokEqEq, "==", line}, nil
	case "!=":
		l.pos += 2
		return token{tokNe, "!=", line}, nil
	case "<=":
		l.pos += 2
		return token{tokLe, "<=", line}, nil
	case ">=":
		l.pos += 2
		return token{tokGe, ">=", line}, nil
	case "&&":
		l.pos += 2
		return token{tokAndAnd, "&&", line}, nil
	case "||":
		l.pos += 2
		return token{tokOrOr, "||", line}, nil
	}
	switch c {
	case '(':
		l.pos++
		return token{tokLParen, "(", line}, nil
	case ')':
		l.pos++
		return token{tokRParen, ")", line}, nil
	case '{':
		l.pos++
		return token{tokLBrace, "{", line}, nil
	case '}':
		l.pos++
		return token{tokRBrace, "}", line}, nil
	case ',':
		l.pos++
		return token{tokComma, ",", line}, nil
	case ':':
		l.pos++
		return token{tokColon, ":", line}, nil
	case ';':
		l.pos++
		return token{tokSemi, ";", line}, nil
	case '=':
		l.pos++
		return token{tokEq, "=", line}, nil
	case '*':
		l.pos++
		return token{tokStar, "*", line}, nil
	case '<':
		l.pos++
		return token{tokLt, "<", line}, nil
	case '>':
		l.pos++
		return token{tokGt, ">", line}, nil
	case '!':
		l.pos++
		return token{tokBang, "!", line}, nil
	case '"':
		return l.lexString()
	}
	if isDigit(c) || ((c == '-' || c == '+') && l.pos+1 < len(l.src) && isDigit(l.src[l.pos+1])) {
		return l.lexNumber()
	}
	if c == '-' {
		l.pos++
		return token{tokMinus, "-", line}, nil
	}
	if c == '+' {
		l.pos++
		return token{tokPlus, "+", line}, nil
	}
	if c == '/' {
		l.pos++
		return token{tokSlash, "/", line}, nil
	}
	if isIdentStart(c) {
		start := l.pos
		for l.pos < len(l.src) && isIdentPart(l.src[l.pos]) {
			l.pos++
		}
		text := l.src[start:l.pos]
		if text == "true" || text == "false" {
			return token{tokBool, text, line}, nil
		}
		return token{tokIdent, text, line}, nil
	}
	return token{}, fmt.Errorf("felix: unexpected %q on line %d", c, line)
}

func (l *lexer) lexNumber() (token, error) {
	line := l.line
	start := l.pos
	if l.src[l.pos] == '-' || l.src[l.pos] == '+' {
		l.pos++
	}
	for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
		l.pos++
	}
	if l.pos < len(l.src) && l.src[l.pos] == 't' {
		text := l.src[start:l.pos]
		l.pos++
		return token{tokTrit, text, line}, nil
	}
	if l.pos < len(l.src) && l.src[l.pos] == '/' && l.pos+1 < len(l.src) && isDigit(l.src[l.pos+1]) {
		l.pos++
		for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
			l.pos++
		}
		return token{tokRat, l.src[start:l.pos], line}, nil
	}
	return token{tokInt, l.src[start:l.pos], line}, nil
}

func (l *lexer) lexString() (token, error) {
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
			case '\\', '"':
				b.WriteByte(l.src[l.pos])
			default:
				b.WriteByte(l.src[l.pos])
			}
			l.pos++
			continue
		}
		if c == '"' {
			l.pos++
			return token{tokString, b.String(), line}, nil
		}
		b.WriteByte(c)
		l.pos++
	}
	return token{}, fmt.Errorf("felix: unterminated string on line %d", line)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
func isIdentPart(c byte) bool {
	return isIdentStart(c) || isDigit(c)
}

// --- parser ---

type parser struct {
	lx   *lexer
	cur  token
	next token
}

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
		return token{}, fmt.Errorf("felix: expected %s on line %d, got %q", what, p.cur.line, p.cur.text)
	}
	t := p.cur
	return t, p.advance()
}

func (p *parser) parseFunc() (*FuncDecl, error) {
	if _, err := p.expect(tokIdent, "fn"); err != nil {
		return nil, err
	}
	name, err := p.expect(tokIdent, "function name")
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(tokLParen, "("); err != nil {
		return nil, err
	}
	var params []Param
	for p.cur.kind != tokRParen {
		pn, err := p.expect(tokIdent, "parameter name")
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tokColon, ":"); err != nil {
			return nil, err
		}
		pt, err := p.parseType()
		if err != nil {
			return nil, err
		}
		params = append(params, Param{Name: pn.text, Type: pt})
		if p.cur.kind == tokComma {
			if err := p.advance(); err != nil {
				return nil, err
			}
		}
	}
	if err := p.advance(); err != nil {
		return nil, err
	}
	if _, err := p.expect(tokArrow, "->"); err != nil {
		return nil, err
	}
	ret, err := p.parseType()
	if err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &FuncDecl{Name: name.text, Params: params, Ret: ret, Body: body}, nil
}

func (p *parser) parseType() (Type, error) {
	t, err := p.expect(tokIdent, "type")
	if err != nil {
		return Type{}, err
	}
	typ := Type{Name: t.text}
	if p.cur.kind == tokLt {
		if err := p.advance(); err != nil {
			return Type{}, err
		}
		elem, err := p.parseType()
		if err != nil {
			return Type{}, err
		}
		if _, err := p.expect(tokGt, ">"); err != nil {
			return Type{}, err
		}
		typ.Elem = &elem
	}
	switch typ.Name {
	case "int", "pq", "trit", "bool", "sym":
		if typ.Elem != nil {
			return Type{}, fmt.Errorf("felix: %s cannot have type arguments", typ.Name)
		}
	case "capsule":
		if typ.Elem == nil {
			return Type{}, fmt.Errorf("felix: capsule requires a type argument")
		}
	default:
		return Type{}, fmt.Errorf("felix: unknown type %q", typ.Name)
	}
	return typ, nil
}

func (p *parser) parseBlock() ([]Stmt, error) {
	if _, err := p.expect(tokLBrace, "{"); err != nil {
		return nil, err
	}
	var out []Stmt
	for p.cur.kind != tokRBrace {
		if p.cur.kind == tokEOF {
			return nil, fmt.Errorf("felix: unterminated block")
		}
		s, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := p.advance(); err != nil {
		return nil, err
	}
	return out, nil
}

func (p *parser) parseStmt() (Stmt, error) {
	if p.cur.kind == tokIdent {
		switch p.cur.text {
		case "let":
			if err := p.advance(); err != nil {
				return nil, err
			}
			name, err := p.expect(tokIdent, "let name")
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(tokEq, "="); err != nil {
				return nil, err
			}
			x, err := p.parseExpr(0)
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(tokSemi, ";"); err != nil {
				return nil, err
			}
			return &LetStmt{Name: name.text, Expr: x}, nil
		case "if":
			if err := p.advance(); err != nil {
				return nil, err
			}
			cond, err := p.parseExpr(0)
			if err != nil {
				return nil, err
			}
			then, err := p.parseBlock()
			if err != nil {
				return nil, err
			}
			var els []Stmt
			if p.cur.kind == tokIdent && p.cur.text == "else" {
				if err := p.advance(); err != nil {
					return nil, err
				}
				els, err = p.parseBlock()
				if err != nil {
					return nil, err
				}
			}
			return &IfStmt{Cond: cond, Then: then, Else: els}, nil
		case "return":
			if err := p.advance(); err != nil {
				return nil, err
			}
			x, err := p.parseExpr(0)
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(tokSemi, ";"); err != nil {
				return nil, err
			}
			return &ReturnStmt{Expr: x}, nil
		}
	}
	x, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(tokSemi, ";"); err != nil {
		return nil, err
	}
	return &ExprStmt{Expr: x}, nil
}

func (p *parser) parseExpr(minPrec int) (Expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		op, prec := binaryOp(p.cur.kind)
		if prec == 0 || prec < minPrec {
			break
		}
		if err := p.advance(); err != nil {
			return nil, err
		}
		right, err := p.parseExpr(prec + 1)
		if err != nil {
			return nil, err
		}
		left = &Binary{Op: op, L: left, R: right}
	}
	return left, nil
}

func (p *parser) parseUnary() (Expr, error) {
	switch p.cur.kind {
	case tokMinus, tokBang:
		op := p.cur.text
		if err := p.advance(); err != nil {
			return nil, err
		}
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &Unary{Op: op, X: x}, nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (Expr, error) {
	t := p.cur
	switch t.kind {
	case tokInt:
		n, err := strconv.ParseInt(t.text, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("felix: bad integer %q", t.text)
		}
		if err := p.advance(); err != nil {
			return nil, err
		}
		return &IntLit{Value: n}, nil
	case tokRat:
		parts := strings.SplitN(t.text, "/", 2)
		n, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("felix: bad rational %q", t.text)
		}
		d, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil || d == 0 {
			return nil, fmt.Errorf("felix: bad rational %q", t.text)
		}
		if err := p.advance(); err != nil {
			return nil, err
		}
		return &RatLit{Num: n, Den: d}, nil
	case tokTrit:
		text := strings.TrimSuffix(t.text, "t")
		n, err := strconv.ParseInt(text, 10, 8)
		if err != nil || n < -1 || n > 1 {
			return nil, fmt.Errorf("felix: bad trit %q", t.text)
		}
		if err := p.advance(); err != nil {
			return nil, err
		}
		return &TritLit{Value: int8(n)}, nil
	case tokString:
		if err := p.advance(); err != nil {
			return nil, err
		}
		return &StringLit{Value: t.text}, nil
	case tokBool:
		if err := p.advance(); err != nil {
			return nil, err
		}
		return &BoolLit{Value: t.text == "true"}, nil
	case tokIdent:
		if err := p.advance(); err != nil {
			return nil, err
		}
		if p.cur.kind == tokLParen {
			if err := p.advance(); err != nil {
				return nil, err
			}
			var args []Expr
			for p.cur.kind != tokRParen {
				a, err := p.parseExpr(0)
				if err != nil {
					return nil, err
				}
				args = append(args, a)
				if p.cur.kind == tokComma {
					if err := p.advance(); err != nil {
						return nil, err
					}
				}
			}
			if err := p.advance(); err != nil {
				return nil, err
			}
			return &Call{Name: t.text, Args: args}, nil
		}
		return &Var{Name: t.text}, nil
	case tokLParen:
		if err := p.advance(); err != nil {
			return nil, err
		}
		x, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tokRParen, ")"); err != nil {
			return nil, err
		}
		return x, nil
	default:
		return nil, fmt.Errorf("felix: expected expression on line %d, got %q", t.line, t.text)
	}
}

func binaryOp(k tokenKind) (string, int) {
	switch k {
	case tokOrOr:
		return "||", 1
	case tokAndAnd:
		return "&&", 2
	case tokEqEq:
		return "==", 3
	case tokNe:
		return "!=", 3
	case tokLt:
		return "<", 4
	case tokLe:
		return "<=", 4
	case tokGt:
		return ">", 4
	case tokGe:
		return ">=", 4
	case tokPlus:
		return "+", 5
	case tokMinus:
		return "-", 5
	case tokStar:
		return "*", 6
	case tokSlash:
		return "/", 6
	default:
		return "", 0
	}
}
