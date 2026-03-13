package runtime

import (
	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/ArubikU/polyloft-bvm/internal/value"
)

var (
	mathRandomMu  sync.Mutex
	mathRandomGen = rand.New(rand.NewSource(time.Now().UnixNano()))
)

func BuildMathModule() *RuntimeModule {
	builder := NewModuleBuilder("Math")

	builder.AddTypedValue("PI", value.FloatValue(math.Pi), TypeFloat)
	builder.AddTypedValue("E", value.FloatValue(math.E), TypeFloat)

	builder.AddTypedFunction("abs", []string{TypeNumber}, TypeFloat, false, func(args []value.Value) (value.Value, error) {
		return value.FloatValue(math.Abs(args[0].Num)), nil
	})

	builder.AddTypedFunction("floor", []string{TypeNumber}, TypeFloat, false, func(args []value.Value) (value.Value, error) {
		return value.FloatValue(math.Floor(args[0].Num)), nil
	})

	builder.AddTypedFunction("ceil", []string{TypeNumber}, TypeFloat, false, func(args []value.Value) (value.Value, error) {
		return value.FloatValue(math.Ceil(args[0].Num)), nil
	})

	builder.AddTypedFunction("round", []string{TypeNumber}, TypeFloat, false, func(args []value.Value) (value.Value, error) {
		return value.FloatValue(math.Round(args[0].Num)), nil
	})

	builder.AddTypedFunction("sqrt", []string{TypeNumber}, TypeFloat, false, func(args []value.Value) (value.Value, error) {
		if args[0].Num < 0 {
			return value.NilValue(), fmt.Errorf("sqrt expects a non-negative number")
		}
		return value.FloatValue(math.Sqrt(args[0].Num)), nil
	})

	builder.AddTypedFunction("pow", []string{TypeNumber, TypeNumber}, TypeFloat, false, func(args []value.Value) (value.Value, error) {
		return value.FloatValue(math.Pow(args[0].Num, args[1].Num)), nil
	})

	builder.AddTypedFunction("sin", []string{TypeNumber}, TypeFloat, false, func(args []value.Value) (value.Value, error) {
		return value.FloatValue(math.Sin(args[0].Num)), nil
	})

	builder.AddTypedFunction("cos", []string{TypeNumber}, TypeFloat, false, func(args []value.Value) (value.Value, error) {
		return value.FloatValue(math.Cos(args[0].Num)), nil
	})

	builder.AddTypedFunction("tan", []string{TypeNumber}, TypeFloat, false, func(args []value.Value) (value.Value, error) {
		return value.FloatValue(math.Tan(args[0].Num)), nil
	})

	builder.AddTypedFunction("min", []string{TypeNumber, TypeNumber}, TypeFloat, false, func(args []value.Value) (value.Value, error) {
		return value.FloatValue(math.Min(args[0].Num, args[1].Num)), nil
	})

	builder.AddTypedFunction("max", []string{TypeNumber, TypeNumber}, TypeFloat, false, func(args []value.Value) (value.Value, error) {
		return value.FloatValue(math.Max(args[0].Num, args[1].Num)), nil
	})

	builder.AddTypedFunction("clamp", []string{TypeNumber, TypeNumber, TypeNumber}, TypeFloat, false, func(args []value.Value) (value.Value, error) {
		valueNum := args[0].Num
		minNum := args[1].Num
		maxNum := args[2].Num
		if minNum > maxNum {
			minNum, maxNum = maxNum, minNum
		}
		return value.FloatValue(math.Min(math.Max(valueNum, minNum), maxNum)), nil
	})

	builder.AddTypedFunction("random", []string{}, TypeFloat, false, func(args []value.Value) (value.Value, error) {
		mathRandomMu.Lock()
		defer mathRandomMu.Unlock()
		return value.FloatValue(mathRandomGen.Float64()), nil
	})

	return builder.Build()
}
