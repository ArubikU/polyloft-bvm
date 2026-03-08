package runtime

import (
	"fmt"
	"time"

	"github.com/ArubikU/polyloft-bvm/internal/value"
)

func BuildSysModule() *RuntimeModule {
	return NewModuleBuilder("Sys").
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
