package value

import (
	"encoding/gob"
	"fmt"
	"strconv"

	"github.com/ArubikU/polyloft-bvm/internal/bytecode"
)

func init() {
	// register all concrete value types so gob can encode/decode them when
	// they're stored in interface{} fields (e.g. Value.Object or constants).
	gob.Register(Value{})
	gob.Register(&Class{})
	gob.Register(&Instance{})
	gob.Register(&Array{})
	gob.Register(&Map{})
	gob.Register(&Tuple{})
	gob.Register(&Builtin{})
	gob.Register(&Module{})
	gob.Register(&Cell{})
	gob.Register(&Range{})
	gob.Register(&Closure{})
	gob.Register(&BoundMethod{})
	gob.Register(&BoundStaticMethod{})
	gob.Register(&SAMWrapper{})
	gob.Register(&SAMBoundMethod{})
	gob.Register(&Iterator{})
	gob.Register([]Value{})
}

type NumberKind uint8

const (
	NumberFloat NumberKind = iota
	NumberInt
)

type Kind uint8

const (
	Nil Kind = iota
	Number
	Bool
	Char
	String
	Object
)

type BuiltinFunc func(args []Value) (Value, error)

type Builtin struct {
	Name  string
	Arity int
	Fn    BuiltinFunc
}

type Module struct {
	Name    string
	Members map[string]Value
}

type Tuple struct {
	Elements []Value
}

type Array struct {
	Elements []Value
}

type Map struct {
	Entries map[string]Value
}

type FieldDef struct {
	Default    Value
	Mutable    bool
	IsFinal    bool
	TypeName   string
	Visibility string
}

type Cell struct {
	Value Value
}

type Class struct {
	ClassDecl
	ClassRuntime
	defaultFields []Value
}

type SpecialMethodSlot uint8

const (
	SpecialMethodIterableLength SpecialMethodSlot = iota
	SpecialMethodIterableGet
	SpecialMethodPieces
	SpecialMethodGetPiece
	SpecialMethodIndexGet
	SpecialMethodIndexSet
	SpecialMethodContains
	SpecialMethodSlice
	SpecialMethodEquals
	SpecialMethodHash
)

var specialMethodSlotOrder = []SpecialMethodSlot{
	SpecialMethodIterableLength,
	SpecialMethodIterableGet,
	SpecialMethodPieces,
	SpecialMethodGetPiece,
	SpecialMethodIndexGet,
	SpecialMethodIndexSet,
	SpecialMethodContains,
	SpecialMethodSlice,
	SpecialMethodEquals,
	SpecialMethodHash,
}

type ClassDecl struct {
	Name       string
	Superclass *Class

	Implements map[string]bool
	Permits    map[string]bool

	IsAbstract bool
	IsEnum     bool
	IsSealed   bool
	IsRecord   bool

	EnumOrder []string

	Fields       map[string]FieldDef
	FieldOrder   []string
	FieldIndex   map[string]int
	StaticFields map[string]FieldDef

	MethodVisibility      map[string]string
	StaticVisibility      map[string]string
	ConstructorVisibility string
	MethodAnnotations     map[string][]string
}

type ClassRuntime struct {
	FastConstructor   *FastConstructorPlan
	FastMethods       []*FastMethodPlan
	FastStaticMethods map[string]*FastMethodPlan

	MethodOrder []string
	MethodIndex map[string]int
	MethodTable []*bytecode.Function
	Methods     map[string]*bytecode.Function

	MethodOverloads       map[string][]*bytecode.Function
	Constructor           *bytecode.Function
	ConstructorOverloads  []*bytecode.Function
	StaticMethods         map[string]*bytecode.Function
	StaticMethodOverloads map[string][]*bytecode.Function
	StaticValues          map[string]Value
	SpecialMethods        map[SpecialMethodSlot]*bytecode.Function
}

type FastConstructorPlan struct {
	Arity      int
	FieldSlots []int
	ArgIndexes []int
}

type FastMethodExprKind uint8

