package runtime

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/ArubikU/polyloft-bvm/internal/value"
)

func InstallCoreGlobals(registry *Registry, stdout io.Writer) {
	registry.DefineTypedBuiltin("print", []string{TypeAny}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
		_, err := fmt.Fprint(stdout, args[0].String())
		return value.NilValue(), err
	})

	registry.DefineTypedBuiltin("println", []string{TypeAny}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
		_, err := fmt.Fprintln(stdout, args[0].String())
		return value.NilValue(), err
	})

	registry.DefineTypedBuiltin("input", []string{TypeString}, TypeString, true, func(args []value.Value) (value.Value, error) {
		if len(args) > 1 {
			return value.NilValue(), fmt.Errorf("input expects 0 or 1 argument")
		}
		if len(args) == 1 {
			fmt.Fprint(stdout, args[0].String())
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

	registry.DefineTypedBuiltin("delete", []string{TypeMap, TypeString}, TypeBool, false, func(args []value.Value) (value.Value, error) {
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

	registry.DefineTypedBuiltin("keys", []string{TypeMap}, TypeArray, false, func(args []value.Value) (value.Value, error) {
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

	registry.DefineTypedBuiltin("values", []string{TypeMap}, TypeArray, false, func(args []value.Value) (value.Value, error) {
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
}
