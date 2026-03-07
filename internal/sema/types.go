package sema

import "github.com/ArubikU/polyloft-bvm/internal/runtime"

type Type struct {
	Name     string
	Callable *CallableType
	Module   *runtime.ModuleSpec
	Members  map[string]Type
	Tuple    []Type
}

type CallableType struct {
	Params   []Type
	Return   Type
	Variadic bool
}

func Primitive(name string) Type {
	switch name {
	case "int", "float", "number", "Int", "Float", "Number":
		return Type{Name: runtime.TypeNumber}
	case "bool", "Bool", "boolean", "Boolean":
		return Type{Name: runtime.TypeBool}
	case "string", "String":
		return Type{Name: runtime.TypeString}
	case "nil", "Nil":
		return Type{Name: runtime.TypeNil}
	case "void", "Void":
		return Type{Name: runtime.TypeVoid}
	case "range", "Range":
		return Type{Name: runtime.TypeRange}
	case "tuple", "Tuple":
		return Type{Name: runtime.TypeTuple}
	case "any", "Any", "":
		return Type{Name: runtime.TypeAny}
	default:
		return Type{Name: name}
	}
}

func Any() Type {
	return Type{Name: runtime.TypeAny}
}

func Unknown() Type {
	return Type{Name: "Unknown"}
}

func TupleOf(elements []Type) Type {
	return Type{Name: runtime.TypeTuple, Tuple: elements}
}

func (t Type) IsAssignableFrom(other Type) bool {
	if t.Name == runtime.TypeTuple && other.Name == runtime.TypeTuple {
		if len(t.Tuple) != len(other.Tuple) {
			return false
		}
		for i := range t.Tuple {
			if !t.Tuple[i].IsAssignableFrom(other.Tuple[i]) {
				return false
			}
		}
		return true
	}
	if t.Name == runtime.TypeAny || other.Name == runtime.TypeAny || other.Name == "Unknown" {
		return true
	}
	if t.Name == other.Name {
		return true
	}
	if t.Name == runtime.TypeNumber && (other.Name == "Int" || other.Name == "Float") {
		return true
	}
	return false
}

func (t Type) SupportsIterable() bool {
	switch t.Name {
	case runtime.TypeAny, runtime.TypeRange, runtime.TypeTuple:
		return true
	}
	length, hasLength := t.Members["__length"]
	getter, hasGetter := t.Members["__get"]
	return hasLength && hasGetter && length.Callable != nil && getter.Callable != nil
}

func (t Type) SupportsUnstructured() bool {
	switch t.Name {
	case runtime.TypeAny, runtime.TypeTuple:
		return true
	}
	pieces, hasPieces := t.Members["__pieces"]
	getter, hasGetter := t.Members["__get_piece"]
	if !hasGetter {
		getter, hasGetter = t.Members["__getPiece"]
	}
	return hasPieces && hasGetter && pieces.Callable != nil && getter.Callable != nil
}

func (t Type) IterableItemType() Type {
	switch t.Name {
	case runtime.TypeRange:
		return Primitive(runtime.TypeNumber)
	case runtime.TypeTuple:
		if len(t.Tuple) == 0 {
			return Any()
		}
		item := t.Tuple[0]
		for _, candidate := range t.Tuple[1:] {
			if item.Name != candidate.Name {
				return Any()
			}
		}
		return item
	case runtime.TypeAny:
		return Any()
	default:
		getter := t.Members["__get"]
		if getter.Callable != nil {
			return getter.Callable.Return
		}
		return Any()
	}
}

func (t Type) DestructureTypes(count int) ([]Type, bool) {
	switch t.Name {
	case runtime.TypeAny:
		items := make([]Type, count)
		for i := range items {
			items[i] = Any()
		}
		return items, true
	case runtime.TypeTuple:
		if len(t.Tuple) != count {
			return nil, false
		}
		items := make([]Type, len(t.Tuple))
		copy(items, t.Tuple)
		return items, true
	default:
		if !t.SupportsUnstructured() {
			return nil, false
		}
		getter := t.Members["__get_piece"]
		if getter.Callable == nil {
			getter = t.Members["__getPiece"]
		}
		item := Any()
		if getter.Callable != nil {
			item = getter.Callable.Return
		}
		items := make([]Type, count)
		for i := range items {
			items[i] = item
		}
		return items, true
	}
}
