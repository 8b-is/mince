package felix

import (
	"fmt"
	"strconv"

	"mince/internal/pq"
)

// Kind is a runtime value kind.
type Kind uint8

const (
	KInt Kind = iota + 1
	KPQ
	KTrit
	KBool
	KSym
	KCapsule
	KConsumed
)

// Value is a fixed-size Felix runtime value. Symbols are interned ids, so a
// Value never owns a heap string.
type Value struct {
	Kind   Kind
	Int    int64
	PQ     pq.RationalPQ
	Trit   int8
	Bool   bool
	Sym    uint32
	CapSym uint32
}

// IntValue builds an int value.
func IntValue(n int64) Value { return Value{Kind: KInt, Int: n} }

// PQValue builds a pq value.
func PQValue(r pq.RationalPQ) Value { return Value{Kind: KPQ, PQ: r} }

// TritValue builds a trit value.
func TritValue(t int8) Value { return Value{Kind: KTrit, Trit: t} }

// BoolValue builds a bool value.
func BoolValue(b bool) Value { return Value{Kind: KBool, Bool: b} }

// SymValue builds a symbol value from an interned id.
func SymValue(id uint32) Value { return Value{Kind: KSym, Sym: id} }

// CapsuleValue builds a capsule value from an interned id.
func CapsuleValue(id uint32) Value { return Value{Kind: KCapsule, CapSym: id} }

// String renders a value for diagnostics.
func (v Value) String(p *Program) string {
	switch v.Kind {
	case KInt:
		return strconv.FormatInt(v.Int, 10)
	case KPQ:
		return v.PQ.String()
	case KTrit:
		return strconv.FormatInt(int64(v.Trit), 10) + "t"
	case KBool:
		if v.Bool {
			return "true"
		}
		return "false"
	case KSym:
		if int(v.Sym) < len(p.Syms) {
			return p.Syms[v.Sym]
		}
		return "sym(?)"
	case KCapsule:
		if int(v.CapSym) < len(p.Syms) {
			return "capsule(" + p.Syms[v.CapSym] + ")"
		}
		return "capsule(?)"
	case KConsumed:
		return "consumed"
	default:
		return "?"
	}
}

// RuntimeError is a Felix runtime failure.
type RuntimeError struct{ Msg string }

func (e *RuntimeError) Error() string { return "felix: " + e.Msg }

func rerror(format string, args ...any) error {
	return &RuntimeError{Msg: fmt.Sprintf(format, args...)}
}

type frame struct {
	fn   int
	pc   int
	base int
}

const (
	maxStack  = 256
	maxLocals = 256
	maxFrames = 32
)

type vm struct {
	p      *Program
	stack  [maxStack]Value
	sp     int
	locals [maxLocals]Value
	frames [maxFrames]frame
	fp     int
	fn     int
	pc     int
	lb     int
	gas    uint64
}

// Call invokes a compiled function with the given arguments.
func (p *Program) Call(name string, args []Value) (Value, error) {
	idx, ok := p.index[name]
	if !ok {
		return Value{}, rerror("unknown function %q", name)
	}
	fn := p.Funcs[idx]
	if len(args) != len(fn.Params) {
		return Value{}, rerror("%s wants %d args, got %d", name, len(fn.Params), len(args))
	}
	v := &vm{p: p, fn: idx, gas: p.Gas}
	if v.gas == 0 {
		v.gas = 100000
	}
	for i, a := range args {
		want := fn.Params[i]
		if want.Name == "capsule" {
			switch a.Kind {
			case KSym:
				v.locals[i] = Value{Kind: KCapsule, CapSym: a.Sym}
			case KCapsule:
				v.locals[i] = a
			default:
				return Value{}, rerror("arg %d wants %s", i+1, want)
			}
			continue
		}
		if a.Kind != kindOf(want) {
			return Value{}, rerror("arg %d wants %s", i+1, want)
		}
		v.locals[i] = a
	}
	return v.run()
}

func kindOf(t Type) Kind {
	switch t.Name {
	case "int":
		return KInt
	case "pq":
		return KPQ
	case "trit":
		return KTrit
	case "bool":
		return KBool
	case "sym":
		return KSym
	case "capsule":
		return KCapsule
	default:
		return 0
	}
}

