package jit

import (
	"runtime"

	"github.com/ArubikU/polyloft-bvm/internal/bytecode"
	"github.com/ArubikU/polyloft-bvm/internal/value"
)

type irOp uint8

const (
	irLoadLocal irOp = iota
	irSetLocal
	irLoadGlobalSlot
	irDefineGlobalSlot
	irLoadField
	irLoadConst
	irClosure
	irLoadNil
	irLoadTrue
	irLoadFalse
	irPop
	irCallGlobalSlot
	irCallConst
	irInvokeMember
	irRangeInitFast
	irRangeNextFast
	irCastInt
	irCastFloat
	irAddValue
	irArray
	irMap
	irGetIndex
	irGetIndexArray
	irGetIndexMap
	irSetIndex
	irSetIndexArray
	irSetIndexMap
	irSlice
	irAddLocalMulLocal
	irEqual
	irLessNum
	irGreaterNum
	irNot
	irAddNumber
	irSubNumber
	irMulNumber
	irDivNumber
	irModNumber
	irNegNumber
	irJump
	irJumpIfFalse
	irJumpIfTrue
	irReturn
)

type irInstruction struct {
	opcode  irOp
	operand uint16
	aux     uint16
}

type Instruction struct {
	Opcode  byte
	Operand uint16
	Aux     uint16
}

type InstructionSpec struct {
	BytecodeOp  bytecode.Op
	Name        string
	BackendCode byte
	Implemented bool
}

type Program struct {
	Name         string
	Backend      string
	TargetOS     string
	TargetArch   string
	Arity        int
	LocalCount   int
	UsesReceiver bool
	MaxStack     int
	Constants    []value.Value
	Metadata     []any
	Code         []Instruction
}

type RuntimeContext struct {
	GlobalSlots   []value.Value
	GlobalDefined []bool
}

type rangeInitMeta struct {
	CurrentSlot byte
	EndSlot     byte
	StepSlot    byte
	Argc        int
}

type rangeNextMeta struct {
	CurrentSlot byte
	EndSlot     byte
	StepSlot    byte
	ValueSlot   byte
}

type backend interface {
	name() string
	encode(fnName string, arity int, localCount int, usesReceiver bool, maxStack int, constants []value.Value, metadata []any, ir []irInstruction) *Program
	instructions() []InstructionSpec
}

type opcodeSet struct {
	loadLocal        byte
	setLocal         byte
	loadGlobalSlot   byte
	defineGlobalSlot byte
	loadField        byte
	loadConst        byte
	closure          byte
	loadNil          byte
	loadTrue         byte
	loadFalse        byte
	pop              byte
	callGlobalSlot   byte
	callConst        byte
	invokeMember     byte
	rangeInitFast    byte
	rangeNextFast    byte
	castInt          byte
	castFloat        byte
	addValue         byte
	array            byte
	mapValue         byte
	getIndex         byte
	getIndexArray    byte
	getIndexMap      byte
	setIndex         byte
	setIndexArray    byte
	setIndexMap      byte
	slice            byte
	addLocalMulLocal byte
	equal            byte
	lessNum          byte
	greaterNum       byte
	not              byte
	addNumber        byte
	subNumber        byte
	mulNumber        byte
	divNumber        byte
	modNumber        byte
	negNumber        byte
	jump             byte
	jumpIfFalse      byte
	jumpIfTrue       byte
	ret              byte
}

func encodeInstructions(ir []irInstruction, ops opcodeSet) []Instruction {
	code := make([]Instruction, len(ir))
	for i, instruction := range ir {
		code[i] = Instruction{Opcode: opcodeFor(instruction.opcode, ops), Operand: instruction.operand, Aux: instruction.aux}
	}
	return code
}

