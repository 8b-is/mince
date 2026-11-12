package felix

import (
	"fmt"

	"mince/internal/pq"
)

// Op is a Felix bytecode opcode.
type Op uint8

const (
	OpPushInt Op = iota
	OpPushPQ
	OpPushTrit
	OpPushBool
	OpPushSym
	OpLoad
	OpStore
	OpPop
	OpAdd
	OpSub
	OpMul
	OpDiv
	OpEq
	OpNe
	OpLt
	OpLe
	OpGt
	OpGe
	OpAnd
	OpOr
	OpNot
	OpNeg
	OpCall
	OpConsume
	OpCapsule
	OpDotTrit
	OpMaxPQ
	OpMinPQ
	OpJump
	OpJumpIfFalse
	OpRet
)

// Instr is one fixed-size instruction.
type Instr struct {
	Op Op
	A  int32
	B  int32
}

// Function is a compiled function.
type Function struct {
	Name      string
	Params    []Type
	Ret       Type
	Code      []Instr
	NumSlots  int
	CapsuleAt []bool
}

// Program is a compiled Felix module.
type Program struct {
	Funcs    []*Function
	index    map[string]int
	Syms     []string
	symIndex map[string]uint32
	PQConsts []pq.RationalPQ
	Gas      uint64
}

// CompileError is a static Felix error.
type CompileError struct{ Msg string }

func (e *CompileError) Error() string { return "felix: " + e.Msg }

func cerror(format string, args ...any) error {
	return &CompileError{Msg: fmt.Sprintf(format, args...)}
}

// Compile parses and compiles Felix source.
func Compile(src string) (*Program, error) {
	decls, err := Parse(src)
	if err != nil {
		return nil, err
	}
	return CompileDecls(decls)
}

// CompileDecls compiles already-parsed declarations.
func CompileDecls(decls []*FuncDecl) (*Program, error) {
	p := &Program{
		index:    map[string]int{},
		symIndex: map[string]uint32{},
		Gas:      100000,
	}
	p.intern("")
	for _, d := range decls {
		if isBuiltin(d.Name) {
			return nil, cerror("cannot redefine builtin %q", d.Name)
		}
		if _, dup := p.index[d.Name]; dup {
			return nil, cerror("duplicate function %q", d.Name)
		}
		p.index[d.Name] = len(p.Funcs)
		fn := &Function{Name: d.Name, Ret: d.Ret}
		for _, prm := range d.Params {
			fn.Params = append(fn.Params, prm.Type)
			fn.CapsuleAt = append(fn.CapsuleAt, prm.Type.Name == "capsule")
		}
		p.Funcs = append(p.Funcs, fn)
	}
	for i, d := range decls {
		if err := checkLinearity(d); err != nil {
			return nil, err
		}
		c := &compiler{p: p, fn: p.Funcs[i], locals: map[string]local{}}
		for j, prm := range d.Params {
			c.locals[prm.Name] = local{slot: j, typ: prm.Type}
		}
		c.next = len(d.Params)
		if err := c.compileStmts(d.Body); err != nil {
			return nil, err
		}
		if len(c.fn.Code) == 0 || c.fn.Code[len(c.fn.Code)-1].Op != OpRet {
			if err := c.emitZero(c.fn.Ret); err != nil {
				return nil, err
			}
			c.emit(OpRet, 0, 0)
		}
		c.fn.NumSlots = c.next
	}
	return p, nil
}

// Intern returns the stable symbol id for s.
func (p *Program) Intern(s string) uint32 { return p.intern(s) }

func (p *Program) intern(s string) uint32 {
	if id, ok := p.symIndex[s]; ok {
		return id
	}
	id := uint32(len(p.Syms))
	p.Syms = append(p.Syms, s)
	p.symIndex[s] = id
	return id
}

func isBuiltin(name string) bool {
	switch name {
	case "consume", "capsule", "dot_trit", "max_pq", "min_pq":
		return true
	default:
		return false
	}
}

type local struct {
	slot int
	typ  Type
}

type compiler struct {
	p      *Program
	fn     *Function
	locals map[string]local
	next   int
}

func (c *compiler) emit(op Op, a, b int32) int {
	c.fn.Code = append(c.fn.Code, Instr{Op: op, A: a, B: b})
	return len(c.fn.Code) - 1
}

