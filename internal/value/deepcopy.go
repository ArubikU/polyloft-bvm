package value

import "github.com/ArubikU/polyloft-bvm/internal/bytecode"

func DeepCopy(v Value) Value {
	visited := make(map[any]Value)
	return deepCopyValue(v, visited)
}

func deepCopyValue(v Value, visited map[any]Value) Value {
	switch v.Kind {
	case Nil, Number, Bool, Char, String:
		return v
	}
	if v.Object == nil {
		return v
	}
	if cached, ok := visited[v.Object]; ok {
		return cached
	}
	switch obj := v.Object.(type) {
	case *Tuple:
		clone := &Tuple{Elements: make([]Value, len(obj.Elements))}
		copied := ObjectValue(clone)
		visited[obj] = copied
		for i, element := range obj.Elements {
			clone.Elements[i] = deepCopyValue(element, visited)
		}
		return copied
	case *Array:
		if obj.AKind != ArrAny {
			// Dense primitive array: elements are scalar value types with no
			// nested references, so a shallow copy of the backing preserves both
			// correctness and density.
			clone := &Array{AKind: obj.AKind}
			switch obj.AKind {
			case ArrInt:
				clone.ints = append([]int64(nil), obj.ints...)
			case ArrFloat:
				clone.floats = append([]float64(nil), obj.floats...)
			case ArrBool:
				clone.bools = append([]bool(nil), obj.bools...)
			}
			copied := ObjectValue(clone)
			visited[obj] = copied
			return copied
		}
		vals := obj.Values()
		clone := NewArray(make([]Value, len(vals)))
		copied := ObjectValue(clone)
		visited[obj] = copied
		for i, element := range vals {
			clone.elems[i] = deepCopyValue(element, visited)
		}
		return copied
	case *Map:
		clone := &Map{Entries: make(map[string]Value, len(obj.Entries))}
		copied := ObjectValue(clone)
		visited[obj] = copied
		for key, element := range obj.Entries {
			clone.Entries[key] = deepCopyValue(element, visited)
		}
		return copied
	case *Instance:
		clone := &Instance{Class: obj.Class, Frozen: obj.Frozen}
		n := len(obj.Fields)
		if n <= 4 {
			clone.Fields = clone.Inline[:n]
		} else {
			clone.Fields = make([]Value, n)
		}
		copied := ObjectValue(clone)
		visited[obj] = copied
		for i, field := range obj.Fields {
			clone.Fields[i] = deepCopyValue(field, visited)
		}
		return copied
	case *Cell:
		clone := &Cell{}
		copied := ObjectValue(clone)
		visited[obj] = copied
		clone.Value = deepCopyValue(obj.Value, visited)
		return copied
	case *Closure:
		clone := &Closure{Function: obj.Function, Captures: make([]*Cell, len(obj.Captures))}
		copied := ObjectValue(clone)
		visited[obj] = copied
		for i, capture := range obj.Captures {
			captured := deepCopyValue(ObjectValue(capture), visited)
			clone.Captures[i], _ = captured.Object.(*Cell)
		}
		return copied
	case *BoundMethod:
		receiverCopy := deepCopyValue(ObjectValue(obj.Receiver), visited)
		receiver, _ := receiverCopy.Object.(*Instance)
		clone := &BoundMethod{Receiver: receiver, Name: obj.Name, Method: obj.Method, Owner: obj.Owner}
		copied := ObjectValue(clone)
		visited[obj] = copied
		return copied
	case *BoundStaticMethod:
		clone := &BoundStaticMethod{Class: obj.Class, Name: obj.Name, Owner: obj.Owner}
		copied := ObjectValue(clone)
		visited[obj] = copied
		return copied
	case *SAMWrapper:
		clone := &SAMWrapper{InterfaceName: obj.InterfaceName, MethodName: obj.MethodName, Callable: deepCopyValue(obj.Callable, visited)}
		copied := ObjectValue(clone)
		visited[obj] = copied
		return copied
	case *SAMBoundMethod:
		wrapperCopy := deepCopyValue(ObjectValue(obj.Wrapper), visited)
		wrapper, _ := wrapperCopy.Object.(*SAMWrapper)
		clone := &SAMBoundMethod{Wrapper: wrapper}
		copied := ObjectValue(clone)
		visited[obj] = copied
		return copied
	case *Range:
		clone := &Range{Start: obj.Start, End: obj.End, Step: obj.Step}
		copied := ObjectValue(clone)
		visited[obj] = copied
		return copied
	case *Iterator:
		clone := &Iterator{Current: obj.Current, Started: obj.Started, Index: obj.Index, Length: obj.Length, GetFn: obj.GetFn}
		copied := ObjectValue(clone)
		visited[obj] = copied
		if obj.Range != nil {
			rangeCopy := deepCopyValue(ObjectValue(obj.Range), visited)
			clone.Range, _ = rangeCopy.Object.(*Range)
		}
		if obj.Receiver != nil {
			receiverCopy := deepCopyValue(ObjectValue(obj.Receiver), visited)
			clone.Receiver, _ = receiverCopy.Object.(*Instance)
		}
		if obj.Items != nil {
			clone.Items = make([]Value, len(obj.Items))
			for i, item := range obj.Items {
				clone.Items[i] = deepCopyValue(item, visited)
			}
		}
		if obj.Arr != nil {
			arrCopy := deepCopyValue(ObjectValue(obj.Arr), visited)
			clone.Arr, _ = arrCopy.Object.(*Array)
		}
		return copied
	case *bytecode.Function, *Builtin, *Module, *Class:
		return v
	default:
		// Native runtime handles such as futures/channels are intentionally shared.
		return v
	}
}
