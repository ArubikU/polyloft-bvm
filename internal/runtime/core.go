package runtime

import (
	"fmt"
	"io"

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

	registry.DefineTypedBuiltin("range", []string{TypeNumber, TypeNumber}, TypeRange, true, func(args []value.Value) (value.Value, error) {
		return value.NilValue(), fmt.Errorf("range is compiled as an opcode and should not be called dynamically")
	})

	registry.DefineModule(BuildSysModule())
}
