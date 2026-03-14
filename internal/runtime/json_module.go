package runtime

import (
	"encoding/json"
	"fmt"

	"github.com/ArubikU/polyloft-bvm/internal/value"
)

// BuildJsonModule creates the `polyloft.json` native module providing
// json.stringify(any) -> String and json.parse(str) -> any.
func BuildJsonModule() *RuntimeModule {
	builder := NewModuleBuilder("Json")

	// json.stringify(value) -> String
	// Converts a Polyloft value to a JSON string.
	builder.AddTypedFunction("stringify", []string{TypeAny}, TypeString, false, func(args []value.Value) (value.Value, error) {
		native := valueToNative(args[0])
		b, err := json.Marshal(native)
		if err != nil {
			return value.NilValue(), fmt.Errorf("json.stringify: %w", err)
		}
		return value.StringValue(string(b)), nil
	})

	// json.parse(str) -> any
	// Parses a JSON string and converts it to a Polyloft value (map/array/primitives).
	builder.AddTypedFunction("parse", []string{TypeString}, TypeAny, false, func(args []value.Value) (value.Value, error) {
		jsonStr := args[0].Str
		var raw any
		if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
			return value.NilValue(), fmt.Errorf("json.parse: %w", err)
		}
		return nativeToValue(raw), nil
	})

	return builder.Build()
}

// valueToNative converts a Polyloft value into a Go-native type for JSON encoding.
func valueToNative(v value.Value) any {
	switch v.Kind {
	case value.Number:
		if v.NumberKind == value.NumberInt {
			return v.Int
		}
		return v.Num
	case value.Bool:
		return v.Bool
	case value.String:
		return v.Str
	case value.Nil:
		return nil
	case value.Object:
		if m, ok := v.AsMap(); ok {
			result := make(map[string]any, len(m.Entries))
			for k, val := range m.Entries {
				result[k] = valueToNative(val)
			}
			return result
		}
		if arr, ok := v.AsArray(); ok {
			result := make([]any, len(arr.Elements))
			for i, el := range arr.Elements {
				result[i] = valueToNative(el)
			}
			return result
		}
		// Instance: serialize fields
		if inst, ok := v.AsInstance(); ok {
			result := make(map[string]any)
			for i, name := range inst.Class.FieldOrder {
				if i < len(inst.Fields) {
					result[name] = valueToNative(inst.Fields[i])
				}
			}
			return result
		}
		return v.String()
	default:
		return v.String()
	}
}

// nativeToValue converts a Go-native value (from JSON decode) into a Polyloft Value.
func nativeToValue(raw any) value.Value {
	if raw == nil {
		return value.NilValue()
	}
	switch v := raw.(type) {
	case bool:
		return value.BoolValue(v)
	case float64:
		return value.NumberValue(v)
	case string:
		return value.StringValue(v)
	case []any:
		arr := &value.Array{Elements: make([]value.Value, len(v))}
		for i, el := range v {
			arr.Elements[i] = nativeToValue(el)
		}
		return value.ObjectValue(arr)
	case map[string]any:
		entries := make(map[string]value.Value, len(v))
		for k, val := range v {
			entries[k] = nativeToValue(val)
		}
		return value.ObjectValue(&value.Map{Entries: entries})
	default:
		return value.StringValue(fmt.Sprintf("%v", v))
	}
}
