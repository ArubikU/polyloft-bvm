package runtime

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/ArubikU/polyloft-bvm/internal/bytecode"
	"github.com/ArubikU/polyloft-bvm/internal/value"
)

func InstallCoreGlobals(registry *Registry, stdout io.Writer) {
	installExceptionClasses(registry)

	registry.DefineTypedBuiltin("print", []string{TypeAny}, TypeVoid, true, func(args []value.Value) (value.Value, error) {
		parts := make([]string, 0, len(args))
		for _, arg := range args {
			text := arg.String()
			if GlobalVMProxy != nil {
				resolved, err := GlobalVMProxy.StringifyValue(arg)
				if err != nil {
					return value.NilValue(), err
				}
				text = resolved
			}
			parts = append(parts, text)
		}
		_, err := fmt.Fprint(stdout, strings.Join(parts, " "))
		return value.NilValue(), err
	})

	registry.DefineTypedBuiltin("println", []string{TypeAny}, TypeVoid, true, func(args []value.Value) (value.Value, error) {
		parts := make([]string, 0, len(args))
		for _, arg := range args {
			text := arg.String()
			if GlobalVMProxy != nil {
				resolved, err := GlobalVMProxy.StringifyValue(arg)
				if err != nil {
					return value.NilValue(), err
				}
				text = resolved
			}
			parts = append(parts, text)
		}
		_, err := fmt.Fprintln(stdout, strings.Join(parts, " "))
		return value.NilValue(), err
	})

	registry.DefineTypedBuiltin("input", []string{TypeString}, TypeString, true, func(args []value.Value) (value.Value, error) {
		if len(args) > 1 {
			return value.NilValue(), fmt.Errorf("input expects 0 or 1 argument")
		}
		if len(args) == 1 {
			text := args[0].String()
			if GlobalVMProxy != nil {
				resolved, err := GlobalVMProxy.StringifyValue(args[0])
				if err != nil {
					return value.NilValue(), err
				}
				text = resolved
			}
			fmt.Fprint(stdout, text)
		}
		reader := bufio.NewReader(os.Stdin)
		text, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return value.NilValue(), err
		}
		return value.StringValue(strings.TrimSuffix(strings.TrimSuffix(text, "\n"), "\r")), nil
	})

	registry.DefineTypedBuiltin("range", []string{TypeInt, TypeInt}, TypeRange, true, func(args []value.Value) (value.Value, error) {
		return value.NilValue(), fmt.Errorf("range is compiled as an opcode and should not be called dynamically")
	})

	registry.DefineTypedBuiltin("len", []string{TypeAny}, TypeInt, false, func(args []value.Value) (value.Value, error) {
		item := args[0]
		switch item.Kind {
		case value.String:
			return value.IntValue(int64(len([]rune(item.Str)))), nil
		}
		if array, ok := item.AsArray(); ok {
			return value.IntValue(int64(len(array.Elements))), nil
		}
		if tuple, ok := item.AsTuple(); ok {
			return value.IntValue(int64(len(tuple.Elements))), nil
		}
		if m, ok := item.AsMap(); ok {
			return value.IntValue(int64(len(m.Entries))), nil
		}
		return value.NilValue(), fmt.Errorf("len expects String, array, tuple, or map")
	})

	registry.DefineTypedBuiltin("__array_new", []string{TypeInt}, TypeArray, false, func(args []value.Value) (value.Value, error) {
		size := args[0]
		if size.Kind != value.Number || size.NumberKind != value.NumberInt {
			return value.NilValue(), fmt.Errorf("__array_new expects an int size")
		}
		if size.Num < 0 {
			return value.NilValue(), fmt.Errorf("__array_new expects a non-negative size")
		}
		items := make([]value.Value, int(size.Num))
		for i := range items {
			items[i] = value.NilValue()
		}
		return value.ObjectValue(&value.Array{Elements: items}), nil
	})

	registry.DefineTypedBuiltin("__list_new", []string{}, TypeAny, false, func(args []value.Value) (value.Value, error) {
		handle := &nativeListHandle{items: make([]value.Value, 10), size: 0}
		return value.ObjectValue(handle), nil
	})

	registry.DefineTypedBuiltin("__list_add", []string{TypeAny, TypeAny}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
		handle, err := asNativeListHandle(args[0])
		if err != nil {
			return value.NilValue(), err
		}
		handle.ensureCapacity(handle.size + 1)
		handle.items[handle.size] = args[1]
		handle.size++
		return value.NilValue(), nil
	})

	registry.DefineTypedBuiltin("__list_as_array", []string{TypeAny}, TypeArray, false, func(args []value.Value) (value.Value, error) {
		handle, err := asNativeListHandle(args[0])
		if err != nil {
			return value.NilValue(), err
		}
		items := append([]value.Value(nil), handle.items[:handle.size]...)
		return value.ObjectValue(&value.Array{Elements: items}), nil
	})

	registry.DefineTypedBuiltin("sqrt", []string{TypeNumber}, TypeFloat, false, func(args []value.Value) (value.Value, error) {
		item := args[0]
		if item.Kind != value.Number {
			return value.NilValue(), fmt.Errorf("sqrt expects a numeric argument")
		}
		if item.Num < 0 {
			return value.NilValue(), fmt.Errorf("sqrt expects a non-negative number")
		}
		return value.FloatValue(math.Sqrt(item.Num)), nil
	})

	registry.DefineGenericBuiltin("delete", []string{"V"}, []string{"map<String, V>", TypeString}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		m, ok := args[0].AsMap()
		if !ok {
			return value.NilValue(), fmt.Errorf("delete expects map as first argument")
		}
		if args[1].Kind != value.String {
			return value.NilValue(), fmt.Errorf("delete expects string as second argument")
		}
		_, existed := m.Entries[args[1].Str]
		delete(m.Entries, args[1].Str)
		return value.BoolValue(existed), nil
	})

	registry.DefineGenericBuiltin("keys", []string{"V"}, []string{"map<String, V>"}, "array<String>", false, func(args []value.Value) (value.Value, error) {
		m, ok := args[0].AsMap()
		if !ok {
			return value.NilValue(), fmt.Errorf("keys expects map")
		}
		ordered := make([]string, 0, len(m.Entries))
		for key := range m.Entries {
			ordered = append(ordered, key)
		}
		sort.Strings(ordered)
		items := make([]value.Value, len(ordered))
		for i, key := range ordered {
			items[i] = value.StringValue(key)
		}
		return value.ObjectValue(&value.Array{Elements: items}), nil
	})

	registry.DefineGenericBuiltin("values", []string{"V"}, []string{"map<String, V>"}, "array<V>", false, func(args []value.Value) (value.Value, error) {
		m, ok := args[0].AsMap()
		if !ok {
			return value.NilValue(), fmt.Errorf("values expects map")
		}
		ordered := make([]string, 0, len(m.Entries))
		for key := range m.Entries {
			ordered = append(ordered, key)
		}
		sort.Strings(ordered)
		items := make([]value.Value, len(ordered))
		for i, key := range ordered {
			items[i] = m.Entries[key]
		}
		return value.ObjectValue(&value.Array{Elements: items}), nil
	})

	registry.DefineTypedBuiltin("hash", []string{TypeAny}, TypeString, false, func(args []value.Value) (value.Value, error) {
		return value.NilValue(), fmt.Errorf("hash is resolved by the VM and should not be called directly")
	})

	registry.DefineModule(BuildSysModule())
	registry.DefineModule(BuildHttpModule())
	registry.DefineModule(BuildJsonModule())
	registry.DefineModule(BuildIoModule())
	registry.DefineModule(BuildMathModule())
	registry.DefineModule(BuildCryptoModule())
	registry.DefineModule(BuildCollectionsModule())
	registry.DefineModule(BuildConcurrentModule())
}