func (c *compiler) patch(at int, target int) {
	c.fn.Code[at].A = int32(target)
}

func (c *compiler) alloc(name string, t Type) (local, error) {
	if _, exists := c.locals[name]; exists {
		return local{}, cerror("duplicate local %q", name)
	}
	if c.next >= 256 {
		return local{}, cerror("too many locals")
	}
	l := local{slot: c.next, typ: t}
	c.next++
	c.locals[name] = l
	return l, nil
}

func (c *compiler) compileStmts(stmts []Stmt) error {
	for _, s := range stmts {
		switch st := s.(type) {
		case *LetStmt:
			t, err := c.compileExpr(st.Expr)
			if err != nil {
				return err
			}
			l, err := c.alloc(st.Name, t)
			if err != nil {
				return err
			}
			c.emit(OpStore, int32(l.slot), 0)
		case *ReturnStmt:
			t, err := c.compileExpr(st.Expr)
			if err != nil {
				return err
			}
			if !t.Equal(c.fn.Ret) {
				return cerror("return type %s does not match %s", t, c.fn.Ret)
			}
			c.emit(OpRet, 0, 0)
		case *IfStmt:
			ct, err := c.compileExpr(st.Cond)
			if err != nil {
				return err
			}
			if ct.Name != "bool" {
				return cerror("if condition must be bool, got %s", ct)
			}
			jf := c.emit(OpJumpIfFalse, 0, 0)
			if err := c.compileStmts(st.Then); err != nil {
				return err
			}
			if len(st.Else) > 0 {
				jend := c.emit(OpJump, 0, 0)
				c.patch(jf, len(c.fn.Code))
				if err := c.compileStmts(st.Else); err != nil {
					return err
				}
				c.patch(jend, len(c.fn.Code))
			} else {
				c.patch(jf, len(c.fn.Code))
			}
		case *ExprStmt:
			if _, err := c.compileExpr(st.Expr); err != nil {
				return err
			}
			c.emit(OpPop, 0, 0)
		default:
			return cerror("unknown statement")
		}
	}
	return nil
}

