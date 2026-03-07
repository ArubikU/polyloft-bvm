package vm

import (
	"fmt"
	"io"
	"os"

	"github.com/ArubikU/polyloft-bvm/internal/bytecode"
	bvmruntime "github.com/ArubikU/polyloft-bvm/internal/runtime"
	"github.com/ArubikU/polyloft-bvm/internal/value"
)

type frame struct {
	fn       *bytecode.Function
	ip       int
	locals   []value.Value
	receiver *value.Instance
	init     bool
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
	vm.frames = append(vm.frames, vm.acquireFrame(fn, nil, false))
	return vm.execute()
}

func (vm *VM) execute() (value.Value, error) {
	for len(vm.frames) > 0 {
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
		case bytecode.OpGetLocal:
			slot := vm.readByte(frame)
			vm.push(frame.locals[slot])
		case bytecode.OpSetLocal:
			slot := vm.readByte(frame)
			frame.locals[slot] = vm.pop()
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
			vm.push(value.BoolValue(value.Equal(left, right)))
		case bytecode.OpGreater:
			if err := vm.binaryCompare(func(a, b float64) bool { return a > b }); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpLess:
			if err := vm.binaryCompare(func(a, b float64) bool { return a < b }); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpAdd:
			right := vm.pop()
			left := vm.pop()
			if left.Kind == value.Number && right.Kind == value.Number {
				vm.push(value.NumberValue(left.Num + right.Num))
				continue
			}
			if left.Kind == value.String || right.Kind == value.String {
				vm.push(value.StringValue(left.String() + right.String()))
				continue
			}
			return value.NilValue(), fmt.Errorf("ADD expects numbers or strings")
		case bytecode.OpSub:
			if err := vm.binaryNumber(func(a, b float64) float64 { return a - b }); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpMul:
			if err := vm.binaryNumber(func(a, b float64) float64 { return a * b }); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpDiv:
			if err := vm.binaryNumber(func(a, b float64) float64 { return a / b }); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpNot:
			vm.push(value.BoolValue(!vm.pop().IsTruthy()))
		case bytecode.OpNegate:
			operand := vm.pop()
			if operand.Kind != value.Number {
				return value.NilValue(), fmt.Errorf("NEGATE expects number")
			}
			vm.push(value.NumberValue(-operand.Num))
		case bytecode.OpJump:
			offset := vm.readUint16(frame)
			frame.ip += int(offset)
		case bytecode.OpJumpIfFalse:
			offset := vm.readUint16(frame)
			if !vm.peek(0).IsTruthy() {
				frame.ip += int(offset)
			}
		case bytecode.OpLoop:
			offset := vm.readUint16(frame)
			frame.ip -= int(offset)
		case bytecode.OpAddNum:
			if err := vm.binaryNumber(func(a, b float64) float64 { return a + b }); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpSubNum:
			if err := vm.binaryNumber(func(a, b float64) float64 { return a - b }); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpMulNum:
			if err := vm.binaryNumber(func(a, b float64) float64 { return a * b }); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpDivNum:
			if err := vm.binaryNumber(func(a, b float64) float64 { return a / b }); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpLessNum:
			if err := vm.binaryCompare(func(a, b float64) bool { return a < b }); err != nil {
				return value.NilValue(), err
			}
		case bytecode.OpGreaterNum:
			if err := vm.binaryCompare(func(a, b float64) bool { return a > b }); err != nil {
				return value.NilValue(), err
			}
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
			rngVal := vm.pop()
			rng, ok := rngVal.AsRange()
			if !ok {
				return value.NilValue(), fmt.Errorf("ITER_INIT expects range")
			}
			frame.locals[slot] = value.ObjectValue(&value.Iterator{Range: rng, Current: rng.Start})
		case bytecode.OpIterNext:
			iterSlot := vm.readByte(frame)
			valueSlot := vm.readByte(frame)
			offset := vm.readUint16(frame)
			iterator, ok := frame.locals[iterSlot].AsIterator()
			if !ok {
				return value.NilValue(), fmt.Errorf("ITER_NEXT expects iterator")
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
			frame.locals[valueSlot] = value.NumberValue(float64(iterator.Current))
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
		case bytecode.OpSetField:
			slot := int(vm.readByte(frame))
			assigned := vm.pop()
			object := vm.pop()
			instance, ok := object.AsInstance()
			if !ok {
				return value.NilValue(), fmt.Errorf("SET_FIELD expects instance")
			}
			if slot < 0 || slot >= len(instance.Fields) {
				return value.NilValue(), fmt.Errorf("invalid field slot %d for %s", slot, instance.Class.Name)
			}
			instance.Fields[slot] = assigned
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
			if class, ok := object.AsClass(); ok {
				member, exists := class.LookupStatic(name)
				if !exists {
					return value.NilValue(), fmt.Errorf("class %s has no static member %s", class.Name, name)
				}
				vm.push(member)
				continue
			}
			if instance, ok := object.AsInstance(); ok {
				if field, exists := instance.GetField(name); exists {
					vm.push(field)
					continue
				}
				if method, owner, exists := instance.Class.LookupMethod(name); exists {
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
					if err := class.SetStatic(name, assigned); err != nil {
						return value.NilValue(), err
					}
					continue
				}
				return value.NilValue(), fmt.Errorf("cannot assign property %s on %s", name, object.String())
			}
			if !instance.SetField(name, assigned) {
				return value.NilValue(), fmt.Errorf("instance %s has no field %s", instance.Class.Name, name)
			}
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
			if len(vm.frames) == 0 {
				return result, nil
			}
			vm.push(result)
		default:
			return value.NilValue(), fmt.Errorf("unknown opcode %d", op)
		}
	}

	return value.NilValue(), nil
}

