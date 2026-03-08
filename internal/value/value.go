package value

import (
	"encoding/gob"
	"fmt"
	"math"
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
	TypeName   string
	Visibility string
}

type Cell struct {
	Value Value
}

type Class struct {
	Name                  string
	Superclass            *Class
	Implements            map[string]bool
	Permits               map[string]bool
	IsAbstract            bool
	IsEnum                bool
	IsSealed              bool
	IsRecord              bool
	EnumOrder             []string
	FastConstructor       *FastConstructorPlan
	FastMethods           []*FastMethodPlan
	Fields                map[string]FieldDef
	FieldOrder            []string
	FieldIndex            map[string]int
	MethodOrder           []string
	MethodIndex           map[string]int
	MethodTable           []*bytecode.Function
	Methods               map[string]*bytecode.Function
	Constructor           *bytecode.Function
	StaticFields          map[string]FieldDef
	StaticValues          map[string]Value
	StaticMethods         map[string]*bytecode.Function
	MethodVisibility      map[string]string
	StaticVisibility      map[string]string
	ConstructorVisibility string
	MethodAnnotations     map[string][]string
	IterableLength        *bytecode.Function
	IterableGet           *bytecode.Function
	PiecesMethod          *bytecode.Function
	GetPieceMethod        *bytecode.Function
	IndexGetMethod        *bytecode.Function
	IndexSetMethod        *bytecode.Function
	ContainsMethod        *bytecode.Function
	SliceMethod           *bytecode.Function
	EqualMethod           *bytecode.Function
	HashMethod            *bytecode.Function
}

type FastConstructorPlan struct {
	Arity      int
	FieldSlots []int
	ArgIndexes []int
}

type FastMethodExprKind uint8

const (
	FastMethodExprNumber FastMethodExprKind = iota
	FastMethodExprField
	FastMethodExprAdd
	FastMethodExprSub
	FastMethodExprMul
	FastMethodExprDiv
	FastMethodExprNegate
)

type FastMethodExpr struct {
	Kind      FastMethodExprKind
	Number    float64
	FieldSlot int
	Left      *FastMethodExpr
	Right     *FastMethodExpr
}

type FastMethodPlan struct {
	Arity int
	Expr  *FastMethodExpr
}

type Instance struct {
	Class  *Class
	Fields []Value
	Frozen bool
}

type BoundMethod struct {
	Receiver *Instance
	Method   *bytecode.Function
	Owner    *Class
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
	return Value{Kind: Number, Num: float64(v), NumberKind: NumberInt}
}

func FloatValue(v float64) Value {
	return Value{Kind: Number, Num: v, NumberKind: NumberFloat}
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
	if c.Superclass != nil {
		return c.Superclass.LookupMethod(name)
	}
	return nil, nil, false
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

func (c *Class) LookupStatic(name string) (Value, bool) {
	if field, ok := c.StaticValues[name]; ok {
		return field, true
	}
	if method, ok := c.StaticMethods[name]; ok {
		return ObjectValue(method), true
	}
	if c.Superclass != nil {
		return c.Superclass.LookupStatic(name)
	}
	return NilValue(), false
}

func (c *Class) LookupStaticOwner(name string) (*Class, Value, bool) {
	if field, ok := c.StaticValues[name]; ok {
		return c, field, true
	}
	if method, ok := c.StaticMethods[name]; ok {
		return c, ObjectValue(method), true
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
	instance := &Instance{Class: c, Fields: make([]Value, len(c.FieldOrder))}
	for idx, name := range c.FieldOrder {
		instance.Fields[idx] = c.Fields[name].Default
	}
	return instance
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
		if v.NumberKind == NumberInt && math.Trunc(v.Num) == v.Num {
			return strconv.FormatInt(int64(v.Num), 10)
		}
		return fmt.Sprintf("%g", v.Num)
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