func (v *vm) run() (Value, error) {
	for {
		if v.gas == 0 {
			return Value{}, rerror("out of gas")
		}
		v.gas--
		fn := v.p.Funcs[v.fn]
		if v.pc < 0 || v.pc >= len(fn.Code) {
			return Value{}, rerror("pc out of range")
		}
		in := fn.Code[v.pc]
		v.pc++
		switch in.Op {
		case OpPushInt:
			if err := v.push(IntValue(int64(in.A))); err != nil {
				return Value{}, err
			}
		case OpPushPQ:
			if int(in.A) < 0 || int(in.A) >= len(v.p.PQConsts) {
				return Value{}, rerror("bad pq constant")
			}
			if err := v.push(PQValue(v.p.PQConsts[in.A])); err != nil {
				return Value{}, err
			}
		case OpPushTrit:
			if err := v.push(TritValue(int8(in.A))); err != nil {
				return Value{}, err
			}
		case OpPushBool:
			if err := v.push(BoolValue(in.A != 0)); err != nil {
				return Value{}, err
			}
		case OpPushSym:
			if err := v.push(SymValue(uint32(in.A))); err != nil {
				return Value{}, err
			}
		case OpLoad:
			slot := v.lb + int(in.A)
			if slot < 0 || slot >= maxLocals {
				return Value{}, rerror("bad local slot")
			}
			if err := v.push(v.locals[slot]); err != nil {
				return Value{}, err
			}
		case OpStore:
			x, err := v.pop()
			if err != nil {
				return Value{}, err
			}
			slot := v.lb + int(in.A)
			if slot < 0 || slot >= maxLocals {
				return Value{}, rerror("bad local slot")
			}
			v.locals[slot] = x
		case OpPop:
			if _, err := v.pop(); err != nil {
				return Value{}, err
			}
		case OpAdd, OpSub, OpMul, OpDiv:
			b, err := v.pop()
			if err != nil {
				return Value{}, err
			}
			a, err := v.pop()
			if err != nil {
				return Value{}, err
			}
			out, err := arith(in.Op, a, b)
			if err != nil {
				return Value{}, err
			}
			if err := v.push(out); err != nil {
				return Value{}, err
			}
		case OpEq, OpNe:
			b, err := v.pop()
			if err != nil {
				return Value{}, err
			}
			a, err := v.pop()
			if err != nil {
				return Value{}, err
			}
			eq, err := equalValues(a, b)
			if err != nil {
				return Value{}, err
			}
			if in.Op == OpNe {
				eq = !eq
			}
			if err := v.push(BoolValue(eq)); err != nil {
				return Value{}, err
			}
		case OpLt, OpLe, OpGt, OpGe:
			b, err := v.pop()
			if err != nil {
				return Value{}, err
			}
			a, err := v.pop()
			if err != nil {
				return Value{}, err
			}
			ar, ok1 := numeric(a)
			br, ok2 := numeric(b)
			if !ok1 || !ok2 {
				return Value{}, rerror("comparison needs numerics")
			}
			c := ar.Cmp(br)
			res := false
			switch in.Op {
			case OpLt:
				res = c < 0
			case OpLe:
				res = c <= 0
			case OpGt:
				res = c > 0
			case OpGe:
				res = c >= 0
			}
			if err := v.push(BoolValue(res)); err != nil {
				return Value{}, err
			}
		case OpAnd, OpOr:
			b, err := v.pop()
			if err != nil {
				return Value{}, err
			}
			a, err := v.pop()
			if err != nil {
				return Value{}, err
			}
			if a.Kind != KBool || b.Kind != KBool {
				return Value{}, rerror("boolean operator needs bools")
			}
			res := a.Bool && b.Bool
			if in.Op == OpOr {
				res = a.Bool || b.Bool
			}
			if err := v.push(BoolValue(res)); err != nil {
				return Value{}, err
			}
		case OpNot:
			a, err := v.pop()
			if err != nil {
				return Value{}, err
			}
			if a.Kind != KBool {
				return Value{}, rerror("! needs bool")
			}
			if err := v.push(BoolValue(!a.Bool)); err != nil {
				return Value{}, err
			}
		case OpNeg:
			a, err := v.pop()
			if err != nil {
				return Value{}, err
			}
			switch a.Kind {
			case KInt:
				if err := v.push(IntValue(-a.Int)); err != nil {
					return Value{}, err
				}
			case KPQ:
				if err := v.push(PQValue(a.PQ.Neg())); err != nil {
					return Value{}, err
				}
			case KTrit:
				if err := v.push(TritValue(-a.Trit)); err != nil {
					return Value{}, err
				}
			default:
				return Value{}, rerror("unary - needs numeric")
			}
		case OpCall:
			argc := int(in.B)
			if v.sp < argc {
				return Value{}, rerror("stack underflow on call")
			}
			caller := v.p.Funcs[v.fn]
			newBase := v.lb + caller.NumSlots
			if newBase+argc > maxLocals {
				return Value{}, rerror("local overflow")
			}
			baseStack := v.sp - argc
			for i := 0; i < argc; i++ {
				v.locals[newBase+i] = v.stack[baseStack+i]
			}
			v.sp = baseStack
			if v.fp >= maxFrames {
				return Value{}, rerror("call stack overflow")
			}
			v.frames[v.fp] = frame{fn: v.fn, pc: v.pc, base: v.lb}
			v.fp++
			v.fn = int(in.A)
			v.pc = 0
			v.lb = newBase
		case OpConsume:
			if in.A >= 0 {
				slot := v.lb + int(in.A)
				if slot < 0 || slot >= maxLocals {
					return Value{}, rerror("bad capsule slot")
				}
				x := v.locals[slot]
				if x.Kind == KConsumed {
					return Value{}, rerror("capsule already consumed")
				}
				if x.Kind != KCapsule {
					return Value{}, rerror("consume needs capsule")
				}
				v.locals[slot] = Value{Kind: KConsumed}
				if err := v.push(SymValue(x.CapSym)); err != nil {
					return Value{}, err
				}
			} else {
				x, err := v.pop()
				if err != nil {
					return Value{}, err
				}
				if x.Kind != KCapsule {
					return Value{}, rerror("consume needs capsule")
				}
				if err := v.push(SymValue(x.CapSym)); err != nil {
					return Value{}, err
				}
			}
		case OpCapsule:
			x, err := v.pop()
			if err != nil {
				return Value{}, err
			}
			if x.Kind != KSym {
				return Value{}, rerror("capsule needs sym")
			}
			if err := v.push(Value{Kind: KCapsule, CapSym: x.Sym}); err != nil {
				return Value{}, err
			}
		case OpDotTrit:
			b, err := v.pop()
			if err != nil {
				return Value{}, err
			}
			a, err := v.pop()
			if err != nil {
				return Value{}, err
			}
			if a.Kind != KTrit || b.Kind != KTrit {
				return Value{}, rerror("dot_trit needs trits")
			}
			if err := v.push(PQValue(pq.FromInt(int64(a.Trit * b.Trit)))); err != nil {
				return Value{}, err
			}
		case OpMaxPQ, OpMinPQ:
			b, err := v.pop()
			if err != nil {
				return Value{}, err
			}
			a, err := v.pop()
			if err != nil {
				return Value{}, err
			}
			if a.Kind != KPQ || b.Kind != KPQ {
				return Value{}, rerror("pq min/max needs pq")
			}
			if in.Op == OpMaxPQ {
				if err := v.push(PQValue(a.PQ.Max(b.PQ))); err != nil {
					return Value{}, err
				}
			} else {
				if err := v.push(PQValue(a.PQ.Min(b.PQ))); err != nil {
					return Value{}, err
				}
			}
		case OpJump:
			v.pc = int(in.A)
		case OpJumpIfFalse:
			x, err := v.pop()
			if err != nil {
				return Value{}, err
			}
			if x.Kind != KBool {
				return Value{}, rerror("branch needs bool")
			}
			if !x.Bool {
				v.pc = int(in.A)
			}
		case OpRet:
			val, err := v.pop()
			if err != nil {
				return Value{}, err
			}
			if v.fp == 0 {
				return val, nil
			}
			v.fp--
			f := v.frames[v.fp]
			v.fn = f.fn
			v.pc = f.pc
			v.lb = f.base
			if err := v.push(val); err != nil {
				return Value{}, err
			}
		default:
			return Value{}, rerror("unknown opcode %d", in.Op)
		}
	}
}