const (
	FastMethodExprNumber FastMethodExprKind = iota
	FastMethodExprLiteral
	FastMethodExprParam
	FastMethodExprField
	FastMethodExprAdd
	FastMethodExprSub
	FastMethodExprMul
	FastMethodExprDiv
	FastMethodExprNegate
)

type FastMethodExpr struct {
	Kind       FastMethodExprKind
	Number     float64
	Literal    Value
	ParamIndex int
	FieldSlot  int
	Left       *FastMethodExpr
	Right      *FastMethodExpr
}

// FastMethodOp is one instruction in a flat postfix evaluation sequence.
type FastMethodOp struct {
	Kind  FastMethodExprKind
	Slot  int   // field slot (for FastMethodExprField)
	Const Value // constant value (for FastMethodExprNumber/Literal)
}

type FastMethodPlan struct {
	Arity int
	Expr  *FastMethodExpr
	Ops   []FastMethodOp // flat postfix sequence; non-nil when pre-compiled
}

// CompileOps converts the expression tree to a flat postfix instruction sequence.
func (p *FastMethodPlan) CompileOps() {
	if p.Arity != 0 || p.Expr == nil {
		return
	}
	var ops []FastMethodOp
	if buildPostfix(p.Expr, &ops) {
		p.Ops = ops
	}
}

func buildPostfix(expr *FastMethodExpr, ops *[]FastMethodOp) bool {
	switch expr.Kind {
	case FastMethodExprNumber:
		var v Value
		if i := int64(expr.Number); float64(i) == expr.Number {
			v = IntValue(i)
		} else {
			v = FloatValue(expr.Number)
		}
		*ops = append(*ops, FastMethodOp{Kind: FastMethodExprNumber, Const: v})
		return true
	case FastMethodExprLiteral:
		*ops = append(*ops, FastMethodOp{Kind: FastMethodExprLiteral, Const: expr.Literal})
		return true
	case FastMethodExprField:
		*ops = append(*ops, FastMethodOp{Kind: FastMethodExprField, Slot: expr.FieldSlot})
		return true
	case FastMethodExprNegate:
		if !buildPostfix(expr.Left, ops) {
			return false
		}
		*ops = append(*ops, FastMethodOp{Kind: FastMethodExprNegate})
		return true
	case FastMethodExprAdd, FastMethodExprSub, FastMethodExprMul, FastMethodExprDiv:
		if !buildPostfix(expr.Left, ops) || !buildPostfix(expr.Right, ops) {
			return false
		}
		*ops = append(*ops, FastMethodOp{Kind: expr.Kind})
		return true
	default:
		return false
	}
}

// EvalOps executes the flat postfix sequence against an instance.
// Uses a fixed-size stack to avoid allocations.
func (p *FastMethodPlan) EvalOps(inst *Instance) Value {
	var stack [16]Value
	sp := 0
	for i := range p.Ops {
		op := &p.Ops[i]
		switch op.Kind {
		case FastMethodExprField:
			stack[sp] = inst.Fields[op.Slot]
			sp++
		case FastMethodExprNumber, FastMethodExprLiteral:
			stack[sp] = op.Const
			sp++
		case FastMethodExprNegate:
			v := &stack[sp-1]
			if v.Kind == Number {
				if v.NumberKind == NumberInt {
					stack[sp-1] = IntValue(-v.Int)
				} else {
					stack[sp-1] = FloatValue(-v.Num)
				}
			}
		case FastMethodExprAdd, FastMethodExprSub, FastMethodExprMul, FastMethodExprDiv:
			sp--
			l := &stack[sp-1]
			r := &stack[sp]
			if l.Kind == Number && r.Kind == Number {
				if l.NumberKind == NumberInt && r.NumberKind == NumberInt {
					switch op.Kind {
					case FastMethodExprAdd:
						stack[sp-1] = IntValue(l.Int + r.Int)
					case FastMethodExprSub:
						stack[sp-1] = IntValue(l.Int - r.Int)
					case FastMethodExprMul:
						stack[sp-1] = IntValue(l.Int * r.Int)
					default:
						if r.Int != 0 {
							stack[sp-1] = IntValue(l.Int / r.Int)
						}
					}
				} else {
					switch op.Kind {
					case FastMethodExprAdd:
						stack[sp-1] = FloatValue(l.Num + r.Num)
					case FastMethodExprSub:
						stack[sp-1] = FloatValue(l.Num - r.Num)
					case FastMethodExprMul:
						stack[sp-1] = FloatValue(l.Num * r.Num)
					default:
						stack[sp-1] = FloatValue(l.Num / r.Num)
					}
				}
			}
		}
	}
	if sp == 0 {
		return NilValue()
	}
	return stack[0]
}

