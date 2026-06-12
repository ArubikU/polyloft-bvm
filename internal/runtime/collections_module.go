package runtime

import (
	"fmt"

	"github.com/ArubikU/polyloft-bvm/internal/value"
)

type nativeListHandle struct {
	items []value.Value
	size  int
}

func BuildCollectionsModule() *RuntimeModule {
	builder := NewModuleBuilder("Collections")

	builder.AddTypedFunction("list_new", []string{}, TypeAny, false, func(args []value.Value) (value.Value, error) {
		handle := &nativeListHandle{items: make([]value.Value, 10), size: 0}
		return value.ObjectValue(handle), nil
	})

	builder.AddTypedFunction("list_size", []string{TypeAny}, TypeInt, false, func(args []value.Value) (value.Value, error) {
		handle, err := asNativeListHandle(args[0])
		if err != nil {
			return value.NilValue(), err
		}
		return value.IntValue(int64(handle.size)), nil
	})

	builder.AddTypedFunction("list_add", []string{TypeAny, TypeAny}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
		handle, err := asNativeListHandle(args[0])
		if err != nil {
			return value.NilValue(), err
		}
		handle.ensureCapacity(handle.size + 1)
		handle.items[handle.size] = args[1]
		handle.size++
		return value.NilValue(), nil
	})

	builder.AddTypedFunction("list_add_first", []string{TypeAny, TypeAny}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
		handle, err := asNativeListHandle(args[0])
		if err != nil {
			return value.NilValue(), err
		}
		handle.ensureCapacity(handle.size + 1)
		copy(handle.items[1:handle.size+1], handle.items[:handle.size])
		handle.items[0] = args[1]
		handle.size++
		return value.NilValue(), nil
	})

	builder.AddTypedFunction("list_add_last", []string{TypeAny, TypeAny}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
		handle, err := asNativeListHandle(args[0])
		if err != nil {
			return value.NilValue(), err
		}
		handle.ensureCapacity(handle.size + 1)
		handle.items[handle.size] = args[1]
		handle.size++
		return value.NilValue(), nil
	})

	builder.AddTypedFunction("list_remove", []string{TypeAny, TypeAny}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		handle, err := asNativeListHandle(args[0])
		if err != nil {
			return value.NilValue(), err
		}
		for idx := 0; idx < handle.size; idx++ {
			if value.Equal(handle.items[idx], args[1]) {
				copy(handle.items[idx:], handle.items[idx+1:handle.size])
				handle.size--
				handle.items[handle.size] = value.NilValue()
				return value.BoolValue(true), nil
			}
		}
		return value.BoolValue(false), nil
	})

	builder.AddTypedFunction("list_contains", []string{TypeAny, TypeAny}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		handle, err := asNativeListHandle(args[0])
		if err != nil {
			return value.NilValue(), err
		}
		for idx := 0; idx < handle.size; idx++ {
			if value.Equal(handle.items[idx], args[1]) {
				return value.BoolValue(true), nil
			}
		}
		return value.BoolValue(false), nil
	})

	builder.AddTypedFunction("list_clear", []string{TypeAny}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
		handle, err := asNativeListHandle(args[0])
		if err != nil {
			return value.NilValue(), err
		}
		handle.items = make([]value.Value, 10)
		handle.size = 0
		return value.NilValue(), nil
	})

	builder.AddTypedFunction("list_as_array", []string{TypeAny}, TypeArray, false, func(args []value.Value) (value.Value, error) {
		handle, err := asNativeListHandle(args[0])
		if err != nil {
			return value.NilValue(), err
		}
		items := append([]value.Value(nil), handle.items[:handle.size]...)
		return value.ObjectValue(value.NewArray(items)), nil
	})

	builder.AddTypedFunction("list_get", []string{TypeAny, TypeInt}, TypeAny, false, func(args []value.Value) (value.Value, error) {
		handle, err := asNativeListHandle(args[0])
		if err != nil {
			return value.NilValue(), err
		}
		idx := int(args[1].Num)
		if idx < 0 || idx >= handle.size {
			return value.NilValue(), fmt.Errorf("array list index out of range")
		}
		return handle.items[idx], nil
	})

	builder.AddTypedFunction("list_remove_first", []string{TypeAny}, TypeAny, false, func(args []value.Value) (value.Value, error) {
		handle, err := asNativeListHandle(args[0])
		if err != nil {
			return value.NilValue(), err
		}
		if handle.size == 0 {
			return value.NilValue(), nil
		}
		removed := handle.items[0]
		copy(handle.items[0:handle.size-1], handle.items[1:handle.size])
		handle.size--
		handle.items[handle.size] = value.NilValue()
		return removed, nil
	})

	builder.AddTypedFunction("list_remove_last", []string{TypeAny}, TypeAny, false, func(args []value.Value) (value.Value, error) {
		handle, err := asNativeListHandle(args[0])
		if err != nil {
			return value.NilValue(), err
		}
		if handle.size == 0 {
			return value.NilValue(), nil
		}
		handle.size--
		removed := handle.items[handle.size]
		handle.items[handle.size] = value.NilValue()
		return removed, nil
	})

	builder.AddTypedFunction("list_set", []string{TypeAny, TypeInt, TypeAny}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
		handle, err := asNativeListHandle(args[0])
		if err != nil {
			return value.NilValue(), err
		}
		idx := int(args[1].Num)
		if idx < 0 || idx >= handle.size {
			return value.NilValue(), fmt.Errorf("array list index out of range")
		}
		handle.items[idx] = args[2]
		return value.NilValue(), nil
	})

	return builder.Build()
}

func asNativeListHandle(candidate value.Value) (*nativeListHandle, error) {
	handle, ok := candidate.Object.(*nativeListHandle)
	if !ok || handle == nil {
		return nil, fmt.Errorf("list handle expected")
	}
	return handle, nil
}

func (handle *nativeListHandle) ensureCapacity(minCapacity int) {
	if minCapacity <= len(handle.items) {
		return
	}
	nextCapacity := len(handle.items)
	if nextCapacity < 1 {
		nextCapacity = 1
	}
	for nextCapacity < minCapacity {
		nextCapacity *= 2
	}
	grown := make([]value.Value, nextCapacity)
	copy(grown, handle.items[:handle.size])
	handle.items = grown
}