func (v *vm) push(x Value) error {
	if v.sp >= maxStack {
		return rerror("stack overflow")
	}
	v.stack[v.sp] = x
	v.sp++
	return nil
}

func (v *vm) pop() (Value, error) {
	if v.sp == 0 {
		return Value{}, rerror("stack underflow")
	}
	v.sp--
	return v.stack[v.sp], nil
}

func numeric(x Value) (pq.RationalPQ, bool) {
	switch x.Kind {
	case KInt:
		return pq.FromInt(x.Int), true
	case KTrit:
		return pq.FromInt(int64(x.Trit)), true
	case KPQ:
		return x.PQ, true
	default:
		return pq.RationalPQ{}, false
	}
}

func arith(op Op, a, b Value) (Value, error) {
	ar, ok1 := numeric(a)
	br, ok2 := numeric(b)
	if !ok1 || !ok2 {
		return Value{}, rerror("arithmetic needs numerics")
	}
	if op == OpDiv {
		if br.IsZero() {
			return Value{}, rerror("division by zero")
		}
		return PQValue(ar.Div(br)), nil
	}
	if a.Kind != KPQ && b.Kind != KPQ {
		ai, bi := ar.Num, br.Num
		switch op {
		case OpAdd:
			return IntValue(ai + bi), nil
		case OpSub:
			return IntValue(ai - bi), nil
		case OpMul:
			return IntValue(ai * bi), nil
		}
	}
	switch op {
	case OpAdd:
		return PQValue(ar.Add(br)), nil
	case OpSub:
		return PQValue(ar.Sub(br)), nil
	case OpMul:
		return PQValue(ar.Mul(br)), nil
	default:
		return Value{}, rerror("unknown arithmetic")
	}
}

func equalValues(a, b Value) (bool, error) {
	if a.Kind == b.Kind {
		switch a.Kind {
		case KInt:
			return a.Int == b.Int, nil
		case KPQ:
			return a.PQ.Cmp(b.PQ) == 0, nil
		case KTrit:
			return a.Trit == b.Trit, nil
		case KBool:
			return a.Bool == b.Bool, nil
		case KSym:
			return a.Sym == b.Sym, nil
		case KCapsule:
			return a.CapSym == b.CapSym, nil
		}
	}
	ar, ok1 := numeric(a)
	br, ok2 := numeric(b)
	if ok1 && ok2 {
		return ar.Cmp(br) == 0, nil
	}
	return false, rerror("cannot compare values")
}