func (c *compiler) compileExpr(e Expr) (Type, error) {
	switch x := e.(type) {
	case *IntLit:
		c.emit(OpPushInt, int32(x.Value), 0)
		return Type{Name: "int"}, nil
	case *RatLit:
		idx := len(c.p.PQConsts)
		c.p.PQConsts = append(c.p.PQConsts, pq.New(x.Num, x.Den))
		c.emit(OpPushPQ, int32(idx), 0)
		return Type{Name: "pq"}, nil
	case *TritLit:
		c.emit(OpPushTrit, int32(x.Value), 0)
		return Type{Name: "trit"}, nil
	case *BoolLit:
		v := int32(0)
		if x.Value {
			v = 1
		}
		c.emit(OpPushBool, v, 0)
		return Type{Name: "bool"}, nil
	case *StringLit:
		c.emit(OpPushSym, int32(c.p.intern(x.Value)), 0)
		return Type{Name: "sym"}, nil
	case *Var:
		l, ok := c.locals[x.Name]
		if !ok {
			return Type{}, cerror("unknown variable %q", x.Name)
		}
		c.emit(OpLoad, int32(l.slot), 0)
		return l.typ, nil
	case *Unary:
		t, err := c.compileExpr(x.X)
		if err != nil {
			return Type{}, err
		}
		switch x.Op {
		case "-":
			if t.Name != "int" && t.Name != "pq" && t.Name != "trit" {
				return Type{}, cerror("unary - needs int, pq or trit, got %s", t)
			}
			c.emit(OpNeg, 0, 0)
			return t, nil
		case "!":
			if t.Name != "bool" {
				return Type{}, cerror("! needs bool, got %s", t)
			}
			c.emit(OpNot, 0, 0)
			return Type{Name: "bool"}, nil
		default:
			return Type{}, cerror("unknown unary %q", x.Op)
		}
	case *Binary:
		lt, err := c.compileExpr(x.L)
		if err != nil {
			return Type{}, err
		}
		rt, err := c.compileExpr(x.R)
		if err != nil {
			return Type{}, err
		}
		switch x.Op {
		case "&&", "||":
			if lt.Name != "bool" || rt.Name != "bool" {
				return Type{}, cerror("%s needs bool operands", x.Op)
			}
			if x.Op == "&&" {
				c.emit(OpAnd, 0, 0)
			} else {
				c.emit(OpOr, 0, 0)
			}
			return Type{Name: "bool"}, nil
		case "==", "!=":
			if !lt.Equal(rt) && !(isNumeric(lt) && isNumeric(rt)) {
				return Type{}, cerror("%s operands are %s and %s", x.Op, lt, rt)
			}
			if x.Op == "==" {
				c.emit(OpEq, 0, 0)
			} else {
				c.emit(OpNe, 0, 0)
			}
			return Type{Name: "bool"}, nil
		case "<", "<=", ">", ">=":
			if !isNumeric(lt) || !isNumeric(rt) {
				return Type{}, cerror("%s needs numeric operands", x.Op)
			}
			switch x.Op {
			case "<":
				c.emit(OpLt, 0, 0)
			case "<=":
				c.emit(OpLe, 0, 0)
			case ">":
				c.emit(OpGt, 0, 0)
			case ">=":
				c.emit(OpGe, 0, 0)
			}
			return Type{Name: "bool"}, nil
		case "+", "-", "*":
			if !isNumeric(lt) || !isNumeric(rt) {
				return Type{}, cerror("%s needs numeric operands", x.Op)
			}
			out := promote(lt, rt)
			switch x.Op {
			case "+":
				c.emit(OpAdd, 0, 0)
			case "-":
				c.emit(OpSub, 0, 0)
			case "*":
				c.emit(OpMul, 0, 0)
			}
			return out, nil
		case "/":
			if !isNumeric(lt) || !isNumeric(rt) {
				return Type{}, cerror("/ needs numeric operands")
			}
			c.emit(OpDiv, 0, 0)
			return Type{Name: "pq"}, nil
		default:
			return Type{}, cerror("unknown binary %q", x.Op)
		}
	case *Call:
		return c.compileCall(x)
	default:
		return Type{}, cerror("unknown expression")
	}
}

func (c *compiler) compileCall(x *Call) (Type, error) {
	switch x.Name {
	case "consume":
		if len(x.Args) != 1 {
			return Type{}, cerror("consume takes one argument")
		}
		if v, ok := x.Args[0].(*Var); ok {
			l, found := c.locals[v.Name]
			if !found {
				return Type{}, cerror("unknown variable %q", v.Name)
			}
			if l.typ.Name != "capsule" {
				return Type{}, cerror("consume needs capsule, got %s", l.typ)
			}
			c.emit(OpConsume, int32(l.slot), 0)
			return *l.typ.Elem, nil
		}
		t, err := c.compileExpr(x.Args[0])
		if err != nil {
			return Type{}, err
		}
		if t.Name != "capsule" {
			return Type{}, cerror("consume needs capsule, got %s", t)
		}
		c.emit(OpConsume, -1, 0)
		return *t.Elem, nil
	case "capsule":
		if len(x.Args) != 1 {
			return Type{}, cerror("capsule takes one argument")
		}
		t, err := c.compileExpr(x.Args[0])
		if err != nil {
			return Type{}, err
		}
		c.emit(OpCapsule, 0, 0)
		return Type{Name: "capsule", Elem: &t}, nil
	case "dot_trit":
		if len(x.Args) != 2 {
			return Type{}, cerror("dot_trit takes two arguments")
		}
		for _, a := range x.Args {
			t, err := c.compileExpr(a)
			if err != nil {
				return Type{}, err
			}
			if t.Name != "trit" {
				return Type{}, cerror("dot_trit needs trits, got %s", t)
			}
		}
		c.emit(OpDotTrit, 0, 0)
		return Type{Name: "pq"}, nil
	case "max_pq", "min_pq":
		if len(x.Args) != 2 {
			return Type{}, cerror("%s takes two arguments", x.Name)
		}
		for _, a := range x.Args {
			t, err := c.compileExpr(a)
			if err != nil {
				return Type{}, err
			}
			if t.Name != "pq" {
				return Type{}, cerror("%s needs pq, got %s", x.Name, t)
			}
		}
		if x.Name == "max_pq" {
			c.emit(OpMaxPQ, 0, 0)
		} else {
			c.emit(OpMinPQ, 0, 0)
		}
		return Type{Name: "pq"}, nil
	}
	idx, ok := c.p.index[x.Name]
	if !ok {
		return Type{}, cerror("unknown function %q", x.Name)
	}
	fn := c.p.Funcs[idx]
	if len(x.Args) != len(fn.Params) {
		return Type{}, cerror("%s wants %d args, got %d", x.Name, len(fn.Params), len(x.Args))
	}
	for i, a := range x.Args {
		at, err := c.compileExpr(a)
		if err != nil {
			return Type{}, err
		}
		want := fn.Params[i]
		if want.Name == "capsule" {
			switch {
			case at.Name == "capsule" && at.Elem != nil && at.Elem.Equal(*want.Elem):
			case at.Name == "sym" && want.Elem != nil && want.Elem.Name == "sym":
				c.emit(OpCapsule, 0, 0)
			default:
				return Type{}, cerror("arg %d wants %s, got %s", i+1, want, at)
			}
			continue
		}
		if !at.Equal(want) {
			return Type{}, cerror("arg %d wants %s, got %s", i+1, want, at)
		}
	}
	c.emit(OpCall, int32(idx), int32(len(x.Args)))
	return fn.Ret, nil
}

