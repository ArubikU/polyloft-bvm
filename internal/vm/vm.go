package vm

import (
	"fmt"
	"hash/fnv"
	"io"
	"math"
	"os"
	"sort"

	"github.com/ArubikU/polyloft-bvm/internal/bytecode"
	bvmruntime "github.com/ArubikU/polyloft-bvm/internal/runtime"
	"github.com/ArubikU/polyloft-bvm/internal/value"
)

type frame struct {
	fn        *bytecode.Function
	closure   *value.Closure
	ip        int
	locals    []value.Value
	localRefs map[byte]*value.Cell
	receiver  *value.Instance
	init      bool
}

type VM struct {
	stdout        io.Writer
	stack         []value.Value
	frames        []*frame
	framePool     []*frame
	globals       map[string]value.Value
	globalSlots   []value.Value
	globalDefined []bool
	builtinArgs   []value.Value
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
	if stdout == nil {
		stdout = os.Stdout
	}

	return &VM{
		stdout:      stdout,
		stack:       make([]value.Value, 0, 256),
		frames:      make([]*frame, 0, 16),
		framePool:   make([]*frame, 0, 16),
		globals:     registry.Globals(),
		builtinArgs: make([]value.Value, 0, 8),
	}
}

// Globals returns the flat global name->value map used by the VM.
// This is used externally to resolve exported values after running a module.
func (vm *VM) Globals() map[string]value.Value {
	return vm.globals
}