type Instance struct {
	Class  *Class
	Fields []Value
	Frozen bool
	Inline [1]Value
}

type BoundMethod struct {
	Receiver *Instance
	Name     string
	Method   *bytecode.Function
	Owner    *Class
}

type BoundStaticMethod struct {
	Class *Class
	Name  string
	Owner *Class
}

type Closure struct {
	Function *bytecode.Function
	Captures []*Cell
}

type SAMWrapper struct {
	InterfaceName string
	MethodName    string
	Callable      Value
}

type SAMBoundMethod struct {
	Wrapper *SAMWrapper
}

type Range struct {
	Start int
	End   int
	Step  int
}

type Iterator struct {
	Range    *Range
	Current  int
	Started  bool
	Items    []Value
	Index    int
	Receiver *Instance
	Length   int
	GetFn    *bytecode.Function
}

type Value struct {
	Kind       Kind
	Num        float64
	Int        int64
	NumberKind NumberKind
	Bool       bool
	Str        string
	Object     any
}

func NilValue() Value {
	return Value{Kind: Nil}
}

func NumberValue(v float64) Value {
	return FloatValue(v)
}

func IntValue(v int64) Value {
	return Value{Kind: Number, Num: float64(v), Int: v, NumberKind: NumberInt}
}

func FloatValue(v float64) Value {
	return Value{Kind: Number, Num: v, Int: int64(v), NumberKind: NumberFloat}
}

func BoolValue(v bool) Value {
	return Value{Kind: Bool, Bool: v}
}

func CharValue(v rune) Value {
	return Value{Kind: Char, Str: string(v)}
}

func StringValue(v string) Value {
	return Value{Kind: String, Str: v}
}

func ObjectValue(v any) Value {
	return Value{Kind: Object, Object: v}
}

func (v Value) IsTruthy() bool {
	switch v.Kind {
	case Nil:
		return false
	case Bool:
		return v.Bool
	default:
		return true
	}
}

func (v Value) AsFunction() (*bytecode.Function, bool) {
	fn, ok := v.Object.(*bytecode.Function)
	return fn, ok
}

func (v Value) AsClosure() (*Closure, bool) {
	closure, ok := v.Object.(*Closure)
	return closure, ok
}

func (v Value) AsBuiltin() (*Builtin, bool) {
	builtin, ok := v.Object.(*Builtin)
	return builtin, ok
}

func (v Value) AsModule() (*Module, bool) {
	module, ok := v.Object.(*Module)
	return module, ok
}

func (v Value) AsTuple() (*Tuple, bool) {
	tuple, ok := v.Object.(*Tuple)
	return tuple, ok
}

func (v Value) AsArray() (*Array, bool) {
	array, ok := v.Object.(*Array)
	return array, ok
}

func (v Value) AsMap() (*Map, bool) {
	m, ok := v.Object.(*Map)
	return m, ok
}

func (v Value) AsClass() (*Class, bool) {
	class, ok := v.Object.(*Class)
	return class, ok
}

func (v Value) AsInstance() (*Instance, bool) {
	instance, ok := v.Object.(*Instance)
	return instance, ok
}

func (v Value) AsBoundMethod() (*BoundMethod, bool) {
	method, ok := v.Object.(*BoundMethod)
	return method, ok
}

func (v Value) AsBoundStaticMethod() (*BoundStaticMethod, bool) {
	method, ok := v.Object.(*BoundStaticMethod)
	return method, ok
}

func (v Value) AsSAMWrapper() (*SAMWrapper, bool) {
	wrapper, ok := v.Object.(*SAMWrapper)
	return wrapper, ok
}

