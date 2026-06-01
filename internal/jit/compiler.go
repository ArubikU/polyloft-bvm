package jit

import (
	"fmt"
	"io"
	"math"
	"sync"

	"github.com/ArubikU/polyloft-bvm/internal/bytecode"
	"github.com/ArubikU/polyloft-bvm/internal/value"
)

type Stats struct {
	Compiled    int
	Executed    int
	Rejected    int
	SkippedCold int
}

type Engine struct {
	backend         backend
	warmupThreshold int
	logger          io.Writer
	mu              sync.RWMutex
	cache           map[*bytecode.Function]*Program
	rejected        map[*bytecode.Function]bool
	callCounts      map[*bytecode.Function]int
	stats           Stats
}

func NewEngine() *Engine {
	return NewEngineWithThreshold(64)
}

func NewEngineWithThreshold(threshold int) *Engine {
	if threshold < 1 {
		threshold = 1
	}
	return &Engine{
		backend:         selectBackend(),
		warmupThreshold: threshold,
		cache:           make(map[*bytecode.Function]*Program),
		rejected:        make(map[*bytecode.Function]bool),
		callCounts:      make(map[*bytecode.Function]int),
	}
}

func (engine *Engine) SetWarmupThreshold(threshold int) {
	if threshold < 1 {
		threshold = 1
	}
	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.warmupThreshold = threshold
}

func (engine *Engine) WarmupThreshold() int {
	engine.mu.RLock()
	defer engine.mu.RUnlock()
	return engine.warmupThreshold
}

func (engine *Engine) SetLogger(w io.Writer) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.logger = w
}

func (engine *Engine) Stats() Stats {
	engine.mu.RLock()
	defer engine.mu.RUnlock()
	return engine.stats
}

func (engine *Engine) Backend() string {
	return engine.backend.name()
}

func (engine *Engine) TryExecute(fn *bytecode.Function, receiver *value.Instance, args []value.Value) (value.Value, bool, error) {
	return engine.TryExecuteWithContext(fn, receiver, args, nil)
}

func (engine *Engine) TryExecuteWithContext(fn *bytecode.Function, receiver *value.Instance, args []value.Value, runtime *RuntimeContext) (value.Value, bool, error) {
	program := engine.lookup(fn)
	if program == nil {
		engine.mu.Lock()
		if engine.rejected[fn] {
			engine.logLocked("skip rejected fn=%s", safeFunctionName(fn))
			engine.mu.Unlock()
			return value.NilValue(), false, nil
		}
		engine.callCounts[fn]++
		callCount := engine.callCounts[fn]
		threshold := engine.warmupThreshold
		if callCount < threshold {
			engine.stats.SkippedCold++
			engine.logLocked("cold fn=%s calls=%d threshold=%d", safeFunctionName(fn), callCount, threshold)
			engine.mu.Unlock()
			return value.NilValue(), false, nil
		}
		engine.logLocked("hot fn=%s calls=%d threshold=%d backend=%s", safeFunctionName(fn), callCount, threshold, engine.backend.name())
		engine.mu.Unlock()
		compiled, ok, reason := compileFunction(engine.backend, fn)
		engine.mu.Lock()
		if ok {
			engine.cache[fn] = compiled
			engine.stats.Compiled++
			program = compiled
			engine.logLocked("compiled fn=%s backend=%s instructions=%d", compiled.Name, compiled.Backend, len(compiled.Code))
		} else {
			engine.rejected[fn] = true
			engine.stats.Rejected++
			engine.logLocked("rejected fn=%s backend=%s reason=%s", safeFunctionName(fn), engine.backend.name(), reason)
		}
		engine.mu.Unlock()
		if !ok {
			return value.NilValue(), false, nil
		}
	}
	result, err := executeProgram(program, receiver, args, runtime)
	if err != nil {
		return value.NilValue(), false, err
	}
	engine.mu.Lock()
	engine.stats.Executed++
	engine.logLocked("executed fn=%s backend=%s", program.Name, program.Backend)
	engine.mu.Unlock()
	return result, true, nil
}

func (engine *Engine) logLocked(format string, args ...any) {
	if engine.logger == nil {
		return
	}
	_, _ = fmt.Fprintf(engine.logger, "[jit] "+format+"\n", args...)
}

func safeFunctionName(fn *bytecode.Function) string {
	if fn == nil || fn.Name == "" {
		return "<anonymous>"
	}
	return fn.Name
}

func (engine *Engine) lookup(fn *bytecode.Function) *Program {
	engine.mu.RLock()
	defer engine.mu.RUnlock()
	if engine.rejected[fn] {
		return nil
	}
	return engine.cache[fn]
}

func compileFunction(target backend, fn *bytecode.Function) (*Program, bool, string) {
	if fn == nil || fn.Chunk == nil || len(fn.Upvalues) != 0 {
		return nil, false, "missing chunk or unsupported captures"
	}
	ir, constants, metadata, usesReceiver, maxStack, ok, reason := lowerBytecode(fn)
	if !ok {
		return nil, false, reason
	}
	return target.encode(fn.Name, fn.Arity, fn.MaxLocals, usesReceiver, maxStack, constants, metadata, ir), true, ""
}

