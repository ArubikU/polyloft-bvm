package runtime

import (
	"fmt"
	"time"

	"github.com/ArubikU/polyloft-bvm/internal/value"
)

func BuildSysModule() *RuntimeModule {
	return NewModuleBuilder("Sys").
		AddTypedFunction("type", []string{TypeAny}, TypeString, false, func(args []value.Value) (value.Value, error) {
			if GlobalVMProxy == nil {
				return value.StringValue("Unknown"), nil
			}
			return value.StringValue(GlobalVMProxy.TypeOfValue(args[0])), nil
		}).
		AddTypedFunction("instanceof", []string{TypeAny, TypeAny}, TypeBool, false, func(args []value.Value) (value.Value, error) {
			if GlobalVMProxy == nil {
				return value.BoolValue(false), nil
			}
			matched, err := GlobalVMProxy.InstanceOfValue(args[0], args[1])
			if err != nil {
				return value.NilValue(), err
			}
			return value.BoolValue(matched), nil
		}).
		AddTypedFunction("time", nil, TypeInt, false, func(args []value.Value) (value.Value, error) {
			return value.IntValue(time.Now().UnixMilli()), nil
		}).
		AddTypedFunction("sleep", []string{TypeInt}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
			if args[0].Kind != value.Number {
				return value.NilValue(), fmt.Errorf("Sys.sleep expects number of milliseconds")
			}
			time.Sleep(time.Duration(args[0].Num) * time.Millisecond)
			return value.NilValue(), nil
		}).
		Build()
}