func (v Value) AsSAMBoundMethod() (*SAMBoundMethod, bool) {
	method, ok := v.Object.(*SAMBoundMethod)
	return method, ok
}

func (c *Class) LookupField(name string) (FieldDef, bool) {
	if field, ok := c.Fields[name]; ok {
		return field, true
	}
	if c.Superclass != nil {
		return c.Superclass.LookupField(name)
	}
	return FieldDef{}, false
}

func (c *Class) LookupFieldOwner(name string) (*Class, int, FieldDef, bool) {
	idx, ok := c.FieldIndex[name]
	if ok {
		field, ok := c.Fields[name]
		if ok {
			return c, idx, field, true
		}
	}
	if c.Superclass != nil {
		return c.Superclass.LookupFieldOwner(name)
	}
	return nil, 0, FieldDef{}, false
}

func (c *Class) LookupFieldSlot(name string) (int, FieldDef, bool) {
	idx, ok := c.FieldIndex[name]
	if !ok {
		return 0, FieldDef{}, false
	}
	field, ok := c.Fields[name]
	if !ok {
		return 0, FieldDef{}, false
	}
	return idx, field, true
}

func (c *Class) LookupMethod(name string) (*bytecode.Function, *Class, bool) {
	if method, ok := c.Methods[name]; ok {
		return method, c, true
	}
	if overloads, ok := c.MethodOverloads[name]; ok {
		for _, method := range overloads {
			if method != nil {
				return method, c, true
			}
		}
	}
	if c.Superclass != nil {
		return c.Superclass.LookupMethod(name)
	}
	return nil, nil, false
}

func (c *Class) LookupMethodByArity(name string, argc int) (*bytecode.Function, *Class, bool) {
	if overloads, ok := c.MethodOverloads[name]; ok {
		for _, method := range overloads {
			if method != nil && method.Arity == argc {
				return method, c, true
			}
		}
	}
	if method, ok := c.Methods[name]; ok && method != nil && method.Arity == argc {
		return method, c, true
	}
	if c.Superclass != nil {
		return c.Superclass.LookupMethodByArity(name, argc)
	}
	return nil, nil, false
}

func (c *Class) SetSpecialMethod(slot SpecialMethodSlot, fn *bytecode.Function) {
	if c.SpecialMethods == nil {
		c.SpecialMethods = make(map[SpecialMethodSlot]*bytecode.Function)
	}
	if fn == nil {
		delete(c.SpecialMethods, slot)
		return
	}
	c.SpecialMethods[slot] = fn
}

func (c *Class) DeclaredSpecialMethod(slot SpecialMethodSlot) (*bytecode.Function, bool) {
	if c == nil || c.SpecialMethods == nil {
		return nil, false
	}
	fn, ok := c.SpecialMethods[slot]
	return fn, ok && fn != nil
}

func (c *Class) SpecialMethod(slot SpecialMethodSlot) *bytecode.Function {
	if fn, ok := c.DeclaredSpecialMethod(slot); ok {
		return fn
	}
	if c != nil && c.Superclass != nil {
		return c.Superclass.SpecialMethod(slot)
	}
	return nil
}

func (c *Class) LookupMethodSlot(name string) (int, *bytecode.Function, bool) {
	idx, ok := c.MethodIndex[name]
	if !ok || idx < 0 || idx >= len(c.MethodTable) {
		return 0, nil, false
	}
	fn := c.MethodTable[idx]
	if fn == nil {
		return 0, nil, false
	}
	return idx, fn, true
}

func (c *Class) LookupMethodBySlot(slot int) (*bytecode.Function, bool) {
	if slot < 0 || slot >= len(c.MethodTable) {
		return nil, false
	}
	fn := c.MethodTable[slot]
	return fn, fn != nil
}

func (c *Class) LookupFastMethodBySlot(slot int) (*FastMethodPlan, bool) {
	if slot < 0 || slot >= len(c.FastMethods) {
		return nil, false
	}
	plan := c.FastMethods[slot]
	return plan, plan != nil
}