func lowerBytecode(fn *bytecode.Function) ([]irInstruction, []value.Value, []any, bool, int, bool, string) {
	code := fn.Chunk.Code
	stackDepth := 0
	maxStack := 0
	usesReceiver := false
	ir := make([]irInstruction, 0, len(code))
	constants := make([]value.Value, 0)
	metadata := make([]any, 0)
	constantIndex := make(map[string]uint16)
	metadataIndex := make(map[string]uint16)
	offsetToIndex := make(map[int]int)
	targetStackDepth := make(map[int]int)
	type jumpPatch struct {
		irIndex    int
		targetByte int
	}
	patches := make([]jumpPatch, 0)
	recordTargetDepth := func(target int, depth int) {
		if existing, ok := targetStackDepth[target]; !ok || depth > existing {
			targetStackDepth[target] = depth
		}
	}
	addMetadata := func(key string, entry any) uint16 {
		if idx, exists := metadataIndex[key]; exists {
			return idx
		}
		idx := uint16(len(metadata))
		metadataIndex[key] = idx
		metadata = append(metadata, entry)
		return idx
	}

	for offset := 0; offset < len(code); {
		if depth, ok := targetStackDepth[offset]; ok && depth > stackDepth {
			stackDepth = depth
		}
		op := bytecode.Op(code[offset])
		offsetToIndex[offset] = len(ir)
		switch op {
		case bytecode.OpGetLocal:
			if offset+1 >= len(code) {
				return nil, nil, nil, false, 0, false, "truncated GET_LOCAL"
			}
			ir = append(ir, irInstruction{opcode: irLoadLocal, operand: uint16(code[offset+1])})
			stackDepth++
			offset += 2
		case bytecode.OpSetLocal:
			if offset+1 >= len(code) || stackDepth < 1 {
				return nil, nil, nil, false, 0, false, "invalid SET_LOCAL"
			}
			ir = append(ir, irInstruction{opcode: irSetLocal, operand: uint16(code[offset+1])})
			offset += 2
		case bytecode.OpGetGlobalSlot:
			if offset+1 >= len(code) {
				return nil, nil, nil, false, 0, false, "truncated GET_GLOBAL_SLOT"
			}
			ir = append(ir, irInstruction{opcode: irLoadGlobalSlot, operand: uint16(code[offset+1])})
			stackDepth++
			offset += 2
		case bytecode.OpDefineGlobalSlot:
			if offset+1 >= len(code) || stackDepth < 1 {
				return nil, nil, nil, false, 0, false, "invalid DEFINE_GLOBAL_SLOT"
			}
			ir = append(ir, irInstruction{opcode: irDefineGlobalSlot, operand: uint16(code[offset+1])})
			stackDepth--
			offset += 2
		case bytecode.OpGetThisField:
			if offset+1 >= len(code) {
				return nil, nil, nil, false, 0, false, "truncated GET_THIS_FIELD"
			}
			usesReceiver = true
			ir = append(ir, irInstruction{opcode: irLoadField, operand: uint16(code[offset+1])})
			stackDepth++
			offset += 2
		case bytecode.OpConstant:
			if offset+2 >= len(code) {
				return nil, nil, nil, false, 0, false, "truncated CONSTANT"
			}
			idx := readUint16(code[offset+1:])
			constant, ok := jitConstantValue(fn.Chunk.Constants[idx])
			if !ok {
				return nil, nil, nil, false, 0, false, fmt.Sprintf("unsupported constant %T", fn.Chunk.Constants[idx])
			}
			key := constantCacheKey(constant)
			jitIndex, exists := constantIndex[key]
			if !exists {
				jitIndex = uint16(len(constants))
				constantIndex[key] = jitIndex
				constants = append(constants, constant)
			}
			ir = append(ir, irInstruction{opcode: irLoadConst, operand: jitIndex})
			stackDepth++
			offset += 3
		case bytecode.OpClosure:
			if offset+2 >= len(code) {
				return nil, nil, nil, false, 0, false, "truncated CLOSURE"
			}
			idx := readUint16(code[offset+1:])
			raw := fn.Chunk.Constants[idx]
			compiledFn, ok := raw.(*bytecode.Function)
			if !ok {
				return nil, nil, nil, false, 0, false, fmt.Sprintf("unsupported closure constant %T", raw)
			}
			if len(compiledFn.Upvalues) != 0 {
				return nil, nil, nil, false, 0, false, "closures with captures not supported in jit"
			}
			callable := value.ObjectValue(compiledFn)
			key := constantCacheKey(callable)
			jitIndex, exists := constantIndex[key]
			if !exists {
				jitIndex = uint16(len(constants))
				constantIndex[key] = jitIndex
				constants = append(constants, callable)
			}
			ir = append(ir, irInstruction{opcode: irClosure, operand: jitIndex})
			stackDepth++
			offset += 3
		case bytecode.OpNil:
			ir = append(ir, irInstruction{opcode: irLoadNil})
			stackDepth++
			offset++
		case bytecode.OpTrue:
			ir = append(ir, irInstruction{opcode: irLoadTrue})
			stackDepth++
			offset++
		case bytecode.OpFalse:
			ir = append(ir, irInstruction{opcode: irLoadFalse})
			stackDepth++
			offset++
		case bytecode.OpPop:
			if stackDepth < 1 {
				return nil, nil, nil, false, 0, false, "stack underflow POP"
			}
			ir = append(ir, irInstruction{opcode: irPop})
			stackDepth--
			offset++
		case bytecode.OpCallGlobalSlot:
			if offset+2 >= len(code) || stackDepth < int(code[offset+2]) {
				return nil, nil, nil, false, 0, false, "invalid CALL_GLOBAL_SLOT"
			}
			ir = append(ir, irInstruction{opcode: irCallGlobalSlot, operand: uint16(code[offset+1]), aux: uint16(code[offset+2])})
			stackDepth = stackDepth - int(code[offset+2]) + 1
			offset += 3
		case bytecode.OpCallConst:
			if offset+3 >= len(code) || stackDepth < int(code[offset+3]) {
				return nil, nil, nil, false, 0, false, "invalid CALL_CONST"
			}
			idx := readUint16(code[offset+1:])
			constant, ok := jitConstantValue(fn.Chunk.Constants[idx])
			if !ok {
				return nil, nil, nil, false, 0, false, fmt.Sprintf("unsupported call const %T", fn.Chunk.Constants[idx])
			}
			if fnValue, ok := constant.AsFunction(); ok {
				if len(fnValue.Upvalues) != 0 {
					return nil, nil, nil, false, 0, false, "call const with captures not supported in jit"
				}
				if _, _, _, _, _, ok, reason := lowerBytecode(fnValue); !ok {
					return nil, nil, nil, false, 0, false, "call const target not jit-compilable: " + reason
				}
			}
			key := constantCacheKey(constant)
			jitIndex, exists := constantIndex[key]
			if !exists {
				jitIndex = uint16(len(constants))
				constantIndex[key] = jitIndex
				constants = append(constants, constant)
			}
			ir = append(ir, irInstruction{opcode: irCallConst, operand: jitIndex, aux: uint16(code[offset+3])})
			stackDepth = stackDepth - int(code[offset+3]) + 1
			offset += 4
		case bytecode.OpInvoke:
			if offset+3 >= len(code) || stackDepth < int(code[offset+3])+1 {
				return nil, nil, nil, false, 0, false, "invalid INVOKE"
			}
			ir = append(ir, irInstruction{opcode: irInvokeMember, operand: readUint16(code[offset+1:]), aux: uint16(code[offset+3])})
			stackDepth -= int(code[offset+3])
			offset += 4
		case bytecode.OpRangeInitFast:
			if offset+4 >= len(code) || stackDepth < int(code[offset+4]) {
				return nil, nil, nil, false, 0, false, "invalid RANGE_INIT_FAST"
			}
			meta := rangeInitMeta{CurrentSlot: code[offset+1], EndSlot: code[offset+2], StepSlot: code[offset+3], Argc: int(code[offset+4])}
			metaIdx := addMetadata(fmt.Sprintf("rangeinit:%d:%d:%d:%d", meta.CurrentSlot, meta.EndSlot, meta.StepSlot, meta.Argc), meta)
			ir = append(ir, irInstruction{opcode: irRangeInitFast, operand: metaIdx})
			stackDepth -= meta.Argc
			offset += 5
		case bytecode.OpRangeNextFast:
			if offset+6 >= len(code) {
				return nil, nil, nil, false, 0, false, "truncated RANGE_NEXT_FAST"
			}
			meta := rangeNextMeta{CurrentSlot: code[offset+1], EndSlot: code[offset+2], StepSlot: code[offset+3], ValueSlot: code[offset+4]}
			metaIdx := addMetadata(fmt.Sprintf("rangenext:%d:%d:%d:%d", meta.CurrentSlot, meta.EndSlot, meta.StepSlot, meta.ValueSlot), meta)
			ir = append(ir, irInstruction{opcode: irRangeNextFast, aux: metaIdx})
			patches = append(patches, jumpPatch{irIndex: len(ir) - 1, targetByte: offset + 7 + int(readUint16(code[offset+5:]))})
			offset += 7
		case bytecode.OpCastInt:
			if stackDepth < 1 {
				return nil, nil, nil, false, 0, false, "stack underflow CAST_INT"
			}
			ir = append(ir, irInstruction{opcode: irCastInt})
			offset++
		case bytecode.OpCastFloat:
			if stackDepth < 1 {
				return nil, nil, nil, false, 0, false, "stack underflow CAST_FLOAT"
			}
			ir = append(ir, irInstruction{opcode: irCastFloat})
			offset++
		case bytecode.OpAdd:
			if stackDepth < 2 {
				return nil, nil, nil, false, 0, false, "stack underflow ADD"
			}
			ir = append(ir, irInstruction{opcode: irAddValue})
			stackDepth--
			offset++
		case bytecode.OpSub:
			if stackDepth < 2 {
				return nil, nil, nil, false, 0, false, "stack underflow SUB"
			}
			ir = append(ir, irInstruction{opcode: irSubNumber})
			stackDepth--
			offset++
		case bytecode.OpMul:
			if stackDepth < 2 {
				return nil, nil, nil, false, 0, false, "stack underflow MUL"
			}
			ir = append(ir, irInstruction{opcode: irMulNumber})
			stackDepth--
			offset++
		case bytecode.OpDiv:
			if stackDepth < 2 {
				return nil, nil, nil, false, 0, false, "stack underflow DIV"
			}
			ir = append(ir, irInstruction{opcode: irDivNumber})
			stackDepth--
			offset++
		case bytecode.OpMod:
			if stackDepth < 2 {
				return nil, nil, nil, false, 0, false, "stack underflow MOD"
			}
			ir = append(ir, irInstruction{opcode: irModNumber})
			stackDepth--
			offset++
		case bytecode.OpAddLocalMulLocal:
			if offset+3 >= len(code) {
				return nil, nil, nil, false, 0, false, "truncated ADD_LOCAL_MUL_LOCAL"
			}
			ir = append(ir, irInstruction{opcode: irAddLocalMulLocal, operand: uint16(code[offset+1]) | uint16(code[offset+2])<<8, aux: uint16(code[offset+3])})
			offset += 4
		case bytecode.OpArray:
			if offset+1 >= len(code) {
				return nil, nil, nil, false, 0, false, "truncated ARRAY"
			}
			count := int(code[offset+1])
			if stackDepth < count {
				return nil, nil, nil, false, 0, false, "stack underflow ARRAY"
			}
			ir = append(ir, irInstruction{opcode: irArray, operand: uint16(count)})
			stackDepth = stackDepth - count + 1
			offset += 2
		case bytecode.OpMap:
			if offset+1 >= len(code) {
				return nil, nil, nil, false, 0, false, "truncated MAP"
			}
			count := int(code[offset+1])
			if stackDepth < count*2 {
				return nil, nil, nil, false, 0, false, "stack underflow MAP"
			}
			ir = append(ir, irInstruction{opcode: irMap, operand: uint16(count)})
			stackDepth = stackDepth - (count * 2) + 1
			offset += 2
		case bytecode.OpGetIndex, bytecode.OpGetIndexArray, bytecode.OpGetIndexMap:
			if stackDepth < 2 {
				return nil, nil, nil, false, 0, false, "stack underflow GET_INDEX"
			}
			mapped := irGetIndex
			switch op {
			case bytecode.OpGetIndexArray:
				mapped = irGetIndexArray
			case bytecode.OpGetIndexMap:
				mapped = irGetIndexMap
			}
			ir = append(ir, irInstruction{opcode: mapped})
			stackDepth--
			offset++
		case bytecode.OpSetIndex, bytecode.OpSetIndexArray, bytecode.OpSetIndexMap:
			if stackDepth < 3 {
				return nil, nil, nil, false, 0, false, "stack underflow SET_INDEX"
			}
			mapped := irSetIndex
			switch op {
			case bytecode.OpSetIndexArray:
				mapped = irSetIndexArray
			case bytecode.OpSetIndexMap:
				mapped = irSetIndexMap
			}
			ir = append(ir, irInstruction{opcode: mapped})
			stackDepth -= 3
			offset++
		case bytecode.OpSlice:
			if stackDepth < 3 {
				return nil, nil, nil, false, 0, false, "stack underflow SLICE"
			}
			ir = append(ir, irInstruction{opcode: irSlice})
			stackDepth -= 2
			offset++
		case bytecode.OpEqual:
			if stackDepth < 2 {
				return nil, nil, nil, false, 0, false, "stack underflow EQUAL"
			}
			ir = append(ir, irInstruction{opcode: irEqual})
			stackDepth--
			offset++
		case bytecode.OpLessNum:
			if stackDepth < 2 {
				return nil, nil, nil, false, 0, false, "stack underflow LESS_NUM"
			}
			ir = append(ir, irInstruction{opcode: irLessNum})
			stackDepth--
			offset++
		case bytecode.OpGreaterNum:
			if stackDepth < 2 {
				return nil, nil, nil, false, 0, false, "stack underflow GREATER_NUM"
			}
			ir = append(ir, irInstruction{opcode: irGreaterNum})
			stackDepth--
			offset++
		case bytecode.OpNot:
			if stackDepth < 1 {
				return nil, nil, nil, false, 0, false, "stack underflow NOT"
			}
			ir = append(ir, irInstruction{opcode: irNot})
			offset++
		case bytecode.OpAddNum, bytecode.OpSubNum, bytecode.OpMulNum, bytecode.OpDivNum, bytecode.OpModNum:
			if stackDepth < 2 {
				return nil, nil, nil, false, 0, false, "stack underflow numeric op"
			}
			mapped := irAddNumber
			switch op {
			case bytecode.OpSubNum:
				mapped = irSubNumber
			case bytecode.OpMulNum:
				mapped = irMulNumber
			case bytecode.OpDivNum:
				mapped = irDivNumber
			case bytecode.OpModNum:
				mapped = irModNumber
			}
			ir = append(ir, irInstruction{opcode: mapped})
			stackDepth--
			offset++
		case bytecode.OpNegate:
			if stackDepth < 1 {
				return nil, nil, nil, false, 0, false, "stack underflow NEGATE"
			}
			ir = append(ir, irInstruction{opcode: irNegNumber})
			offset++
		case bytecode.OpJump:
			if offset+2 >= len(code) {
				return nil, nil, nil, false, 0, false, "truncated JUMP"
			}
			ir = append(ir, irInstruction{opcode: irJump})
			target := offset + 3 + int(readUint16(code[offset+1:]))
			patches = append(patches, jumpPatch{irIndex: len(ir) - 1, targetByte: target})
			recordTargetDepth(target, stackDepth)
			offset += 3
		case bytecode.OpJumpIfFalse:
			if offset+2 >= len(code) || stackDepth < 1 {
				return nil, nil, nil, false, 0, false, "invalid JUMP_IF_FALSE"
			}
			ir = append(ir, irInstruction{opcode: irJumpIfFalse})
			target := offset + 3 + int(readUint16(code[offset+1:]))
			patches = append(patches, jumpPatch{irIndex: len(ir) - 1, targetByte: target})
			recordTargetDepth(target, stackDepth)
			offset += 3
		case bytecode.OpJumpIfTrue:
			if offset+2 >= len(code) || stackDepth < 1 {
				return nil, nil, nil, false, 0, false, "invalid JUMP_IF_TRUE"
			}
			ir = append(ir, irInstruction{opcode: irJumpIfTrue})
			target := offset + 3 + int(readUint16(code[offset+1:]))
			patches = append(patches, jumpPatch{irIndex: len(ir) - 1, targetByte: target})
			recordTargetDepth(target, stackDepth)
			offset += 3
		case bytecode.OpLoop:
			if offset+2 >= len(code) {
				return nil, nil, nil, false, 0, false, "truncated LOOP"
			}
			ir = append(ir, irInstruction{opcode: irJump})
			target := offset + 3 - int(readUint16(code[offset+1:]))
			patches = append(patches, jumpPatch{irIndex: len(ir) - 1, targetByte: target})
			recordTargetDepth(target, stackDepth)
			offset += 3
		case bytecode.OpReturn:
			if stackDepth < 1 {
				return nil, nil, nil, false, 0, false, "stack underflow RETURN"
			}
			ir = append(ir, irInstruction{opcode: irReturn})
			if stackDepth > maxStack {
				maxStack = stackDepth
			}
			if !hasOnlyDeadEpilogue(code[offset+1:]) {
				return nil, nil, nil, false, 0, false, "live code after RETURN"
			}
			offsetToIndex[len(code)] = len(ir)
			for _, patch := range patches {
				target, ok := offsetToIndex[patch.targetByte]
				if !ok {
					return nil, nil, nil, false, 0, false, "invalid jump target"
				}
				ir[patch.irIndex].operand = uint16(target)
			}
			return ir, constants, metadata, usesReceiver, maxStack, true, ""
		default:
			return nil, nil, nil, false, 0, false, fmt.Sprintf("unsupported opcode %s", op.String())
		}
		if stackDepth > maxStack {
			maxStack = stackDepth
		}
	}
	return nil, nil, nil, false, 0, false, "function terminated without return"
}

