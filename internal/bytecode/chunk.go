package bytecode

import (
	"encoding/gob"
	"fmt"
	"io"
	"reflect"
	"strings"
)

type Chunk struct {
	Code      []byte
	Constants []any
	Lines     []int
}

type Function struct {
	Name            string
	Arity           int
	ParamTypes      []string
	OwnerClassName  string
	Upvalues        []UpvalueSpec
	ReturnType      string
	MaxLocals       int
	GlobalSlotCount int
	GlobalSlotNames []string
	Interfaces      map[string]InterfaceSpec
	Chunk           *Chunk
}

type UpvalueSpec struct {
	Index   byte
	IsLocal bool
}

// NilConst is used during serialization to represent a nil constant value.
// Gob cannot encode a nil element directly, so we substitute this sentinel.
type NilConst struct{}

type InterfaceMethodSpec struct {
	Name   string
	Params []string
	Return string
}

type InterfaceSpec struct {
	Name             string
	Methods          map[string]InterfaceMethodSpec
	FunctionalMethod string
}

func NewChunk() *Chunk {
	return &Chunk{}
}

// WriteTo serializes the function (including its chunk and constants) to a writer
// using gob encoding. This is used by the CLI "dump -o" command to produce
// .pfbc files.
func (f *Function) WriteTo(w io.Writer) error {
	registerGobTypes()
	// sanitize constants recursively so gob never sees a nil interface element
	for i, c := range f.Chunk.Constants {
		f.Chunk.Constants[i] = sanitizeConst(c)
	}
	return gob.NewEncoder(w).Encode(f)
}

// sanitizeConst walks through a constant value and replaces any nil entries
// with NilConst{} so that gob encoding will succeed.  It handles slices,
// maps and nested combinations.
func sanitizeConst(c any) any {
	if c == nil {
		return NilConst{}
	}
	// check for pointer to struct with Elements/Entries or Object field
	vptr := reflect.ValueOf(c)
	if vptr.Kind() == reflect.Ptr {
		vstruct := vptr.Elem()
		if vstruct.Kind() == reflect.Struct {
			// Elements field (for arrays)
			if field := vstruct.FieldByName("Elements"); field.IsValid() && field.Kind() == reflect.Slice {
				for i := 0; i < field.Len(); i++ {
					field.Index(i).Set(reflect.ValueOf(sanitizeConst(field.Index(i).Interface())))
				}
				return c
			}
			// Entries field (for maps)
			if field := vstruct.FieldByName("Entries"); field.IsValid() && field.Kind() == reflect.Map {
				for _, key := range field.MapKeys() {
					val := field.MapIndex(key).Interface()
					field.SetMapIndex(key, reflect.ValueOf(sanitizeConst(val)))
				}
				return c
			}
			// Object field (for value.Value and possibly others)
			if field := vstruct.FieldByName("Object"); field.IsValid() {
				obj := field.Interface()
				san := sanitizeConst(obj)
				field.Set(reflect.ValueOf(san))
				return c
			}
		}
	}
	// use reflection to handle slices/arrays/maps of arbitrary element types
	v := reflect.ValueOf(c)
	switch v.Kind() {
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			elem := v.Index(i).Interface()
			san := sanitizeConst(elem)
			v.Index(i).Set(reflect.ValueOf(san))
		}
		return v.Interface()
	case reflect.Map:
		if v.Type().Key().Kind() == reflect.String {
			for _, key := range v.MapKeys() {
				val := v.MapIndex(key).Interface()
				v.SetMapIndex(key, reflect.ValueOf(sanitizeConst(val)))
			}
		}
		return v.Interface()
	default:
		return c
	}
}

// ReadFunction deserializes a function from a reader produced by WriteTo.
func ReadFunction(r io.Reader) (*Function, error) {
	registerGobTypes()
	var fn Function
	if err := gob.NewDecoder(r).Decode(&fn); err != nil {
		return nil, err
	}
	return &fn, nil
}

// registerGobTypes ensures all relevant concrete types that may appear in
// bytecode constants or values are registered with gob.  This must be called
// before encoding or decoding.
func registerGobTypes() {
	// bytecode structures
	gob.Register(&Function{})
	gob.Register(InterfaceSpec{})
	gob.Register(InterfaceMethodSpec{})
	// sentinel
	gob.Register(NilConst{})
	// value package handles its own types; register our own helpers
}

func (c *Chunk) WriteOp(op Op, line int) int {
	return c.EmitByte(byte(op), line)
}

func (c *Chunk) EmitByte(b byte, line int) int {
	c.Code = append(c.Code, b)
	c.Lines = append(c.Lines, line)
	return len(c.Code) - 1
}

func (c *Chunk) WriteUint16(v uint16, line int) int {
	c.EmitByte(byte(v>>8), line)
	return c.EmitByte(byte(v), line)
}

func (c *Chunk) AddConstant(v any) uint16 {
	c.Constants = append(c.Constants, v)
	return uint16(len(c.Constants) - 1)
}

func (c *Chunk) PatchUint16(offset int, v uint16) {
	c.Code[offset] = byte(v >> 8)
	c.Code[offset+1] = byte(v)
}