func (vm *VM) Run(fn *bytecode.Function) (value.Value, error) {
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

func (vm *VM) executeUntilDepth(baseDepth int) (value.Value, error) {
	for len(vm.frames) > baseDepth {
		frame := vm.currentFrame()
		if frame.ip >= len(frame.fn.Chunk.Code) {
			return value.NilValue(), fmt.Errorf("unexpected end of bytecode")
		}

		op := bytecode.Op(frame.fn.Chunk.Code[frame.ip])
		frame.ip++

		switch op {
		case bytecode.OpConstant:
			idx := vm.readUint16(frame)
			vm.push(vm.constantToValue(frame.fn.Chunk.Constants[idx]))
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
			slot := vm.readByte(frame)
			vm.push(vm.localGet(frame, slot))
		case bytecode.OpSetLocal:
			slot := vm.readByte(frame)
			vm.localSet(frame, slot, vm.pop())
		case bytecode.OpGetCapture:
			slot := vm.readByte(frame)
			vm.push(frame.closure.Captures[slot].Value)
		case bytecode.OpSetCapture:
			slot := vm.readByte(frame)
			frame.closure.Captures[slot].Value = vm.pop()
		case bytecode.OpDefineGlobal:
			name := frame.fn.Chunk.Constants[vm.readUint16(frame)].(string)
			vm.globals[name] = vm.pop()
		case bytecode.OpDefineGlobalSlot:
			slot := int(vm.readByte(frame))
			vm.globalSlots[slot] = vm.pop()
			vm.globalDefined[slot] = true
		case bytecode.OpGetGlobal:
			name := frame.fn.Chunk.Constants[vm.readUint16(frame)].(string)
			val, ok := vm.globals[name]
			if !ok {
				return value.NilValue(), fmt.Errorf("undefined variable %s", name)
			}
			vm.push(val)
		case bytecode.OpGetGlobalSlot:
			slot := int(vm.readByte(frame))
			if !vm.globalDefined[slot] {
				return value.NilValue(), fmt.Errorf("undefined global slot %d", slot)
			}
			vm.push(vm.globalSlots[slot])
		case bytecode.OpSetGlobal:
			name := frame.fn.Chunk.Constants[vm.readUint16(frame)].(string)
			if _, ok := vm.globals[name]; !ok {
				return value.NilValue(), fmt.Errorf("undefined variable %s", name)
			}
			vm.globals[name] = vm.pop()
		case bytecode.OpSetGlobalSlot:
			slot := int(vm.readByte(frame))
			if !vm.globalDefined[slot] {
				return value.NilValue(), fmt.Errorf("undefined global slot %d", slot)
			}
			vm.globalSlots[slot] = vm.pop()
		case bytecode.OpEqual:
			right := vm.pop()
			left := vm.pop()
			vm.push(value.BoolValue(vm.valuesEqual(left, right)))
		case bytecode.OpMatchType:
			typeName := frame.fn.Chunk.Constants[vm.readUint16(frame)].(string)
			candidate := vm.pop()
			vm.push(value.BoolValue(vm.matchesType(candidate, typeName)))
		case bytecode.OpCastRef:
			typeName := frame.fn.Chunk.Constants[vm.readUint16(frame)].(string)
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
			if err := vm.binaryCompare(func(a, b float64) bool { return a > b }, func(a, b string) bool { return a > b }); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpLess:
			if err := vm.binaryCompare(func(a, b float64) bool { return a < b }, func(a, b string) bool { return a < b }); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpAdd:
			right := vm.pop()
			left := vm.pop()
			if leftNum, ok := vm.numericOperand(left); ok {
				if rightNum, ok := vm.numericOperand(right); ok {
					vm.push(vm.numericResult(left, right, bytecode.OpAdd, leftNum+rightNum))
					continue
				}
			}
			if left.Kind == value.Number && right.Kind == value.Number {
				vm.push(vm.numericResult(left, right, bytecode.OpAdd, left.Num+right.Num))
				continue
			}
			// string/char concatenation
			if left.Kind == value.String || right.Kind == value.String || left.Kind == value.Char || right.Kind == value.Char {
				vm.push(value.StringValue(left.String() + right.String()))
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
			return value.NilValue(), fmt.Errorf("ADD expects numbers or strings")
		case bytecode.OpSub:
			if err := vm.binaryNumberOp(bytecode.OpSub, func(a, b float64) float64 { return a - b }); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpMul:
			if err := vm.binaryNumberOp(bytecode.OpMul, func(a, b float64) float64 { return a * b }); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpDiv:
			if err := vm.binaryNumberOp(bytecode.OpDiv, func(a, b float64) float64 { return a / b }); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpMod:
			if err := vm.binaryNumberOp(bytecode.OpMod, func(a, b float64) float64 { return math.Mod(a, b) }); err != nil {
				return value.NilValue(), err
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
				return value.NilValue(), fmt.Errorf("NEGATE expects number")
			}
			if operand.Kind == value.Number && operand.NumberKind == value.NumberInt {
				vm.push(value.IntValue(-int64(numericValue)))
			} else {
				vm.push(value.FloatValue(-numericValue))
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
			offset := vm.readUint16(frame)
			frame.ip += int(offset)
		case bytecode.OpJumpIfFalse:
			offset := vm.readUint16(frame)
			condition := vm.peek(0)
			if booleanValue, ok := vm.booleanOperand(condition); ok {
				if !booleanValue {
					frame.ip += int(offset)
				}
				continue
			}
			if !condition.IsTruthy() {
				frame.ip += int(offset)
			}
		case bytecode.OpLoop:
			offset := vm.readUint16(frame)
			frame.ip -= int(offset)
		case bytecode.OpAddNum:
			if err := vm.binaryNumberOp(bytecode.OpAddNum, func(a, b float64) float64 { return a + b }); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpSubNum:
			if err := vm.binaryNumberOp(bytecode.OpSubNum, func(a, b float64) float64 { return a - b }); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpMulNum:
			if err := vm.binaryNumberOp(bytecode.OpMulNum, func(a, b float64) float64 { return a * b }); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpDivNum:
			if err := vm.binaryNumberOp(bytecode.OpDivNum, func(a, b float64) float64 { return a / b }); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpLessNum:
			if err := vm.binaryCompare(func(a, b float64) bool { return a < b }, func(a, b string) bool { return a < b }); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpGreaterNum:
			if err := vm.binaryCompare(func(a, b float64) bool { return a > b }, func(a, b string) bool { return a > b }); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpAddLocalMulThisField:
			targetSlot := vm.readByte(frame)
			localSlot := vm.readByte(frame)
			fieldSlot := int(vm.readByte(frame))
			if frame.receiver == nil {
				return value.NilValue(), fmt.Errorf("ADD_LOCAL_MUL_THIS_FIELD expects receiver")
			}
			if fieldSlot < 0 || fieldSlot >= len(frame.receiver.Fields) {
				return value.NilValue(), fmt.Errorf("invalid field slot %d for %s", fieldSlot, frame.receiver.Class.Name)
			}
			target := vm.localGet(frame, targetSlot)
			multiplier := vm.localGet(frame, localSlot)
			factor := frame.receiver.Fields[fieldSlot]
			if target.Kind != value.Number || multiplier.Kind != value.Number || factor.Kind != value.Number {
				return value.NilValue(), fmt.Errorf("ADD_LOCAL_MUL_THIS_FIELD expects numbers")
			}
			vm.localSet(frame, targetSlot, value.NumberValue(target.Num+multiplier.Num*factor.Num))
		case bytecode.OpClosure:
			idx := vm.readUint16(frame)
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
			argc := int(vm.readByte(frame))
			if err := vm.call(argc); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpInvoke:
			name := frame.fn.Chunk.Constants[vm.readUint16(frame)].(string)
			argc := int(vm.readByte(frame))
			if err := vm.invoke(name, argc); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpInvokeMethod:
			slot := int(vm.readByte(frame))
			argc := int(vm.readByte(frame))
			if err := vm.invokeMethodSlot(slot, argc); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpRange:
			argc := int(vm.readByte(frame))
			rng, err := vm.buildRange(argc)
			if err != nil {
				return value.NilValue(), err
			}
			vm.push(value.ObjectValue(rng))
		case bytecode.OpRangeInitFast:
			currentSlot := vm.readByte(frame)
			endSlot := vm.readByte(frame)
			stepSlot := vm.readByte(frame)
			argc := int(vm.readByte(frame))
			if err := vm.initFastRange(frame, currentSlot, endSlot, stepSlot, argc); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpRangeNextFast:
			currentSlot := vm.readByte(frame)
			endSlot := vm.readByte(frame)
			stepSlot := vm.readByte(frame)
			valueSlot := vm.readByte(frame)
			offset := vm.readUint16(frame)
			advance, err := vm.rangeNextFast(frame, currentSlot, endSlot, stepSlot)
			if err != nil {
				return value.NilValue(), err
			}
			if !advance {
				frame.ip += int(offset)
				continue
			}
			frame.locals[valueSlot] = frame.locals[currentSlot]
		case bytecode.OpIterInit:
			slot := vm.readByte(frame)
			mode := vm.readByte(frame)
			iterable := vm.pop()
			if instance, ok := iterable.AsInstance(); ok {
				if instance.Class.IterableLength != nil && instance.Class.IterableGet != nil {
					lengthValue, err := vm.invokeInstanceMethod(instance, instance.Class.IterableLength)
					if err != nil {
						return value.NilValue(), err
					}
					if lengthValue.Kind != value.Number {
						return value.NilValue(), fmt.Errorf("%s.__length() must return Number", instance.Class.Name)
					}
					vm.localSet(frame, slot, value.ObjectValue(&value.Iterator{Receiver: instance, Index: 0, Length: int(lengthValue.Num), GetFn: instance.Class.IterableGet}))
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
			iterSlot := vm.readByte(frame)
			valueSlot := vm.readByte(frame)
			offset := vm.readUint16(frame)
			iterator, ok := vm.localGet(frame, iterSlot).AsIterator()
			if !ok {
				return value.NilValue(), fmt.Errorf("ITER_NEXT expects iterator")
			}
			if iterator.Items != nil {
				if iterator.Index >= iterator.Length {
					frame.ip += int(offset)
					continue
				}
				vm.localSet(frame, valueSlot, iterator.Items[iterator.Index])
				iterator.Index++
				continue
			}
			if iterator.Receiver != nil && iterator.GetFn != nil {
				if iterator.Index >= iterator.Length {
					frame.ip += int(offset)
					continue
				}
				item, err := vm.invokeInstanceMethod(iterator.Receiver, iterator.GetFn, value.NumberValue(float64(iterator.Index)))
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
			slot := int(vm.readByte(frame))
			object := vm.pop()
			instance, ok := object.AsInstance()
			if !ok {
				return value.NilValue(), fmt.Errorf("GET_FIELD expects instance")
			}
			if slot < 0 || slot >= len(instance.Fields) {
				return value.NilValue(), fmt.Errorf("invalid field slot %d for %s", slot, instance.Class.Name)
			}
			vm.push(instance.Fields[slot])
		case bytecode.OpGetThisField:
			slot := int(vm.readByte(frame))
			if frame.receiver == nil {
				return value.NilValue(), fmt.Errorf("GET_THIS_FIELD expects receiver")
			}
			if slot < 0 || slot >= len(frame.receiver.Fields) {
				return value.NilValue(), fmt.Errorf("invalid field slot %d for %s", slot, frame.receiver.Class.Name)
			}
			vm.push(frame.receiver.Fields[slot])
		case bytecode.OpSetField:
			slot := int(vm.readByte(frame))
			assigned := vm.pop()
			object := vm.pop()
			instance, ok := object.AsInstance()
			if !ok {
				return value.NilValue(), fmt.Errorf("SET_FIELD expects instance")
			}
			if instance.Frozen {
				return value.NilValue(), fmt.Errorf("instance %s is frozen", instance.Class.Name)
			}
			if slot < 0 || slot >= len(instance.Fields) {
				return value.NilValue(), fmt.Errorf("invalid field slot %d for %s", slot, instance.Class.Name)
			}
			instance.Fields[slot] = assigned
		case bytecode.OpSetThisField:
			slot := int(vm.readByte(frame))
			assigned := vm.pop()
			if frame.receiver == nil {
				return value.NilValue(), fmt.Errorf("SET_THIS_FIELD expects receiver")
			}
			if frame.receiver.Frozen {
				return value.NilValue(), fmt.Errorf("instance %s is frozen", frame.receiver.Class.Name)
			}
			if slot < 0 || slot >= len(frame.receiver.Fields) {
				return value.NilValue(), fmt.Errorf("invalid field slot %d for %s", slot, frame.receiver.Class.Name)
			}
			frame.receiver.Fields[slot] = assigned
		case bytecode.OpGetProperty:
			name := frame.fn.Chunk.Constants[vm.readUint16(frame)].(string)
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
				if err := vm.ensureAccess(owner, class.StaticVisibility[name]); err != nil {
					return value.NilValue(), err
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
					vm.push(value.ObjectValue(&value.BoundMethod{Receiver: instance, Method: method, Owner: owner}))
					continue
				}
				return value.NilValue(), fmt.Errorf("instance %s has no member %s", instance.Class.Name, name)
			}
			return value.NilValue(), fmt.Errorf("cannot access property %s on %s", name, object.String())
		case bytecode.OpSetProperty:
			name := frame.fn.Chunk.Constants[vm.readUint16(frame)].(string)
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
			ifaceName := frame.fn.Chunk.Constants[vm.readUint16(frame)].(string)
			methodName := frame.fn.Chunk.Constants[vm.readUint16(frame)].(string)
			callable := vm.pop()
			vm.push(value.ObjectValue(&value.SAMWrapper{InterfaceName: ifaceName, MethodName: methodName, Callable: callable}))
		case bytecode.OpArray:
			count := int(vm.readByte(frame))
			elements := make([]value.Value, count)
			for i := count - 1; i >= 0; i-- {
				elements[i] = vm.pop()
			}
			vm.push(value.ObjectValue(&value.Array{Elements: elements}))
		case bytecode.OpMap:
			count := int(vm.readByte(frame))
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
			count := int(vm.readByte(frame))
			elements := make([]value.Value, count)
			for i := count - 1; i >= 0; i-- {
				elements[i] = vm.pop()
			}
			vm.push(value.ObjectValue(&value.Tuple{Elements: elements}))
		case bytecode.OpUnpack:
			count := int(vm.readByte(frame))
			source := vm.pop()
			if instance, ok := source.AsInstance(); ok {
				if instance.Class.PiecesMethod != nil && instance.Class.GetPieceMethod != nil {
					pieces, err := vm.invokeInstanceMethod(instance, instance.Class.PiecesMethod)
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
						piece, err := vm.invokeInstanceMethod(instance, instance.Class.GetPieceMethod, value.NumberValue(float64(i)))
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
				idx := int(index.Num)
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
				if instance.Class.IndexGetMethod != nil {
					result, err := vm.invokeInstanceMethod(instance, instance.Class.IndexGetMethod, index)
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
			idx := int(index.Num)
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
				vm.push(value.BoolValue(containsValue(array.Elements, needle, vm.valuesEqual)))
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
			if instance, ok := container.AsInstance(); ok && instance.Class.ContainsMethod != nil {
				result, err := vm.invokeInstanceMethod(instance, instance.Class.ContainsMethod, needle)
				if err != nil {
					return value.NilValue(), err
				}
				vm.push(result)
				continue
			}
			return value.NilValue(), fmt.Errorf("value does not support contains")
		case bytecode.OpContainsArray:
			container := vm.pop()
			needle := vm.pop()
			array, ok := container.AsArray()
			if !ok {
				return value.NilValue(), fmt.Errorf("CONTAINS_ARRAY expects array")
			}
			vm.push(value.BoolValue(containsValue(array.Elements, needle, vm.valuesEqual)))
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
				if instance.Class.SliceMethod != nil {
					result, err := vm.invokeInstanceMethod(instance, instance.Class.SliceMethod, start, end)
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
				idx := int(index.Num)
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
				if instance.Class.IndexSetMethod != nil {
					if _, err := vm.invokeInstanceMethod(instance, instance.Class.IndexSetMethod, index, assigned); err != nil {
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
			idx := int(index.Num)
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
			argc := int(vm.readByte(frame))
			if err := vm.callSuper(argc); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpReturn:
			result := vm.pop()
			if frame.init && frame.receiver != nil {
				result = value.ObjectValue(frame.receiver)
			}
			vm.frames = vm.frames[:len(vm.frames)-1]
			vm.releaseFrame(frame)
			if len(vm.frames) == baseDepth {
				return result, nil
			}
			vm.push(result)
		default:
			return value.NilValue(), fmt.Errorf("unknown opcode %d", op)
		}
	}

	return value.NilValue(), nil
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

func (vm *VM) call(argc int) error {
	callee := vm.peek(argc)
	if bound, ok := callee.AsSAMBoundMethod(); ok {
		vm.stack[len(vm.stack)-1-argc] = bound.Wrapper.Callable
		return vm.call(argc)
	}
	if bound, ok := callee.AsBoundMethod(); ok {
		fn := bound.Method
		if fn.Arity != argc {
			return fmt.Errorf("%s expects %d args, got %d", fn.Name, fn.Arity, argc)
		}
		child := vm.acquireFrame(fn, nil, bound.Receiver, false)
		vm.localSet(child, 0, value.ObjectValue(bound.Receiver))
		for i := argc - 1; i >= 0; i-- {
			vm.localSet(child, byte(i+1), vm.pop())
		}
		vm.pop()
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
		ctor := class.Constructor
		fastCtor := class.FastConstructor
		if ctor == nil && argc != 0 {
			return fmt.Errorf("%s expects 0 args, got %d", class.Name, argc)
		}
		if ctor != nil && ctor.Arity != argc {
			return fmt.Errorf("%s expects %d args, got %d", class.Name, ctor.Arity, argc)
		}
		if fastCtor != nil {
			if fastCtor.Arity != argc {
				return fmt.Errorf("%s expects %d args, got %d", class.Name, fastCtor.Arity, argc)
			}
			args := make([]value.Value, argc)
			for i := argc - 1; i >= 0; i-- {
				args[i] = vm.pop()
			}
			vm.pop()
			instance := class.NewInstance()
			for i, slot := range fastCtor.FieldSlots {
				instance.Fields[slot] = args[fastCtor.ArgIndexes[i]]
			}
			vm.push(value.ObjectValue(instance))
			return nil
		}
		instance := class.NewInstance()
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
		return candidate.Kind == value.Number && candidate.NumberKind == value.NumberInt
	case bvmruntime.TypeFloat:
		return candidate.Kind == value.Number && candidate.NumberKind == value.NumberFloat
	case bvmruntime.TypeNumber:
		return candidate.Kind == value.Number
	case bvmruntime.TypeChar:
		return candidate.Kind == value.Char
	case bvmruntime.TypeString:
		return candidate.Kind == value.String
	case bvmruntime.TypeBool:
		return candidate.Kind == value.Bool
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
		_, ok := candidate.AsBuiltin()
		return ok
	}
	if wrapper, ok := candidate.AsSAMWrapper(); ok {
		return wrapper.InterfaceName == rootRuntimeTypeName(typeName)
	}
	if instance, ok := candidate.AsInstance(); ok {
		if instance.Class.Implements[rootRuntimeTypeName(typeName)] {
			return true
		}
		for current := instance.Class; current != nil; current = current.Superclass {
			if current.Name == typeName {
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
	return trimTypeSpace(base)
}

func valueAsText(candidate value.Value) (string, bool) {
	switch candidate.Kind {
	case value.Char, value.String:
		return candidate.Str, true
	default:
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
	if len(needle) == 0 {
		return true
	}
	return indexString(haystack, needle) >= 0
}

func indexString(haystack string, needle string) int {
	limit := len(haystack) - len(needle)
	for i := 0; i <= limit; i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func containsValue(items []value.Value, needle value.Value, equals func(value.Value, value.Value) bool) bool {
	for _, item := range items {
		if equals(item, needle) {
			return true
		}
	}
	return false
}

func (vm *VM) invoke(name string, argc int) error {
	receiver := vm.peek(argc)
	if module, ok := receiver.AsModule(); ok {
		member, exists := module.Members[name]
		if !exists {
			return fmt.Errorf("module %s has no member %s", module.Name, name)
		}
		vm.stack[len(vm.stack)-1-argc] = member
		return vm.call(argc)
	}
	if instance, ok := receiver.AsInstance(); ok {
		method, owner, exists := instance.Class.LookupMethod(name)
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
					vm.stack[len(vm.stack)-1] = result
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
		vm.stack[len(vm.stack)-1-argc] = wrapper.Callable
		return vm.call(argc)
	}
	if class, ok := receiver.AsClass(); ok {
		owner, member, exists := class.LookupStaticOwner(name)
		if !exists {
			return fmt.Errorf("class %s has no static member %s", class.Name, name)
		}
		if err := vm.ensureAccess(owner, owner.StaticVisibility[name]); err != nil {
			return err
		}
		vm.stack[len(vm.stack)-1-argc] = member
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
			vm.stack[len(vm.stack)-1] = result
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
		return value.NumberValue(expr.Number), nil
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
	vm.localSet(child, 0, value.ObjectValue(receiver))
	for i := argc - 1; i >= 0; i-- {
		vm.localSet(child, byte(i+1), vm.pop())
	}
	vm.pop()
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
	ctor := superclass.Constructor
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
	vm.frames = append(vm.frames, child)
	return nil
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

func (vm *VM) initFastRange(frame *frame, currentSlot, endSlot, stepSlot byte, argc int) error {
	switch argc {
	case 1:
		end := vm.pop()
		if end.Kind != value.Number {
			return fmt.Errorf("range expects numeric arguments")
		}
		vm.localSet(frame, currentSlot, value.NumberValue(-1))
		vm.localSet(frame, endSlot, end)
		vm.localSet(frame, stepSlot, value.NumberValue(1))
		return nil
	case 2:
		end := vm.pop()
		start := vm.pop()
		if start.Kind != value.Number || end.Kind != value.Number {
			return fmt.Errorf("range expects numeric arguments")
		}
		step := 1.0
		if start.Num > end.Num {
			step = -1
		}
		vm.localSet(frame, currentSlot, value.NumberValue(start.Num-step))
		vm.localSet(frame, endSlot, end)
		vm.localSet(frame, stepSlot, value.NumberValue(step))
		return nil
	default:
		return fmt.Errorf("range expects 1 or 2 arguments")
	}
}

func (vm *VM) rangeNextFast(frame *frame, currentSlot, endSlot, stepSlot byte) (bool, error) {
	current := vm.localGet(frame, currentSlot)
	end := vm.localGet(frame, endSlot)
	step := vm.localGet(frame, stepSlot)
	if current.Kind != value.Number || end.Kind != value.Number || step.Kind != value.Number {
		return false, fmt.Errorf("fast range expects numeric locals")
	}
	next := current.Num + step.Num
	if step.Num > 0 && next >= end.Num {
		return false, nil
	}
	if step.Num < 0 && next <= end.Num {
		return false, nil
	}
	vm.localSet(frame, currentSlot, value.NumberValue(next))
	return true, nil
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
	child.receiver = receiver
	child.init = init
	if child.localRefs == nil {
		child.localRefs = make(map[byte]*value.Cell)
	} else {
		clear(child.localRefs)
	}
	if cap(child.locals) < fn.MaxLocals {
		child.locals = make([]value.Value, fn.MaxLocals)
	} else {
		child.locals = child.locals[:fn.MaxLocals]
		clear(child.locals)
	}
	return child
}

func (vm *VM) releaseFrame(child *frame) {
	clear(child.locals)
	child.fn = nil
	child.closure = nil
	child.ip = 0
	child.receiver = nil
	child.init = false
	clear(child.localRefs)
	vm.framePool = append(vm.framePool, child)
}

func (vm *VM) borrowBuiltinArgs(argc int) []value.Value {
	if cap(vm.builtinArgs) < argc {
		vm.builtinArgs = make([]value.Value, argc)
	} else {
		vm.builtinArgs = vm.builtinArgs[:argc]
	}
	return vm.builtinArgs
}

func (vm *VM) push(v value.Value) {
	vm.stack = append(vm.stack, v)
}

func (vm *VM) pop() value.Value {
	v := vm.stack[len(vm.stack)-1]
	vm.stack = vm.stack[:len(vm.stack)-1]
	return v
}

func (vm *VM) peek(distance int) value.Value {
	return vm.stack[len(vm.stack)-1-distance]
}

func (vm *VM) readByte(frame *frame) byte {
	b := frame.fn.Chunk.Code[frame.ip]
	frame.ip++
	return b
}

func (vm *VM) readUint16(frame *frame) uint16 {
	high := uint16(vm.readByte(frame))
	low := uint16(vm.readByte(frame))
	return high<<8 | low
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
	if cell, ok := frame.localRefs[slot]; ok {
		return cell.Value
	}
	return frame.locals[slot]
}

func (vm *VM) localSet(frame *frame, slot byte, v value.Value) {
	if cell, ok := frame.localRefs[slot]; ok {
		cell.Value = v
		return
	}
	frame.locals[slot] = v
}

func (vm *VM) captureLocal(frame *frame, slot byte) *value.Cell {
	if cell, ok := frame.localRefs[slot]; ok {
		return cell
	}
	cell := &value.Cell{Value: frame.locals[slot]}
	frame.localRefs[slot] = cell
	return cell
}

func (vm *VM) binaryNumberOp(operator bytecode.Op, op func(float64, float64) float64) error {
	right := vm.pop()
	left := vm.pop()
	leftNum, ok := vm.numericOperand(left)
	if !ok {
		return fmt.Errorf("numeric operation expects numbers")
	}
	rightNum, ok := vm.numericOperand(right)
	if !ok {
		return fmt.Errorf("numeric operation expects numbers")
	}
	vm.push(vm.numericResult(left, right, operator, op(leftNum, rightNum)))
	return nil
}

func (vm *VM) numericResult(left value.Value, right value.Value, operator bytecode.Op, result float64) value.Value {
	if operator == bytecode.OpDiv || operator == bytecode.OpDivNum {
		return value.FloatValue(result)
	}
	if left.Kind == value.Number && right.Kind == value.Number && left.NumberKind == value.NumberInt && right.NumberKind == value.NumberInt {
		return value.IntValue(int64(result))
	}
	return value.FloatValue(result)
}

func (vm *VM) binaryCompare(numberOp func(float64, float64) bool, textOp func(string, string) bool) error {
	right := vm.pop()
	left := vm.pop()
	if leftText, ok := textualOperand(left); ok {
		if rightText, ok := textualOperand(right); ok {
			vm.push(value.BoolValue(textOp(leftText, rightText)))
			return nil
		}
	}
	leftNum, ok := vm.numericOperand(left)
	if !ok {
		return fmt.Errorf("comparison expects numbers or text")
	}
	rightNum, ok := vm.numericOperand(right)
	if !ok {
		return fmt.Errorf("comparison expects numbers or text")
	}
	vm.push(value.BoolValue(numberOp(leftNum, rightNum)))
	return nil
}

func (vm *VM) numericOperand(candidate value.Value) (float64, bool) {
	if candidate.Kind == value.Number {
		return candidate.Num, true
	}
	instance, ok := candidate.AsInstance()
	if !ok || instance.Class == nil {
		return 0, false
	}
	switch instance.Class.Name {
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
	if instance.Class.Name != "Boolean" {
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

func (vm *VM) valuesEqual(left value.Value, right value.Value) bool {
	if leftText, ok := textualOperand(left); ok {
		if rightText, ok := textualOperand(right); ok {
			return leftText == rightText
		}
	}
	if leftNum, ok := vm.numericOperand(left); ok {
		if rightNum, ok := vm.numericOperand(right); ok {
			return leftNum == rightNum
		}
	}
	if leftBool, ok := vm.booleanOperand(left); ok {
		if rightBool, ok := vm.booleanOperand(right); ok {
			return leftBool == rightBool
		}
	}
	return value.Equal(left, right)
}

func (vm *VM) convertReferenceCast(candidate value.Value, typeName string) (value.Value, bool, error) {
	switch rootRuntimeTypeName(typeName) {
	case bvmruntime.TypeString:
		if candidate.Kind == value.Char {
			return value.StringValue(candidate.Str), true, nil
		}
	case bvmruntime.TypeChar:
		if candidate.Kind == value.String {
			runes := []rune(candidate.Str)
			if len(runes) != 1 {
				return value.NilValue(), true, fmt.Errorf("cannot cast %s to %s", vm.runtimeTypeName(candidate), typeName)
			}
			return value.CharValue(runes[0]), true, nil
		}
	}
	return value.NilValue(), false, nil
}

func textualOperand(candidate value.Value) (string, bool) {
	switch candidate.Kind {
	case value.Char, value.String:
		return candidate.Str, true
	default:
		return "", false
	}
}