func hasOnlyDeadEpilogue(code []byte) bool {
	for offset := 0; offset < len(code); {
		op := bytecode.Op(code[offset])
		switch op {
		case bytecode.OpNil, bytecode.OpReturn:
			offset++
		default:
			return false
		}
	}
	return true
}

func jitConstantValue(constant any) (value.Value, bool) {
	switch candidate := constant.(type) {
	case int:
		return value.IntValue(int64(candidate)), true
	case int64:
		return value.IntValue(candidate), true
	case *bytecode.Function:
		return value.ObjectValue(candidate), true
	case float64:
		return value.FloatValue(candidate), true
	case string:
		return value.StringValue(candidate), true
	case rune:
		return value.CharValue(candidate), true
	case bool:
		return value.BoolValue(candidate), true
	case value.Value:
		if candidate.Kind == value.Number || candidate.Kind == value.Nil || candidate.Kind == value.Bool || candidate.Kind == value.String || candidate.Kind == value.Char {
			return candidate, true
		}
		return value.NilValue(), false
	default:
		return value.NilValue(), false
	}
}

func constantCacheKey(v value.Value) string {
	switch v.Kind {
	case value.Nil:
		return "nil"
	case value.Number:
		if v.NumberKind == value.NumberInt {
			return fmt.Sprintf("num:%d:%d", v.NumberKind, v.Int)
		}
		return fmt.Sprintf("num:%d:%g", v.NumberKind, v.Num)
	case value.Bool:
		return fmt.Sprintf("bool:%t", v.Bool)
	case value.String, value.Char:
		return fmt.Sprintf("str:%d:%s", v.Kind, v.Str)
	default:
		return fmt.Sprintf("kind:%d", v.Kind)
	}
}

