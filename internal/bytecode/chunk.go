package bytecode

import (
	"fmt"
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
	return out.String()
}

func readUint16(code []byte) uint16 {
	return uint16(code[0])<<8 | uint16(code[1])
}
