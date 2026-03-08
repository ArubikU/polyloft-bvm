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
		f.Chunk.Constants[i] = SanitizeConst(c)
	}
	return gob.NewEncoder(w).Encode(f)
}

// SanitizeConst walks through a constant value and replaces any nil entries
// with NilConst{} so that gob encoding will succeed.  It handles slices,
// maps and nested combinations.
func SanitizeConst(c any) any {
	if c == nil {
		return NilConst{}
	}

	v := reflect.ValueOf(c)
	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return NilConst{}
		}
		vstruct := v.Elem()
		if vstruct.Kind() == reflect.Struct {
			for i := 0; i < vstruct.NumField(); i++ {
				field := vstruct.Field(i)
				if field.CanSet() {
					fk := field.Kind()
					if fk == reflect.Interface || fk == reflect.Slice || fk == reflect.Map || fk == reflect.Ptr {
						san := SanitizeConst(field.Interface())
						sanV := reflect.ValueOf(san)
						if sanV.IsValid() && sanV.Type().AssignableTo(field.Type()) {
							field.Set(sanV)
						}
					} else if fk == reflect.Struct {
						san := SanitizeConst(field.Interface())
						sanV := reflect.ValueOf(san)
						if sanV.IsValid() && sanV.Type().AssignableTo(field.Type()) {
							field.Set(sanV)
						}
					}
				}
			}
		}
		return c

	case reflect.Struct:
		newStruct := reflect.New(v.Type()).Elem()
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			newField := newStruct.Field(i)
			if newField.CanSet() {
				san := SanitizeConst(field.Interface())
				sanV := reflect.ValueOf(san)
				if sanV.IsValid() && sanV.Type().AssignableTo(newField.Type()) {
					newField.Set(sanV)
				} else if !sanV.IsValid() || sanV.Type().Kind() == reflect.Ptr {
					// keep zero element
				} else {
					newField.Set(field)
				}
			}
		}
		return newStruct.Interface()

	case reflect.Slice, reflect.Array:
		newSlice := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			elem := v.Index(i).Interface()
			san := SanitizeConst(elem)
			sanV := reflect.ValueOf(san)
			if sanV.IsValid() && sanV.Type().AssignableTo(v.Type().Elem()) {
				newSlice.Index(i).Set(sanV)
			} else {
				if !sanV.IsValid() || sanV.Type().Kind() == reflect.Ptr {
					// already zeroed by MakeSlice
				} else {
					newSlice.Index(i).Set(v.Index(i))
				}
			}
		}
		return newSlice.Interface()

	case reflect.Map:
		newMap := reflect.MakeMap(v.Type())
		for _, key := range v.MapKeys() {
			val := v.MapIndex(key).Interface()
			san := SanitizeConst(val)
			sanV := reflect.ValueOf(san)
			if sanV.IsValid() && sanV.Type().AssignableTo(v.Type().Elem()) {
				newMap.SetMapIndex(key, sanV)
			} else {
				if san == nil || reflect.TypeOf(san) == reflect.TypeOf(NilConst{}) {
					newMap.SetMapIndex(key, reflect.Zero(v.Type().Elem()))
				} else {
					newMap.SetMapIndex(key, v.MapIndex(key))
				}
			}
		}
		return newMap.Interface()

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
	var nested []*Function

	fmt.Fprintf(&out, "== %s ==\n", name)
	for offset := 0; offset < len(c.Code); {
		op := Op(c.Code[offset])
		line := c.Lines[offset]
		fmt.Fprintf(&out, "%04d L%-3d %-20s ", offset, line, op.String())
		switch op {
		case OpConstant, OpDefineGlobal, OpGetGlobal, OpSetGlobal, OpGetProperty, OpSetProperty, OpClosure:
			idx := readUint16(c.Code[offset+1:])
			constant := c.Constants[idx]
			fmt.Fprintf(&out, "%d (%v)", idx, constant)
			if fn, ok := constant.(*Function); ok {
				nested = append(nested, fn)
			}
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
		case OpAddLocalMulThisField:
			fmt.Fprintf(&out, "target=%d local=%d field=%d", c.Code[offset+1], c.Code[offset+2], c.Code[offset+3])
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
		case OpInvoke:
			idx := readUint16(c.Code[offset+1:])
			fmt.Fprintf(&out, "%v argc=%d", c.Constants[idx], c.Code[offset+3])
			offset += 4
		case OpInvokeMethod:
			fmt.Fprintf(&out, "slot=%d argc=%d", c.Code[offset+1], c.Code[offset+2])
			offset += 3
		case OpJump, OpJumpIfFalse, OpLoop:
			jump := readUint16(c.Code[offset+1:])
			fmt.Fprintf(&out, "%d", jump)
			offset += 3
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

	for _, fn := range nested {
		out.WriteByte('\n')
		out.WriteString(fn.Chunk.Disassemble(fn.Name))
	}

	return out.String()
}

func readUint16(code []byte) uint16 {
	return uint16(code[0])<<8 | uint16(code[1])
}