func executeProgram(program *Program, receiver *value.Instance, args []value.Value, runtime *RuntimeContext) (value.Value, error) {
	if program == nil {
		return value.NilValue(), fmt.Errorf("missing jit program")
	}
	ops, ok := opcodeSetForBackend(program.Backend)
	if !ok {
		return value.NilValue(), fmt.Errorf("unknown jit backend %s", program.Backend)
	}
	if len(args) != program.Arity {
		return value.NilValue(), fmt.Errorf("%s expects %d args, got %d", program.Name, program.Arity, len(args))
	}

	locals := make([]value.Value, program.LocalCount)
	base := 0
	if receiver != nil {
		if len(locals) > 0 {
			locals[0] = value.ObjectValue(receiver)
		}
		base = 1
	}
	for i, arg := range args {
		slot := base + i
		if slot >= len(locals) {
			return value.NilValue(), fmt.Errorf("jit local slot %d out of range for %s", slot, program.Name)
		}
		locals[slot] = arg
	}
	stack := make([]value.Value, 0, max(1, program.MaxStack))
	for ip := 0; ip < len(program.Code); {
		instruction := program.Code[ip]
		switch instruction.Opcode {
		case ops.loadLocal:
			slot := int(instruction.Operand)
			if slot < 0 || slot >= len(locals) {
				return value.NilValue(), fmt.Errorf("jit local slot %d out of range for %s", slot, program.Name)
			}
			stack = append(stack, locals[slot])
			ip++
		case ops.setLocal:
			slot := int(instruction.Operand)
			if len(stack) < 1 {
				return value.NilValue(), fmt.Errorf("jit stack underflow in %s", program.Name)
			}
			if slot < 0 || slot >= len(locals) {
				return value.NilValue(), fmt.Errorf("jit local slot %d out of range for %s", slot, program.Name)
			}
			locals[slot] = stack[len(stack)-1]
			ip++
		case ops.loadGlobalSlot:
			slot := int(instruction.Operand)
			if runtime == nil || slot < 0 || slot >= len(runtime.GlobalSlots) || slot >= len(runtime.GlobalDefined) || !runtime.GlobalDefined[slot] {
				return value.NilValue(), fmt.Errorf("jit global slot %d is undefined for %s", slot, program.Name)
			}
			stack = append(stack, runtime.GlobalSlots[slot])
			ip++
		case ops.defineGlobalSlot:
			slot := int(instruction.Operand)
			if len(stack) < 1 {
				return value.NilValue(), fmt.Errorf("jit stack underflow in %s", program.Name)
			}
			if runtime == nil || slot < 0 || slot >= len(runtime.GlobalSlots) || slot >= len(runtime.GlobalDefined) {
				return value.NilValue(), fmt.Errorf("jit global slot %d out of range for %s", slot, program.Name)
			}
			runtime.GlobalSlots[slot] = stack[len(stack)-1]
			runtime.GlobalDefined[slot] = true
			stack = stack[:len(stack)-1]
			ip++
		case ops.loadField:
			slot := int(instruction.Operand)
			if receiver == nil {
				return value.NilValue(), fmt.Errorf("jit field load requires receiver for %s", program.Name)
			}
			if slot < 0 || slot >= len(receiver.Fields) {
				return value.NilValue(), fmt.Errorf("jit field slot %d out of range for %s", slot, receiver.Class.Name)
			}
			stack = append(stack, receiver.Fields[slot])
			ip++
		case ops.loadConst:
			idx := int(instruction.Operand)
			if idx < 0 || idx >= len(program.Constants) {
				return value.NilValue(), fmt.Errorf("jit constant %d out of range for %s", idx, program.Name)
			}
			stack = append(stack, program.Constants[idx])
			ip++
		case ops.closure:
			idx := int(instruction.Operand)
			if idx < 0 || idx >= len(program.Constants) {
				return value.NilValue(), fmt.Errorf("jit constant %d out of range for %s", idx, program.Name)
			}
			stack = append(stack, program.Constants[idx])
			ip++
		case ops.loadNil:
			stack = append(stack, value.NilValue())
			ip++
		case ops.loadTrue:
			stack = append(stack, value.BoolValue(true))
			ip++
		case ops.loadFalse:
			stack = append(stack, value.BoolValue(false))
			ip++
		case ops.pop:
			if len(stack) < 1 {
				return value.NilValue(), fmt.Errorf("jit stack underflow in %s", program.Name)
			}
			stack = stack[:len(stack)-1]
			ip++
		case ops.callGlobalSlot:
			slot := int(instruction.Operand)
			argc := int(instruction.Aux)
			if len(stack) < argc {
				return value.NilValue(), fmt.Errorf("jit stack underflow in %s", program.Name)
			}
			if runtime == nil || slot < 0 || slot >= len(runtime.GlobalSlots) || slot >= len(runtime.GlobalDefined) || !runtime.GlobalDefined[slot] {
				return value.NilValue(), fmt.Errorf("jit global slot %d is undefined for %s", slot, program.Name)
			}
			callArgs := append([]value.Value(nil), stack[len(stack)-argc:]...)
			stack = stack[:len(stack)-argc]
			result, err := jitCallValue(runtime.GlobalSlots[slot], callArgs, runtime, program.Name)
			if err != nil {
				return value.NilValue(), err
			}
			stack = append(stack, result)
			ip++
		case ops.callConst:
			argc := int(instruction.Aux)
			if len(stack) < argc {
				return value.NilValue(), fmt.Errorf("jit stack underflow in %s", program.Name)
			}
			constIdx := int(instruction.Operand)
			if constIdx < 0 || constIdx >= len(program.Constants) {
				return value.NilValue(), fmt.Errorf("jit constant %d out of range for %s", constIdx, program.Name)
			}
			callee := program.Constants[constIdx]
			callArgs := append([]value.Value(nil), stack[len(stack)-argc:]...)
			stack = stack[:len(stack)-argc]
			result, err := jitCallValue(callee, callArgs, runtime, program.Name)
			if err != nil {
				return value.NilValue(), err
			}
			stack = append(stack, result)
			ip++
		case ops.invokeMember:
			argc := int(instruction.Aux)
			if len(stack) < argc+1 {
				return value.NilValue(), fmt.Errorf("jit stack underflow in %s", program.Name)
			}
			nameIdx := int(instruction.Operand)
			if nameIdx < 0 || nameIdx >= len(program.Constants) {
				return value.NilValue(), fmt.Errorf("jit constant %d out of range for %s", nameIdx, program.Name)
			}
			name := program.Constants[nameIdx].Str
			callArgs := append([]value.Value(nil), stack[len(stack)-argc:]...)
			receiverValue := stack[len(stack)-argc-1]
			stack = stack[:len(stack)-argc-1]
			result, err := jitInvokeMember(receiverValue, name, callArgs, runtime, program.Name)
			if err != nil {
				return value.NilValue(), err
			}
			stack = append(stack, result)
			ip++
		case ops.rangeInitFast:
			meta, ok := program.Metadata[int(instruction.Operand)].(rangeInitMeta)
			if !ok {
				return value.NilValue(), fmt.Errorf("jit invalid range init metadata in %s", program.Name)
			}
			if len(stack) < meta.Argc {
				return value.NilValue(), fmt.Errorf("jit stack underflow in %s", program.Name)
			}
			if err := jitInitFastRange(locals, &stack, meta); err != nil {
				return value.NilValue(), err
			}
			ip++
		case ops.rangeNextFast:
			meta, ok := program.Metadata[int(instruction.Aux)].(rangeNextMeta)
			if !ok {
				return value.NilValue(), fmt.Errorf("jit invalid range next metadata in %s", program.Name)
			}
			advance, err := jitRangeNextFast(locals, meta)
			if err != nil {
				return value.NilValue(), err
			}
			if !advance {
				ip = int(instruction.Operand)
				continue
			}
			locals[int(meta.ValueSlot)] = locals[int(meta.CurrentSlot)]
			ip++
		case ops.castInt:
			if len(stack) < 1 {
				return value.NilValue(), fmt.Errorf("jit stack underflow in %s", program.Name)
			}
			operand := stack[len(stack)-1]
			if operand.Kind != value.Number {
				return value.NilValue(), fmt.Errorf("jit cast int expects number in %s", program.Name)
			}
			if operand.NumberKind == value.NumberInt {
				stack[len(stack)-1] = value.IntValue(operand.Int)
			} else {
				stack[len(stack)-1] = value.IntValue(int64(operand.Num))
			}
			ip++
		case ops.castFloat:
			if len(stack) < 1 {
				return value.NilValue(), fmt.Errorf("jit stack underflow in %s", program.Name)
			}
			operand := stack[len(stack)-1]
			if operand.Kind != value.Number {
				return value.NilValue(), fmt.Errorf("jit cast float expects number in %s", program.Name)
			}
			stack[len(stack)-1] = value.FloatValue(operand.Num)
			ip++
		case ops.addValue:
			if len(stack) < 2 {
				return value.NilValue(), fmt.Errorf("jit stack underflow in %s", program.Name)
			}
			right := stack[len(stack)-1]
			left := stack[len(stack)-2]
			stack = stack[:len(stack)-2]
			result, err := jitAddValues(left, right, program.Name)
			if err != nil {
				return value.NilValue(), err
			}
			stack = append(stack, result)
			ip++
		case ops.array:
			count := int(instruction.Operand)
			if len(stack) < count {
				return value.NilValue(), fmt.Errorf("jit stack underflow in %s", program.Name)
			}
			elements := make([]value.Value, count)
			copy(elements, stack[len(stack)-count:])
			stack = stack[:len(stack)-count]
			stack = append(stack, value.ObjectValue(&value.Array{Elements: elements}))
			ip++
		case ops.mapValue:
			count := int(instruction.Operand)
			if len(stack) < count*2 {
				return value.NilValue(), fmt.Errorf("jit stack underflow in %s", program.Name)
			}
			entries := make(map[string]value.Value, count)
			for i := 0; i < count; i++ {
				val := stack[len(stack)-1]
				key := stack[len(stack)-2]
				stack = stack[:len(stack)-2]
				if key.Kind != value.String {
					return value.NilValue(), fmt.Errorf("map key must be string")
				}
				entries[key.Str] = val
			}
			stack = append(stack, value.ObjectValue(&value.Map{Entries: entries}))
			ip++
		case ops.getIndex, ops.getIndexArray, ops.getIndexMap:
			if len(stack) < 2 {
				return value.NilValue(), fmt.Errorf("jit stack underflow in %s", program.Name)
			}
			index := stack[len(stack)-1]
			object := stack[len(stack)-2]
			stack = stack[:len(stack)-2]
			result, err := jitGetIndex(instruction.Opcode, ops, object, index)
			if err != nil {
				return value.NilValue(), err
			}
			stack = append(stack, result)
			ip++
		case ops.setIndex, ops.setIndexArray, ops.setIndexMap:
			if len(stack) < 3 {
				return value.NilValue(), fmt.Errorf("jit stack underflow in %s", program.Name)
			}
			assigned := stack[len(stack)-1]
			index := stack[len(stack)-2]
			object := stack[len(stack)-3]
			stack = stack[:len(stack)-3]
			if err := jitSetIndex(instruction.Opcode, ops, object, index, assigned); err != nil {
				return value.NilValue(), err
			}
			ip++
		case ops.slice:
			if len(stack) < 3 {
				return value.NilValue(), fmt.Errorf("jit stack underflow in %s", program.Name)
			}
			end := stack[len(stack)-1]
			start := stack[len(stack)-2]
			object := stack[len(stack)-3]
			stack = stack[:len(stack)-3]
			result, err := jitSlice(object, start, end)
			if err != nil {
				return value.NilValue(), err
			}
			stack = append(stack, result)
			ip++
		case ops.addLocalMulLocal:
			targetSlot := int(instruction.Operand & 0x00FF)
			leftSlot := int((instruction.Operand >> 8) & 0x00FF)
			rightSlot := int(instruction.Aux)
			if targetSlot >= len(locals) || leftSlot >= len(locals) || rightSlot >= len(locals) {
				return value.NilValue(), fmt.Errorf("jit local slot out of range for %s", program.Name)
			}
			target := locals[targetSlot]
			left := locals[leftSlot]
			right := locals[rightSlot]
			result, err := jitAddLocalMulLocal(target, left, right, program.Name)
			if err != nil {
				return value.NilValue(), err
			}
			locals[targetSlot] = result
			ip++
		case ops.equal:
			if len(stack) < 2 {
				return value.NilValue(), fmt.Errorf("jit stack underflow in %s", program.Name)
			}
			right := stack[len(stack)-1]
			left := stack[len(stack)-2]
			stack = stack[:len(stack)-2]
			stack = append(stack, value.BoolValue(jitValuesEqual(left, right)))
			ip++
		case ops.lessNum, ops.greaterNum:
			if len(stack) < 2 {
				return value.NilValue(), fmt.Errorf("jit stack underflow in %s", program.Name)
			}
			right := stack[len(stack)-1]
			left := stack[len(stack)-2]
			stack = stack[:len(stack)-2]
			result, err := executeNumericCompare(instruction.Opcode, ops, left, right, program.Name)
			if err != nil {
				return value.NilValue(), err
			}
			stack = append(stack, result)
			ip++
		case ops.not:
			if len(stack) < 1 {
				return value.NilValue(), fmt.Errorf("jit stack underflow in %s", program.Name)
			}
			operand := stack[len(stack)-1]
			stack[len(stack)-1] = value.BoolValue(!operand.IsTruthy())
			ip++
		case ops.negNumber:
			if len(stack) < 1 {
				return value.NilValue(), fmt.Errorf("jit stack underflow in %s", program.Name)
			}
			operand := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if operand.Kind != value.Number {
				return value.NilValue(), fmt.Errorf("jit negate expects number in %s", program.Name)
			}
			if operand.NumberKind == value.NumberInt {
				stack = append(stack, value.IntValue(-operand.Int))
			} else {
				stack = append(stack, value.FloatValue(-operand.Num))
			}
			ip++
		case ops.addNumber, ops.subNumber, ops.mulNumber, ops.divNumber, ops.modNumber:
			if len(stack) < 2 {
				return value.NilValue(), fmt.Errorf("jit stack underflow in %s", program.Name)
			}
			right := stack[len(stack)-1]
			left := stack[len(stack)-2]
			stack = stack[:len(stack)-2]
			result, err := executeNumeric(instruction.Opcode, ops, left, right, program.Name)
			if err != nil {
				return value.NilValue(), err
			}
			stack = append(stack, result)
			ip++
		case ops.jump:
			ip = int(instruction.Operand)
		case ops.jumpIfFalse:
			if len(stack) < 1 {
				return value.NilValue(), fmt.Errorf("jit stack underflow in %s", program.Name)
			}
			if !stack[len(stack)-1].IsTruthy() {
				ip = int(instruction.Operand)
				continue
			}
			ip++
		case ops.jumpIfTrue:
			if len(stack) < 1 {
				return value.NilValue(), fmt.Errorf("jit stack underflow in %s", program.Name)
			}
			if stack[len(stack)-1].IsTruthy() {
				ip = int(instruction.Operand)
				continue
			}
			ip++
		case ops.ret:
			if len(stack) < 1 {
				return value.NilValue(), fmt.Errorf("jit return stack underflow in %s", program.Name)
			}
			return stack[len(stack)-1], nil
		default:
			return value.NilValue(), fmt.Errorf("unsupported jit opcode %d in %s", instruction.Opcode, program.Name)
		}
	}
	return value.NilValue(), fmt.Errorf("jit program %s terminated without return", program.Name)
}