func (c *compiler) emitZero(t Type) error {
	switch t.Name {
	case "int":
		c.emit(OpPushInt, 0, 0)
	case "pq":
		idx := len(c.p.PQConsts)
		c.p.PQConsts = append(c.p.PQConsts, pq.New(0, 1))
		c.emit(OpPushPQ, int32(idx), 0)
	case "trit":
		c.emit(OpPushTrit, 0, 0)
	case "bool":
		c.emit(OpPushBool, 0, 0)
	case "sym":
		c.emit(OpPushSym, int32(c.p.intern("")), 0)
	case "capsule":
		c.emit(OpPushSym, int32(c.p.intern("")), 0)
		c.emit(OpCapsule, 0, 0)
	default:
		return cerror("cannot zero type %s", t)
	}
	return nil
}

func isNumeric(t Type) bool {
	return t.Name == "int" || t.Name == "pq" || t.Name == "trit"
}

func promote(a, b Type) Type {
	if a.Name == "pq" || b.Name == "pq" {
		return Type{Name: "pq"}
	}
	return Type{Name: "int"}
}

// checkLinearity enforces: a capsule parameter may appear only as the direct
// argument of consume, and at most once.
func checkLinearity(fn *FuncDecl) error {
	for _, prm := range fn.Params {
		if prm.Type.Name != "capsule" {
			continue
		}
		count := 0
		var walk func(Expr) error
		walk = func(e Expr) error {
			switch x := e.(type) {
			case *Var:
				if x.Name == prm.Name {
					return cerror("capsule %q used outside consume", prm.Name)
				}
			case *Unary:
				return walk(x.X)
			case *Binary:
				if err := walk(x.L); err != nil {
					return err
				}
				return walk(x.R)
			case *Call:
				if x.Name == "consume" && len(x.Args) == 1 {
					if v, ok := x.Args[0].(*Var); ok && v.Name == prm.Name {
						count++
						if count > 1 {
							return cerror("capsule %q consumed more than once", prm.Name)
						}
						return nil
					}
				}
				for _, a := range x.Args {
					if err := walk(a); err != nil {
						return err
					}
				}
			}
			return nil
		}
		var walkStmt func(Stmt) error
		walkStmt = func(s Stmt) error {
			switch x := s.(type) {
			case *LetStmt:
				return walk(x.Expr)
			case *ReturnStmt:
				return walk(x.Expr)
			case *ExprStmt:
				return walk(x.Expr)
			case *IfStmt:
				if err := walk(x.Cond); err != nil {
					return err
				}
				for _, st := range x.Then {
					if err := walkStmt(st); err != nil {
						return err
					}
				}
				for _, st := range x.Else {
					if err := walkStmt(st); err != nil {
						return err
					}
				}
			}
			return nil
		}
		for _, s := range fn.Body {
			if err := walkStmt(s); err != nil {
				return err
			}
		}
	}
	return nil
}
