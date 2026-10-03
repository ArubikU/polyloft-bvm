package value

import "math"

// SetInt, SetFloat and SetBool overwrite *dst with a scalar Value. When dst holds no
// reference (Str empty, Object nil -- the common case for numeric locals and loop
// variables) only the plain scalar fields are written: no pointer stores, hence no
// GC write barrier and no struct copy. If dst does hold a reference the whole
// Value is replaced so the stale pointer is dropped and never visible under a
// scalar Kind.
func SetInt(dst *Value, v int64) {
	if dst.Object != nil || dst.Str != "" {
		*dst = IntValue(v)
		return
	}
	dst.Kind, dst.NumberKind, dst.Bool = Number, NumberInt, false
	dst.W = v
}

func SetFloat(dst *Value, v float64) {
	if dst.Object != nil || dst.Str != "" {
		*dst = FloatValue(v)
		return
	}
	dst.Kind, dst.NumberKind, dst.Bool = Number, NumberFloat, false
	dst.W = int64(math.Float64bits(v))
}

func SetBool(dst *Value, v bool) {
	if dst.Object != nil || dst.Str != "" {
		*dst = BoolValue(v)
		return
	}
	dst.Kind, dst.NumberKind, dst.Bool = Bool, 0, v
	dst.W = 0
}

// StoreAt writes element i of the array into *dst (scalar-only writes for dense storage).
func (a *Array) StoreAt(i int, dst *Value) {
	switch a.AKind {
	case ArrInt:
		SetInt(dst, a.ints[i])
	case ArrFloat:
		SetFloat(dst, a.floats[i])
	case ArrBool:
		SetBool(dst, a.bools[i])
	default:
		*dst = a.elems[i]
	}
}

// CopyInto copies *src into *dst. When neither side holds a reference (numbers, booleans,
// nil: the bulk of the traffic in numeric code) only the plain scalar fields are written,
// avoiding pointer stores (GC write barriers) and the full struct copy.
func CopyInto(dst, src *Value) {
	if src.Object == nil && src.Str == "" && dst.Object == nil && dst.Str == "" {
		dst.Kind, dst.NumberKind, dst.Bool = src.Kind, src.NumberKind, src.Bool
		dst.W = src.W
		return
	}
	*dst = *src
}