func executeNumericCompare(opcode byte, ops opcodeSet, left value.Value, right value.Value, name string) (value.Value, error) {
	if left.Kind != value.Number || right.Kind != value.Number {
		return value.NilValue(), fmt.Errorf("jit numeric compare expects numbers in %s", name)
	}
	switch opcode {
	case ops.lessNum:
		if left.NumberKind == value.NumberInt && right.NumberKind == value.NumberInt {
			return value.BoolValue(left.Int < right.Int), nil
		}
		return value.BoolValue(left.Num < right.Num), nil
	case ops.greaterNum:
		if left.NumberKind == value.NumberInt && right.NumberKind == value.NumberInt {
			return value.BoolValue(left.Int > right.Int), nil
		}
		return value.BoolValue(left.Num > right.Num), nil
	default:
		return value.NilValue(), fmt.Errorf("unsupported numeric compare jit opcode %d in %s", opcode, name)
	}
}

func jitValuesEqual(left value.Value, right value.Value) bool {
	if left.Kind != right.Kind {
		return false
	}
	switch left.Kind {
	case value.Nil:
		return true
	case value.Bool:
		return left.Bool == right.Bool
	case value.Number:
		if left.NumberKind != right.NumberKind {
			return false
		}
		if left.NumberKind == value.NumberInt {
			return left.Int == right.Int
		}
		return left.Num == right.Num
	case value.Char, value.String:
		return left.Str == right.Str
	default:
		return false
	}
}

