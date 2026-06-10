package vm

import (
	"fmt"
	"hash/fnv"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/ArubikU/polyloft-bvm/internal/bytecode"
	"github.com/ArubikU/polyloft-bvm/internal/diagnostic"
	bvmruntime "github.com/ArubikU/polyloft-bvm/internal/runtime"
	"github.com/ArubikU/polyloft-bvm/internal/value"
)

type exceptionHandler struct {
	catchIP    int
	stackDepth int
}

type frame struct {
	fn            *bytecode.Function
	closure       *value.Closure
	ip            int
	stackBase     int
	locals        []value.Value
	localRefs     map[byte]*value.Cell
	stringBuffers map[byte]*stringAccumulator
	handlers      []exceptionHandler
	receiver      *value.Instance
	init          bool
	hasCells      bool // true only when localRefs or stringBuffers are non-empty
	// consts is the chunk's constant pool pre-converted to value.Value,
	// fetched lazily from VM.constCache on first use (see resolvedConsts).
	consts []value.Value
}

type stringAccumulator struct {
	builder strings.Builder
}

type VM struct {
	stdout          io.Writer
	stack           []value.Value
	sp              int // explicit stack pointer (next free slot index)
	stackCap        int // cached len(stack); updated only when stack is grown
	frames          []*frame
	framePool       []*frame
	globals         map[string]value.Value
	globalSlots     []value.Value
	globalDefined   []bool
	builtinArgs     []value.Value
	globalSlotNames []string
	callbackMu      sync.Mutex
	instancePools   map[*value.Class][]*value.Instance
	constCache      map[*bytecode.Chunk][]value.Value
}

func New(stdout io.Writer) *VM {
	if stdout == nil {
		stdout = os.Stdout
	}

	registry := bvmruntime.NewRegistry()
	bvmruntime.InstallCoreGlobals(registry, stdout)
	return NewWithRegistry(stdout, registry)
}

func NewWithRegistry(stdout io.Writer, registry *bvmruntime.Registry) *VM {
	return newWithRegistry(stdout, registry, true)
}

func newWithRegistry(stdout io.Writer, registry *bvmruntime.Registry, setProxy bool) *VM {
	if stdout == nil {
		stdout = os.Stdout
	}
	machine := &VM{
		stdout:      stdout,
		stack:       make([]value.Value, 4096), // pre-allocated; sp tracks top
		sp:          0,
		stackCap:    4096,
		frames:      make([]*frame, 0, 16),
		framePool:   make([]*frame, 0, 16),
		globals:     registry.Globals(),
		builtinArgs: make([]value.Value, 0, 8),
	}
	if setProxy {
		bvmruntime.GlobalVMProxy = machine
	}
	return machine
}

// Globals returns the flat global name->value map used by the VM.
// This is used externally to resolve exported values after running a module.
func (vm *VM) Globals() map[string]value.Value {
	return vm.globals
}

// SetJITWarmupThreshold is kept for CLI compatibility.
// The current VM runtime does not wire JIT execution through this type.
func (vm *VM) SetJITWarmupThreshold(threshold int) {
	_ = vm
	_ = threshold
}

// SetJITLogger is kept for CLI compatibility.
// The current VM runtime does not wire JIT execution through this type.
func (vm *VM) SetJITLogger(logger io.Writer) {
	_ = vm
	_ = logger
}

func (vm *VM) Run(fn *bytecode.Function) (value.Value, error) {
	vm.globalSlotNames = append(vm.globalSlotNames[:0], fn.GlobalSlotNames...)
	if cap(vm.globalSlots) < fn.GlobalSlotCount {
		vm.globalSlots = make([]value.Value, fn.GlobalSlotCount)
		vm.globalDefined = make([]bool, fn.GlobalSlotCount)
	} else {
		vm.globalSlots = vm.globalSlots[:fn.GlobalSlotCount]
		vm.globalDefined = vm.globalDefined[:fn.GlobalSlotCount]
		clear(vm.globalSlots)
		clear(vm.globalDefined)
	}
	for idx, name := range fn.GlobalSlotNames {
		if value, ok := vm.globals[name]; ok {
			vm.globalSlots[idx] = value
			vm.globalDefined[idx] = true
		}
	}
	vm.frames = append(vm.frames, vm.acquireFrame(fn, nil, nil, false))
	return vm.executeUntilDepth(0)
}

func (vm *VM) CallClosure(closure value.Value, args []value.Value) (value.Value, error) {
	vm.callbackMu.Lock()
	defer vm.callbackMu.Unlock()
	return vm.callValue(closure, args)
}

func (vm *VM) CallClosureIsolated(closure value.Value, args []value.Value) (value.Value, error) {
	registry := bvmruntime.NewRegistry()
	for name, candidate := range vm.globals {
		registry.Define(name, value.DeepCopy(candidate))
	}
	for index, name := range vm.globalSlotNames {
		if index >= len(vm.globalDefined) || !vm.globalDefined[index] {
			continue
		}
		registry.Define(name, value.DeepCopy(vm.globalSlots[index]))
	}
	isolated := newWithRegistry(vm.stdout, registry, false)
	clonedArgs := make([]value.Value, len(args))
	for i, arg := range args {
		clonedArgs[i] = value.DeepCopy(arg)
	}
	return isolated.callValue(value.DeepCopy(closure), clonedArgs)
}

func (vm *VM) callValue(callable value.Value, args []value.Value) (value.Value, error) {
	baseDepth := len(vm.frames)
	baseStack := vm.sp
	vm.push(callable)
	for _, arg := range args {
		vm.push(arg)
	}
	if err := vm.call(len(args)); err != nil {
		vm.sp = baseStack
		return value.NilValue(), err
	}
	result, err := vm.executeUntilDepth(baseDepth)
	if err != nil {
		for len(vm.frames) > baseDepth {
			frame := vm.currentFrame()
			vm.frames = vm.frames[:len(vm.frames)-1]
			vm.releaseFrame(frame)
		}
		vm.sp = baseStack
		return value.NilValue(), err
	}
	if vm.sp > baseStack {
		vm.sp = baseStack
	}
	return result, nil
}

// readB and readU16At are the dispatch-loop bytecode readers. They take the
// cached code slice as an explicit argument instead of chasing
// frame.fn.Chunk.Code on every read, and they are deliberately tiny:
// executeUntilDepth exceeds the compiler's big-function node limit, so calls
// inside it are only inlined when the callee cost is ≤ 20
// (inlineBigFunctionMaxCost). Check with `go build -gcflags='-m -m'` after
// editing these.
func readB(code []byte, f *frame) byte {
	b := code[f.ip]
	f.ip++
	return b
}

// readU16At is stateless — the caller advances frame.ip by 2 itself. Folding
// the ip update into this function pushed its inline cost to 26 (> 20), which
// blocked inlining inside the dispatch loop.
func readU16At(code []byte, i int) uint16 {
	return uint16(code[i])<<8 | uint16(code[i+1])
}