func (vm *VM) call(argc int) error {
	callee := vm.peek(argc)
	if bound, ok := callee.AsBoundMethod(); ok {
		fn := bound.Method
		if fn.Arity != argc {
			return fmt.Errorf("%s expects %d args, got %d", fn.Name, fn.Arity, argc)
		}
		child := vm.acquireFrame(fn, bound.Receiver, false)
		child.locals[0] = value.ObjectValue(bound.Receiver)
		for i := argc - 1; i >= 0; i-- {
			child.locals[i+1] = vm.pop()
		}
		vm.pop()
		vm.frames = append(vm.frames, child)
		return nil
	}
	if class, ok := callee.AsClass(); ok {
		ctor := class.Constructor
		if ctor == nil && argc != 0 {
			return fmt.Errorf("%s expects 0 args, got %d", class.Name, argc)
		}
		if ctor != nil && ctor.Arity != argc {
			return fmt.Errorf("%s expects %d args, got %d", class.Name, ctor.Arity, argc)
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
		child := vm.acquireFrame(ctor, instance, true)
		child.locals[0] = value.ObjectValue(instance)
		for i := argc - 1; i >= 0; i-- {
			child.locals[i+1] = vm.pop()
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
		result, err := builtin.Fn(args)
		if err != nil {
			return err
		}
		clear(args)
		vm.push(result)
		return nil
	}

	fn, ok := callee.AsFunction()
	if !ok {
		return fmt.Errorf("attempted to call non-callable %s", callee.String())
	}
	if fn.Arity != argc {
		return fmt.Errorf("%s expects %d args, got %d", fn.Name, fn.Arity, argc)
	}

	child := vm.acquireFrame(fn, nil, false)
	for i := argc - 1; i >= 0; i-- {
		child.locals[i] = vm.pop()
	}
	vm.pop()
	vm.frames = append(vm.frames, child)
	return nil
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
		return vm.callMethod(instance, method, owner, argc)
	}
	if class, ok := receiver.AsClass(); ok {
		member, exists := class.LookupStatic(name)
		if !exists {
			return fmt.Errorf("class %s has no static member %s", class.Name, name)
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
	return vm.callMethod(instance, fn, instance.Class, argc)
}

func (vm *VM) callMethod(receiver *value.Instance, fn *bytecode.Function, owner *value.Class, argc int) error {
	if fn.Arity != argc {
		return fmt.Errorf("%s expects %d args, got %d", fn.Name, fn.Arity, argc)
	}
	child := vm.acquireFrame(fn, receiver, false)
	child.locals[0] = value.ObjectValue(receiver)
	for i := argc - 1; i >= 0; i-- {
		child.locals[i+1] = vm.pop()
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
	child := vm.acquireFrame(ctor, currentFrame.receiver, true)
	child.locals[0] = value.ObjectValue(currentFrame.receiver)
	for i := argc - 1; i >= 0; i-- {
		child.locals[i+1] = vm.pop()
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
		frame.locals[currentSlot] = value.NumberValue(-1)
		frame.locals[endSlot] = end
		frame.locals[stepSlot] = value.NumberValue(1)
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
		frame.locals[currentSlot] = value.NumberValue(start.Num - step)
		frame.locals[endSlot] = end
		frame.locals[stepSlot] = value.NumberValue(step)
		return nil
	default:
		return fmt.Errorf("range expects 1 or 2 arguments")
	}
}

func (vm *VM) rangeNextFast(frame *frame, currentSlot, endSlot, stepSlot byte) (bool, error) {
	current := frame.locals[currentSlot]
	end := frame.locals[endSlot]
	step := frame.locals[stepSlot]
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
	frame.locals[currentSlot] = value.NumberValue(next)
	return true, nil
}

func (vm *VM) currentFrame() *frame {
	return vm.frames[len(vm.frames)-1]
}

func (vm *VM) acquireFrame(fn *bytecode.Function, receiver *value.Instance, init bool) *frame {
	var child *frame
	if len(vm.framePool) > 0 {
		last := len(vm.framePool) - 1
		child = vm.framePool[last]
		vm.framePool = vm.framePool[:last]
	} else {
		child = &frame{}
	}
	child.fn = fn
	child.ip = 0
	child.receiver = receiver
	child.init = init
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
	child.ip = 0
	child.receiver = nil
	child.init = false
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
	case float64:
		return value.NumberValue(v)
	case bool:
		return value.BoolValue(v)
	case string:
		return value.StringValue(v)
	case *bytecode.Function:
		return value.ObjectValue(v)
	default:
		return value.ObjectValue(v)
	}
}

func (vm *VM) binaryNumber(op func(float64, float64) float64) error {
	right := vm.pop()
	left := vm.pop()
	if left.Kind != value.Number || right.Kind != value.Number {
		return fmt.Errorf("numeric operation expects numbers")
	}
	vm.push(value.NumberValue(op(left.Num, right.Num)))
	return nil
}

func (vm *VM) binaryCompare(op func(float64, float64) bool) error {
	right := vm.pop()
	left := vm.pop()
	if left.Kind != value.Number || right.Kind != value.Number {
		return fmt.Errorf("comparison expects numbers")
	}
	vm.push(value.BoolValue(op(left.Num, right.Num)))
	return nil
}