func jitGetIndex(opcode byte, ops opcodeSet, object value.Value, index value.Value) (value.Value, error) {
	switch opcode {
	case ops.getIndexArray:
		array, ok := object.AsArray()
		if !ok {
			return value.NilValue(), fmt.Errorf("GET_INDEX_ARRAY expects array")
		}
		return jitArrayIndex(array, index)
	case ops.getIndexMap:
		m, ok := object.AsMap()
		if !ok {
			return value.NilValue(), fmt.Errorf("GET_INDEX_MAP expects map")
		}
		return jitMapIndex(m, index)
	case ops.getIndex:
		if object.Kind == value.String {
			return jitStringIndex(object, index)
		}
		if array, ok := object.AsArray(); ok {
			return jitArrayIndex(array, index)
		}
		if tuple, ok := object.AsTuple(); ok {
			return jitTupleIndex(tuple, index)
		}
		if m, ok := object.AsMap(); ok {
			return jitMapIndex(m, index)
		}
		return value.NilValue(), fmt.Errorf("value is not indexable")
	default:
		return value.NilValue(), fmt.Errorf("unsupported jit index opcode %d", opcode)
	}
}

func jitSetIndex(opcode byte, ops opcodeSet, object value.Value, index value.Value, assigned value.Value) error {
	switch opcode {
	case ops.setIndexArray:
		array, ok := object.AsArray()
		if !ok {
			return fmt.Errorf("SET_INDEX_ARRAY expects array")
		}
		return jitArrayAssign(array, index, assigned)
	case ops.setIndexMap:
		m, ok := object.AsMap()
		if !ok {
			return fmt.Errorf("SET_INDEX_MAP expects map")
		}
		return jitMapAssign(m, index, assigned)
	case ops.setIndex:
		if array, ok := object.AsArray(); ok {
			return jitArrayAssign(array, index, assigned)
		}
		if m, ok := object.AsMap(); ok {
			return jitMapAssign(m, index, assigned)
		}
		return fmt.Errorf("value is not index-assignable")
	default:
		return fmt.Errorf("unsupported jit set-index opcode %d", opcode)
	}
}