func (vm *VM) executeUntilDepth(baseDepth int) (value.Value, error) {
	if len(vm.frames) <= baseDepth {
		return value.NilValue(), nil
	}
	frame := vm.frames[len(vm.frames)-1]
	// code mirrors frame.fn.Chunk.Code; it must be refreshed together with
	// frame (after calls, returns and handled exceptions).
	code := frame.fn.Chunk.Code
	for {
		if frame.ip >= len(code) {
			if handled, raised := vm.handleRaised(baseDepth, frame, diagnostic.Runtime("RuntimeError", "unexpected end of bytecode", value.NilValue())); handled {
				frame = vm.frames[len(vm.frames)-1]
				code = frame.fn.Chunk.Code
				continue
			} else {
				return value.NilValue(), raised
			}
		}

		op := bytecode.Op(code[frame.ip])
		frame.ip++

		switch op {
		case bytecode.OpConstant:
			idx := readU16At(code, frame.ip)
			frame.ip += 2
			if frame.consts == nil {
				frame.consts = vm.resolvedConsts(frame.fn.Chunk)
			}
			vm.push(frame.consts[idx])
		case bytecode.OpNil:
			vm.push(value.NilValue())
		case bytecode.OpTrue:
			vm.push(value.BoolValue(true))
		case bytecode.OpFalse:
			vm.push(value.BoolValue(false))
		case bytecode.OpPop:
			vm.pop()
		case bytecode.OpDup:
			vm.push(vm.peek(0))
		case bytecode.OpDupTwo:
			first := vm.peek(1)
			second := vm.peek(0)
			vm.push(first)
			vm.push(second)
		case bytecode.OpGetLocal:
			slot := readB(code, frame)
			// localGet hand-inlined: its cost (71) exceeds the big-function
			// inline limit (20), and this is the hottest opcode in the VM.
			if !frame.hasCells {
				vm.push(frame.locals[slot])
			} else {
				vm.push(vm.localGetSlow(frame, slot))
			}
		case bytecode.OpSetLocal:
			slot := readB(code, frame)
			newVal := vm.pop()
			// Fast path: peek at old local and recycle if it's an Instance with no aliases
			if !frame.hasCells {
				if old := frame.locals[slot]; old.Kind == value.Object {
					vm.tryRecycleLocal(frame, slot, old)
				}
				frame.locals[slot] = newVal
			} else {
				if old := vm.localGetSlow(frame, slot); old.Kind == value.Object {
					vm.tryRecycleLocal(frame, slot, old)
				}
				vm.localSetSlow(frame, slot, newVal)
			}
		case bytecode.OpAppendLocalString:
			slot := readB(code, frame)
			vm.appendLocalString(frame, slot, vm.pop())
		case bytecode.OpIncLocal:
			vm.adjustIntLocal(frame, readB(code, frame), 1)
		case bytecode.OpDecLocal:
			vm.adjustIntLocal(frame, readB(code, frame), -1)
		// The four local-vs-local fused jumps repeat the same 4-line body on
		// purpose: a shared helper exceeds the big-function inline limit (20)
		// and compiles to a real call plus an indirect comparator call per
		// jump (~9% of bench_array). If you change the decode or comparison
		// semantics, update all four cases together.
		case bytecode.OpJumpIfLocalGtLocalTrue:
			slotA := readB(code, frame)
			slotB := readB(code, frame)
			offset := readU16At(code, frame.ip)
			frame.ip += 2
			if frame.locals[slotA].Num > frame.locals[slotB].Num {
				frame.ip += int(offset)
			}
		case bytecode.OpJumpIfLocalLtLocalFalse:
			slotA := readB(code, frame)
			slotB := readB(code, frame)
			offset := readU16At(code, frame.ip)
			frame.ip += 2
			if frame.locals[slotA].Num >= frame.locals[slotB].Num {
				frame.ip += int(offset)
			}
		case bytecode.OpJumpIfLocalGtLocalFalse:
			slotA := readB(code, frame)
			slotB := readB(code, frame)
			offset := readU16At(code, frame.ip)
			frame.ip += 2
			if frame.locals[slotA].Num <= frame.locals[slotB].Num {
				frame.ip += int(offset)
			}
		case bytecode.OpJumpIfLocalLtLocalTrue:
			slotA := readB(code, frame)
			slotB := readB(code, frame)
			offset := readU16At(code, frame.ip)
			frame.ip += 2
			if frame.locals[slotA].Num < frame.locals[slotB].Num {
				frame.ip += int(offset)
			}
		case bytecode.OpJumpIfArrayFieldGteLocalTrue:
			fieldNum, cmpSlot, offset, err := vm.readArrayFieldCmpArgs(frame)
			if err != nil {
				return value.NilValue(), err
			}
			if fieldNum >= frame.locals[cmpSlot].Num {
				frame.ip += int(offset)
			}
		case bytecode.OpJumpIfArrayFieldLteLocalTrue:
			fieldNum, cmpSlot, offset, err := vm.readArrayFieldCmpArgs(frame)
			if err != nil {
				return value.NilValue(), err
			}
			if fieldNum <= frame.locals[cmpSlot].Num {
				frame.ip += int(offset)
			}
		case bytecode.OpAddConstLocalInt:
			slot := readB(code, frame)
			constant := frame.fn.Chunk.Constants[readU16At(code, frame.ip)]
			frame.ip += 2
			increment, ok := constant.(int64)
			if !ok {
				return value.NilValue(), fmt.Errorf("ADD_CONST_LOCAL_INT expects int constant")
			}
			// localGet/localSet hand-inlined (cost > big-function inline limit).
			var current value.Value
			if !frame.hasCells {
				current = frame.locals[slot]
			} else {
				current = vm.localGetSlow(frame, slot)
			}
			if current.Kind != value.Number || current.NumberKind != value.NumberInt {
				return value.NilValue(), fmt.Errorf("ADD_CONST_LOCAL_INT expects int local")
			}
			if !frame.hasCells {
				frame.locals[slot] = value.IntValue(current.Int + increment)
			} else {
				vm.localSetSlow(frame, slot, value.IntValue(current.Int+increment))
			}
		case bytecode.OpJumpIfLocalLessEqualIntConstFalse:
			slot := readB(code, frame)
			constant := frame.fn.Chunk.Constants[readU16At(code, frame.ip)]
			frame.ip += 2
			offset := readU16At(code, frame.ip)
			frame.ip += 2
			limit, ok := constant.(int64)
			if !ok {
				return value.NilValue(), fmt.Errorf("JUMP_IF_LOCAL_LE_INT_CONST_FALSE expects int constant")
			}
			// localGet hand-inlined: hottest branch opcode (fib's base case).
			var current value.Value
			if !frame.hasCells {
				current = frame.locals[slot]
			} else {
				current = vm.localGetSlow(frame, slot)
			}
			if current.Kind != value.Number || current.Num > float64(limit) {
				frame.ip += int(offset)
			}
		case bytecode.OpJumpIfLocalDivisibleByIntConstFalse:
			slot := readB(code, frame)
			constant := frame.fn.Chunk.Constants[readU16At(code, frame.ip)]
			frame.ip += 2
			offset := readU16At(code, frame.ip)
			frame.ip += 2
			divisor, ok := constant.(int64)
			if !ok || divisor == 0 {
				return value.NilValue(), fmt.Errorf("JUMP_IF_LOCAL_DIVISIBLE_INT_CONST_FALSE expects non-zero int constant")
			}
			current := vm.localGet(frame, slot)
			if current.Kind != value.Number || current.Num != math.Trunc(current.Num) || int64(current.Num)%divisor != 0 {
				frame.ip += int(offset)
			}
		case bytecode.OpJumpIfNotContainsStringConst:
			haystackSlot := readB(code, frame)
			needleConst := frame.fn.Chunk.Constants[readU16At(code, frame.ip)]
			frame.ip += 2
			offset := readU16At(code, frame.ip)
			frame.ip += 2
			needle, ok := needleConst.(string)
			if !ok {
				return value.NilValue(), fmt.Errorf("JUMP_IF_NOT_CONTAINS_STRING_CONST expects string constant")
			}
			haystack := vm.localGet(frame, haystackSlot)
			if haystack.Kind != value.String || !strings.Contains(haystack.Str, needle) {
				frame.ip += int(offset)
			}
		case bytecode.OpAddToLocal:
			slot := readB(code, frame)
			rhs := vm.pop()
			if frame.hasCells {
				// Closure-captured local: route through cell abstraction (Fix: was bypassed).
				if err := vm.applyToLocalSlow(frame, slot, rhs, bytecode.OpAddNum); err != nil {
					return value.NilValue(), err
				}
			} else {
				lhs := frame.locals[slot]
				if lhs.Kind == value.Number && rhs.Kind == value.Number {
					if lhs.NumberKind == value.NumberInt && rhs.NumberKind == value.NumberInt {
						frame.locals[slot] = value.IntValue(lhs.Int + rhs.Int)
					} else {
						frame.locals[slot] = value.FloatValue(lhs.Num + rhs.Num)
					}
				} else {
					vm.push(lhs)
					vm.push(rhs)
					if err := vm.binaryNumberOp(bytecode.OpAddNum, func(a, b float64) float64 { return a + b }); err != nil {
						return value.NilValue(), err
					}
					frame.locals[slot] = vm.pop()
				}
			}
		case bytecode.OpSubToLocal:
			slot := readB(code, frame)
			rhs := vm.pop()
			if frame.hasCells {
				if err := vm.applyToLocalSlow(frame, slot, rhs, bytecode.OpSubNum); err != nil {
					return value.NilValue(), err
				}
			} else {
				lhs := frame.locals[slot]
				if lhs.Kind == value.Number && rhs.Kind == value.Number {
					if lhs.NumberKind == value.NumberInt && rhs.NumberKind == value.NumberInt {
						frame.locals[slot] = value.IntValue(lhs.Int - rhs.Int)
					} else {
						frame.locals[slot] = value.FloatValue(lhs.Num - rhs.Num)
					}
				} else {
					vm.push(lhs)
					vm.push(rhs)
					if err := vm.binaryNumberOp(bytecode.OpSubNum, func(a, b float64) float64 { return a - b }); err != nil {
						return value.NilValue(), err
					}
					frame.locals[slot] = vm.pop()
				}
			}
		case bytecode.OpMulToLocal:
			slot := readB(code, frame)
			rhs := vm.pop()
			if frame.hasCells {
				if err := vm.applyToLocalSlow(frame, slot, rhs, bytecode.OpMulNum); err != nil {
					return value.NilValue(), err
				}
			} else {
				lhs := frame.locals[slot]
				if lhs.Kind == value.Number && rhs.Kind == value.Number {
					if lhs.NumberKind == value.NumberInt && rhs.NumberKind == value.NumberInt {
						frame.locals[slot] = value.IntValue(lhs.Int * rhs.Int)
					} else {
						frame.locals[slot] = value.FloatValue(lhs.Num * rhs.Num)
					}
				} else {
					vm.push(lhs)
					vm.push(rhs)
					if err := vm.binaryNumberOp(bytecode.OpMulNum, func(a, b float64) float64 { return a * b }); err != nil {
						return value.NilValue(), err
					}
					frame.locals[slot] = vm.pop()
				}
			}
		case bytecode.OpGetCapture:
			slot := readB(code, frame)
			vm.push(frame.closure.Captures[slot].Value)
		case bytecode.OpSetCapture:
			slot := readB(code, frame)
			frame.closure.Captures[slot].Value = vm.pop()
		case bytecode.OpDefineGlobal:
			name := frame.fn.Chunk.Constants[readU16At(code, frame.ip)].(string)
			frame.ip += 2
			vm.globals[name] = vm.pop()
		case bytecode.OpDefineGlobalSlot:
			slot := int(readB(code, frame))
			vm.globalSlots[slot] = vm.pop()
			vm.globalDefined[slot] = true
		case bytecode.OpGetGlobal:
			name := frame.fn.Chunk.Constants[readU16At(code, frame.ip)].(string)
			frame.ip += 2
			val, ok := vm.globals[name]
			if !ok {
				return value.NilValue(), fmt.Errorf("undefined variable %s", name)
			}
			vm.push(val)
		case bytecode.OpGetGlobalSlot:
			slot := int(readB(code, frame))
			if !vm.globalDefined[slot] {
				return value.NilValue(), fmt.Errorf("undefined global slot %d", slot)
			}
			vm.push(vm.globalSlots[slot])
		case bytecode.OpSetGlobal:
			name := frame.fn.Chunk.Constants[readU16At(code, frame.ip)].(string)
			frame.ip += 2
			if _, ok := vm.globals[name]; !ok {
				return value.NilValue(), fmt.Errorf("undefined variable %s", name)
			}
			vm.globals[name] = vm.pop()
		case bytecode.OpSetGlobalSlot:
			slot := int(readB(code, frame))
			if !vm.globalDefined[slot] {
				return value.NilValue(), fmt.Errorf("undefined global slot %d", slot)
			}
			vm.globalSlots[slot] = vm.pop()
		case bytecode.OpEqual:
			right := vm.pop()
			left := vm.pop()
			// Number-vs-number fast path: valuesEqual is a real call (cost
			// above the big-function inline limit) and numeric equality is
			// by far the most common case.
			if left.Kind == value.Number && right.Kind == value.Number {
				if left.NumberKind == value.NumberInt && right.NumberKind == value.NumberInt {
					vm.push(value.BoolValue(left.Int == right.Int))
				} else {
					vm.push(value.BoolValue(left.Num == right.Num))
				}
				continue
			}
			equal, err := vm.valuesEqual(left, right)
			if err != nil {
				return value.NilValue(), err
			}
			vm.push(value.BoolValue(equal))
		case bytecode.OpMatchType:
			typeName := frame.fn.Chunk.Constants[readU16At(code, frame.ip)].(string)
			frame.ip += 2
			candidate := vm.pop()
			vm.push(value.BoolValue(vm.matchesType(candidate, typeName)))
		case bytecode.OpCastRef:
			typeName := frame.fn.Chunk.Constants[readU16At(code, frame.ip)].(string)
			frame.ip += 2
			candidate := vm.pop()
			if converted, ok, err := vm.convertReferenceCast(candidate, typeName); err != nil {
				return value.NilValue(), err
			} else if ok {
				vm.push(converted)
				continue
			}
			if candidate.Kind != value.Nil && !vm.matchesType(candidate, typeName) {
				return value.NilValue(), fmt.Errorf("cannot cast %s to %s", vm.runtimeTypeName(candidate), typeName)
			}
			vm.push(candidate)
		case bytecode.OpGreater:
			if err := vm.binaryCompare(bytecode.OpGreater, func(a, b float64) bool { return a > b }, func(a, b string) bool { return a > b }); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpLess:
			if err := vm.binaryCompare(bytecode.OpLess, func(a, b float64) bool { return a < b }, func(a, b string) bool { return a < b }); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpAdd:
			right := vm.pop()
			left := vm.pop()
			if left.Kind == value.Number && right.Kind == value.Number {
				if left.NumberKind == value.NumberInt && right.NumberKind == value.NumberInt {
					vm.push(value.IntValue(left.Int + right.Int))
					continue
				}
				vm.push(value.FloatValue(left.Num + right.Num))
				continue
			}
			if leftNum, ok := vm.numericOperand(left); ok {
				if rightNum, ok := vm.numericOperand(right); ok {
					vm.push(vm.numericResult(left, right, bytecode.OpAdd, leftNum+rightNum))
					continue
				}
			}
			// string/char concatenation
			if leftText, ok := textualOperand(left); ok {
				if rightText, ok := textualOperand(right); ok {
					vm.push(value.StringValue(leftText + rightText))
					continue
				}
			}
			if left.Kind == value.String || right.Kind == value.String || left.Kind == value.Char || right.Kind == value.Char {
				leftText, err := vm.StringifyValue(left)
				if err != nil {
					return value.NilValue(), err
				}
				rightText, err := vm.StringifyValue(right)
				if err != nil {
					return value.NilValue(), err
				}
				vm.push(value.StringValue(leftText + rightText))
				continue
			}
			// array concatenation support
			if la, ok := left.Object.(*value.Array); ok {
				if ra, ok2 := right.Object.(*value.Array); ok2 {
					elems := make([]value.Value, len(la.Elements)+len(ra.Elements))
					copy(elems, la.Elements)
					copy(elems[len(la.Elements):], ra.Elements)
					vm.push(value.ObjectValue(&value.Array{Elements: elems}))
					continue
				}
			}
			if result, ok, err := vm.tryBinaryOperator(left, right, bytecode.OpAdd); err != nil {
				return value.NilValue(), err
			} else if ok {
				vm.push(result)
				continue
			}
			return value.NilValue(), fmt.Errorf("ADD expects numbers or strings")
		case bytecode.OpSub:
			right := vm.pop()
			left := vm.pop()
			if left.Kind == value.Number && right.Kind == value.Number {
				if left.NumberKind == value.NumberInt && right.NumberKind == value.NumberInt {
					vm.push(value.IntValue(left.Int - right.Int))
					continue
				}
				vm.push(value.FloatValue(left.Num - right.Num))
				continue
			}
			vm.push(left)
			vm.push(right)
			if err := vm.binaryNumberOp(bytecode.OpSub, func(a, b float64) float64 { return a - b }); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpMul:
			right := vm.pop()
			left := vm.pop()
			if left.Kind == value.Number && right.Kind == value.Number {
				if left.NumberKind == value.NumberInt && right.NumberKind == value.NumberInt {
					vm.push(value.IntValue(left.Int * right.Int))
					continue
				}
				vm.push(value.FloatValue(left.Num * right.Num))
				continue
			}
			vm.push(left)
			vm.push(right)
			if err := vm.binaryNumberOp(bytecode.OpMul, func(a, b float64) float64 { return a * b }); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpPow:
			if err := vm.binaryPowOp(bytecode.OpPow); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpDiv:
			right := vm.pop()
			left := vm.pop()
			if left.Kind == value.Number && right.Kind == value.Number {
				if right.Num == 0 {
					return value.NilValue(), fmt.Errorf("division by zero")
				}
				vm.push(value.FloatValue(left.Num / right.Num))
				continue
			}
			vm.push(left)
			vm.push(right)
			if err := vm.binaryNumberOp(bytecode.OpDiv, func(a, b float64) float64 { return a / b }); err != nil {
				handled, raised := vm.handleRaised(baseDepth, frame, err)
				if handled {
					frame = vm.frames[len(vm.frames)-1]
					code = frame.fn.Chunk.Code
					continue
				}
				return value.NilValue(), raised
			}
		case bytecode.OpModNum:
			if err := vm.binaryNumberOp(bytecode.OpModNum, func(a, b float64) float64 { return math.Mod(a, b) }); err != nil {
				if handled, raised := vm.handleRaised(baseDepth, frame, err); handled {
					frame = vm.frames[len(vm.frames)-1]
					code = frame.fn.Chunk.Code
					continue
				} else {
					return value.NilValue(), raised
				}
			}
		case bytecode.OpMod:
			if err := vm.binaryNumberOp(bytecode.OpMod, func(a, b float64) float64 { return math.Mod(a, b) }); err != nil {
				handled, raised := vm.handleRaised(baseDepth, frame, err)
				if handled {
					frame = vm.frames[len(vm.frames)-1]
					code = frame.fn.Chunk.Code
					continue
				}
				return value.NilValue(), raised
			}
		case bytecode.OpNot:
			operand := vm.pop()
			if booleanValue, ok := vm.booleanOperand(operand); ok {
				vm.push(value.BoolValue(!booleanValue))
				continue
			}
			vm.push(value.BoolValue(!operand.IsTruthy()))
		case bytecode.OpNegate:
			operand := vm.pop()
			numericValue, ok := vm.numericOperand(operand)
			if !ok {
				if result, applied, err := vm.tryUnaryOperator(operand, "__neg"); err != nil {
					return value.NilValue(), err
				} else if applied {
					vm.push(result)
					continue
				}
				return value.NilValue(), fmt.Errorf("NEGATE expects number")
			}
			if operand.Kind == value.Number && operand.NumberKind == value.NumberInt {
				vm.push(value.IntValue(-int64(numericValue)))
			} else {
				vm.push(value.FloatValue(-numericValue))
			}
		case bytecode.OpPushHandler:
			offset := readU16At(code, frame.ip)
			frame.ip += 2
			frame.handlers = append(frame.handlers, exceptionHandler{catchIP: frame.ip + int(offset), stackDepth: vm.sp})
		case bytecode.OpPopHandler:
			if len(frame.handlers) > 0 {
				frame.handlers = frame.handlers[:len(frame.handlers)-1]
			}
		case bytecode.OpThrow:
			if handled, err := vm.handleRaised(baseDepth, frame, vm.explicitThrow(vm.pop())); handled {
				frame = vm.frames[len(vm.frames)-1]
				code = frame.fn.Chunk.Code
				continue
			} else {
				return value.NilValue(), err
			}
		case bytecode.OpCastInt:
			operand := vm.pop()
			numericValue, ok := vm.numericOperand(operand)
			if !ok {
				return value.NilValue(), fmt.Errorf("CAST_INT expects number")
			}
			vm.push(value.IntValue(int64(numericValue)))
		case bytecode.OpCastFloat:
			operand := vm.pop()
			numericValue, ok := vm.numericOperand(operand)
			if !ok {
				return value.NilValue(), fmt.Errorf("CAST_FLOAT expects number")
			}
			vm.push(value.FloatValue(numericValue))
		case bytecode.OpJump:
			offset := readU16At(code, frame.ip)
			frame.ip += 2
			frame.ip += int(offset)
		// The four conditional-jump opcodes repeat the same truthiness logic
		// (booleanOperand fast path, IsTruthy fallback) on purpose: they are
		// among the hottest dispatch paths and even one extra function call
		// per jump is measurable on million-iteration loops. If you change
		// the truthiness rules, update all four cases together.
		case bytecode.OpJumpIfFalse:
			offset := readU16At(code, frame.ip)
			frame.ip += 2
			condition := vm.peek(0)
			if condition.Kind == value.Bool {
				if !condition.Bool {
					frame.ip += int(offset)
				}
				continue
			}
			if booleanValue, ok := vm.booleanOperand(condition); ok {
				if !booleanValue {
					frame.ip += int(offset)
				}
				continue
			}
			if !condition.IsTruthy() {
				frame.ip += int(offset)
			}
		case bytecode.OpJumpIfTrue:
			offset := readU16At(code, frame.ip)
			frame.ip += 2
			condition := vm.peek(0)
			if condition.Kind == value.Bool {
				if condition.Bool {
					frame.ip += int(offset)
				}
				continue
			}
			if booleanValue, ok := vm.booleanOperand(condition); ok {
				if booleanValue {
					frame.ip += int(offset)
				}
				continue
			}
			if condition.IsTruthy() {
				frame.ip += int(offset)
			}
		case bytecode.OpJumpIfFalsePop:
			offset := readU16At(code, frame.ip)
			frame.ip += 2
			condition := vm.pop()
			if condition.Kind == value.Bool {
				if !condition.Bool {
					frame.ip += int(offset)
				}
				continue
			}
			if booleanValue, ok := vm.booleanOperand(condition); ok {
				if !booleanValue {
					frame.ip += int(offset)
				}
				continue
			}
			if !condition.IsTruthy() {
				frame.ip += int(offset)
			}
		case bytecode.OpJumpIfTruePop:
			offset := readU16At(code, frame.ip)
			frame.ip += 2
			condition := vm.pop()
			if condition.Kind == value.Bool {
				if condition.Bool {
					frame.ip += int(offset)
				}
				continue
			}
			if booleanValue, ok := vm.booleanOperand(condition); ok {
				if booleanValue {
					frame.ip += int(offset)
				}
				continue
			}
			if condition.IsTruthy() {
				frame.ip += int(offset)
			}
		case bytecode.OpLoop:
			offset := readU16At(code, frame.ip)
			frame.ip += 2
			frame.ip -= int(offset)
		case bytecode.OpAddNum:
			s1, s2 := vm.sp-1, vm.sp-2
			if vm.stack[s2].Kind == value.Number && vm.stack[s1].Kind == value.Number {
				if vm.stack[s2].NumberKind == value.NumberInt && vm.stack[s1].NumberKind == value.NumberInt {
					vm.stack[s2].Int += vm.stack[s1].Int
					vm.stack[s2].Num = float64(vm.stack[s2].Int)
				} else {
					vm.stack[s2].Num += vm.stack[s1].Num
					vm.stack[s2].Int = int64(vm.stack[s2].Num)
					vm.stack[s2].NumberKind = value.NumberFloat
				}
				vm.sp--
				continue
			}
			if err := vm.binaryNumberOp(bytecode.OpAddNum, func(a, b float64) float64 { return a + b }); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpSubNum:
			s1, s2 := vm.sp-1, vm.sp-2
			if vm.stack[s2].Kind == value.Number && vm.stack[s1].Kind == value.Number {
				if vm.stack[s2].NumberKind == value.NumberInt && vm.stack[s1].NumberKind == value.NumberInt {
					vm.stack[s2].Int -= vm.stack[s1].Int
					vm.stack[s2].Num = float64(vm.stack[s2].Int)
				} else {
					vm.stack[s2].Num -= vm.stack[s1].Num
					vm.stack[s2].Int = int64(vm.stack[s2].Num)
					vm.stack[s2].NumberKind = value.NumberFloat
				}
				vm.sp--
				continue
			}
			if err := vm.binaryNumberOp(bytecode.OpSubNum, func(a, b float64) float64 { return a - b }); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpMulNum:
			s1, s2 := vm.sp-1, vm.sp-2
			if vm.stack[s2].Kind == value.Number && vm.stack[s1].Kind == value.Number {
				if vm.stack[s2].NumberKind == value.NumberInt && vm.stack[s1].NumberKind == value.NumberInt {
					vm.stack[s2].Int *= vm.stack[s1].Int
					vm.stack[s2].Num = float64(vm.stack[s2].Int)
				} else {
					vm.stack[s2].Num *= vm.stack[s1].Num
					vm.stack[s2].Int = int64(vm.stack[s2].Num)
					vm.stack[s2].NumberKind = value.NumberFloat
				}
				vm.sp--
				continue
			}
			if err := vm.binaryNumberOp(bytecode.OpMulNum, func(a, b float64) float64 { return a * b }); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpPowNum:
			if err := vm.binaryPowOp(bytecode.OpPowNum); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpDivNum:
			s1, s2 := vm.sp-1, vm.sp-2
			if vm.stack[s2].Kind == value.Number && vm.stack[s1].Kind == value.Number {
				if vm.stack[s1].Num == 0 {
					err := fmt.Errorf("division by zero")
					handled, raised := vm.handleRaised(baseDepth, frame, err)
					if handled {
						frame = vm.frames[len(vm.frames)-1]
						code = frame.fn.Chunk.Code
						continue
					}
					return value.NilValue(), raised
				}
				vm.stack[s2].Num /= vm.stack[s1].Num
				vm.stack[s2].Int = int64(vm.stack[s2].Num)
				vm.stack[s2].NumberKind = value.NumberFloat
				vm.sp--
				continue
			}
			if err := vm.binaryNumberOp(bytecode.OpDivNum, func(a, b float64) float64 { return a / b }); err != nil {
				handled, raised := vm.handleRaised(baseDepth, frame, err)
				if handled {
					frame = vm.frames[len(vm.frames)-1]
					code = frame.fn.Chunk.Code
					continue
				}
				return value.NilValue(), raised
			}
		case bytecode.OpLessNum:
			s1, s2 := vm.sp-1, vm.sp-2
			if vm.stack[s2].Kind == value.Number && vm.stack[s1].Kind == value.Number {
				var result bool
				if vm.stack[s2].NumberKind == value.NumberInt && vm.stack[s1].NumberKind == value.NumberInt {
					result = vm.stack[s2].Int < vm.stack[s1].Int
				} else {
					result = vm.stack[s2].Num < vm.stack[s1].Num
				}
				vm.stack[s2].Kind = value.Bool
				vm.stack[s2].Bool = result
				vm.sp--
				continue
			}
			if err := vm.binaryNumericCompareOp(bytecode.OpLessNum); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpGreaterNum:
			s1, s2 := vm.sp-1, vm.sp-2
			if vm.stack[s2].Kind == value.Number && vm.stack[s1].Kind == value.Number {
				var result bool
				if vm.stack[s2].NumberKind == value.NumberInt && vm.stack[s1].NumberKind == value.NumberInt {
					result = vm.stack[s2].Int > vm.stack[s1].Int
				} else {
					result = vm.stack[s2].Num > vm.stack[s1].Num
				}
				vm.stack[s2].Kind = value.Bool
				vm.stack[s2].Bool = result
				vm.sp--
				continue
			}
			if err := vm.binaryNumericCompareOp(bytecode.OpGreaterNum); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpAddLocalMulThisField:
			targetSlot := readB(code, frame)
			localSlot := readB(code, frame)
			fieldSlot := int(readB(code, frame))
			if frame.receiver == nil {
				return value.NilValue(), fmt.Errorf("ADD_LOCAL_MUL_THIS_FIELD expects receiver")
			}
			target := frame.locals[targetSlot]
			multiplier := frame.locals[localSlot]
			factor := frame.receiver.Fields[fieldSlot]
			if target.Kind != value.Number || multiplier.Kind != value.Number || factor.Kind != value.Number {
				return value.NilValue(), fmt.Errorf("ADD_LOCAL_MUL_THIS_FIELD expects numbers")
			}
			if target.NumberKind == value.NumberInt && multiplier.NumberKind == value.NumberInt && factor.NumberKind == value.NumberInt {
				frame.locals[targetSlot] = value.IntValue(target.Int + multiplier.Int*factor.Int)
				continue
			}
			frame.locals[targetSlot] = value.NumberValue(target.Num + multiplier.Num*factor.Num)
		case bytecode.OpAddLocalMulThisFieldAddThisField:
			targetSlot := readB(code, frame)
			localSlot := readB(code, frame)
			mulFieldSlot := int(readB(code, frame))
			addFieldSlot := int(readB(code, frame))
			if frame.receiver == nil {
				return value.NilValue(), fmt.Errorf("ADD_LOCAL_MUL_THIS_FIELD_ADD_THIS_FIELD expects receiver")
			}
			target := frame.locals[targetSlot]
			multiplier := frame.locals[localSlot]
			mulField := frame.receiver.Fields[mulFieldSlot]
			addField := frame.receiver.Fields[addFieldSlot]
			if target.Kind != value.Number || multiplier.Kind != value.Number || mulField.Kind != value.Number || addField.Kind != value.Number {
				return value.NilValue(), fmt.Errorf("ADD_LOCAL_MUL_THIS_FIELD_ADD_THIS_FIELD expects numbers")
			}
			if target.NumberKind == value.NumberInt && multiplier.NumberKind == value.NumberInt && mulField.NumberKind == value.NumberInt && addField.NumberKind == value.NumberInt {
				frame.locals[targetSlot] = value.IntValue(target.Int + multiplier.Int*mulField.Int + addField.Int)
				continue
			}
			frame.locals[targetSlot] = value.NumberValue(target.Num + multiplier.Num*mulField.Num + addField.Num)
		case bytecode.OpClosure:
			idx := readU16At(code, frame.ip)
			frame.ip += 2
			fn := frame.fn.Chunk.Constants[idx].(*bytecode.Function)
			captures := make([]*value.Cell, len(fn.Upvalues))
			for i, upvalue := range fn.Upvalues {
				if upvalue.IsLocal {
					captures[i] = vm.captureLocal(frame, upvalue.Index)
				} else {
					captures[i] = frame.closure.Captures[upvalue.Index]
				}
			}
			vm.push(value.ObjectValue(&value.Closure{Function: fn, Captures: captures}))
		case bytecode.OpCall:
			argc := int(readB(code, frame))
			if err := vm.call(argc); err != nil {
				handled, raised := vm.handleRaised(baseDepth, frame, err)
				if handled {
					frame = vm.frames[len(vm.frames)-1]
					code = frame.fn.Chunk.Code
					continue
				}
				return value.NilValue(), raised
			}
			frame = vm.frames[len(vm.frames)-1]
			code = frame.fn.Chunk.Code
		case bytecode.OpCallConst:
			if frame.consts == nil {
				frame.consts = vm.resolvedConsts(frame.fn.Chunk)
			}
			callable := frame.consts[readU16At(code, frame.ip)]
			frame.ip += 2
			argc := int(readB(code, frame))
			if err := vm.callKnownValue(callable, argc); err != nil {
				handled, raised := vm.handleRaised(baseDepth, frame, err)
				if handled {
					frame = vm.frames[len(vm.frames)-1]
					code = frame.fn.Chunk.Code
					continue
				}
				return value.NilValue(), raised
			}
			frame = vm.frames[len(vm.frames)-1]
			code = frame.fn.Chunk.Code
		case bytecode.OpCallConstLocalSubInt:
			if frame.consts == nil {
				frame.consts = vm.resolvedConsts(frame.fn.Chunk)
			}
			callable := frame.consts[readU16At(code, frame.ip)]
			frame.ip += 2
			slot := readB(code, frame)
			constant := frame.fn.Chunk.Constants[readU16At(code, frame.ip)]
			frame.ip += 2
			subValue, ok := constant.(int64)
			if !ok {
				return value.NilValue(), fmt.Errorf("CALL_CONST_LOCAL_SUB_INT expects int constant")
			}
			current := vm.localGet(frame, slot)
			if current.Kind != value.Number || current.Num != math.Trunc(current.Num) {
				return value.NilValue(), fmt.Errorf("CALL_CONST_LOCAL_SUB_INT expects int local")
			}
			if err := vm.callKnownValueWithArgs(callable, value.IntValue(int64(current.Num)-subValue)); err != nil {
				handled, raised := vm.handleRaised(baseDepth, frame, err)
				if handled {
					frame = vm.frames[len(vm.frames)-1]
					code = frame.fn.Chunk.Code
					continue
				}
				return value.NilValue(), raised
			}
			frame = vm.frames[len(vm.frames)-1]
			code = frame.fn.Chunk.Code
		case bytecode.OpCallSelfLocalSubInt:
			slot := readB(code, frame)
			constant := frame.fn.Chunk.Constants[readU16At(code, frame.ip)]
			frame.ip += 2
			subValue, ok := constant.(int64)
			if !ok {
				return value.NilValue(), fmt.Errorf("CALL_SELF_LOCAL_SUB_INT expects int constant")
			}
			// localGet hand-inlined: the recursion hot path (fib).
			var current value.Value
			if !frame.hasCells {
				current = frame.locals[slot]
			} else {
				current = vm.localGetSlow(frame, slot)
			}
			if current.Kind != value.Number || current.Num != math.Trunc(current.Num) {
				return value.NilValue(), fmt.Errorf("CALL_SELF_LOCAL_SUB_INT expects int local")
			}
			if err := vm.callSelfLocalSubInt(frame, int64(current.Num)-subValue); err != nil {
				handled, raised := vm.handleRaised(baseDepth, frame, err)
				if handled {
					frame = vm.frames[len(vm.frames)-1]
					code = frame.fn.Chunk.Code
					continue
				}
				return value.NilValue(), raised
			}
			frame = vm.frames[len(vm.frames)-1]
			code = frame.fn.Chunk.Code
		case bytecode.OpCallGlobalSlot:
			slot := int(readB(code, frame))
			argc := int(readB(code, frame))
			if !vm.globalDefined[slot] {
				return value.NilValue(), fmt.Errorf("undefined global slot %d", slot)
			}
			if err := vm.callGlobalSlot(slot, argc); err != nil {
				handled, raised := vm.handleRaised(baseDepth, frame, err)
				if handled {
					frame = vm.frames[len(vm.frames)-1]
					code = frame.fn.Chunk.Code
					continue
				}
				return value.NilValue(), raised
			}
			frame = vm.frames[len(vm.frames)-1]
			code = frame.fn.Chunk.Code
		case bytecode.OpInvoke:
			name := frame.fn.Chunk.Constants[readU16At(code, frame.ip)].(string)
			frame.ip += 2
			argc := int(readB(code, frame))
			if err := vm.invoke(name, argc); err != nil {
				handled, raised := vm.handleRaised(baseDepth, frame, err)
				if handled {
					frame = vm.frames[len(vm.frames)-1]
					code = frame.fn.Chunk.Code
					continue
				}
				return value.NilValue(), raised
			}
			frame = vm.frames[len(vm.frames)-1]
			code = frame.fn.Chunk.Code
		case bytecode.OpInvokeMethod:
			slot := int(readB(code, frame))
			argc := int(readB(code, frame))
			if err := vm.invokeMethodSlot(slot, argc); err != nil {
				handled, raised := vm.handleRaised(baseDepth, frame, err)
				if handled {
					frame = vm.frames[len(vm.frames)-1]
					code = frame.fn.Chunk.Code
					continue
				}
				return value.NilValue(), raised
			}
			frame = vm.frames[len(vm.frames)-1]
			code = frame.fn.Chunk.Code
		case bytecode.OpInvokeSuper:
			name := frame.fn.Chunk.Constants[readU16At(code, frame.ip)].(string)
			frame.ip += 2
			argc := int(readB(code, frame))
			if err := vm.invokeSuper(name, argc); err != nil {
				handled, raised := vm.handleRaised(baseDepth, frame, err)
				if handled {
					frame = vm.frames[len(vm.frames)-1]
					code = frame.fn.Chunk.Code
					continue
				}
				return value.NilValue(), raised
			}
			frame = vm.frames[len(vm.frames)-1]
			code = frame.fn.Chunk.Code
		case bytecode.OpRange:
			argc := int(readB(code, frame))
			rng, err := vm.buildRange(argc)
			if err != nil {
				return value.NilValue(), err
			}
			vm.push(value.ObjectValue(rng))
		case bytecode.OpRangeInitFast:
			currentSlot := readB(code, frame)
			endSlot := readB(code, frame)
			stepSlot := readB(code, frame)
			argc := int(readB(code, frame))
			switch argc {
			case 1:
				end := vm.pop()
				if end.Kind != value.Number {
					return value.NilValue(), fmt.Errorf("range expects numeric arguments")
				}
				if end.NumberKind == value.NumberInt {
					frame.locals[currentSlot] = value.IntValue(-1)
					frame.locals[endSlot] = end
					frame.locals[stepSlot] = value.IntValue(1)
				} else {
					frame.locals[currentSlot] = value.NumberValue(-1)
					frame.locals[endSlot] = end
					frame.locals[stepSlot] = value.NumberValue(1)
				}
			case 2:
				end := vm.pop()
				start := vm.pop()
				if start.Kind != value.Number || end.Kind != value.Number {
					return value.NilValue(), fmt.Errorf("range expects numeric arguments")
				}
				if start.NumberKind == value.NumberInt && end.NumberKind == value.NumberInt {
					step := int64(1)
					if start.Int > end.Int {
						step = -1
					}
					frame.locals[currentSlot] = value.IntValue(start.Int - step)
					frame.locals[endSlot] = end
					frame.locals[stepSlot] = value.IntValue(step)
				} else {
					step := 1.0
					if start.Num > end.Num {
						step = -1
					}
					frame.locals[currentSlot] = value.NumberValue(start.Num - step)
					frame.locals[endSlot] = end
					frame.locals[stepSlot] = value.NumberValue(step)
				}
			case 3:
				step := vm.pop()
				end := vm.pop()
				start := vm.pop()
				if start.Kind != value.Number || end.Kind != value.Number || step.Kind != value.Number {
					return value.NilValue(), fmt.Errorf("range expects numeric arguments")
				}
				if start.NumberKind == value.NumberInt && end.NumberKind == value.NumberInt && step.NumberKind == value.NumberInt {
					frame.locals[currentSlot] = value.IntValue(start.Int - step.Int)
					frame.locals[endSlot] = end
					frame.locals[stepSlot] = step
				} else {
					frame.locals[currentSlot] = value.NumberValue(start.Num - step.Num)
					frame.locals[endSlot] = end
					frame.locals[stepSlot] = step
				}
			default:
				return value.NilValue(), fmt.Errorf("range expects 1, 2, or 3 arguments")
			}
		case bytecode.OpRangeNextFast:
			currentSlot := readB(code, frame)
			endSlot := readB(code, frame)
			stepSlot := readB(code, frame)
			valueSlot := readB(code, frame)
			offset := readU16At(code, frame.ip)
			frame.ip += 2
			current := frame.locals[currentSlot]
			end := frame.locals[endSlot]
			step := frame.locals[stepSlot]
			if current.Kind != value.Number || end.Kind != value.Number || step.Kind != value.Number {
				return value.NilValue(), fmt.Errorf("fast range expects numeric locals")
			}
			// Fast integer path: keeps loop variable as IntValue so arithmetic stays integer
			if current.NumberKind == value.NumberInt && step.NumberKind == value.NumberInt {
				next := current.Int + step.Int
				if (step.Int > 0 && next >= end.Int) || (step.Int < 0 && next <= end.Int) {
					frame.ip += int(offset)
					continue
				}
				nextVal := value.IntValue(next)
				frame.locals[currentSlot] = nextVal
				frame.locals[valueSlot] = nextVal
				continue
			}
			next := current.Num + step.Num
			if (step.Num > 0 && next >= end.Num) || (step.Num < 0 && next <= end.Num) {
				frame.ip += int(offset)
				continue
			}
			nextVal := value.NumberValue(next)
			frame.locals[currentSlot] = nextVal
			frame.locals[valueSlot] = nextVal
		case bytecode.OpIterInit:
			slot := readB(code, frame)
			mode := readB(code, frame)
			iterable := vm.pop()
			if instance, ok := iterable.AsInstance(); ok {
				lengthMethod := instance.Class.SpecialMethod(value.SpecialMethodIterableLength)
				getMethod := instance.Class.SpecialMethod(value.SpecialMethodIterableGet)
				if lengthMethod != nil && getMethod != nil {
					lengthValue, err := vm.invokeInstanceMethod(instance, lengthMethod)
					if err != nil {
						return value.NilValue(), err
					}
					if lengthValue.Kind != value.Number {
						return value.NilValue(), fmt.Errorf("%s.__length() must return Number", instance.Class.Name)
					}
					vm.localSet(frame, slot, value.ObjectValue(&value.Iterator{Receiver: instance, Index: 0, Length: int(lengthValue.Num), GetFn: getMethod}))
					continue
				}
			}
			if rng, ok := iterable.AsRange(); ok {
				vm.localSet(frame, slot, value.ObjectValue(&value.Iterator{Range: rng, Current: rng.Start}))
				continue
			}
			if array, ok := iterable.AsArray(); ok {
				vm.localSet(frame, slot, value.ObjectValue(&value.Iterator{Items: array.Elements, Index: 0, Length: len(array.Elements)}))
				continue
			}
			if tuple, ok := iterable.AsTuple(); ok {
				vm.localSet(frame, slot, value.ObjectValue(&value.Iterator{Items: tuple.Elements, Index: 0, Length: len(tuple.Elements)}))
				continue
			}
			if m, ok := iterable.AsMap(); ok {
				keys := make([]string, 0, len(m.Entries))
				for key := range m.Entries {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				items := make([]value.Value, len(keys))
				for i, key := range keys {
					if mode == 1 {
						items[i] = value.ObjectValue(&value.Tuple{Elements: []value.Value{value.StringValue(key), m.Entries[key]}})
					} else {
						items[i] = value.StringValue(key)
					}
				}
				vm.localSet(frame, slot, value.ObjectValue(&value.Iterator{Items: items, Index: 0, Length: len(items)}))
				continue
			}
			return value.NilValue(), fmt.Errorf("ITER_INIT expects iterable")
		case bytecode.OpIterNext:
			iterSlot := readB(code, frame)
			valueSlot := readB(code, frame)
			offset := readU16At(code, frame.ip)
			frame.ip += 2
			// localGet hand-inlined: hottest iteration opcode.
			iterVal := frame.locals[iterSlot]
			if frame.hasCells {
				iterVal = vm.localGetSlow(frame, iterSlot)
			}
			iterator, ok := iterVal.AsIterator()
			if !ok {
				return value.NilValue(), fmt.Errorf("ITER_NEXT expects iterator")
			}
			if iterator.Items != nil {
				if iterator.Index >= iterator.Length {
					frame.ip += int(offset)
					continue
				}
				// Direct write: iterator value slots never have captures or string buffers.
				frame.locals[valueSlot] = iterator.Items[iterator.Index]
				iterator.Index++
				continue
			}
			if iterator.Receiver != nil && iterator.GetFn != nil {
				if iterator.Index >= iterator.Length {
					frame.ip += int(offset)
					continue
				}
				item, err := vm.invokeInstanceMethod(iterator.Receiver, iterator.GetFn, value.IntValue(int64(iterator.Index)))
				if err != nil {
					return value.NilValue(), err
				}
				vm.localSet(frame, valueSlot, item)
				iterator.Index++
				continue
			}
			if !iterator.Started {
				iterator.Started = true
			} else {
				iterator.Current += iterator.Range.Step
			}
			if iterator.Range.Step > 0 && iterator.Current >= iterator.Range.End {
				frame.ip += int(offset)
				continue
			}
			if iterator.Range.Step < 0 && iterator.Current <= iterator.Range.End {
				frame.ip += int(offset)
				continue
			}
			vm.localSet(frame, valueSlot, value.NumberValue(float64(iterator.Current)))
		case bytecode.OpGetField:
			slot := int(readB(code, frame))
			object := vm.pop()
			if instance, ok := object.Object.(*value.Instance); ok {
				vm.push(instance.Fields[slot])
				continue
			}
			return value.NilValue(), fmt.Errorf("GET_FIELD expects instance")
		case bytecode.OpGetThisField:
			slot := int(readB(code, frame))
			vm.push(frame.receiver.Fields[slot])
		case bytecode.OpSetField:
			slot := int(readB(code, frame))
			assigned := vm.pop()
			object := vm.pop()
			if instance, ok := object.Object.(*value.Instance); ok {
				if instance.Frozen {
					return value.NilValue(), fmt.Errorf("instance %s is frozen", instance.Class.Name)
				}
				instance.Fields[slot] = assigned
				continue
			}
			return value.NilValue(), fmt.Errorf("SET_FIELD expects instance")
		case bytecode.OpSetThisField:
			slot := int(readB(code, frame))
			assigned := vm.pop()
			if frame.receiver.Frozen {
				return value.NilValue(), fmt.Errorf("instance %s is frozen", frame.receiver.Class.Name)
			}
			frame.receiver.Fields[slot] = assigned
		case bytecode.OpGetProperty:
			name := frame.fn.Chunk.Constants[readU16At(code, frame.ip)].(string)
			frame.ip += 2
			object := vm.pop()
			if module, ok := object.AsModule(); ok {
				member, exists := module.Members[name]
				if !exists {
					return value.NilValue(), fmt.Errorf("module %s has no member %s", module.Name, name)
				}
				vm.push(member)
				continue
			}
			if wrapper, ok := object.AsSAMWrapper(); ok {
				if name != wrapper.MethodName {
					return value.NilValue(), fmt.Errorf("interface %s has no member %s", wrapper.InterfaceName, name)
				}
				vm.push(value.ObjectValue(&value.SAMBoundMethod{Wrapper: wrapper}))
				continue
			}
			if class, ok := object.AsClass(); ok {
				owner, member, exists := class.LookupStaticOwner(name)
				if !exists {
					return value.NilValue(), fmt.Errorf("class %s has no static member %s", class.Name, name)
				}
				if err := vm.ensureAccess(owner, owner.StaticVisibility[name]); err != nil {
					return value.NilValue(), err
				}
				if _, ok := member.AsFunction(); ok {
					vm.push(value.ObjectValue(&value.BoundStaticMethod{Class: class, Name: name, Owner: owner}))
					continue
				}
				vm.push(member)
				continue
			}
			if instance, ok := object.AsInstance(); ok {
				if owner, _, fieldDef, exists := instance.Class.LookupFieldOwner(name); exists {
					if err := vm.ensureAccess(owner, fieldDef.Visibility); err != nil {
						return value.NilValue(), err
					}
					field, _ := instance.GetField(name)
					vm.push(field)
					continue
				}
				if method, owner, exists := instance.Class.LookupMethod(name); exists {
					if err := vm.ensureAccess(owner, owner.MethodVisibility[name]); err != nil {
						return value.NilValue(), err
					}
					vm.push(value.ObjectValue(&value.BoundMethod{Receiver: instance, Name: name, Method: method, Owner: owner}))
					continue
				}
				return value.NilValue(), fmt.Errorf("instance %s has no member %s", instance.Class.Name, name)
			}
			return value.NilValue(), fmt.Errorf("cannot access property %s on %s", name, object.String())
		case bytecode.OpSetProperty:
			name := frame.fn.Chunk.Constants[readU16At(code, frame.ip)].(string)
			frame.ip += 2
			assigned := vm.pop()
			object := vm.pop()
			instance, ok := object.AsInstance()
			if !ok {
				if class, ok := object.AsClass(); ok {
					owner, _, exists := class.LookupStaticOwner(name)
					if exists {
						if err := vm.ensureAccess(owner, owner.StaticVisibility[name]); err != nil {
							return value.NilValue(), err
						}
					}
					if err := class.SetStatic(name, assigned); err != nil {
						return value.NilValue(), err
					}
					continue
				}
				return value.NilValue(), fmt.Errorf("cannot assign property %s on %s", name, object.String())
			}
			if owner, _, fieldDef, exists := instance.Class.LookupFieldOwner(name); exists {
				if err := vm.ensureAccess(owner, fieldDef.Visibility); err != nil {
					return value.NilValue(), err
				}
			}
			if !instance.SetField(name, assigned) {
				return value.NilValue(), fmt.Errorf("instance %s has no field %s", instance.Class.Name, name)
			}
		case bytecode.OpWrapInterface:
			ifaceName := frame.fn.Chunk.Constants[readU16At(code, frame.ip)].(string)
			frame.ip += 2
			methodName := frame.fn.Chunk.Constants[readU16At(code, frame.ip)].(string)
			frame.ip += 2
			callable := vm.pop()
			vm.push(value.ObjectValue(&value.SAMWrapper{InterfaceName: ifaceName, MethodName: methodName, Callable: callable}))
		case bytecode.OpArray:
			count := int(readB(code, frame))
			elements := make([]value.Value, count)
			for i := count - 1; i >= 0; i-- {
				elements[i] = vm.pop()
			}
			vm.push(value.ObjectValue(&value.Array{Elements: elements}))
		case bytecode.OpArrayAlloc:
			sizeVal := vm.pop()
			n := int(sizeVal.Int)
			if n < 0 {
				return value.NilValue(), fmt.Errorf("array size must be non-negative, got %d", n)
			}
			vm.push(value.ObjectValue(&value.Array{Elements: make([]value.Value, n)}))
		case bytecode.OpArrayFill:
			fill := vm.pop()
			sizeVal := vm.pop()
			n := int(sizeVal.Int)
			if n < 0 {
				return value.NilValue(), fmt.Errorf("array size must be non-negative, got %d", n)
			}
			elements := make([]value.Value, n)
			for i := range elements {
				elements[i] = fill
			}
			vm.push(value.ObjectValue(&value.Array{Elements: elements}))
		case bytecode.OpArrayPush:
			element := vm.pop()
			arr, ok := vm.peek(0).AsArray()
			if !ok {
				return value.NilValue(), fmt.Errorf("ARRAY_PUSH expects array on stack")
			}
			arr.Elements = append(arr.Elements, element)
		case bytecode.OpSetArrayLocals:
			arrSlot := readB(code, frame)
			idxSlot := readB(code, frame)
			assigned := vm.pop()
			arrVal := frame.locals[arrSlot]
			idxVal := frame.locals[idxSlot]
			if frame.hasCells {
				arrVal = vm.localGetSlow(frame, arrSlot)
				idxVal = vm.localGetSlow(frame, idxSlot)
			}
			arr, ok := arrVal.AsArray()
			if !ok {
				return value.NilValue(), fmt.Errorf("SET_ARRAY_LOCALS expects array")
			}
			idx := int(idxVal.Int)
			if idxVal.NumberKind != value.NumberInt {
				idx = int(idxVal.Num)
			}
			if idx < 0 || idx >= len(arr.Elements) {
				return value.NilValue(), fmt.Errorf("array index out of range")
			}
			arr.Elements[idx] = assigned
		case bytecode.OpSetLocalArrayBool:
			arrSlot := readB(code, frame)
			idxSlot := readB(code, frame)
			boolByte := readB(code, frame)
			arr, ok := frame.locals[arrSlot].AsArray()
			if !ok {
				return value.NilValue(), fmt.Errorf("SET_LOCAL_ARRAY_BOOL expects array")
			}
			idx := int(frame.locals[idxSlot].Int)
			if idx < 0 || idx >= len(arr.Elements) {
				return value.NilValue(), fmt.Errorf("array index out of range")
			}
			arr.Elements[idx] = value.BoolValue(boolByte != 0)
		case bytecode.OpAddLocalLocal:
			dstSlot := readB(code, frame)
			srcSlot := readB(code, frame)
			frame.locals[dstSlot] = value.IntValue(frame.locals[dstSlot].Int + frame.locals[srcSlot].Int)
		case bytecode.OpGetLocalArrayField:
			arrSlot := readB(code, frame)
			idxSlot := readB(code, frame)
			fieldSlot := int(readB(code, frame))
			arr, ok := frame.locals[arrSlot].AsArray()
			if !ok {
				return value.NilValue(), fmt.Errorf("GET_LOCAL_ARRAY_FIELD expects array")
			}
			idx := int(frame.locals[idxSlot].Int)
			if idx < 0 || idx >= len(arr.Elements) {
				return value.NilValue(), fmt.Errorf("array index out of range")
			}
			instance, ok := arr.Elements[idx].Object.(*value.Instance)
			if !ok {
				return value.NilValue(), fmt.Errorf("GET_LOCAL_ARRAY_FIELD expects instance element")
			}
			vm.push(instance.Fields[fieldSlot])
		case bytecode.OpMap:
			count := int(readB(code, frame))
			entries := map[string]value.Value{}
			for i := 0; i < count; i++ {
				val := vm.pop()
				key := vm.pop()
				if key.Kind != value.String {
					return value.NilValue(), fmt.Errorf("map key must be string")
				}
				entries[key.Str] = val
			}
			vm.push(value.ObjectValue(&value.Map{Entries: entries}))
		case bytecode.OpTuple:
			count := int(readB(code, frame))
			elements := make([]value.Value, count)
			for i := count - 1; i >= 0; i-- {
				elements[i] = vm.pop()
			}
			vm.push(value.ObjectValue(&value.Tuple{Elements: elements}))
		case bytecode.OpUnpack:
			count := int(readB(code, frame))
			source := vm.pop()
			if instance, ok := source.AsInstance(); ok {
				piecesMethod := instance.Class.SpecialMethod(value.SpecialMethodPieces)
				getPieceMethod := instance.Class.SpecialMethod(value.SpecialMethodGetPiece)
				if piecesMethod != nil && getPieceMethod != nil {
					pieces, err := vm.invokeInstanceMethod(instance, piecesMethod)
					if err != nil {
						return value.NilValue(), err
					}
					if pieces.Kind != value.Number {
						return value.NilValue(), fmt.Errorf("%s.__pieces() must return Number", instance.Class.Name)
					}
					if int(pieces.Num) != count {
						return value.NilValue(), fmt.Errorf("cannot unpack %d values from %s", count, instance.Class.Name)
					}
					for i := 0; i < count; i++ {
						piece, err := vm.invokeInstanceMethod(instance, getPieceMethod, value.NumberValue(float64(i)))
						if err != nil {
							return value.NilValue(), err
						}
						vm.push(piece)
					}
					continue
				}
			}
			if tuple, ok := source.AsTuple(); ok {
				if len(tuple.Elements) != count {
					return value.NilValue(), fmt.Errorf("cannot unpack %d values from tuple of size %d", count, len(tuple.Elements))
				}
				for _, element := range tuple.Elements {
					vm.push(element)
				}
				continue
			}
			if array, ok := source.AsArray(); ok {
				if len(array.Elements) != count {
					return value.NilValue(), fmt.Errorf("cannot unpack %d values from array of size %d", count, len(array.Elements))
				}
				for _, element := range array.Elements {
					vm.push(element)
				}
				continue
			}
			return value.NilValue(), fmt.Errorf("value is not unpackable")
		case bytecode.OpGetIndex:
			index := vm.pop()
			object := vm.pop()
			if object.Kind == value.String {
				if index.Kind != value.Number {
					return value.NilValue(), fmt.Errorf("String index must be number")
				}
				runes := []rune(object.Str)
				idx := int(index.Num)
				if idx < 0 || idx >= len(runes) {
					return value.NilValue(), fmt.Errorf("String index out of range")
				}
				vm.push(value.CharValue(runes[idx]))
				continue
			}
			if array, ok := object.AsArray(); ok {
				if index.Kind != value.Number {
					return value.NilValue(), fmt.Errorf("array index must be number")
				}
				var idx int
				if index.NumberKind == value.NumberInt {
					idx = int(index.Int)
				} else {
					idx = int(index.Num)
				}
				if idx < 0 || idx >= len(array.Elements) {
					return value.NilValue(), fmt.Errorf("array index out of range")
				}
				vm.push(array.Elements[idx])
				continue
			}
			if tuple, ok := object.AsTuple(); ok {
				if index.Kind != value.Number {
					return value.NilValue(), fmt.Errorf("tuple index must be number")
				}
				idx := int(index.Num)
				if idx < 0 || idx >= len(tuple.Elements) {
					return value.NilValue(), fmt.Errorf("tuple index out of range")
				}
				vm.push(tuple.Elements[idx])
				continue
			}
			if m, ok := object.AsMap(); ok {
				if index.Kind != value.String {
					return value.NilValue(), fmt.Errorf("map index must be string")
				}
				val, exists := m.Entries[index.Str]
				if !exists {
					vm.push(value.NilValue())
				} else {
					vm.push(val)
				}
				continue
			}
			if instance, ok := object.AsInstance(); ok {
				if indexGetMethod := instance.Class.SpecialMethod(value.SpecialMethodIndexGet); indexGetMethod != nil {
					result, err := vm.invokeInstanceMethod(instance, indexGetMethod, index)
					if err != nil {
						return value.NilValue(), err
					}
					vm.push(result)
					continue
				}
			}
			return value.NilValue(), fmt.Errorf("value is not indexable")
		case bytecode.OpGetIndexArray:
			index := vm.pop()
			object := vm.pop()
			array, ok := object.AsArray()
			if !ok {
				return value.NilValue(), fmt.Errorf("GET_INDEX_ARRAY expects array")
			}
			if index.Kind != value.Number {
				return value.NilValue(), fmt.Errorf("array index must be number")
			}
			var idx int
			if index.NumberKind == value.NumberInt {
				idx = int(index.Int)
			} else {
				idx = int(index.Num)
			}
			if idx < 0 || idx >= len(array.Elements) {
				return value.NilValue(), fmt.Errorf("array index out of range")
			}
			vm.push(array.Elements[idx])
		case bytecode.OpGetIndexMap:
			index := vm.pop()
			object := vm.pop()
			m, ok := object.AsMap()
			if !ok {
				return value.NilValue(), fmt.Errorf("GET_INDEX_MAP expects map")
			}
			if index.Kind != value.String {
				return value.NilValue(), fmt.Errorf("map index must be string")
			}
			if val, exists := m.Entries[index.Str]; exists {
				vm.push(val)
			} else {
				vm.push(value.NilValue())
			}
		case bytecode.OpContains:
			container := vm.pop()
			needle := vm.pop()
			if container.Kind == value.String {
				needleText, ok := valueAsText(needle)
				if !ok {
					return value.NilValue(), fmt.Errorf("String contains expects String or char")
				}
				vm.push(value.BoolValue(containsString(container.Str, needleText)))
				continue
			}
			if array, ok := container.AsArray(); ok {
				contains, err := containsValue(array.Elements, needle, vm.valuesEqual)
				if err != nil {
					return value.NilValue(), err
				}
				vm.push(value.BoolValue(contains))
				continue
			}
			if m, ok := container.AsMap(); ok {
				if needle.Kind != value.String {
					return value.NilValue(), fmt.Errorf("map contains expects string key")
				}
				_, exists := m.Entries[needle.Str]
				vm.push(value.BoolValue(exists))
				continue
			}
			if instance, ok := container.AsInstance(); ok {
				if containsMethod := instance.Class.SpecialMethod(value.SpecialMethodContains); containsMethod != nil {
					result, err := vm.invokeInstanceMethod(instance, containsMethod, needle)
					if err != nil {
						return value.NilValue(), err
					}
					vm.push(result)
					continue
				}
			}
			return value.NilValue(), fmt.Errorf("value does not support contains")
		case bytecode.OpContainsArray:
			container := vm.pop()
			needle := vm.pop()
			array, ok := container.AsArray()
			if !ok {
				return value.NilValue(), fmt.Errorf("CONTAINS_ARRAY expects array")
			}
			contains, err := containsValue(array.Elements, needle, vm.valuesEqual)
			if err != nil {
				return value.NilValue(), err
			}
			vm.push(value.BoolValue(contains))
		case bytecode.OpContainsMap:
			container := vm.pop()
			needle := vm.pop()
			m, ok := container.AsMap()
			if !ok {
				return value.NilValue(), fmt.Errorf("CONTAINS_MAP expects map")
			}
			if needle.Kind != value.String {
				return value.NilValue(), fmt.Errorf("map contains expects string key")
			}
			_, exists := m.Entries[needle.Str]
			vm.push(value.BoolValue(exists))
		case bytecode.OpContainsString:
			container := vm.pop()
			needle := vm.pop()
			if container.Kind != value.String {
				return value.NilValue(), fmt.Errorf("CONTAINS_STRING expects String")
			}
			needleText, ok := valueAsText(needle)
			if !ok {
				return value.NilValue(), fmt.Errorf("String contains expects String or char")
			}
			vm.push(value.BoolValue(containsString(container.Str, needleText)))
		case bytecode.OpSlice:
			end := vm.pop()
			start := vm.pop()
			object := vm.pop()
			if start.Kind != value.Number || end.Kind != value.Number {
				return value.NilValue(), fmt.Errorf("slice bounds must be numbers")
			}
			startIdx := int(start.Num)
			endIdx := int(end.Num)
			if object.Kind == value.String {
				runes := []rune(object.Str)
				sliced, err := sliceRunes(runes, startIdx, endIdx)
				if err != nil {
					return value.NilValue(), err
				}
				vm.push(value.StringValue(string(sliced)))
				continue
			}
			if array, ok := object.AsArray(); ok {
				sliced, err := sliceValues(array.Elements, startIdx, endIdx)
				if err != nil {
					return value.NilValue(), err
				}
				copyElements := append([]value.Value(nil), sliced...)
				vm.push(value.ObjectValue(&value.Array{Elements: copyElements}))
				continue
			}
			if tuple, ok := object.AsTuple(); ok {
				sliced, err := sliceValues(tuple.Elements, startIdx, endIdx)
				if err != nil {
					return value.NilValue(), err
				}
				copyElements := append([]value.Value(nil), sliced...)
				vm.push(value.ObjectValue(&value.Tuple{Elements: copyElements}))
				continue
			}
			if instance, ok := object.AsInstance(); ok {
				if sliceMethod := instance.Class.SpecialMethod(value.SpecialMethodSlice); sliceMethod != nil {
					result, err := vm.invokeInstanceMethod(instance, sliceMethod, start, end)
					if err != nil {
						return value.NilValue(), err
					}
					vm.push(result)
					continue
				}
			}
			return value.NilValue(), fmt.Errorf("value is not sliceable")
		case bytecode.OpSetIndex:
			assigned := vm.pop()
			index := vm.pop()
			object := vm.pop()
			if array, ok := object.AsArray(); ok {
				if index.Kind != value.Number {
					return value.NilValue(), fmt.Errorf("array index must be number")
				}
				var idx int
				if index.NumberKind == value.NumberInt {
					idx = int(index.Int)
				} else {
					idx = int(index.Num)
				}
				if idx < 0 || idx >= len(array.Elements) {
					return value.NilValue(), fmt.Errorf("array index out of range")
				}
				array.Elements[idx] = assigned
				continue
			}
			if m, ok := object.AsMap(); ok {
				if index.Kind != value.String {
					return value.NilValue(), fmt.Errorf("map index must be string")
				}
				m.Entries[index.Str] = assigned
				continue
			}
			if instance, ok := object.AsInstance(); ok {
				if indexSetMethod := instance.Class.SpecialMethod(value.SpecialMethodIndexSet); indexSetMethod != nil {
					if _, err := vm.invokeInstanceMethod(instance, indexSetMethod, index, assigned); err != nil {
						return value.NilValue(), err
					}
					continue
				}
			}
			return value.NilValue(), fmt.Errorf("value is not index-assignable")
		case bytecode.OpSetIndexArray:
			assigned := vm.pop()
			index := vm.pop()
			object := vm.pop()
			array, ok := object.AsArray()
			if !ok {
				return value.NilValue(), fmt.Errorf("SET_INDEX_ARRAY expects array")
			}
			if index.Kind != value.Number {
				return value.NilValue(), fmt.Errorf("array index must be number")
			}
			var idx int
			if index.NumberKind == value.NumberInt {
				idx = int(index.Int)
			} else {
				idx = int(index.Num)
			}
			if idx < 0 || idx >= len(array.Elements) {
				return value.NilValue(), fmt.Errorf("array index out of range")
			}
			array.Elements[idx] = assigned
		case bytecode.OpSetIndexMap:
			assigned := vm.pop()
			index := vm.pop()
			object := vm.pop()
			m, ok := object.AsMap()
			if !ok {
				return value.NilValue(), fmt.Errorf("SET_INDEX_MAP expects map")
			}
			if index.Kind != value.String {
				return value.NilValue(), fmt.Errorf("map index must be string")
			}
			m.Entries[index.Str] = assigned
		case bytecode.OpFreeze:
			frozen := vm.pop()
			if instance, ok := frozen.AsInstance(); ok {
				instance.Frozen = true
			}
			vm.push(frozen)
		case bytecode.OpCallSuper:
			argc := int(readB(code, frame))
			if err := vm.callSuper(argc); err != nil {
				handled, raised := vm.handleRaised(baseDepth, frame, err)
				if handled {
					frame = vm.frames[len(vm.frames)-1]
					code = frame.fn.Chunk.Code
					continue
				}
				return value.NilValue(), raised
			}
			frame = vm.frames[len(vm.frames)-1]
			code = frame.fn.Chunk.Code
		case bytecode.OpReturn:
			result := vm.pop()
			if frame.init && frame.receiver != nil {
				result = value.ObjectValue(frame.receiver)
			}
			if vm.sp > frame.stackBase {
				vm.sp = frame.stackBase
			}
			vm.frames = vm.frames[:len(vm.frames)-1]
			vm.releaseFrame(frame)
			if len(vm.frames) == baseDepth {
				return result, nil
			}
			vm.push(result)
			frame = vm.frames[len(vm.frames)-1]
			code = frame.fn.Chunk.Code
		default:
			return value.NilValue(), fmt.Errorf("unknown opcode %d", op)
		}
	}
}

func (vm *VM) invokeInstanceMethod(receiver *value.Instance, fn *bytecode.Function, args ...value.Value) (value.Value, error) {
	if fn == nil {
		return value.NilValue(), fmt.Errorf("missing protocol method on %s", receiver.Class.Name)
	}
	if fn.Arity != len(args) {
		return value.NilValue(), fmt.Errorf("%s expects %d args, got %d", fn.Name, fn.Arity, len(args))
	}
	baseDepth := len(vm.frames)
	child := vm.acquireFrame(fn, nil, receiver, false)
	vm.localSet(child, 0, value.ObjectValue(receiver))
	for i, arg := range args {
		vm.localSet(child, byte(i+1), arg)
	}
	vm.frames = append(vm.frames, child)
	return vm.executeUntilDepth(baseDepth)
}

// pickOverload selects the function overload that exactly matches argc.
// Returns nil if no matching overload is found.
func pickOverload(overloads []*bytecode.Function, argc int) *bytecode.Function {
	for _, fn := range overloads {
		if fn != nil && fn.Arity == argc {
			return fn
		}
	}
	return nil
}

func (vm *VM) call(argc int) error {
	callee := vm.peek(argc)
	if wrapper, ok := callee.AsSAMWrapper(); ok {
		vm.stack[vm.sp-1-argc] = wrapper.Callable
		return vm.call(argc)
	}
	if bound, ok := callee.AsSAMBoundMethod(); ok {
		vm.stack[vm.sp-1-argc] = bound.Wrapper.Callable
		return vm.call(argc)
	}
	if bound, ok := callee.AsBoundMethod(); ok {
		args := vm.peekArgs(argc)
		fn := bound.Method
		if bound.Name != "" {
			if resolved, owner, ok := vm.resolveMethodOverload(bound.Receiver.Class, bound.Name, args); ok {
				fn = resolved
				bound.Owner = owner
			}
		}
		if fn.Arity != argc {
			return fmt.Errorf("%s expects %d args, got %d", fn.Name, fn.Arity, argc)
		}
		child := vm.acquireFrame(fn, nil, bound.Receiver, false)
		vm.localSet(child, 0, value.ObjectValue(bound.Receiver))
		for i := argc - 1; i >= 0; i-- {
			vm.localSet(child, byte(i+1), vm.pop())
		}
		vm.pop()
		child.stackBase = vm.sp
		vm.frames = append(vm.frames, child)
		return nil
	}
	if bound, ok := callee.AsBoundStaticMethod(); ok {
		args := vm.peekArgs(argc)
		fn, _, ok := vm.resolveStaticOverload(bound.Class, bound.Name, args)
		if !ok || fn == nil {
			return fmt.Errorf("class %s has no static overload %s/%d", bound.Class.Name, bound.Name, argc)
		}
		vm.stack[vm.sp-1-argc] = value.ObjectValue(fn)
		return vm.call(argc)
	}
	if class, ok := callee.AsClass(); ok {
		if class.IsAbstract {
			return fmt.Errorf("cannot instantiate abstract class %s", class.Name)
		}
		if err := vm.ensureAccess(class, class.ConstructorVisibility); err != nil {
			return err
		}
		if fastCtor := class.FastConstructor; fastCtor != nil {
			if fastCtor.Arity != argc {
				return fmt.Errorf("%s expects %d args, got %d", class.Name, fastCtor.Arity, argc)
			}
			instance := vm.acquireInstance(class)
			baseIdx := vm.sp - argc
			for i, slot := range fastCtor.FieldSlots {
				instance.Fields[slot] = vm.stack[baseIdx+fastCtor.ArgIndexes[i]]
			}
			vm.sp -= argc + 1
			vm.push(value.ObjectValue(instance))
			return nil
		}
		var ctor *bytecode.Function
		if len(class.ConstructorOverloads) <= 1 {
			ctor = class.Constructor
		} else {
			args := vm.peekArgs(argc)
			ctor = vm.selectBestOverload(class.ConstructorOverloads, args)
			if ctor == nil && class.Constructor != nil && vm.overloadMatches(class.Constructor, args) {
				ctor = class.Constructor
			}
		}

		if ctor == nil && argc != 0 {
			return fmt.Errorf("%s expects 0 args, got %d", class.Name, argc)
		}
		if ctor != nil && ctor.Arity != argc {
			return fmt.Errorf("%s expects %d args, got %d", class.Name, ctor.Arity, argc)
		}
		instance := vm.acquireInstance(class)
		if ctor == nil {
			for i := 0; i < argc; i++ {
				vm.pop()
			}
			vm.pop()
			vm.push(value.ObjectValue(instance))
			return nil
		}
		child := vm.acquireFrame(ctor, nil, instance, true)
		vm.localSet(child, 0, value.ObjectValue(instance))
		for i := argc - 1; i >= 0; i-- {
			vm.localSet(child, byte(i+1), vm.pop())
		}
		vm.pop()
		child.stackBase = vm.sp
		vm.frames = append(vm.frames, child)
		return nil
	}
	if builtin, ok := callee.AsBuiltin(); ok {
		if builtin.Arity >= 0 && builtin.Arity != argc {
			return fmt.Errorf("%s expects %d args, got %d", builtin.Name, builtin.Arity, argc)
		}
		args := vm.borrowBuiltinArgs(argc)
		for i := argc - 1; i >= 0; i-- {
			args[i] = vm.pop()
		}
		vm.pop()
		var (
			result value.Value
			err    error
		)
		if builtin.Name == "hash" {
			result, err = vm.hashValue(args[0])
		} else {
			result, err = builtin.Fn(args)
		}
		if err != nil {
			return err
		}
		clear(args)
		vm.push(result)
		return nil
	}
	if closure, ok := callee.AsClosure(); ok {
		fn := closure.Function
		if fn.Arity != argc {
			return fmt.Errorf("%s expects %d args, got %d", fn.Name, fn.Arity, argc)
		}
		child := vm.acquireFrame(fn, closure, nil, false)
		for i := argc - 1; i >= 0; i-- {
			vm.localSet(child, byte(i), vm.pop())
		}
		vm.pop()
		child.stackBase = vm.sp
		vm.frames = append(vm.frames, child)
		return nil
	}

	fn, ok := callee.AsFunction()
	if !ok {
		return fmt.Errorf("attempted to call non-callable %s", callee.String())
	}
	if fn.Arity != argc {
		return fmt.Errorf("%s expects %d args, got %d", fn.Name, fn.Arity, argc)
	}

	child := vm.acquireFrame(fn, nil, nil, false)
	for i := argc - 1; i >= 0; i-- {
		vm.localSet(child, byte(i), vm.pop())
	}
	vm.pop()
	child.stackBase = vm.sp
	vm.frames = append(vm.frames, child)
	return nil
}

func (vm *VM) callGlobalSlot(slot int, argc int) error {
	return vm.callKnownValue(vm.globalSlots[slot], argc)
}

func (vm *VM) callKnownValueWithArgs(callee value.Value, args ...value.Value) error {
	for _, arg := range args {
		vm.push(arg)
	}
	return vm.callKnownValue(callee, len(args))
}

func (vm *VM) callKnownValue(callee value.Value, argc int) error {
	if wrapper, ok := callee.AsSAMWrapper(); ok {
		return vm.callKnownValue(wrapper.Callable, argc)
	}
	if builtin, ok := callee.AsBuiltin(); ok {
		if builtin.Arity >= 0 && builtin.Arity != argc {
			return fmt.Errorf("%s expects %d args, got %d", builtin.Name, builtin.Arity, argc)
		}
		args := vm.borrowBuiltinArgs(argc)
		for i := argc - 1; i >= 0; i-- {
			args[i] = vm.pop()
		}
		var (
			result value.Value
			err    error
		)
		if builtin.Name == "hash" {
			result, err = vm.hashValue(args[0])
		} else {
			result, err = builtin.Fn(args)
		}
		if err != nil {
			return err
		}
		clear(args)
		vm.push(result)
		return nil
	}
	if closure, ok := callee.AsClosure(); ok {
		fn := closure.Function
		if fn.Arity != argc {
			return fmt.Errorf("%s expects %d args, got %d", fn.Name, fn.Arity, argc)
		}
		child := vm.acquireFrame(fn, closure, nil, false)
		for i := argc - 1; i >= 0; i-- {
			vm.localSet(child, byte(i), vm.pop())
		}
		child.stackBase = vm.sp
		vm.frames = append(vm.frames, child)
		return nil
	}
	if fn, ok := callee.AsFunction(); ok {
		if fn.Arity != argc {
			return fmt.Errorf("%s expects %d args, got %d", fn.Name, fn.Arity, argc)
		}
		child := vm.acquireFrame(fn, nil, nil, false)
		for i := argc - 1; i >= 0; i-- {
			vm.localSet(child, byte(i), vm.pop())
		}
		child.stackBase = vm.sp
		vm.frames = append(vm.frames, child)
		return nil
	}
	if class, ok := callee.AsClass(); ok {
		if class.IsAbstract {
			return fmt.Errorf("cannot instantiate abstract class %s", class.Name)
		}
		if err := vm.ensureAccess(class, class.ConstructorVisibility); err != nil {
			return err
		}
		var ctor *bytecode.Function
		fastCtor := class.FastConstructor

		if len(class.ConstructorOverloads) <= 1 {
			ctor = class.Constructor
		} else {
			args := vm.peekArgs(argc)
			ctor = vm.selectBestOverload(class.ConstructorOverloads, args)
			if ctor == nil && class.Constructor != nil && vm.overloadMatches(class.Constructor, args) {
				ctor = class.Constructor
			}
		}

		if ctor == nil && argc != 0 {
			return fmt.Errorf("%s expects 0 args, got %d", class.Name, argc)
		}
		if ctor != nil && ctor.Arity != argc {
			return fmt.Errorf("%s expects %d args, got %d", class.Name, ctor.Arity, argc)
		}
		if fastCtor != nil && ctor == class.Constructor {
			if fastCtor.Arity != argc {
				return fmt.Errorf("%s expects %d args, got %d", class.Name, fastCtor.Arity, argc)
			}
			instance := vm.acquireInstance(class)
			baseIdx := vm.sp - argc
			for i, slot := range fastCtor.FieldSlots {
				instance.Fields[slot] = vm.stack[baseIdx+fastCtor.ArgIndexes[i]]
			}
			vm.sp -= argc
			vm.push(value.ObjectValue(instance))
			return nil
		}
		instance := vm.acquireInstance(class)
		if ctor == nil {
			for i := 0; i < argc; i++ {
				vm.pop()
			}
			vm.push(value.ObjectValue(instance))
			return nil
		}
		child := vm.acquireFrame(ctor, nil, instance, true)
		vm.localSet(child, 0, value.ObjectValue(instance))
		for i := argc - 1; i >= 0; i-- {
			vm.localSet(child, byte(i+1), vm.pop())
		}
		child.stackBase = vm.sp
		vm.frames = append(vm.frames, child)
		return nil
	}
	return fmt.Errorf("attempted to call non-callable %s", callee.String())
}

func (vm *VM) callSelfLocalSubInt(current *frame, arg int64) error {
	fn := current.fn
	if fn.Arity != 1 {
		return fmt.Errorf("%s expects %d args, got %d", fn.Name, fn.Arity, 1)
	}
	child := vm.acquireFrame(fn, current.closure, current.receiver, false)
	vm.localSet(child, 0, value.IntValue(arg))
	vm.frames = append(vm.frames, child)
	return nil
}

func (vm *VM) hashValue(v value.Value) (value.Value, error) {
	hasher := fnv.New64a()
	if err := vm.writeHashValue(hasher, v, map[string]bool{}); err != nil {
		return value.NilValue(), err
	}
	return value.StringValue(fmt.Sprintf("%016x", hasher.Sum64())), nil
}

func (vm *VM) writeHashValue(hasher io.Writer, v value.Value, seen map[string]bool) error {
	switch v.Kind {
	case value.Nil:
		_, _ = io.WriteString(hasher, "nil;")
		return nil
	case value.Number:
		_, _ = io.WriteString(hasher, fmt.Sprintf("number:%g;", v.Num))
		return nil
	case value.Bool:
		_, _ = io.WriteString(hasher, fmt.Sprintf("bool:%t;", v.Bool))
		return nil
	case value.Char:
		_, _ = io.WriteString(hasher, "char:")
		_, _ = io.WriteString(hasher, v.Str)
		_, _ = io.WriteString(hasher, ";")
		return nil
	case value.String:
		_, _ = io.WriteString(hasher, "string:")
		_, _ = io.WriteString(hasher, v.Str)
		_, _ = io.WriteString(hasher, ";")
		return nil
	}

	switch obj := v.Object.(type) {
	case *value.Tuple:
		key := fmt.Sprintf("tuple:%p", obj)
		if seen[key] {
			_, _ = io.WriteString(hasher, key)
			return nil
		}
		seen[key] = true
		defer delete(seen, key)
		_, _ = io.WriteString(hasher, "tuple[")
		for _, element := range obj.Elements {
			if err := vm.writeHashValue(hasher, element, seen); err != nil {
				return err
			}
		}
		_, _ = io.WriteString(hasher, "];")
		return nil
	case *value.Array:
		key := fmt.Sprintf("array:%p", obj)
		if seen[key] {
			_, _ = io.WriteString(hasher, key)
			return nil
		}
		seen[key] = true
		defer delete(seen, key)
		_, _ = io.WriteString(hasher, "array[")
		for _, element := range obj.Elements {
			if err := vm.writeHashValue(hasher, element, seen); err != nil {
				return err
			}
		}
		_, _ = io.WriteString(hasher, "];")
		return nil
	case *value.Map:
		key := fmt.Sprintf("map:%p", obj)
		if seen[key] {
			_, _ = io.WriteString(hasher, key)
			return nil
		}
		seen[key] = true
		defer delete(seen, key)
		ordered := make([]string, 0, len(obj.Entries))
		for entryKey := range obj.Entries {
			ordered = append(ordered, entryKey)
		}
		sort.Strings(ordered)
		_, _ = io.WriteString(hasher, "map{")
		for _, entryKey := range ordered {
			_, _ = io.WriteString(hasher, entryKey)
			_, _ = io.WriteString(hasher, ":")
			if err := vm.writeHashValue(hasher, obj.Entries[entryKey], seen); err != nil {
				return err
			}
		}
		_, _ = io.WriteString(hasher, "};")
		return nil
	case *value.Instance:
		key := fmt.Sprintf("instance:%p", obj)
		if seen[key] {
			_, _ = io.WriteString(hasher, key)
			return nil
		}
		seen[key] = true
		defer delete(seen, key)
		if result, applied, err := vm.tryHashMethod(obj); err != nil {
			return err
		} else if applied {
			return vm.writeHashValue(hasher, result, seen)
		}
		_, _ = io.WriteString(hasher, "instance:")
		_, _ = io.WriteString(hasher, obj.Class.Name)
		_, _ = io.WriteString(hasher, "{")
		for _, fieldName := range obj.Class.FieldOrder {
			slot, _, ok := obj.Class.LookupFieldSlot(fieldName)
			if !ok || slot >= len(obj.Fields) {
				continue
			}
			_, _ = io.WriteString(hasher, fieldName)
			_, _ = io.WriteString(hasher, ":")
			if err := vm.writeHashValue(hasher, obj.Fields[slot], seen); err != nil {
				return err
			}
		}
		_, _ = io.WriteString(hasher, "};")
		return nil
	case *value.Class:
		_, _ = io.WriteString(hasher, "class:")
		_, _ = io.WriteString(hasher, obj.Name)
		_, _ = io.WriteString(hasher, ";")
		return nil
	case *value.Module:
		_, _ = io.WriteString(hasher, "module:")
		_, _ = io.WriteString(hasher, obj.Name)
		_, _ = io.WriteString(hasher, ";")
		return nil
	case *value.Builtin:
		_, _ = io.WriteString(hasher, "builtin:")
		_, _ = io.WriteString(hasher, obj.Name)
		_, _ = io.WriteString(hasher, ";")
		return nil
	case *value.BoundMethod:
		_, _ = io.WriteString(hasher, fmt.Sprintf("bound:%p;", obj))
		return nil
	case *value.Closure:
		_, _ = io.WriteString(hasher, fmt.Sprintf("closure:%p;", obj))
		return nil
	case *bytecode.Function:
		_, _ = io.WriteString(hasher, fmt.Sprintf("function:%s;", obj.Name))
		return nil
	case *value.Range:
		_, _ = io.WriteString(hasher, fmt.Sprintf("range:%d:%d:%d;", obj.Start, obj.End, obj.Step))
		return nil
	default:
		_, _ = io.WriteString(hasher, fmt.Sprintf("object:%T:%p;", obj, obj))
		return nil
	}
}

func (vm *VM) matchesType(candidate value.Value, typeName string) bool {
	for _, option := range splitTopLevelTypeList(typeName, '|') {
		trimmed := trimTypeSpace(option)
		if trimmed != "" && trimmed != typeName && vm.matchesType(candidate, trimmed) {
			return true
		}
	}
	base, args := parseGenericType(typeName)
	base = normalizeRuntimeTypeAlias(base)
	typeName = normalizeRuntimeTypeAlias(typeName)
	if base != typeName {
		switch base {
		case bvmruntime.TypeArray:
			array, ok := candidate.AsArray()
			if !ok {
				return false
			}
			if len(args) != 1 {
				return true
			}
			for _, element := range array.Elements {
				if !vm.matchesType(element, args[0]) {
					return false
				}
			}
			return true
		case bvmruntime.TypeMap:
			mapped, ok := candidate.AsMap()
			if !ok {
				return false
			}
			if len(args) >= 1 && args[0] != bvmruntime.TypeString && args[0] != bvmruntime.TypeAny {
				return false
			}
			if len(args) < 2 {
				return true
			}
			for _, element := range mapped.Entries {
				if !vm.matchesType(element, args[1]) {
					return false
				}
			}
			return true
		case bvmruntime.TypeTuple:
			tuple, ok := candidate.AsTuple()
			if !ok {
				return false
			}
			if len(args) != len(tuple.Elements) {
				return false
			}
			for i, element := range tuple.Elements {
				if !vm.matchesType(element, args[i]) {
					return false
				}
			}
			return true
		case bvmruntime.TypeFunction:
			typeName = bvmruntime.TypeFunction
		default:
			typeName = base
		}
	}
	switch typeName {
	case bvmruntime.TypeAny:
		return true
	case bvmruntime.TypeInt:
		if candidate.Kind == value.Number && candidate.NumberKind == value.NumberInt {
			return true
		}
		if instance, ok := candidate.AsInstance(); ok {
			return instance.Class != nil && rootRuntimeTypeName(instance.Class.Name) == "Integer"
		}
		return false
	case bvmruntime.TypeFloat:
		if candidate.Kind == value.Number && candidate.NumberKind == value.NumberFloat {
			return true
		}
		if instance, ok := candidate.AsInstance(); ok {
			return instance.Class != nil && (rootRuntimeTypeName(instance.Class.Name) == "Integer" || rootRuntimeTypeName(instance.Class.Name) == "Float" || rootRuntimeTypeName(instance.Class.Name) == "Double")
		}
		return false
	case bvmruntime.TypeNumber:
		if candidate.Kind == value.Number {
			return true
		}
		if instance, ok := candidate.AsInstance(); ok {
			return instance.Class != nil && (rootRuntimeTypeName(instance.Class.Name) == "Integer" || rootRuntimeTypeName(instance.Class.Name) == "Float" || rootRuntimeTypeName(instance.Class.Name) == "Double")
		}
		return false
	case bvmruntime.TypeChar:
		if candidate.Kind == value.Char {
			return true
		}
		if instance, ok := candidate.AsInstance(); ok {
			return instance.Class != nil && rootRuntimeTypeName(instance.Class.Name) == "Char"
		}
		return false
	case bvmruntime.TypeString:
		if candidate.Kind == value.String || candidate.Kind == value.Char {
			return true
		}
		if instance, ok := candidate.AsInstance(); ok {
			return instance.Class != nil && rootRuntimeTypeName(instance.Class.Name) == "String"
		}
		return false
	case bvmruntime.TypeBool:
		if candidate.Kind == value.Bool {
			return true
		}
		if instance, ok := candidate.AsInstance(); ok {
			return instance.Class != nil && rootRuntimeTypeName(instance.Class.Name) == "Boolean"
		}
		return false
	case bvmruntime.TypeNil, bvmruntime.TypeVoid:
		return candidate.Kind == value.Nil
	case bvmruntime.TypeArray:
		_, ok := candidate.AsArray()
		return ok
	case bvmruntime.TypeTuple:
		_, ok := candidate.AsTuple()
		return ok
	case bvmruntime.TypeMap:
		_, ok := candidate.AsMap()
		return ok
	case bvmruntime.TypeRange:
		_, ok := candidate.AsRange()
		return ok
	case bvmruntime.TypeFunction:
		if _, ok := candidate.AsFunction(); ok {
			return true
		}
		if _, ok := candidate.AsClosure(); ok {
			return true
		}
		if _, ok := candidate.AsBuiltin(); ok {
			return true
		}
		if wrapper, ok := candidate.AsSAMWrapper(); ok {
			return wrapper.InterfaceName == bvmruntime.TypeFunction
		}
		return false
	}
	switch rootRuntimeTypeName(typeName) {
	case "Integer":
		if candidate.Kind == value.Number && candidate.NumberKind == value.NumberInt {
			return true
		}
		if instance, ok := candidate.AsInstance(); ok {
			return instance.Class != nil && rootRuntimeTypeName(instance.Class.Name) == "Integer"
		}
		return false
	case "Float", "Double":
		if candidate.Kind == value.Number && candidate.NumberKind == value.NumberFloat {
			return true
		}
		if instance, ok := candidate.AsInstance(); ok {
			return instance.Class != nil && (rootRuntimeTypeName(instance.Class.Name) == "Float" || rootRuntimeTypeName(instance.Class.Name) == "Double")
		}
		return false
	case "Boolean":
		if candidate.Kind == value.Bool {
			return true
		}
		if instance, ok := candidate.AsInstance(); ok {
			return instance.Class != nil && rootRuntimeTypeName(instance.Class.Name) == "Boolean"
		}
		return false
	case "Char":
		if candidate.Kind == value.Char {
			return true
		}
		if instance, ok := candidate.AsInstance(); ok {
			return instance.Class != nil && rootRuntimeTypeName(instance.Class.Name) == "Char"
		}
		return false
	}
	if vm.matchesStructuralInterface(candidate, typeName) {
		return true
	}
	if wrapper, ok := candidate.AsSAMWrapper(); ok {
		return wrapper.InterfaceName == rootRuntimeTypeName(typeName)
	}
	if instance, ok := candidate.AsInstance(); ok {
		if instance.Class.Implements[rootRuntimeTypeName(typeName)] {
			return true
		}
		for current := instance.Class; current != nil; current = current.Superclass {
			if current.Name == typeName || rootRuntimeTypeName(current.Name) == rootRuntimeTypeName(typeName) {
				return true
			}
		}
	}
	if classValue, ok := candidate.AsClass(); ok {
		return classValue.Name == typeName
	}
	return false
}

func (vm *VM) runtimeTypeName(candidate value.Value) string {
	switch candidate.Kind {
	case value.Nil:
		return bvmruntime.TypeNil
	case value.Bool:
		return bvmruntime.TypeBool
	case value.Char:
		return bvmruntime.TypeChar
	case value.String:
		return bvmruntime.TypeString
	case value.Number:
		if candidate.NumberKind == value.NumberInt {
			return bvmruntime.TypeInt
		}
		return bvmruntime.TypeFloat
	case value.Object:
		if instance, ok := candidate.AsInstance(); ok {
			return instance.Class.Name
		}
		if wrapper, ok := candidate.AsSAMWrapper(); ok {
			return wrapper.InterfaceName
		}
		if _, ok := candidate.AsArray(); ok {
			return bvmruntime.TypeArray
		}
		if _, ok := candidate.AsMap(); ok {
			return bvmruntime.TypeMap
		}
		if _, ok := candidate.AsTuple(); ok {
			return bvmruntime.TypeTuple
		}
		if _, ok := candidate.AsRange(); ok {
			return bvmruntime.TypeRange
		}
		if _, ok := candidate.AsClass(); ok {
			return "Class"
		}
		if _, ok := candidate.AsFunction(); ok {
			return bvmruntime.TypeFunction
		}
		if _, ok := candidate.AsClosure(); ok {
			return bvmruntime.TypeFunction
		}
		if _, ok := candidate.AsBuiltin(); ok {
			return bvmruntime.TypeFunction
		}
	}
	return "Unknown"
}

func trimTypeSpace(input string) string {
	start := 0
	end := len(input)
	for start < end && input[start] == ' ' {
		start++
	}
	for end > start && input[end-1] == ' ' {
		end--
	}
	return input[start:end]
}

func splitTopLevelTypeList(input string, sep rune) []string {
	depth := 0
	parts := make([]string, 0, 2)
	start := 0
	for i, r := range input {
		switch r {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		default:
			if r == sep && depth == 0 {
				parts = append(parts, trimTypeSpace(input[start:i]))
				start = i + 1
			}
		}
	}
	if start == 0 {
		return nil
	}
	parts = append(parts, trimTypeSpace(input[start:]))
	return parts
}

func parseGenericType(typeName string) (string, []string) {
	start := -1
	for i, r := range typeName {
		if r == '<' {
			start = i
			break
		}
	}
	if start < 0 || len(typeName) == 0 || typeName[len(typeName)-1] != '>' {
		return typeName, nil
	}
	return trimTypeSpace(typeName[:start]), splitTopLevelTypeList(typeName[start+1:len(typeName)-1], ',')
}

func rootRuntimeTypeName(typeName string) string {
	base, _ := parseGenericType(typeName)
	base = trimTypeSpace(base)
	if dot := strings.LastIndex(base, "."); dot >= 0 && dot+1 < len(base) {
		base = base[dot+1:]
	}
	return normalizeRuntimeTypeAlias(base)
}

func normalizeRuntimeTypeAlias(typeName string) string {
	switch trimTypeSpace(typeName) {
	case "Int":
		return bvmruntime.TypeInt
	case "Float":
		return bvmruntime.TypeFloat
	case "Number":
		return bvmruntime.TypeNumber
	case "Bool":
		return bvmruntime.TypeBool
	case "Char":
		return bvmruntime.TypeChar
	case "String", "string":
		return bvmruntime.TypeString
	case "Array", "array":
		return bvmruntime.TypeArray
	case "Map", "map":
		return bvmruntime.TypeMap
	case "Tuple", "tuple":
		return bvmruntime.TypeTuple
	case "Range", "range":
		return bvmruntime.TypeRange
	case "Function":
		return bvmruntime.TypeFunction
	case "Any", "any", "":
		return bvmruntime.TypeAny
	default:
		return trimTypeSpace(typeName)
	}
}

func valueAsText(candidate value.Value) (string, bool) {
	switch candidate.Kind {
	case value.Char, value.String:
		return candidate.Str, true
	default:
		if instance, ok := candidate.AsInstance(); ok && instance.Class != nil {
			switch rootRuntimeTypeName(instance.Class.Name) {
			case "String", "Char":
				if slot, _, ok := instance.Class.LookupFieldSlot("value"); ok && slot >= 0 && slot < len(instance.Fields) {
					inner := instance.Fields[slot]
					if inner.Kind == value.String || inner.Kind == value.Char {
						return inner.Str, true
					}
				}
				if len(instance.Fields) > 0 {
					inner := instance.Fields[0]
					if inner.Kind == value.String || inner.Kind == value.Char {
						return inner.Str, true
					}
				}
			}
		}
		return "", false
	}
}

func sliceRunes(items []rune, start, end int) ([]rune, error) {
	if start < 0 || end < 0 || start > len(items) || end >= len(items) {
		return nil, fmt.Errorf("slice out of range")
	}
	if start > end {
		return []rune{}, nil
	}
	return items[start : end+1], nil
}

func sliceValues(items []value.Value, start, end int) ([]value.Value, error) {
	if start < 0 || end < 0 || start > len(items) || end >= len(items) {
		return nil, fmt.Errorf("slice out of range")
	}
	if start > end {
		return []value.Value{}, nil
	}
	return items[start : end+1], nil
}

func containsString(haystack string, needle string) bool {
	return strings.Contains(haystack, needle)
}

func indexString(haystack string, needle string) int {
	return strings.Index(haystack, needle)
}

func containsValue(items []value.Value, needle value.Value, equals func(value.Value, value.Value) (bool, error)) (bool, error) {
	for _, item := range items {
		matched, err := equals(item, needle)
		if err != nil {
			return false, err
		}
		if matched {
			return true, nil
		}
	}
	return false, nil
}

func (vm *VM) invoke(name string, argc int) error {
	receiver := vm.peek(argc)
	if module, ok := receiver.AsModule(); ok {
		member, exists := module.Members[name]
		if !exists {
			return fmt.Errorf("module %s has no member %s", module.Name, name)
		}
		vm.stack[vm.sp-1-argc] = member
		return vm.call(argc)
	}
	if instance, ok := receiver.AsInstance(); ok {
		args := vm.peekArgs(argc)
		method, owner, exists := vm.resolveMethodOverload(instance.Class, name, args)
		if !exists {
			return fmt.Errorf("instance %s has no method %s", instance.Class.Name, name)
		}
		if err := vm.ensureAccess(owner, owner.MethodVisibility[name]); err != nil {
			return err
		}
		if argc == 0 {
			if slot, _, ok := instance.Class.LookupMethodSlot(name); ok {
				if result, ok, err := vm.tryFastMethodCall(instance, slot); err != nil {
					return err
				} else if ok {
					vm.stack[vm.sp-1] = result
					return nil
				}
			}
		}
		return vm.callMethod(instance, method, owner, argc)
	}
	if wrapper, ok := receiver.AsSAMWrapper(); ok {
		if name != wrapper.MethodName {
			return fmt.Errorf("interface %s has no method %s", wrapper.InterfaceName, name)
		}
		vm.stack[vm.sp-1-argc] = wrapper.Callable
		return vm.call(argc)
	}
	if class, ok := receiver.AsClass(); ok {
		args := vm.peekArgs(argc)
		fn, owner, exists := vm.resolveStaticOverload(class, name, args)
		if exists {
			if err := vm.ensureAccess(owner, owner.StaticVisibility[name]); err != nil {
				return err
			}
			vm.stack[vm.sp-1-argc] = value.ObjectValue(fn)
			return vm.call(argc)
		}
		owner, member, exists := class.LookupStaticOwner(name)
		if !exists {
			return fmt.Errorf("class %s has no static member %s", class.Name, name)
		}
		if err := vm.ensureAccess(owner, owner.StaticVisibility[name]); err != nil {
			return err
		}
		vm.stack[vm.sp-1-argc] = member
		return vm.call(argc)
	}
	return fmt.Errorf("cannot invoke %s on %s", name, receiver.String())
}

func (vm *VM) invokeMethodSlot(slot int, argc int) error {
	receiver := vm.peek(argc)
	instance, ok := receiver.AsInstance()
	if !ok {
		return fmt.Errorf("cannot invoke method slot %d on %s", slot, receiver.String())
	}
	fn, ok := instance.Class.LookupMethodBySlot(slot)
	if !ok {
		return fmt.Errorf("instance %s has no method slot %d", instance.Class.Name, slot)
	}
	if argc == 0 {
		if result, ok, err := vm.tryFastMethodCall(instance, slot); err != nil {
			return err
		} else if ok {
			vm.stack[vm.sp-1] = result
			return nil
		}
	}
	return vm.callMethod(instance, fn, instance.Class, argc)
}

func (vm *VM) tryFastMethodCall(receiver *value.Instance, slot int) (value.Value, bool, error) {
	plan, ok := receiver.Class.LookupFastMethodBySlot(slot)
	if !ok || plan == nil || plan.Arity != 0 {
		return value.NilValue(), false, nil
	}
	if plan.Ops != nil {
		return plan.EvalOps(receiver), true, nil
	}
	result, err := vm.evalFastMethodExpr(receiver, plan.Expr)
	if err != nil {
		return value.NilValue(), false, err
	}
	return result, true, nil
}

func (vm *VM) evalFastMethodExpr(receiver *value.Instance, expr *value.FastMethodExpr) (value.Value, error) {
	if expr == nil {
		return value.NilValue(), fmt.Errorf("invalid fast method expression")
	}
	switch expr.Kind {
	case value.FastMethodExprNumber:
		if i := int64(expr.Number); float64(i) == expr.Number {
			return value.IntValue(i), nil
		}
		return value.FloatValue(expr.Number), nil
	case value.FastMethodExprField:
		if expr.FieldSlot < 0 || expr.FieldSlot >= len(receiver.Fields) {
			return value.NilValue(), fmt.Errorf("invalid fast method field slot %d for %s", expr.FieldSlot, receiver.Class.Name)
		}
		return receiver.Fields[expr.FieldSlot], nil
	case value.FastMethodExprNegate:
		left, err := vm.evalFastMethodExpr(receiver, expr.Left)
		if err != nil {
			return value.NilValue(), err
		}
		if left.Kind != value.Number {
			return value.NilValue(), fmt.Errorf("fast method negate expects number")
		}
		return value.NumberValue(-left.Num), nil
	case value.FastMethodExprAdd, value.FastMethodExprSub, value.FastMethodExprMul, value.FastMethodExprDiv:
		left, err := vm.evalFastMethodExpr(receiver, expr.Left)
		if err != nil {
			return value.NilValue(), err
		}
		right, err := vm.evalFastMethodExpr(receiver, expr.Right)
		if err != nil {
			return value.NilValue(), err
		}
		if left.Kind != value.Number || right.Kind != value.Number {
			return value.NilValue(), fmt.Errorf("fast method arithmetic expects numbers")
		}
		// Use integer arithmetic when both operands are integers
		if left.NumberKind == value.NumberInt && right.NumberKind == value.NumberInt {
			switch expr.Kind {
			case value.FastMethodExprAdd:
				return value.IntValue(left.Int + right.Int), nil
			case value.FastMethodExprSub:
				return value.IntValue(left.Int - right.Int), nil
			case value.FastMethodExprMul:
				return value.IntValue(left.Int * right.Int), nil
			default:
				if right.Int == 0 {
					return value.NilValue(), fmt.Errorf("fast method division by zero")
				}
				return value.IntValue(left.Int / right.Int), nil
			}
		}
		switch expr.Kind {
		case value.FastMethodExprAdd:
			return value.NumberValue(left.Num + right.Num), nil
		case value.FastMethodExprSub:
			return value.NumberValue(left.Num - right.Num), nil
		case value.FastMethodExprMul:
			return value.NumberValue(left.Num * right.Num), nil
		default:
			return value.NumberValue(left.Num / right.Num), nil
		}
	default:
		return value.NilValue(), fmt.Errorf("unknown fast method expression kind %d", expr.Kind)
	}
}

func (vm *VM) callMethod(receiver *value.Instance, fn *bytecode.Function, owner *value.Class, argc int) error {
	if fn.Arity != argc {
		return fmt.Errorf("%s expects %d args, got %d", fn.Name, fn.Arity, argc)
	}
	child := vm.acquireFrame(fn, nil, receiver, false)
	// acquireFrame always returns a fresh frame: no stringBuffers, no localRefs.
	// Write locals directly to avoid the fast-path guard in localSet.
	child.locals[0] = value.ObjectValue(receiver)
	for i := argc - 1; i >= 0; i-- {
		child.locals[i+1] = vm.pop()
	}
	vm.pop()
	child.stackBase = vm.sp
	_ = owner
	vm.frames = append(vm.frames, child)
	return nil
}

func (vm *VM) callMethodDirect(receiver *value.Instance, fn *bytecode.Function, owner *value.Class, argc int) error {
	if fn.Arity != argc {
		return fmt.Errorf("%s expects %d args, got %d", fn.Name, fn.Arity, argc)
	}
	child := vm.acquireFrame(fn, nil, receiver, false)
	// acquireFrame always returns a fresh frame: no stringBuffers, no localRefs.
	child.locals[0] = value.ObjectValue(receiver)
	for i := argc - 1; i >= 0; i-- {
		child.locals[i+1] = vm.pop()
	}
	child.stackBase = vm.sp
	_ = owner
	vm.frames = append(vm.frames, child)
	return nil
}

func (vm *VM) callSuper(argc int) error {
	currentFrame := vm.currentFrame()
	if currentFrame.receiver == nil {
		return fmt.Errorf("super called outside constructor or method")
	}
	superclass := currentFrame.receiver.Class.Superclass
	if superclass == nil {
		return fmt.Errorf("class %s has no superclass", currentFrame.receiver.Class.Name)
	}
	var ctor *bytecode.Function
	if len(superclass.ConstructorOverloads) <= 1 {
		ctor = superclass.Constructor
	} else {
		args := vm.peekArgs(argc)
		ctor = vm.selectBestOverload(superclass.ConstructorOverloads, args)
		if ctor == nil && superclass.Constructor != nil && vm.overloadMatches(superclass.Constructor, args) {
			ctor = superclass.Constructor
		}
	}
	if ctor == nil {
		if argc != 0 {
			return fmt.Errorf("%s expects 0 args, got %d", superclass.Name, argc)
		}
		vm.push(value.ObjectValue(currentFrame.receiver))
		return nil
	}
	if ctor.Arity != argc {
		return fmt.Errorf("%s expects %d args, got %d", superclass.Name, ctor.Arity, argc)
	}
	child := vm.acquireFrame(ctor, nil, currentFrame.receiver, true)
	vm.localSet(child, 0, value.ObjectValue(currentFrame.receiver))
	for i := argc - 1; i >= 0; i-- {
		vm.localSet(child, byte(i+1), vm.pop())
	}
	child.stackBase = vm.sp
	vm.frames = append(vm.frames, child)
	return nil
}

func (vm *VM) invokeSuper(name string, argc int) error {
	currentFrame := vm.currentFrame()
	if currentFrame.receiver == nil {
		return fmt.Errorf("super called outside constructor or method")
	}
	owner := vm.lookupClassByName(currentFrame.fn.OwnerClassName)
	if owner == nil {
		return fmt.Errorf("super method call requires class-owned frame")
	}
	superclass := owner.Superclass
	if superclass == nil {
		return fmt.Errorf("class %s has no superclass", owner.Name)
	}
	args := vm.peekArgs(argc)
	method, resolvedOwner, exists := vm.resolveSuperMethodOverload(superclass, name, args)
	if !exists {
		return fmt.Errorf("superclass %s has no method %s", superclass.Name, name)
	}
	if err := vm.ensureAccess(resolvedOwner, resolvedOwner.MethodVisibility[name]); err != nil {
		return err
	}
	return vm.callMethodDirect(currentFrame.receiver, method, resolvedOwner, argc)
}

func (vm *VM) peekArgs(argc int) []value.Value {
	args := make([]value.Value, argc)
	for i := 0; i < argc; i++ {
		args[i] = vm.peek(argc - 1 - i)
	}
	return args
}

func (vm *VM) resolveMethodOverload(class *value.Class, name string, args []value.Value) (*bytecode.Function, *value.Class, bool) {
	if class == nil {
		return nil, nil, false
	}
	if fn := vm.selectBestOverload(class.MethodOverloads[name], args); fn != nil {
		return fn, class, true
	}
	if fn, ok := class.Methods[name]; ok && vm.overloadMatches(fn, args) {
		return fn, class, true
	}
	if class.Superclass != nil {
		return vm.resolveMethodOverload(class.Superclass, name, args)
	}
	return nil, nil, false
}

func (vm *VM) resolveSuperMethodOverload(class *value.Class, name string, args []value.Value) (*bytecode.Function, *value.Class, bool) {
	if class == nil {
		return nil, nil, false
	}
	if fn := vm.selectBestDeclaredMethodOverload(class, name, args); fn != nil {
		return fn, class, true
	}
	if class.Superclass != nil {
		return vm.resolveSuperMethodOverload(class.Superclass, name, args)
	}
	return nil, nil, false
}

func (vm *VM) selectBestDeclaredMethodOverload(class *value.Class, name string, args []value.Value) *bytecode.Function {
	if class == nil {
		return nil
	}
	var best *bytecode.Function
	bestScore := -1
	for _, fn := range class.MethodOverloads[name] {
		if fn == nil || fn.OwnerClassName != class.Name || !vm.overloadMatches(fn, args) {
			continue
		}
		score := vm.overloadScore(fn, args)
		if score > bestScore {
			best = fn
			bestScore = score
		}
	}
	return best
}

func (vm *VM) resolveStaticOverload(class *value.Class, name string, args []value.Value) (*bytecode.Function, *value.Class, bool) {
	if class == nil {
		return nil, nil, false
	}
	if fn := vm.selectBestOverload(class.StaticMethodOverloads[name], args); fn != nil {
		return fn, class, true
	}
	if fn, ok := class.StaticMethods[name]; ok && vm.overloadMatches(fn, args) {
		return fn, class, true
	}
	if class.Superclass != nil {
		return vm.resolveStaticOverload(class.Superclass, name, args)
	}
	return nil, nil, false
}

func (vm *VM) selectBestOverload(overloads []*bytecode.Function, args []value.Value) *bytecode.Function {
	var best *bytecode.Function
	bestScore := -1
	for _, fn := range overloads {
		if fn == nil || !vm.overloadMatches(fn, args) {
			continue
		}
		score := vm.overloadScore(fn, args)
		if score > bestScore {
			best = fn
			bestScore = score
		}
	}
	return best
}

func (vm *VM) overloadMatches(fn *bytecode.Function, args []value.Value) bool {
	if fn == nil || fn.Arity != len(args) {
		return false
	}
	for i, arg := range args {
		expected := ""
		if i < len(fn.ParamTypes) {
			expected = fn.ParamTypes[i]
		}
		expected = normalizeRuntimeTypeAlias(rootRuntimeTypeName(expected))
		if expected == "" || expected == bvmruntime.TypeAny {
			continue
		}
		if isRuntimeGenericTypeParam(expected) {
			continue
		}
		if !vm.matchesType(arg, expected) {
			return false
		}
	}
	return true
}

func (vm *VM) overloadScore(fn *bytecode.Function, args []value.Value) int {
	score := 0
	for i, arg := range args {
		expected := ""
		if i < len(fn.ParamTypes) {
			expected = fn.ParamTypes[i]
		}
		expected = normalizeRuntimeTypeAlias(rootRuntimeTypeName(expected))
		if expected == "" || expected == bvmruntime.TypeAny {
			continue
		}
		if isRuntimeGenericTypeParam(expected) {
			continue
		}
		actual := vm.runtimeTypeName(arg)
		if normalizeRuntimeTypeAlias(rootRuntimeTypeName(actual)) == expected {
			score += 3
		} else if vm.matchesType(arg, expected) {
			score += 1
		}
	}
	return score
}

func isRuntimeGenericTypeParam(typeName string) bool {
	trimmed := trimTypeSpace(typeName)
	if len(trimmed) == 0 || len(trimmed) > 2 {
		return false
	}
	for index, ch := range trimmed {
		if index == 0 {
			if ch < 'A' || ch > 'Z' {
				return false
			}
			continue
		}
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func (vm *VM) buildRange(argc int) (*value.Range, error) {
	if argc == 1 {
		end := vm.pop()
		if end.Kind != value.Number {
			return nil, fmt.Errorf("range expects numeric arguments")
		}
		return &value.Range{Start: 0, End: int(end.Num), Step: 1}, nil
	}
	if argc == 2 {
		end := vm.pop()
		start := vm.pop()
		if start.Kind != value.Number || end.Kind != value.Number {
			return nil, fmt.Errorf("range expects numeric arguments")
		}
		step := 1
		if start.Num > end.Num {
			step = -1
		}
		return &value.Range{Start: int(start.Num), End: int(end.Num), Step: step}, nil
	}
	return nil, fmt.Errorf("range expects 1 or 2 arguments")
}

func (vm *VM) currentFrame() *frame {
	return vm.frames[len(vm.frames)-1]
}

func (vm *VM) ensureAccess(owner *value.Class, visibility string) error {
	if owner == nil || visibility == "" || visibility == "public" {
		return nil
	}
	frame := vm.currentFrame()
	currentOwnerName := frame.fn.OwnerClassName
	if currentOwnerName == "" {
		return fmt.Errorf("cannot access %s member of %s", visibility, owner.Name)
	}
	if currentOwnerName == owner.Name {
		return nil
	}
	if visibility == "protected" {
		currentOwner := vm.lookupClassByName(currentOwnerName)
		for current := currentOwner; current != nil; current = current.Superclass {
			if current == owner {
				return nil
			}
		}
	}
	return fmt.Errorf("cannot access %s member of %s", visibility, owner.Name)
}

func (vm *VM) lookupClassByName(name string) *value.Class {
	if name == "" {
		return nil
	}
	if candidate, ok := vm.globals[name]; ok {
		if classValue, ok := candidate.AsClass(); ok {
			return classValue
		}
	}
	for i, defined := range vm.globalDefined {
		if !defined {
			continue
		}
		if classValue, ok := vm.globalSlots[i].AsClass(); ok && classValue.Name == name {
			return classValue
		}
	}
	return nil
}

func (vm *VM) ResolveGlobal(fn *bytecode.Function, name string) (value.Value, bool) {
	if candidate, ok := vm.globals[name]; ok {
		return candidate, true
	}
	for i, globalName := range fn.GlobalSlotNames {
		if globalName != name || i >= len(vm.globalDefined) || !vm.globalDefined[i] {
			continue
		}
		return vm.globalSlots[i], true
	}
	return value.NilValue(), false
}

func (vm *VM) acquireFrame(fn *bytecode.Function, closure *value.Closure, receiver *value.Instance, init bool) *frame {
	vm.ensureStackHeadroom()
	var child *frame
	if len(vm.framePool) > 0 {
		last := len(vm.framePool) - 1
		child = vm.framePool[last]
		vm.framePool = vm.framePool[:last]
	} else {
		child = &frame{}
	}
	child.fn = fn
	child.closure = closure
	child.ip = 0
	child.stackBase = vm.sp
	child.receiver = receiver
	child.init = init
	child.hasCells = false // maps are lazily initialized; skip clear unless used
	child.consts = nil
	child.handlers = child.handlers[:0]
	// Invariant: a pooled frame's locals backing array is all-zero up to its
	// capacity (fresh arrays start zeroed; releaseFrame re-zeroes the used
	// prefix before pooling). Re-slicing therefore exposes only zeroed slots
	// and no clear is needed here — locals are cleared exactly once per call,
	// in releaseFrame.
	if cap(child.locals) < fn.MaxLocals {
		child.locals = make([]value.Value, fn.MaxLocals)
	} else {
		child.locals = child.locals[:fn.MaxLocals]
	}
	return child
}

func (vm *VM) releaseFrame(child *frame) {
	// Re-zero the used prefix to release object references for GC and to
	// uphold the acquireFrame invariant that pooled locals are all-zero.
	clear(child.locals)
	child.fn = nil
	child.closure = nil
	child.ip = 0
	child.receiver = nil
	child.init = false
	child.hasCells = false
	if child.localRefs != nil {
		clear(child.localRefs)
	}
	if child.stringBuffers != nil {
		clear(child.stringBuffers)
	}
	child.handlers = child.handlers[:0]
	vm.framePool = append(vm.framePool, child)
}

// acquireInstance returns a recycled instance from the pool if available, or allocates a new one.
func (vm *VM) acquireInstance(cls *value.Class) *value.Instance {
	if vm.instancePools != nil {
		if pool := vm.instancePools[cls]; len(pool) > 0 {
			n := len(pool) - 1
			inst := pool[n]
			vm.instancePools[cls] = pool[:n]
			inst.Frozen = false
			return inst
		}
	}
	return cls.NewInstance()
}

// tryRecycleLocal checks whether the old value at a local slot can be safely pooled.
// Safe when the instance has no alias in another local slot or on the active stack.
func (vm *VM) tryRecycleLocal(frame *frame, slot byte, old value.Value) {
	if old.Kind != value.Object {
		return
	}
	inst, ok := old.Object.(*value.Instance)
	if !ok {
		return
	}
	// Check for aliases in other locals
	for i := range frame.locals {
		if byte(i) == slot {
			continue
		}
		v := frame.locals[i]
		if v.Kind == value.Object && v.Object == old.Object {
			return
		}
	}
	// Check for aliases on the active expression stack
	for i := frame.stackBase; i < vm.sp; i++ {
		v := vm.stack[i]
		if v.Kind == value.Object && v.Object == old.Object {
			return
		}
	}
	// No aliases found – recycle the instance
	if vm.instancePools == nil {
		vm.instancePools = make(map[*value.Class][]*value.Instance, 4)
	}
	cls := inst.Class
	pool := vm.instancePools[cls]
	const maxPoolSize = 32
	if len(pool) < maxPoolSize {
		vm.instancePools[cls] = append(pool, inst)
	}
}

func (vm *VM) borrowBuiltinArgs(argc int) []value.Value {
	if cap(vm.builtinArgs) < argc {
		vm.builtinArgs = make([]value.Value, argc)
	} else {
		vm.builtinArgs = vm.builtinArgs[:argc]
	}
	return vm.builtinArgs
}

// push has deliberately NO bounds check: it is the single hottest function in
// the VM (executed once or more per opcode) and even a predicted branch here
// costs several percent across all benchmarks. Overflow safety is instead
// guaranteed by ensureStackHeadroom, called once per frame creation in
// acquireFrame: a frame's expression-stack usage is bounded by its bytecode,
// so reserving frameStackHeadroom slots per live frame keeps push in bounds.
func (vm *VM) push(v value.Value) {
	vm.stack[vm.sp] = v
	vm.sp++
}

// frameStackHeadroom is the number of free value-stack slots guaranteed to a
// frame when it is created. A frame's peak expression stack depth is bounded
// by its bytecode (expression nesting + at most 255 pending args/elements per
// call or literal), which in practice is a few dozen; 1024 is very generous.
const frameStackHeadroom = 1024

// ensureStackHeadroom grows the value stack until at least
// frameStackHeadroom slots are free above sp.
func (vm *VM) ensureStackHeadroom() {
	if vm.sp+frameStackHeadroom <= vm.stackCap {
		return
	}
	newCap := vm.stackCap * 2
	for vm.sp+frameStackHeadroom > newCap {
		newCap *= 2
	}
	grown := make([]value.Value, newCap)
	copy(grown, vm.stack)
	vm.stack = grown
	vm.stackCap = newCap
}

func (vm *VM) pop() value.Value {
	vm.sp--
	return vm.stack[vm.sp]
}

func (vm *VM) peek(distance int) value.Value {
	return vm.stack[vm.sp-1-distance]
}

func (vm *VM) readByte(frame *frame) byte {
	b := frame.fn.Chunk.Code[frame.ip]
	frame.ip++
	return b
}

// readUint16 must stay below inline cost 20: executeUntilDepth exceeds the
// compiler's big-function node limit, and calls inside big functions are only
// inlined when the callee cost is ≤ inlineBigFunctionMaxCost (20). Keep this
// body minimal — check with `go build -gcflags='-m -m'` after editing.
func (vm *VM) readUint16(frame *frame) uint16 {
	c := frame.fn.Chunk.Code
	i := frame.ip
	frame.ip = i + 2
	return uint16(c[i])<<8 | uint16(c[i+1])
}

// adjustIntLocal adds delta to an integer local, updating both Int and Num
// fields atomically. It respects hasCells so closure-captured locals are
// kept in sync. Used by OpIncLocal (+1) and OpDecLocal (-1).
func (vm *VM) adjustIntLocal(frame *frame, slot byte, delta int64) {
	if frame.hasCells {
		v := vm.localGetSlow(frame, slot)
		vm.localSetSlow(frame, slot, value.IntValue(v.Int+delta))
	} else {
		frame.locals[slot].Int += delta
		frame.locals[slot].Num += float64(delta)
	}
}

// applyToLocalSlow handles OpAddToLocal/SubToLocal/MulToLocal when the local
// is captured by a closure (frame.hasCells == true). It reads and writes
// through localGetSlow/localSetSlow so the closure cell stays in sync.
// The fast (non-closure) path is inlined directly in the dispatch switch.
func (vm *VM) applyToLocalSlow(frame *frame, slot byte, rhs value.Value, op bytecode.Op) error {
	lhs := vm.localGetSlow(frame, slot)
	if lhs.Kind == value.Number && rhs.Kind == value.Number {
		var result value.Value
		if lhs.NumberKind == value.NumberInt && rhs.NumberKind == value.NumberInt {
			switch op {
			case bytecode.OpAddNum:
				result = value.IntValue(lhs.Int + rhs.Int)
			case bytecode.OpSubNum:
				result = value.IntValue(lhs.Int - rhs.Int)
			default:
				result = value.IntValue(lhs.Int * rhs.Int)
			}
		} else {
			switch op {
			case bytecode.OpAddNum:
				result = value.FloatValue(lhs.Num + rhs.Num)
			case bytecode.OpSubNum:
				result = value.FloatValue(lhs.Num - rhs.Num)
			default:
				result = value.FloatValue(lhs.Num * rhs.Num)
			}
		}
		vm.localSetSlow(frame, slot, result)
		return nil
	}
	vm.push(lhs)
	vm.push(rhs)
	if err := vm.binaryNumberOp(op, func(a, b float64) float64 {
		switch op {
		case bytecode.OpAddNum:
			return a + b
		case bytecode.OpSubNum:
			return a - b
		default:
			return a * b
		}
	}); err != nil {
		return err
	}
	vm.localSetSlow(frame, slot, vm.pop())
	return nil
}

// readArrayFieldCmpArgs decodes the 4-byte argument block shared by
// OpJumpIfArrayFieldGteLocalTrue and OpJumpIfArrayFieldLteLocalTrue:
//
//	arr_slot(1) idx_slot(1) field_slot(1) cmp_slot(1) offset(2)
//
// It returns the numeric value of arr[idx].field, the cmpSlot byte, the
// jump offset, and a non-nil error if any runtime type check fails.
func (vm *VM) readArrayFieldCmpArgs(frame *frame) (fieldNum float64, cmpSlot byte, offset uint16, err error) {
	arrSlot := vm.readByte(frame)
	idxSlot := vm.readByte(frame)
	fieldSlot := int(vm.readByte(frame))
	cmpSlot = vm.readByte(frame)
	offset = vm.readUint16(frame)
	arr, ok := frame.locals[arrSlot].AsArray()
	if !ok {
		return 0, 0, 0, fmt.Errorf("JUMP_IF_ARRAY_FIELD: slot %d is not an array", arrSlot)
	}
	idx := int(frame.locals[idxSlot].Int)
	if idx < 0 || idx >= len(arr.Elements) {
		return 0, 0, 0, fmt.Errorf("array index %d out of range [0, %d)", idx, len(arr.Elements))
	}
	instance, ok := arr.Elements[idx].AsInstance()
	if !ok {
		return 0, 0, 0, fmt.Errorf("JUMP_IF_ARRAY_FIELD: element is not an instance")
	}
	if fieldSlot < 0 || fieldSlot >= len(instance.Fields) {
		return 0, 0, 0, fmt.Errorf("JUMP_IF_ARRAY_FIELD: field slot %d out of range", fieldSlot)
	}
	return instance.Fields[fieldSlot].Num, cmpSlot, offset, nil
}

// resolvedConsts returns the chunk's constant pool converted to value.Value,
// building and memoizing it on first request. Conversion through
// constantToValue is deterministic and constants are immutable after
// compilation, so the converted slice is shared by every frame running the
// same chunk for the lifetime of this VM.
func (vm *VM) resolvedConsts(chunk *bytecode.Chunk) []value.Value {
	if cached, ok := vm.constCache[chunk]; ok {
		return cached
	}
	if vm.constCache == nil {
		vm.constCache = make(map[*bytecode.Chunk][]value.Value)
	}
	resolved := make([]value.Value, len(chunk.Constants))
	for i, item := range chunk.Constants {
		resolved[i] = vm.constantToValue(item)
	}
	vm.constCache[chunk] = resolved
	return resolved
}

func (vm *VM) constantToValue(item any) value.Value {
	switch v := item.(type) {
	case nil:
		return value.NilValue()
	case int64:
		return value.IntValue(v)
	case float64:
		return value.FloatValue(v)
	case bool:
		return value.BoolValue(v)
	case rune:
		return value.CharValue(v)
	case string:
		return value.StringValue(v)
	case *bytecode.Function:
		return value.ObjectValue(v)
	default:
		return value.ObjectValue(v)
	}
}

func (vm *VM) localGet(frame *frame, slot byte) value.Value {
	if !frame.hasCells {
		return frame.locals[slot]
	}
	return vm.localGetSlow(frame, slot)
}

func (vm *VM) localGetSlow(frame *frame, slot byte) value.Value {
	if frame.stringBuffers != nil && len(frame.stringBuffers) > 0 {
		vm.materializeLocalString(frame, slot)
	}
	if frame.localRefs != nil {
		if cell, ok := frame.localRefs[slot]; ok {
			return cell.Value
		}
	}
	return frame.locals[slot]
}

func (vm *VM) localSet(frame *frame, slot byte, v value.Value) {
	if !frame.hasCells {
		frame.locals[slot] = v
		return
	}
	vm.localSetSlow(frame, slot, v)
}

func (vm *VM) localSetSlow(frame *frame, slot byte, v value.Value) {
	if frame.stringBuffers != nil && len(frame.stringBuffers) > 0 {
		delete(frame.stringBuffers, slot)
	}
	if frame.localRefs != nil {
		if cell, ok := frame.localRefs[slot]; ok {
			cell.Value = v
			return
		}
	}
	frame.locals[slot] = v
}

func operatorMethodNames(operator bytecode.Op) (string, string) {
	switch operator {
	case bytecode.OpAdd:
		return "__add", "__radd"
	case bytecode.OpSub:
		return "__sub", "__rsub"
	case bytecode.OpMul:
		return "__mul", "__rmul"
	case bytecode.OpDiv:
		return "__div", "__rdiv"
	case bytecode.OpMod:
		return "__mod", "__rmod"
	case bytecode.OpPow:
		return "__pow", "__rpow"
	case bytecode.OpGreater:
		return "__gt", "__rgt"
	case bytecode.OpLess:
		return "__lt", "__rlt"
	default:
		return "", ""
	}
}

func (vm *VM) invokeNamedInstanceMethod(receiver *value.Instance, methodName string, args ...value.Value) (value.Value, bool, error) {
	if receiver == nil || receiver.Class == nil || methodName == "" {
		return value.NilValue(), false, nil
	}
	if _, fn, ok := receiver.Class.LookupMethodSlot(methodName); ok && fn.Arity == len(args) && vm.overloadMatches(fn, args) {
		result, err := vm.invokeInstanceMethod(receiver, fn, args...)
		if err != nil {
			return value.NilValue(), true, err
		}
		return result, true, nil
	}
	fn, _, ok := vm.resolveMethodOverload(receiver.Class, methodName, args)
	if !ok {
		return value.NilValue(), false, nil
	}
	result, err := vm.invokeInstanceMethod(receiver, fn, args...)
	if err != nil {
		return value.NilValue(), true, err
	}
	return result, true, nil
}

func (vm *VM) trySpecialMethod(receiver *value.Instance, slot value.SpecialMethodSlot, fallbackName string, args ...value.Value) (value.Value, bool, error) {
	if receiver == nil || receiver.Class == nil {
		return value.NilValue(), false, nil
	}
	if fn := receiver.Class.SpecialMethod(slot); fn != nil && fn.Arity == len(args) && vm.overloadMatches(fn, args) {
		result, err := vm.invokeInstanceMethod(receiver, fn, args...)
		if err != nil {
			return value.NilValue(), true, err
		}
		return result, true, nil
	}
	if fallbackName != "" {
		return vm.invokeNamedInstanceMethod(receiver, fallbackName, args...)
	}
	return value.NilValue(), false, nil
}

func (vm *VM) tryUnaryOperator(operand value.Value, methodName string) (value.Value, bool, error) {
	instance, ok := operand.AsInstance()
	if !ok {
		return value.NilValue(), false, nil
	}
	return vm.invokeNamedInstanceMethod(instance, methodName)
}

func (vm *VM) tryBinaryOperator(left value.Value, right value.Value, operator bytecode.Op) (value.Value, bool, error) {
	leftMethod, rightMethod := operatorMethodNames(operator)
	if instance, ok := left.AsInstance(); ok {
		if result, applied, err := vm.invokeNamedInstanceMethod(instance, leftMethod, right); applied || err != nil {
			return result, applied, err
		}
	}
	if instance, ok := right.AsInstance(); ok {
		if result, applied, err := vm.invokeNamedInstanceMethod(instance, rightMethod, left); applied || err != nil {
			return result, applied, err
		}
	}
	return value.NilValue(), false, nil
}

func (vm *VM) tryEqualsMethod(instance *value.Instance, other value.Value) (bool, bool, error) {
	result, applied, err := vm.trySpecialMethod(instance, value.SpecialMethodEquals, "__eq", other)
	if err != nil || !applied {
		return false, applied, err
	}
	booleanValue, ok := vm.booleanOperand(result)
	if !ok {
		return false, true, fmt.Errorf("__eq must return Bool")
	}
	return booleanValue, true, nil
}

func (vm *VM) tryHashMethod(instance *value.Instance) (value.Value, bool, error) {
	return vm.trySpecialMethod(instance, value.SpecialMethodHash, "__hash")
}

func (vm *VM) captureLocal(frame *frame, slot byte) *value.Cell {
	vm.materializeLocalString(frame, slot)
	if frame.localRefs == nil {
		frame.localRefs = make(map[byte]*value.Cell)
	}
	if cell, ok := frame.localRefs[slot]; ok {
		return cell
	}
	cell := &value.Cell{Value: frame.locals[slot]}
	frame.localRefs[slot] = cell
	frame.hasCells = true
	return cell
}

func (vm *VM) appendLocalString(frame *frame, slot byte, suffix value.Value) {
	if frame.stringBuffers == nil {
		frame.stringBuffers = make(map[byte]*stringAccumulator)
	}
	frame.hasCells = true
	accumulator, ok := frame.stringBuffers[slot]
	if !ok {
		accumulator = &stringAccumulator{}
		base := frame.locals[slot]
		if frame.localRefs != nil {
			if cell, ok := frame.localRefs[slot]; ok {
				base = cell.Value
			}
		}
		if base.Kind == value.String || base.Kind == value.Char {
			accumulator.builder.WriteString(base.Str)
		} else if base.Kind != value.Nil {
			accumulator.builder.WriteString(base.String())
		}
		frame.stringBuffers[slot] = accumulator
	}
	accumulator.builder.WriteString(suffix.String())
}

func (vm *VM) materializeLocalString(frame *frame, slot byte) {
	if frame.stringBuffers == nil {
		return
	}
	accumulator, ok := frame.stringBuffers[slot]
	if !ok {
		return
	}
	materialized := value.StringValue(string(append([]byte(nil), accumulator.builder.String()...)))
	if frame.localRefs != nil {
		if cell, ok := frame.localRefs[slot]; ok {
			cell.Value = materialized
			return
		}
	}
	frame.locals[slot] = materialized
}

func (vm *VM) binaryNumberOp(operator bytecode.Op, op func(float64, float64) float64) error {
	right := vm.pop()
	left := vm.pop()
	if left.Kind == value.Number && right.Kind == value.Number && left.NumberKind == value.NumberInt && right.NumberKind == value.NumberInt {
		leftInt := left.Int
		rightInt := right.Int
		switch operator {
		case bytecode.OpAdd, bytecode.OpAddNum:
			vm.push(value.IntValue(leftInt + rightInt))
			return nil
		case bytecode.OpSub, bytecode.OpSubNum:
			vm.push(value.IntValue(leftInt - rightInt))
			return nil
		case bytecode.OpMul, bytecode.OpMulNum:
			vm.push(value.IntValue(leftInt * rightInt))
			return nil
		case bytecode.OpMod, bytecode.OpModNum:
			if rightInt == 0 {
				return diagnostic.Runtime("ValueError", "division by zero", vm.makeExceptionValue("ValueError", "division by zero"))
			}
			vm.push(value.IntValue(leftInt % rightInt))
			return nil
		}
	}
	leftNum, ok := vm.numericOperand(left)
	if !ok {
		if result, applied, err := vm.tryBinaryOperator(left, right, operator); err != nil {
			return err
		} else if applied {
			vm.push(result)
			return nil
		}
		return fmt.Errorf("numeric operation expects numbers")
	}
	rightNum, ok := vm.numericOperand(right)
	if !ok {
		if result, applied, err := vm.tryBinaryOperator(left, right, operator); err != nil {
			return err
		} else if applied {
			vm.push(result)
			return nil
		}
		return fmt.Errorf("numeric operation expects numbers")
	}
	if (operator == bytecode.OpDiv || operator == bytecode.OpDivNum || operator == bytecode.OpMod || operator == bytecode.OpModNum) && rightNum == 0 {
		return diagnostic.Runtime("ValueError", "division by zero", vm.makeExceptionValue("ValueError", "division by zero"))
	}
	vm.push(vm.numericResult(left, right, operator, op(leftNum, rightNum)))
	return nil
}

func (vm *VM) binaryPowOp(operator bytecode.Op) error {
	right := vm.pop()
	left := vm.pop()
	if left.Kind == value.Number && right.Kind == value.Number && left.NumberKind == value.NumberInt && right.NumberKind == value.NumberInt {
		base := int64(left.Num)
		exponent := int64(right.Num)
		if exponent >= 0 {
			if result, ok := powInt64SquareMultiply(base, exponent); ok {
				vm.push(value.IntValue(result))
				return nil
			}
		}
	}
	leftNum, ok := vm.numericOperand(left)
	if !ok {
		if result, applied, err := vm.tryBinaryOperator(left, right, operator); err != nil {
			return err
		} else if applied {
			vm.push(result)
			return nil
		}
		return fmt.Errorf("numeric operation expects numbers")
	}
	rightNum, ok := vm.numericOperand(right)
	if !ok {
		if result, applied, err := vm.tryBinaryOperator(left, right, operator); err != nil {
			return err
		} else if applied {
			vm.push(result)
			return nil
		}
		return fmt.Errorf("numeric operation expects numbers")
	}
	vm.push(vm.numericResult(left, right, operator, math.Pow(leftNum, rightNum)))
	return nil
}

func (vm *VM) explicitThrow(raised value.Value) error {
	typeName := vm.runtimeTypeName(raised)
	if instance, ok := raised.AsInstance(); ok {
		typeName = instance.Class.Name
	}
	return diagnostic.Runtime(typeName, vm.exceptionMessage(raised), raised)
}

func (vm *VM) handleRaised(baseDepth int, frame *frame, err error) (bool, error) {
	raised := vm.normalizeRuntimeError(frame, err)
	for depth := len(vm.frames) - 1; depth >= baseDepth; depth-- {
		candidate := vm.frames[depth]
		if len(candidate.handlers) == 0 {
			continue
		}
		handler := candidate.handlers[len(candidate.handlers)-1]
		candidate.handlers = candidate.handlers[:len(candidate.handlers)-1]
		for len(vm.frames)-1 > depth {
			child := vm.frames[len(vm.frames)-1]
			vm.frames = vm.frames[:len(vm.frames)-1]
			vm.releaseFrame(child)
		}
		if handler.stackDepth < vm.sp {
			vm.sp = handler.stackDepth
		}
		candidate.ip = handler.catchIP
		vm.push(raised.CatchValue())
		return true, nil
	}
	return false, raised
}

func (vm *VM) normalizeRuntimeError(frame *frame, err error) *diagnostic.Error {
	if raised, ok := err.(*diagnostic.Error); ok {
		if raised.Kind == "" {
			raised.Kind = diagnostic.KindRuntime
		}
		if raised.TypeName == "" {
			raised.TypeName = string(raised.Kind)
		}
		if raised.Line == 0 {
			raised.Line = vm.currentLine(frame)
		}
		if len(raised.Stack) == 0 {
			raised.Stack = vm.stackTrace()
		}
		if !raised.HasCatch {
			raised.Catch = vm.makeExceptionValue(raised.TypeName, raised.Message)
			raised.HasCatch = true
		}
		if raised.Hint == "" {
			raised.Hint = vm.runtimeHint(raised.Message)
		}
		return raised
	}
	message := err.Error()
	raised := diagnostic.Runtime(vm.inferRuntimeErrorType(message), message, value.NilValue())
	raised.Catch = vm.makeExceptionValue(raised.TypeName, raised.Message)
	raised.HasCatch = true
	raised.Line = vm.currentLine(frame)
	raised.Stack = vm.stackTrace()
	raised.Hint = vm.runtimeHint(message)
	return raised
}

func (vm *VM) currentLine(frame *frame) int {
	if frame == nil || frame.fn == nil || frame.fn.Chunk == nil || len(frame.fn.Chunk.Lines) == 0 {
		return 0
	}
	index := frame.ip - 1
	if index < 0 {
		index = 0
	}
	if index >= len(frame.fn.Chunk.Lines) {
		index = len(frame.fn.Chunk.Lines) - 1
	}
	return frame.fn.Chunk.Lines[index]
}

func (vm *VM) stackTrace() []diagnostic.StackFrame {
	stack := make([]diagnostic.StackFrame, 0, len(vm.frames))
	for i := len(vm.frames) - 1; i >= 0; i-- {
		frame := vm.frames[i]
		stack = append(stack, diagnostic.StackFrame{Function: frame.fn.Name, Line: vm.currentLine(frame)})
	}
	return stack
}

func (vm *VM) inferRuntimeErrorType(message string) string {
	switch {
	case containsString(message, "undefined variable"):
		return "NameError"
	case containsString(message, "expects"):
		return "ArityError"
	case containsString(message, "index out of range"):
		return "IndexError"
	case containsString(message, "cannot cast"):
		return "TypeError"
	case containsString(message, "file not found"):
		return "FileNotFoundException"
	case containsString(message, "no such file"):
		return "FileNotFoundException"
	case containsString(message, "permission denied"):
		return "IOException"
	default:
		return "RuntimeError"
	}
}

func (vm *VM) runtimeHint(message string) string {
	switch {
	case containsString(message, "undefined variable"):
		return "declare the variable before using it"
	case containsString(message, "expects"):
		return "check the argument count and types at the call site"
	case containsString(message, "cannot cast"):
		return "ensure the value really implements or extends the requested type"
	default:
		return ""
	}
}

func (vm *VM) makeExceptionValue(typeName string, message string) value.Value {
	global, ok := vm.globals[typeName]
	if !ok {
		return value.StringValue(message)
	}
	classValue, ok := global.AsClass()
	if !ok {
		return value.StringValue(message)
	}
	instance := classValue.NewInstance()
	if slot, _, found := instance.Class.LookupFieldSlot("message"); found && slot < len(instance.Fields) {
		instance.Fields[slot] = value.StringValue(message)
	}
	if slot, _, found := instance.Class.LookupFieldSlot("type"); found && slot < len(instance.Fields) {
		instance.Fields[slot] = value.StringValue(typeName)
	}
	return value.ObjectValue(instance)
}

func (vm *VM) exceptionMessage(candidate value.Value) string {
	if candidate.Kind == value.String || candidate.Kind == value.Char {
		return candidate.Str
	}
	if instance, ok := candidate.AsInstance(); ok {
		if field, ok := instance.GetField("message"); ok {
			return field.String()
		}
	}
	return candidate.String()
}

func (vm *VM) numericResult(left value.Value, right value.Value, operator bytecode.Op, result float64) value.Value {
	if operator == bytecode.OpDiv || operator == bytecode.OpDivNum {
		return value.FloatValue(result)
	}
	if operator == bytecode.OpPow || operator == bytecode.OpPowNum {
		if left.Kind == value.Number && right.Kind == value.Number && left.NumberKind == value.NumberInt && right.NumberKind == value.NumberInt && right.Num >= 0 {
			return value.IntValue(int64(result))
		}
		return value.FloatValue(result)
	}
	if left.Kind == value.Number && right.Kind == value.Number && left.NumberKind == value.NumberInt && right.NumberKind == value.NumberInt {
		return value.IntValue(int64(result))
	}
	return value.FloatValue(result)
}

func (vm *VM) binaryCompare(operator bytecode.Op, numberOp func(float64, float64) bool, textOp func(string, string) bool) error {
	right := vm.pop()
	left := vm.pop()
	if leftText, ok := textualOperand(left); ok {
		if rightText, ok := textualOperand(right); ok {
			vm.push(value.BoolValue(textOp(leftText, rightText)))
			return nil
		}
	}
	if left.Kind == value.Number && right.Kind == value.Number && left.NumberKind == value.NumberInt && right.NumberKind == value.NumberInt {
		vm.push(value.BoolValue(numberOp(float64(int64(left.Num)), float64(int64(right.Num)))))
		return nil
	}
	leftNum, ok := vm.numericOperand(left)
	if !ok {
		if result, applied, err := vm.tryBinaryOperator(left, right, operator); err != nil {
			return err
		} else if applied {
			booleanValue, ok := vm.booleanOperand(result)
			if !ok {
				return fmt.Errorf("comparison operator %s must return Bool", operator.String())
			}
			vm.push(value.BoolValue(booleanValue))
			return nil
		}
		return fmt.Errorf("comparison expects numbers or text")
	}
	rightNum, ok := vm.numericOperand(right)
	if !ok {
		if result, applied, err := vm.tryBinaryOperator(left, right, operator); err != nil {
			return err
		} else if applied {
			booleanValue, ok := vm.booleanOperand(result)
			if !ok {
				return fmt.Errorf("comparison operator %s must return Bool", operator.String())
			}
			vm.push(value.BoolValue(booleanValue))
			return nil
		}
		return fmt.Errorf("comparison expects numbers or text")
	}
	vm.push(value.BoolValue(numberOp(leftNum, rightNum)))
	return nil
}

func (vm *VM) binaryNumericCompareOp(operator bytecode.Op) error {
	right := vm.pop()
	left := vm.pop()
	if left.Kind == value.Number && right.Kind == value.Number && left.NumberKind == value.NumberInt && right.NumberKind == value.NumberInt {
		leftInt := left.Int
		rightInt := right.Int
		switch operator {
		case bytecode.OpLessNum:
			vm.push(value.BoolValue(leftInt < rightInt))
			return nil
		case bytecode.OpGreaterNum:
			vm.push(value.BoolValue(leftInt > rightInt))
			return nil
		}
	}
	leftNum, ok := vm.numericOperand(left)
	if !ok {
		return fmt.Errorf("comparison expects numbers")
	}
	rightNum, ok := vm.numericOperand(right)
	if !ok {
		return fmt.Errorf("comparison expects numbers")
	}
	switch operator {
	case bytecode.OpLessNum:
		vm.push(value.BoolValue(leftNum < rightNum))
	case bytecode.OpGreaterNum:
		vm.push(value.BoolValue(leftNum > rightNum))
	default:
		return fmt.Errorf("unsupported numeric compare opcode %d", operator)
	}
	return nil
}

func powInt64SquareMultiply(base int64, exponent int64) (int64, bool) {
	result := int64(1)
	current := base
	remaining := exponent
	for remaining > 0 {
		if remaining&1 == 1 {
			next, ok := multiplyInt64Checked(result, current)
			if !ok {
				return 0, false
			}
			result = next
		}
		remaining >>= 1
		if remaining == 0 {
			break
		}
		nextBase, ok := multiplyInt64Checked(current, current)
		if !ok {
			return 0, false
		}
		current = nextBase
	}
	return result, true
}

func multiplyInt64Checked(left int64, right int64) (int64, bool) {
	if left == 0 || right == 0 {
		return 0, true
	}
	product := left * right
	if product/right != left {
		return 0, false
	}
	return product, true
}

func (vm *VM) numericOperand(candidate value.Value) (float64, bool) {
	if candidate.Kind == value.Number {
		return candidate.Num, true
	}
	instance, ok := candidate.AsInstance()
	if !ok || instance.Class == nil {
		return 0, false
	}
	switch rootRuntimeTypeName(instance.Class.Name) {
	case "Integer", "Float", "Double":
		if slot, _, ok := instance.Class.LookupFieldSlot("value"); ok && slot >= 0 && slot < len(instance.Fields) {
			inner := instance.Fields[slot]
			if inner.Kind == value.Number {
				return inner.Num, true
			}
		}
	}
	return 0, false
}

func (vm *VM) booleanOperand(candidate value.Value) (bool, bool) {
	if candidate.Kind == value.Bool {
		return candidate.Bool, true
	}
	instance, ok := candidate.AsInstance()
	if !ok || instance.Class == nil {
		return false, false
	}
	if rootRuntimeTypeName(instance.Class.Name) != "Boolean" {
		return false, false
	}
	if slot, _, ok := instance.Class.LookupFieldSlot("value"); ok && slot >= 0 && slot < len(instance.Fields) {
		inner := instance.Fields[slot]
		if inner.Kind == value.Bool {
			return inner.Bool, true
		}
	}
	return false, false
}

func (vm *VM) valuesEqual(left value.Value, right value.Value) (bool, error) {
	if leftText, ok := textualOperand(left); ok {
		if rightText, ok := textualOperand(right); ok {
			return leftText == rightText, nil
		}
	}
	if leftNum, ok := vm.numericOperand(left); ok {
		if rightNum, ok := vm.numericOperand(right); ok {
			return leftNum == rightNum, nil
		}
	}
	if leftBool, ok := vm.booleanOperand(left); ok {
		if rightBool, ok := vm.booleanOperand(right); ok {
			return leftBool == rightBool, nil
		}
	}
	if instance, ok := left.AsInstance(); ok {
		if equal, applied, err := vm.tryEqualsMethod(instance, right); err != nil {
			return false, err
		} else if applied {
			return equal, nil
		}
	}
	if instance, ok := right.AsInstance(); ok {
		if equal, applied, err := vm.tryEqualsMethod(instance, left); err != nil {
			return false, err
		} else if applied {
			return equal, nil
		}
	}
	return value.Equal(left, right), nil
}

func (vm *VM) wrapPrimitiveValue(targetType string, candidate value.Value) (value.Value, bool, error) {
	classValue, ok := vm.globals[targetType]
	if !ok {
		return value.NilValue(), false, nil
	}
	class, ok := classValue.AsClass()
	if !ok {
		return value.NilValue(), false, nil
	}
	instance := class.NewInstance()
	slot, _, found := instance.Class.LookupFieldSlot("value")
	if !found || slot < 0 || slot >= len(instance.Fields) {
		return value.NilValue(), true, fmt.Errorf("cannot cast %s to %s", vm.runtimeTypeName(candidate), targetType)
	}
	switch targetType {
	case "Integer":
		numericValue, ok := vm.numericOperand(candidate)
		if !ok {
			return value.NilValue(), true, fmt.Errorf("cannot cast %s to %s", vm.runtimeTypeName(candidate), targetType)
		}
		instance.Fields[slot] = value.IntValue(int64(numericValue))
	case "Float", "Double":
		numericValue, ok := vm.numericOperand(candidate)
		if !ok {
			return value.NilValue(), true, fmt.Errorf("cannot cast %s to %s", vm.runtimeTypeName(candidate), targetType)
		}
		instance.Fields[slot] = value.FloatValue(numericValue)
	case "Boolean":
		booleanValue, ok := vm.booleanOperand(candidate)
		if !ok {
			return value.NilValue(), true, fmt.Errorf("cannot cast %s to %s", vm.runtimeTypeName(candidate), targetType)
		}
		instance.Fields[slot] = value.BoolValue(booleanValue)
	case "Char":
		if candidate.Kind != value.Char {
			return value.NilValue(), true, fmt.Errorf("cannot cast %s to %s", vm.runtimeTypeName(candidate), targetType)
		}
		instance.Fields[slot] = candidate
	default:
		return value.NilValue(), false, nil
	}
	return value.ObjectValue(instance), true, nil
}

func (vm *VM) functionalInterfaceSpec(typeName string) (bytecode.InterfaceSpec, bool) {
	resolvedName := rootRuntimeTypeName(typeName)
	for index := len(vm.frames) - 1; index >= 0; index-- {
		frame := vm.frames[index]
		if frame == nil || frame.fn == nil || len(frame.fn.Interfaces) == 0 {
			continue
		}
		if spec, ok := frame.fn.Interfaces[resolvedName]; ok && spec.FunctionalMethod != "" {
			return spec, true
		}
	}
	return bytecode.InterfaceSpec{}, false
}

func (vm *VM) isCallableValue(candidate value.Value) bool {
	if _, ok := candidate.AsFunction(); ok {
		return true
	}
	if _, ok := candidate.AsClosure(); ok {
		return true
	}
	if _, ok := candidate.AsBuiltin(); ok {
		return true
	}
	if _, ok := candidate.AsBoundMethod(); ok {
		return true
	}
	if _, ok := candidate.AsBoundStaticMethod(); ok {
		return true
	}
	return false
}

func (vm *VM) matchesStructuralInterface(candidate value.Value, typeName string) bool {
	switch rootRuntimeTypeName(typeName) {
	case "Iterable":
		if _, ok := candidate.AsRange(); ok {
			return true
		}
		if instance, ok := candidate.AsInstance(); ok && instance.Class != nil {
			return instance.Class.SpecialMethod(value.SpecialMethodIterableLength) != nil && instance.Class.SpecialMethod(value.SpecialMethodIterableGet) != nil
		}
	case "Unstructured":
		if _, ok := candidate.AsTuple(); ok {
			return true
		}
		if instance, ok := candidate.AsInstance(); ok && instance.Class != nil {
			return instance.Class.SpecialMethod(value.SpecialMethodPieces) != nil && instance.Class.SpecialMethod(value.SpecialMethodGetPiece) != nil
		}
	case "Indexable":
		if candidate.Kind == value.String {
			return true
		}
		if _, ok := candidate.AsArray(); ok {
			return true
		}
		if _, ok := candidate.AsTuple(); ok {
			return true
		}
		if _, ok := candidate.AsMap(); ok {
			return true
		}
		if instance, ok := candidate.AsInstance(); ok && instance.Class != nil {
			return instance.Class.SpecialMethod(value.SpecialMethodIndexGet) != nil
		}
	case "Sliceable":
		if candidate.Kind == value.String {
			return true
		}
		if _, ok := candidate.AsArray(); ok {
			return true
		}
		if _, ok := candidate.AsTuple(); ok {
			return true
		}
		if instance, ok := candidate.AsInstance(); ok && instance.Class != nil {
			return instance.Class.SpecialMethod(value.SpecialMethodSlice) != nil
		}
	}
	return false
}

func (vm *VM) wrapCallableAsInterface(candidate value.Value, typeName string) (value.Value, bool) {
	resolvedName := rootRuntimeTypeName(typeName)
	if wrapper, ok := candidate.AsSAMWrapper(); ok {
		if wrapper.InterfaceName == resolvedName {
			return candidate, true
		}
		return value.NilValue(), false
	}
	if !vm.isCallableValue(candidate) {
		return value.NilValue(), false
	}
	spec, ok := vm.functionalInterfaceSpec(resolvedName)
	if !ok {
		return value.NilValue(), false
	}
	return value.ObjectValue(&value.SAMWrapper{InterfaceName: resolvedName, MethodName: spec.FunctionalMethod, Callable: candidate}), true
}

func (vm *VM) convertReferenceCast(candidate value.Value, typeName string) (value.Value, bool, error) {
	if wrapped, ok := vm.wrapCallableAsInterface(candidate, typeName); ok {
		return wrapped, true, nil
	}
	switch rootRuntimeTypeName(typeName) {
	case bvmruntime.TypeString:
		if text, ok := valueAsText(candidate); ok {
			return value.StringValue(text), true, nil
		}
		if candidate.Kind == value.Char {
			return value.StringValue(candidate.Str), true, nil
		}
	case bvmruntime.TypeChar:
		if instance, ok := candidate.AsInstance(); ok && instance.Class != nil && rootRuntimeTypeName(instance.Class.Name) == "Char" {
			if slot, _, ok := instance.Class.LookupFieldSlot("value"); ok && slot >= 0 && slot < len(instance.Fields) {
				inner := instance.Fields[slot]
				if inner.Kind == value.Char {
					return inner, true, nil
				}
			}
		}
		if candidate.Kind == value.String {
			runes := []rune(candidate.Str)
			if len(runes) != 1 {
				return value.NilValue(), true, fmt.Errorf("cannot cast %s to %s", vm.runtimeTypeName(candidate), typeName)
			}
			return value.CharValue(runes[0]), true, nil
		}
	case "Integer", "Float", "Double", "Boolean", "Char":
		return vm.wrapPrimitiveValue(rootRuntimeTypeName(typeName), candidate)
	}
	return value.NilValue(), false, nil
}

func textualOperand(candidate value.Value) (string, bool) {
	return valueAsText(candidate)
}

func (vm *VM) StringifyValue(candidate value.Value) (string, error) {
	if text, ok := valueAsText(candidate); ok {
		return text, nil
	}
	instance, ok := candidate.AsInstance()
	if !ok || instance.Class == nil {
		return candidate.String(), nil
	}
	method, _, exists := instance.Class.LookupMethod("toString")
	if !exists || method == nil || method.Arity != 0 {
		return candidate.String(), nil
	}
	result, err := vm.invokeInstanceMethod(instance, method)
	if err != nil {
		return "", err
	}
	if result.Kind == candidate.Kind && result.Object == candidate.Object {
		return candidate.String(), nil
	}
	if text, ok := valueAsText(result); ok {
		return text, nil
	}
	return result.String(), nil
}

func (vm *VM) TypeOfValue(candidate value.Value) string {
	if classValue, ok := candidate.AsClass(); ok {
		if classValue.IsEnum {
			return "Enum " + classValue.Name
		}
		return "Class " + classValue.Name
	}
	return vm.runtimeTypeName(candidate)
}

func (vm *VM) InstanceOfValue(candidate value.Value, target value.Value) (bool, error) {
	if classValue, ok := target.AsClass(); ok {
		return vm.matchesType(candidate, classValue.Name), nil
	}
	if target.Kind == value.String {
		return vm.matchesType(candidate, target.Str), nil
	}
	return false, fmt.Errorf("Sys.instanceof expects a class or string target")
}