func (c *Class) LookupFastStaticMethod(name string, argc int) (*Class, *FastMethodPlan, bool) {
	if c == nil {
		return nil, nil, false
	}
	if plan, ok := c.FastStaticMethods[name]; ok && plan != nil && plan.Arity == argc {
		return c, plan, true
	}
	if c.Superclass != nil {
		return c.Superclass.LookupFastStaticMethod(name, argc)
	}
	return nil, nil, false
}

func (c *Class) LookupStatic(name string) (Value, bool) {
	if field, ok := c.StaticValues[name]; ok {
		return field, true
	}
	if method, ok := c.StaticMethods[name]; ok {
		return ObjectValue(method), true
	}
	if overloads, ok := c.StaticMethodOverloads[name]; ok {
		for _, method := range overloads {
			if method != nil {
				return ObjectValue(method), true
			}
		}
	}
	if c.Superclass != nil {
		return c.Superclass.LookupStatic(name)
	}
	return NilValue(), false
}

func (c *Class) LookupStaticCallableOwner(name string, argc int) (*Class, Value, bool) {
	if field, ok := c.StaticValues[name]; ok {
		return c, field, true
	}
	if overloads, ok := c.StaticMethodOverloads[name]; ok {
		for _, method := range overloads {
			if method != nil && method.Arity == argc {
				return c, ObjectValue(method), true
			}
		}
	}
	if method, ok := c.StaticMethods[name]; ok && method != nil && method.Arity == argc {
		return c, ObjectValue(method), true
	}
	if c.Superclass != nil {
		return c.Superclass.LookupStaticCallableOwner(name, argc)
	}
	return nil, NilValue(), false
}

func (c *Class) LookupConstructorByArity(argc int) (*bytecode.Function, bool) {
	for _, ctor := range c.ConstructorOverloads {
		if ctor != nil && ctor.Arity == argc {
			return ctor, true
		}
	}
	if c.Constructor != nil && c.Constructor.Arity == argc {
		return c.Constructor, true
	}
	return nil, false
}

func (c *Class) LookupStaticOwner(name string) (*Class, Value, bool) {
	if field, ok := c.StaticValues[name]; ok {
		return c, field, true
	}
	if method, ok := c.StaticMethods[name]; ok {
		return c, ObjectValue(method), true
	}
	if overloads, ok := c.StaticMethodOverloads[name]; ok {
		for _, method := range overloads {
			if method != nil {
				return c, ObjectValue(method), true
			}
		}
	}
	if c.Superclass != nil {
		return c.Superclass.LookupStaticOwner(name)
	}
	return nil, NilValue(), false
}

func (c *Class) SetStatic(name string, v Value) error {
	if _, ok := c.StaticFields[name]; ok {
		field := c.StaticFields[name]
		if !field.Mutable {
			return fmt.Errorf("static member %s.%s is immutable", c.Name, name)
		}
		c.StaticValues[name] = v
		return nil
	}
	if c.Superclass != nil {
		return c.Superclass.SetStatic(name, v)
	}
	return fmt.Errorf("class %s has no static member %s", c.Name, name)
}

func (c *Class) NewInstance() *Instance {
	if c.defaultFields == nil {
		c.defaultFields = make([]Value, len(c.FieldOrder))
		for idx, name := range c.FieldOrder {
			c.defaultFields[idx] = c.Fields[name].Default
		}
	}
	inst := &Instance{Class: c}
	n := len(c.defaultFields)
	switch n {
	case 0:
		inst.Fields = inst.Inline[:0]
	case 1:
		inst.Fields = inst.Inline[:1]
		inst.Fields[0] = c.defaultFields[0]
	default:
		inst.Fields = make([]Value, n)
		copy(inst.Fields, c.defaultFields)
	}
	return inst
}

func (i *Instance) GetField(name string) (Value, bool) {
	idx, _, ok := i.Class.LookupFieldSlot(name)
	if !ok {
		return NilValue(), false
	}
	return i.Fields[idx], true
}