func jitSlice(object value.Value, start value.Value, end value.Value) (value.Value, error) {
	if start.Kind != value.Number || end.Kind != value.Number {
		return value.NilValue(), fmt.Errorf("slice bounds must be numbers")
	}
	startIdx := int(start.Num)
	endIdx := int(end.Num)
	if object.Kind == value.String {
		runes := []rune(object.Str)
		sliced, err := jitSliceRunes(runes, startIdx, endIdx)
		if err != nil {
			return value.NilValue(), err
		}
		return value.StringValue(string(sliced)), nil
	}
	if array, ok := object.AsArray(); ok {
		sliced, err := jitSliceValues(array.Elements, startIdx, endIdx)
		if err != nil {
			return value.NilValue(), err
		}
		return value.ObjectValue(&value.Array{Elements: append([]value.Value(nil), sliced...)}), nil
	}
	if tuple, ok := object.AsTuple(); ok {
		sliced, err := jitSliceValues(tuple.Elements, startIdx, endIdx)
		if err != nil {
			return value.NilValue(), err
		}
		return value.ObjectValue(&value.Tuple{Elements: append([]value.Value(nil), sliced...)}), nil
	}
	return value.NilValue(), fmt.Errorf("value is not sliceable")
}

func jitStringIndex(object value.Value, index value.Value) (value.Value, error) {
	if index.Kind != value.Number {
		return value.NilValue(), fmt.Errorf("String index must be number")
	}
	runes := []rune(object.Str)
	idx := int(index.Num)
	if idx < 0 || idx >= len(runes) {
		return value.NilValue(), fmt.Errorf("String index out of range")
	}
	return value.CharValue(runes[idx]), nil
}

func jitArrayIndex(array *value.Array, index value.Value) (value.Value, error) {
	if index.Kind != value.Number {
		return value.NilValue(), fmt.Errorf("array index must be number")
	}
	idx := int(index.Num)
	if idx < 0 || idx >= len(array.Elements) {
		return value.NilValue(), fmt.Errorf("array index out of range")
	}
	return array.Elements[idx], nil
}

func jitTupleIndex(tuple *value.Tuple, index value.Value) (value.Value, error) {
	if index.Kind != value.Number {
		return value.NilValue(), fmt.Errorf("tuple index must be number")
	}
	idx := int(index.Num)
	if idx < 0 || idx >= len(tuple.Elements) {
		return value.NilValue(), fmt.Errorf("tuple index out of range")
	}
	return tuple.Elements[idx], nil
}

func jitMapIndex(m *value.Map, index value.Value) (value.Value, error) {
	if index.Kind != value.String {
		return value.NilValue(), fmt.Errorf("map index must be string")
	}
	if val, exists := m.Entries[index.Str]; exists {
		return val, nil
	}
	return value.NilValue(), nil
}

func jitArrayAssign(array *value.Array, index value.Value, assigned value.Value) error {
	if index.Kind != value.Number {
		return fmt.Errorf("array index must be number")
	}
	idx := int(index.Num)
	if idx < 0 || idx >= len(array.Elements) {
		return fmt.Errorf("array index out of range")
	}
	array.Elements[idx] = assigned
	return nil
}

func jitMapAssign(m *value.Map, index value.Value, assigned value.Value) error {
	if index.Kind != value.String {
		return fmt.Errorf("map index must be string")
	}
	m.Entries[index.Str] = assigned
	return nil
}

func jitSliceRunes(items []rune, start int, end int) ([]rune, error) {
	if start < 0 || end < start || end > len(items) {
		return nil, fmt.Errorf("slice index out of range")
	}
	return items[start:end], nil
}

func jitSliceValues(items []value.Value, start int, end int) ([]value.Value, error) {
	if start < 0 || end < start || end > len(items) {
		return nil, fmt.Errorf("slice index out of range")
	}
	return items[start:end], nil
}

func jitCallValue(callee value.Value, args []value.Value, runtime *RuntimeContext, name string) (value.Value, error) {
	if builtin, ok := callee.AsBuiltin(); ok {
		if builtin.Arity >= 0 && builtin.Arity != len(args) {
			return value.NilValue(), fmt.Errorf("%s expects %d args, got %d", builtin.Name, builtin.Arity, len(args))
		}
		return builtin.Fn(args)
	}
	if fn, ok := callee.AsFunction(); ok {
		program, err := jitProgramForFunction(fn)
		if err != nil {
			return value.NilValue(), err
		}
		return executeProgram(program, nil, args, runtime)
	}
	if closure, ok := callee.AsClosure(); ok {
		if closure == nil || closure.Function == nil || len(closure.Captures) != 0 {
			return value.NilValue(), fmt.Errorf("unsupported closure call in %s", name)
		}
		program, err := jitProgramForFunction(closure.Function)
		if err != nil {
			return value.NilValue(), err
		}
		return executeProgram(program, nil, args, runtime)
	}
	return value.NilValue(), fmt.Errorf("unsupported jit call target in %s", name)
}

var jitNestedProgramCache sync.Map