func opcodeFor(op irOp, ops opcodeSet) byte {
	switch op {
	case irLoadLocal:
		return ops.loadLocal
	case irSetLocal:
		return ops.setLocal
	case irLoadGlobalSlot:
		return ops.loadGlobalSlot
	case irDefineGlobalSlot:
		return ops.defineGlobalSlot
	case irLoadField:
		return ops.loadField
	case irLoadConst:
		return ops.loadConst
	case irClosure:
		return ops.closure
	case irLoadNil:
		return ops.loadNil
	case irLoadTrue:
		return ops.loadTrue
	case irLoadFalse:
		return ops.loadFalse
	case irPop:
		return ops.pop
	case irCallGlobalSlot:
		return ops.callGlobalSlot
	case irCallConst:
		return ops.callConst
	case irInvokeMember:
		return ops.invokeMember
	case irRangeInitFast:
		return ops.rangeInitFast
	case irRangeNextFast:
		return ops.rangeNextFast
	case irCastInt:
		return ops.castInt
	case irCastFloat:
		return ops.castFloat
	case irAddValue:
		return ops.addValue
	case irArray:
		return ops.array
	case irMap:
		return ops.mapValue
	case irGetIndex:
		return ops.getIndex
	case irGetIndexArray:
		return ops.getIndexArray
	case irGetIndexMap:
		return ops.getIndexMap
	case irSetIndex:
		return ops.setIndex
	case irSetIndexArray:
		return ops.setIndexArray
	case irSetIndexMap:
		return ops.setIndexMap
	case irSlice:
		return ops.slice
	case irAddLocalMulLocal:
		return ops.addLocalMulLocal
	case irEqual:
		return ops.equal
	case irLessNum:
		return ops.lessNum
	case irGreaterNum:
		return ops.greaterNum
	case irNot:
		return ops.not
	case irAddNumber:
		return ops.addNumber
	case irSubNumber:
		return ops.subNumber
	case irMulNumber:
		return ops.mulNumber
	case irDivNumber:
		return ops.divNumber
	case irModNumber:
		return ops.modNumber
	case irNegNumber:
		return ops.negNumber
	case irJump:
		return ops.jump
	case irJumpIfFalse:
		return ops.jumpIfFalse
	case irJumpIfTrue:
		return ops.jumpIfTrue
	case irReturn:
		return ops.ret
	default:
		return 0
	}
}

func buildProgram(name string, backendName string, ops opcodeSet, arity int, localCount int, usesReceiver bool, maxStack int, constants []value.Value, metadata []any, ir []irInstruction) *Program {
	return &Program{
		Name:         name,
		Backend:      backendName,
		TargetOS:     runtime.GOOS,
		TargetArch:   runtime.GOARCH,
		Arity:        arity,
		LocalCount:   localCount,
		UsesReceiver: usesReceiver,
		MaxStack:     maxStack,
		Constants:    constants,
		Metadata:     metadata,
		Code:         encodeInstructions(ir, ops),
	}
}