func (i *Instance) SetField(name string, v Value) bool {
	idx, field, ok := i.Class.LookupFieldSlot(name)
	if !ok {
		return false
	}
	if i.Frozen {
		return false
	}
	if !field.Mutable {
		return false
	}
	i.Fields[idx] = v
	return true
}

func (v Value) AsRange() (*Range, bool) {
	rng, ok := v.Object.(*Range)
	return rng, ok
}

func (v Value) AsIterator() (*Iterator, bool) {
	it, ok := v.Object.(*Iterator)
	return it, ok
}

func Equal(left, right Value) bool {
	if left.Kind != right.Kind {
		return false
	}
	switch left.Kind {
	case Nil:
		return true
	case Number:
		if left.NumberKind == NumberInt && right.NumberKind == NumberInt {
			return left.Int == right.Int
		}
		return left.Num == right.Num
	case Bool:
		return left.Bool == right.Bool
	case Char:
		return left.Str == right.Str
	case String:
		return left.Str == right.Str
	default:
		if leftTuple, ok := left.AsTuple(); ok {
			rightTuple, ok := right.AsTuple()
			if !ok || len(leftTuple.Elements) != len(rightTuple.Elements) {
				return false
			}
			for i := range leftTuple.Elements {
				if !Equal(leftTuple.Elements[i], rightTuple.Elements[i]) {
					return false
				}
			}
			return true
		}
		if leftArray, ok := left.AsArray(); ok {
			rightArray, ok := right.AsArray()
			if !ok || len(leftArray.Elements) != len(rightArray.Elements) {
				return false
			}
			for i := range leftArray.Elements {
				if !Equal(leftArray.Elements[i], rightArray.Elements[i]) {
					return false
				}
			}
			return true
		}
		return left.Object == right.Object
	}
}

func (v Value) String() string {
	switch v.Kind {
	case Nil:
		return "nil"
	case Number:
		if v.NumberKind == NumberInt {
			return strconv.FormatInt(v.Int, 10)
		}
		// For floats, if it's effectively an integer and fits in reasonable range, show as int
		if v.Num == float64(int64(v.Num)) {
			// Ensure it differentiates correctly between integer addition and direct python matching.
			if v.Num > -1e15 && v.Num < 1e15 {
				return strconv.FormatInt(int64(v.Num), 10)
			}
		}
		return strconv.FormatFloat(v.Num, 'g', -1, 64)
	case Bool:
		if v.Bool {
			return "true"
		}
		return "false"
	case Char:
		return v.Str
	case String:
		return v.Str
	default:
		switch obj := v.Object.(type) {
		case *bytecode.Function:
			return fmt.Sprintf("<fn %s>", obj.Name)
		case *Closure:
			return fmt.Sprintf("<closure %s>", obj.Function.Name)
		case *SAMWrapper:
			return fmt.Sprintf("<%s wrapper>", obj.InterfaceName)
		case *Builtin:
			return fmt.Sprintf("<builtin %s>", obj.Name)
		case *Module:
			return fmt.Sprintf("<module %s>", obj.Name)
		case *Tuple:
			return fmt.Sprintf("<tuple %d>", len(obj.Elements))
		case *Array:
			parts := make([]string, len(obj.Elements))
			for i, element := range obj.Elements {
				parts[i] = element.String()
			}
			return "[" + join(parts, ", ") + "]"
		case *Map:
			parts := make([]string, 0, len(obj.Entries))
			for key, val := range obj.Entries {
				parts = append(parts, fmt.Sprintf("%s: %s", key, val.String()))
			}
			return "{" + join(parts, ", ") + "}"
		case *Class:
			return fmt.Sprintf("<class %s>", obj.Name)
		case *Instance:
			return fmt.Sprintf("<instance %s>", obj.Class.Name)
		case *BoundMethod:
			return fmt.Sprintf("<bound-method %s>", obj.Method.Name)
		case *Range:
			return fmt.Sprintf("<range %d..%d step=%d>", obj.Start, obj.End, obj.Step)
		case *Iterator:
			return "<iterator>"
		default:
			return fmt.Sprintf("<object %T>", obj)
		}
	}
}

func join(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, part := range parts[1:] {
		out += sep + part
	}
	return out
}