func jitProgramForFunction(fn *bytecode.Function) (*Program, error) {
	if fn == nil {
		return nil, fmt.Errorf("missing function")
	}
	if cached, ok := jitNestedProgramCache.Load(fn); ok {
		if program, ok := cached.(*Program); ok {
			return program, nil
		}
	}
	program, ok, reason := compileFunction(selectBackend(), fn)
	if !ok {
		return nil, fmt.Errorf("unsupported jit function %s: %s", safeFunctionName(fn), reason)
	}
	jitNestedProgramCache.Store(fn, program)
	return program, nil
}

func jitInvokeMember(receiver value.Value, member string, args []value.Value, runtime *RuntimeContext, name string) (value.Value, error) {
	if module, ok := receiver.AsModule(); ok {
		candidate, exists := module.Members[member]
		if !exists {
			return value.NilValue(), fmt.Errorf("module %s has no member %s", module.Name, member)
		}
		return jitCallValue(candidate, args, runtime, name)
	}
	return value.NilValue(), fmt.Errorf("unsupported jit invoke target for %s", member)
}

func jitInitFastRange(locals []value.Value, stack *[]value.Value, meta rangeInitMeta) error {
	if len(*stack) < meta.Argc {
		return fmt.Errorf("jit stack underflow in fast range init")
	}
	switch meta.Argc {
	case 1:
		end := (*stack)[len(*stack)-1]
		*stack = (*stack)[:len(*stack)-1]
		if end.Kind != value.Number {
			return fmt.Errorf("range expects numeric arguments")
		}
		if end.NumberKind == value.NumberInt {
			locals[int(meta.CurrentSlot)] = value.IntValue(-1)
			locals[int(meta.EndSlot)] = end
			locals[int(meta.StepSlot)] = value.IntValue(1)
			return nil
		}
		locals[int(meta.CurrentSlot)] = value.NumberValue(-1)
		locals[int(meta.EndSlot)] = end
		locals[int(meta.StepSlot)] = value.NumberValue(1)
		return nil
	case 2:
		end := (*stack)[len(*stack)-1]
		start := (*stack)[len(*stack)-2]
		*stack = (*stack)[:len(*stack)-2]
		if start.Kind != value.Number || end.Kind != value.Number {
			return fmt.Errorf("range expects numeric arguments")
		}
		if start.NumberKind == value.NumberInt && end.NumberKind == value.NumberInt {
			step := int64(1)
			if start.Int > end.Int {
				step = -1
			}
			locals[int(meta.CurrentSlot)] = value.IntValue(start.Int - step)
			locals[int(meta.EndSlot)] = end
			locals[int(meta.StepSlot)] = value.IntValue(step)
			return nil
		}
		step := 1.0
		if start.Num > end.Num {
			step = -1
		}
		locals[int(meta.CurrentSlot)] = value.NumberValue(start.Num - step)
		locals[int(meta.EndSlot)] = end
		locals[int(meta.StepSlot)] = value.NumberValue(step)
		return nil
	default:
		return fmt.Errorf("range expects 1 or 2 arguments")
	}
}

func jitRangeNextFast(locals []value.Value, meta rangeNextMeta) (bool, error) {
	current := locals[int(meta.CurrentSlot)]
	end := locals[int(meta.EndSlot)]
	step := locals[int(meta.StepSlot)]
	if current.Kind != value.Number || end.Kind != value.Number || step.Kind != value.Number {
		return false, fmt.Errorf("fast range expects numeric locals")
	}
	if current.NumberKind == value.NumberInt && end.NumberKind == value.NumberInt && step.NumberKind == value.NumberInt {
		next := current.Int + step.Int
		if step.Int > 0 && next >= end.Int {
			return false, nil
		}
		if step.Int < 0 && next <= end.Int {
			return false, nil
		}
		locals[int(meta.CurrentSlot)] = value.IntValue(next)
		return true, nil
	}
	next := current.Num + step.Num
	if step.Num > 0 && next >= end.Num {
		return false, nil
	}
	if step.Num < 0 && next <= end.Num {
		return false, nil
	}
	locals[int(meta.CurrentSlot)] = value.NumberValue(next)
	return true, nil
}

func jitAddValues(left value.Value, right value.Value, name string) (value.Value, error) {
	if left.Kind == value.Number && right.Kind == value.Number {
		return executeNumeric(0xFF, opcodeSet{addNumber: 0xFF}, left, right, name)
	}
	if left.Kind == value.String || right.Kind == value.String || left.Kind == value.Char || right.Kind == value.Char {
		return value.StringValue(left.String() + right.String()), nil
	}
	return value.NilValue(), fmt.Errorf("ADD expects numbers or strings in %s", name)
}

func jitAddLocalMulLocal(target value.Value, left value.Value, right value.Value, name string) (value.Value, error) {
	if target.Kind != value.Number || left.Kind != value.Number || right.Kind != value.Number {
		return value.NilValue(), fmt.Errorf("ADD_LOCAL_MUL_LOCAL expects numbers in %s", name)
	}
	if target.NumberKind == value.NumberInt && left.NumberKind == value.NumberInt && right.NumberKind == value.NumberInt {
		return value.IntValue(target.Int + left.Int*right.Int), nil
	}
	return value.FloatValue(target.Num + left.Num*right.Num), nil
}

func executeNumeric(opcode byte, ops opcodeSet, left value.Value, right value.Value, name string) (value.Value, error) {
	if left.Kind != value.Number || right.Kind != value.Number {
		return value.NilValue(), fmt.Errorf("jit numeric operation expects numbers in %s", name)
	}
	if (opcode == ops.divNumber || opcode == ops.modNumber) && ((right.NumberKind == value.NumberInt && right.Int == 0) || (right.NumberKind != value.NumberInt && right.Num == 0)) {
		return value.NilValue(), fmt.Errorf("division by zero")
	}
	switch opcode {
	case ops.addNumber:
		if left.NumberKind == value.NumberInt && right.NumberKind == value.NumberInt {
			return value.IntValue(left.Int + right.Int), nil
		}
		return value.FloatValue(left.Num + right.Num), nil
	case ops.subNumber:
		if left.NumberKind == value.NumberInt && right.NumberKind == value.NumberInt {
			return value.IntValue(left.Int - right.Int), nil
		}
		return value.FloatValue(left.Num - right.Num), nil
	case ops.mulNumber:
		if left.NumberKind == value.NumberInt && right.NumberKind == value.NumberInt {
			return value.IntValue(left.Int * right.Int), nil
		}
		return value.FloatValue(left.Num * right.Num), nil
	case ops.divNumber:
		result := left.Num / right.Num
		if !math.IsInf(result, 0) && !math.IsNaN(result) {
			return value.FloatValue(result), nil
		}
		return value.NilValue(), fmt.Errorf("invalid division result in %s", name)
	case ops.modNumber:
		if left.NumberKind == value.NumberInt && right.NumberKind == value.NumberInt {
			return value.IntValue(left.Int % right.Int), nil
		}
		return value.FloatValue(math.Mod(left.Num, right.Num)), nil
	default:
		return value.NilValue(), fmt.Errorf("unsupported numeric jit opcode %d in %s", opcode, name)
	}
}

func readUint16(code []byte) uint16 {
	return uint16(code[0])<<8 | uint16(code[1])
}

func max(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