func AllInstructionSpecs() []bytecode.Op {
	return []bytecode.Op{
		bytecode.OpConstant,
		bytecode.OpNil,
		bytecode.OpTrue,
		bytecode.OpFalse,
		bytecode.OpPop,
		bytecode.OpDup,
		bytecode.OpDupTwo,
		bytecode.OpGetLocal,
		bytecode.OpSetLocal,
		bytecode.OpGetCapture,
		bytecode.OpSetCapture,
		bytecode.OpDefineGlobal,
		bytecode.OpGetGlobal,
		bytecode.OpSetGlobal,
		bytecode.OpDefineGlobalSlot,
		bytecode.OpGetGlobalSlot,
		bytecode.OpSetGlobalSlot,
		bytecode.OpEqual,
		bytecode.OpGreater,
		bytecode.OpLess,
		bytecode.OpAdd,
		bytecode.OpSub,
		bytecode.OpMul,
		bytecode.OpPow,
		bytecode.OpDiv,
		bytecode.OpMod,
		bytecode.OpNot,
		bytecode.OpNegate,
		bytecode.OpPushHandler,
		bytecode.OpPopHandler,
		bytecode.OpThrow,
		bytecode.OpJump,
		bytecode.OpJumpIfFalse,
		bytecode.OpJumpIfTrue,
		bytecode.OpLoop,
		bytecode.OpAddNum,
		bytecode.OpSubNum,
		bytecode.OpMulNum,
		bytecode.OpPowNum,
		bytecode.OpDivNum,
		bytecode.OpModNum,
		bytecode.OpLessNum,
		bytecode.OpGreaterNum,
		bytecode.OpAddLocalMulThisField,
		bytecode.OpAddLocalMulLocal,
		bytecode.OpAddLocalMulThisFieldAddThisField,
		bytecode.OpAppendLocalString,
		bytecode.OpClosure,
		bytecode.OpCall,
		bytecode.OpCallConst,
		bytecode.OpAddConstLocalInt,
		bytecode.OpCallConstLocalSubInt,
		bytecode.OpCallSelfLocalSubInt,
		bytecode.OpCallGlobalSlot,
		bytecode.OpInvoke,
		bytecode.OpInvokeMethod,
		bytecode.OpCallSuper,
		bytecode.OpInvokeSuper,
		bytecode.OpRange,
		bytecode.OpRangeInitFast,
		bytecode.OpRangeNextFast,
		bytecode.OpJumpIfLocalLessEqualIntConstFalse,
		bytecode.OpJumpIfLocalDivisibleByIntConstFalse,
		bytecode.OpIterInit,
		bytecode.OpIterNext,
		bytecode.OpGetField,
		bytecode.OpSetField,
		bytecode.OpGetThisField,
		bytecode.OpSetThisField,
		bytecode.OpGetProperty,
		bytecode.OpSetProperty,
		bytecode.OpArray,
		bytecode.OpMap,
		bytecode.OpGetIndex,
		bytecode.OpGetIndexArray,
		bytecode.OpGetIndexMap,
		bytecode.OpContains,
		bytecode.OpContainsArray,
		bytecode.OpContainsMap,
		bytecode.OpContainsString,
		bytecode.OpSetIndex,
		bytecode.OpSetIndexArray,
		bytecode.OpSetIndexMap,
		bytecode.OpSlice,
		bytecode.OpCastInt,
		bytecode.OpCastFloat,
		bytecode.OpCastRef,
		bytecode.OpMatchType,
		bytecode.OpWrapInterface,
		bytecode.OpTuple,
		bytecode.OpUnpack,
		bytecode.OpFreeze,
		bytecode.OpReturn,
	}
}

func implementedBytecodeOps() map[bytecode.Op]bool {
	return map[bytecode.Op]bool{
		bytecode.OpGetLocal:         true,
		bytecode.OpSetLocal:         true,
		bytecode.OpGetGlobalSlot:    true,
		bytecode.OpDefineGlobalSlot: true,
		bytecode.OpGetThisField:     true,
		bytecode.OpConstant:         true,
		bytecode.OpClosure:          true,
		bytecode.OpNil:              true,
		bytecode.OpTrue:             true,
		bytecode.OpFalse:            true,
		bytecode.OpPop:              true,
		bytecode.OpCallGlobalSlot:   true,
		bytecode.OpCallConst:        true,
		bytecode.OpInvoke:           true,
		bytecode.OpRangeInitFast:    true,
		bytecode.OpRangeNextFast:    true,
		bytecode.OpCastInt:          true,
		bytecode.OpCastFloat:        true,
		bytecode.OpAdd:              true,
		bytecode.OpArray:            true,
		bytecode.OpMap:              true,
		bytecode.OpGetIndex:         true,
		bytecode.OpGetIndexArray:    true,
		bytecode.OpGetIndexMap:      true,
		bytecode.OpSetIndex:         true,
		bytecode.OpSetIndexArray:    true,
		bytecode.OpSetIndexMap:      true,
		bytecode.OpSlice:            true,
		bytecode.OpAddLocalMulLocal: true,
		bytecode.OpEqual:            true,
		bytecode.OpLessNum:          true,
		bytecode.OpGreaterNum:       true,
		bytecode.OpNot:              true,
		bytecode.OpAddNum:           true,
		bytecode.OpSub:              true,
		bytecode.OpSubNum:           true,
		bytecode.OpMulNum:           true,
		bytecode.OpDivNum:           true,
		bytecode.OpModNum:           true,
		bytecode.OpNegate:           true,
		bytecode.OpJump:             true,
		bytecode.OpJumpIfFalse:      true,
		bytecode.OpJumpIfTrue:       true,
		bytecode.OpLoop:             true,
		bytecode.OpReturn:           true,
	}
}