func (c *Chunk) Disassemble(name string) string {
	var out strings.Builder
	fmt.Fprintf(&out, "== %s ==\n", name)
	for offset := 0; offset < len(c.Code); {
		op := Op(c.Code[offset])
		line := c.Lines[offset]
		fmt.Fprintf(&out, "%04d L%-3d %-14s", offset, line, op.String())
		switch op {
		case OpConstant, OpDefineGlobal, OpGetGlobal, OpSetGlobal, OpGetProperty, OpSetProperty, OpClosure:
			idx := readUint16(c.Code[offset+1:])
			fmt.Fprintf(&out, "%d (%v)", idx, c.Constants[idx])
			offset += 3
		case OpWrapInterface:
			ifaceIdx := readUint16(c.Code[offset+1:])
			methodIdx := readUint16(c.Code[offset+3:])
			fmt.Fprintf(&out, "%v.%v", c.Constants[ifaceIdx], c.Constants[methodIdx])
			offset += 5
		case OpMatchType, OpCastRef:
			idx := readUint16(c.Code[offset+1:])
			fmt.Fprintf(&out, "%d (%v)", idx, c.Constants[idx])
			offset += 3
		case OpGetLocal, OpSetLocal, OpGetCapture, OpSetCapture, OpCall, OpCallSuper, OpRange, OpTuple, OpUnpack, OpGetField, OpSetField, OpGetThisField, OpSetThisField, OpDefineGlobalSlot, OpGetGlobalSlot, OpSetGlobalSlot, OpArray, OpMap:
			fmt.Fprintf(&out, "%d", c.Code[offset+1])
			offset += 2
		case OpCallGlobalSlot:
			fmt.Fprintf(&out, "slot=%d argc=%d", c.Code[offset+1], c.Code[offset+2])
			offset += 3
		case OpAddLocalMulThisField:
			fmt.Fprintf(&out, "target=%d local=%d field=%d", c.Code[offset+1], c.Code[offset+2], c.Code[offset+3])
			offset += 4
		case OpAddLocalMulLocal:
			fmt.Fprintf(&out, "target=%d left=%d right=%d", c.Code[offset+1], c.Code[offset+2], c.Code[offset+3])
			offset += 4
		case OpAppendLocalString:
			fmt.Fprintf(&out, "slot=%d", c.Code[offset+1])
			offset += 2
		case OpCallConst:
			idx := readUint16(c.Code[offset+1:])
			fmt.Fprintf(&out, "%v argc=%d", c.Constants[idx], c.Code[offset+3])
			offset += 4
		case OpAddConstLocalInt:
			idx := readUint16(c.Code[offset+2:])
			fmt.Fprintf(&out, "slot=%d const=%v", c.Code[offset+1], c.Constants[idx])
			offset += 4
		case OpCallConstLocalSubInt:
			callableIdx := readUint16(c.Code[offset+1:])
			constIdx := readUint16(c.Code[offset+4:])
			fmt.Fprintf(&out, "%v slot=%d sub=%v", c.Constants[callableIdx], c.Code[offset+3], c.Constants[constIdx])
			offset += 6
		case OpCallSelfLocalSubInt:
			constIdx := readUint16(c.Code[offset+2:])
			fmt.Fprintf(&out, "slot=%d sub=%v", c.Code[offset+1], c.Constants[constIdx])
			offset += 4
		case OpIterInit:
			fmt.Fprintf(&out, "slot=%d mode=%d", c.Code[offset+1], c.Code[offset+2])
			offset += 3
		case OpRangeInitFast:
			fmt.Fprintf(&out, "current=%d end=%d step=%d argc=%d", c.Code[offset+1], c.Code[offset+2], c.Code[offset+3], c.Code[offset+4])
			offset += 5
		case OpRangeNextFast:
			fmt.Fprintf(&out, "current=%d end=%d step=%d value=%d exit=%d", c.Code[offset+1], c.Code[offset+2], c.Code[offset+3], c.Code[offset+4], readUint16(c.Code[offset+5:]))
			offset += 7
		case OpJumpIfLocalLessEqualIntConstFalse, OpJumpIfLocalDivisibleByIntConstFalse:
			idx := readUint16(c.Code[offset+2:])
			fmt.Fprintf(&out, "slot=%d const=%v jump=%d", c.Code[offset+1], c.Constants[idx], readUint16(c.Code[offset+4:]))
			offset += 6
		case OpInvoke:
			idx := readUint16(c.Code[offset+1:])
			fmt.Fprintf(&out, "%v argc=%d", c.Constants[idx], c.Code[offset+3])
			offset += 4
		case OpInvokeSuper:
			idx := readUint16(c.Code[offset+1:])
			fmt.Fprintf(&out, "%v argc=%d", c.Constants[idx], c.Code[offset+3])
			offset += 4
		case OpInvokeMethod:
			fmt.Fprintf(&out, "slot=%d argc=%d", c.Code[offset+1], c.Code[offset+2])
			offset += 3
		case OpPushHandler, OpJump, OpJumpIfFalse, OpLoop:
			jump := readUint16(c.Code[offset+1:])
			fmt.Fprintf(&out, "%d", jump)
			offset += 3
		case OpAddNum, OpSubNum, OpMulNum, OpPowNum, OpDivNum, OpModNum, OpLessNum, OpGreaterNum, OpPopHandler, OpThrow, OpReturn, OpNil, OpTrue, OpFalse, OpPop, OpDup, OpDupTwo, OpEqual, OpGreater, OpLess, OpAdd, OpSub, OpMul, OpPow, OpDiv, OpMod, OpNot, OpNegate, OpFreeze:
			offset++
		case OpIterNext:
			fmt.Fprintf(&out, "iter=%d value=%d exit=%d", c.Code[offset+1], c.Code[offset+2], readUint16(c.Code[offset+3:]))
			offset += 5
		case OpGetIndex, OpGetIndexArray, OpGetIndexMap, OpSetIndex, OpSetIndexArray, OpSetIndexMap, OpSlice, OpCastInt, OpCastFloat:
			offset++
		default:
			offset++
		}
		out.WriteByte('\n')
	}
	return out.String()
}

func readUint16(code []byte) uint16 {
	return uint16(code[0])<<8 | uint16(code[1])
}