func installExceptionClasses(registry *Registry) {
	base := newExceptionClass("Exception", nil)
	registry.DefineClass("Exception", base, exceptionSpec("Exception", "Exception"))
	for _, name := range []string{"RuntimeError", "NameError", "TypeError", "ValueError", "ArityError", "IndexError", "KeyError", "IOException", "FileNotFoundException", "NetworkError", "TimeoutError"} {
		registry.DefineClass(name, newExceptionClass(name, base), exceptionSpec(name, name))
	}
}

func newExceptionClass(name string, superclass *value.Class) *value.Class {
	message := value.FieldDef{Default: value.StringValue(""), Mutable: true, TypeName: TypeString, Visibility: "public"}
	kind := value.FieldDef{Default: value.StringValue(name), Mutable: false, TypeName: TypeString, Visibility: "public"}
	ctor := &bytecode.Function{Name: name + ".init", Arity: 1}
	return &value.Class{
		ClassDecl: value.ClassDecl{
			Name:                  name,
			Superclass:            superclass,
			Fields:                map[string]value.FieldDef{"message": message, "type": kind},
			FieldOrder:            []string{"message", "type"},
			FieldIndex:            map[string]int{"message": 0, "type": 1},
			StaticFields:          map[string]value.FieldDef{},
			MethodVisibility:      map[string]string{},
			StaticVisibility:      map[string]string{},
			MethodAnnotations:     map[string][]string{},
			ConstructorVisibility: "public",
			Implements:            map[string]bool{},
			Permits:               map[string]bool{},
		},
		ClassRuntime: value.ClassRuntime{
			Methods:              map[string]*bytecode.Function{},
			MethodOverloads:      map[string][]*bytecode.Function{},
			StaticMethods:        map[string]*bytecode.Function{},
			StaticValues:         map[string]value.Value{},
			MethodIndex:          map[string]int{},
			SpecialMethods:       map[value.SpecialMethodSlot]*bytecode.Function{},
			Constructor:          ctor,
			ConstructorOverloads: []*bytecode.Function{ctor},
			FastConstructor:      &value.FastConstructorPlan{Arity: 1, FieldSlots: []int{0}, ArgIndexes: []int{0}},
		},
	}
}

func exceptionSpec(name string, returnType string) Spec {
	return Spec{
		Name:                  name,
		TypeName:              name,
		ConstructorVisibility: "public",
		Callable:              &CallableSpec{Params: []string{TypeString}, Return: returnType},
		ConstructorOverloads:  []*CallableSpec{{Params: []string{TypeString}, Return: returnType}},
		InstanceMembers: map[string]Spec{
			"message": {Name: name + ".message", TypeName: TypeString},
			"type":    {Name: name + ".type", TypeName: TypeString},
		},
	}
}