func buildInstructionCatalog(base byte, backendName string) []InstructionSpec {
	implemented := implementedBytecodeOps()
	ops := AllInstructionSpecs()
	catalog := make([]InstructionSpec, len(ops))
	for i, op := range ops {
		catalog[i] = InstructionSpec{
			BytecodeOp:  op,
			Name:        op.String(),
			BackendCode: base + byte(i),
			Implemented: implemented[op],
		}
	}
	_ = backendName
	return catalog
}

func backendInstructionCatalog(name string) ([]InstructionSpec, bool) {
	switch name {
	case "windows-stackabi-v1":
		return buildInstructionCatalog(0x10, name), true
	case "posix-stackabi-v1":
		return buildInstructionCatalog(0x80, name), true
	default:
		return nil, false
	}
}

func InstructionCatalogForBackend(name string) ([]InstructionSpec, bool) {
	return backendInstructionCatalog(name)
}

func opcodeSetForBackend(name string) (opcodeSet, bool) {
	catalog, ok := backendInstructionCatalog(name)
	if !ok {
		return opcodeSet{}, false
	}
	lookup := make(map[bytecode.Op]byte, len(catalog))
	for _, spec := range catalog {
		lookup[spec.BytecodeOp] = spec.BackendCode
	}
	return opcodeSet{
		loadLocal:        lookup[bytecode.OpGetLocal],
		setLocal:         lookup[bytecode.OpSetLocal],
		loadGlobalSlot:   lookup[bytecode.OpGetGlobalSlot],
		defineGlobalSlot: lookup[bytecode.OpDefineGlobalSlot],
		loadField:        lookup[bytecode.OpGetThisField],
		loadConst:        lookup[bytecode.OpConstant],
		closure:          lookup[bytecode.OpClosure],
		loadNil:          lookup[bytecode.OpNil],
		loadTrue:         lookup[bytecode.OpTrue],
		loadFalse:        lookup[bytecode.OpFalse],
		pop:              lookup[bytecode.OpPop],
		callGlobalSlot:   lookup[bytecode.OpCallGlobalSlot],
		callConst:        lookup[bytecode.OpCallConst],
		invokeMember:     lookup[bytecode.OpInvoke],
		rangeInitFast:    lookup[bytecode.OpRangeInitFast],
		rangeNextFast:    lookup[bytecode.OpRangeNextFast],
		castInt:          lookup[bytecode.OpCastInt],
		castFloat:        lookup[bytecode.OpCastFloat],
		addValue:         lookup[bytecode.OpAdd],
		array:            lookup[bytecode.OpArray],
		mapValue:         lookup[bytecode.OpMap],
		getIndex:         lookup[bytecode.OpGetIndex],
		getIndexArray:    lookup[bytecode.OpGetIndexArray],
		getIndexMap:      lookup[bytecode.OpGetIndexMap],
		setIndex:         lookup[bytecode.OpSetIndex],
		setIndexArray:    lookup[bytecode.OpSetIndexArray],
		setIndexMap:      lookup[bytecode.OpSetIndexMap],
		slice:            lookup[bytecode.OpSlice],
		addLocalMulLocal: lookup[bytecode.OpAddLocalMulLocal],
		equal:            lookup[bytecode.OpEqual],
		lessNum:          lookup[bytecode.OpLessNum],
		greaterNum:       lookup[bytecode.OpGreaterNum],
		not:              lookup[bytecode.OpNot],
		addNumber:        lookup[bytecode.OpAddNum],
		subNumber:        lookup[bytecode.OpSubNum],
		mulNumber:        lookup[bytecode.OpMulNum],
		divNumber:        lookup[bytecode.OpDivNum],
		modNumber:        lookup[bytecode.OpModNum],
		negNumber:        lookup[bytecode.OpNegate],
		jump:             lookup[bytecode.OpJump],
		jumpIfFalse:      lookup[bytecode.OpJumpIfFalse],
		jumpIfTrue:       lookup[bytecode.OpJumpIfTrue],
		ret:              lookup[bytecode.OpReturn],
	}, true
}
