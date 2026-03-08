package sema

import "github.com/ArubikU/polyloft-bvm/internal/runtime"

type Type struct {
	Name                  string
	Callable              *CallableType
	ConstructorVisibility string
	Module                *runtime.ModuleSpec
	Members               map[string]Type
	Args                  []Type
	Union                 []Type
	Bounds                []Type
	Tuple                 []Type
	IsAbstract            bool
	IsEnum                bool
	IsSealed              bool
	IsInterface           bool
	IsTypeParam           bool
	IsRecord              bool
	Permits               map[string]bool
	WildcardKind          string
	BoundType             *Type
}

type CallableType struct {
	Params   []Type
	Return   Type
	Variadic bool
}

func callableMember(params []Type, ret Type) Type {
	return Type{Callable: &CallableType{Params: params, Return: ret}}
}

func TypeVariable(name string, bounds []Type) Type {
	return Type{Name: name, IsTypeParam: true, Bounds: bounds}
}

func WildcardAny() Type {
	return Type{Name: "?", WildcardKind: "any"}
}

func WildcardExtends(bound Type) Type {
	boundCopy := bound
	return Type{Name: "?", WildcardKind: "extends", BoundType: &boundCopy}
}

func WildcardSuper(bound Type) Type {
	boundCopy := bound
	return Type{Name: "?", WildcardKind: "super", BoundType: &boundCopy}
}

func cloneMembers(members map[string]Type) map[string]Type {
	if len(members) == 0 {
		return nil
	}
	cloned := make(map[string]Type, len(members))
	for name, member := range members {
		cloned[name] = member
	}
	return cloned
}

func joinTypeNames(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	joined := parts[0]
	for i := 1; i < len(parts); i++ {
		joined += " | " + parts[i]
	}
	return joined
}

func typeDisplayName(t Type) string {
	if len(t.Union) > 0 {
		parts := make([]string, len(t.Union))
		for i, option := range t.Union {
			parts[i] = typeDisplayName(option)
		}
		return joinTypeNames(parts)
	}
	if t.WildcardKind != "" {
		switch t.WildcardKind {
		case "any":
			return "?"
		case "extends":
			if t.BoundType != nil {
				return "? extends " + typeDisplayName(*t.BoundType)
			}
		case "super":
			if t.BoundType != nil {
				return "? super " + typeDisplayName(*t.BoundType)
			}
		}
	}
	if len(t.Args) > 0 {
		parts := make([]string, len(t.Args))
		for i, arg := range t.Args {
			parts[i] = typeDisplayName(arg)
		}
		return t.Name + "<" + joinTypeNames(parts) + ">"
	}
	return t.Name
}

func UnionOf(options ...Type) Type {
	flat := make([]Type, 0, len(options))
	for _, option := range options {
		if option.Name == runtime.TypeAny {
			return Any()
		}
		if len(option.Union) > 0 {
			flat = append(flat, option.Union...)
			continue
		}
		duplicate := false
		for _, existing := range flat {
			if existing.ExactlyMatches(option) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			flat = append(flat, option)
		}
	}
	if len(flat) == 0 {
		return Any()
	}
	if len(flat) == 1 {
		return flat[0]
	}
	union := Type{Union: flat}
	union.Name = typeDisplayName(union)
	return union
}

func substituteType(t Type, mapping map[string]Type) Type {
	if t.IsTypeParam {
		if resolved, ok := mapping[t.Name]; ok {
			return resolved
		}
	}
	if t.BoundType != nil {
		bound := substituteType(*t.BoundType, mapping)
		t.BoundType = &bound
	}
	if len(t.Bounds) > 0 {
		bounds := make([]Type, len(t.Bounds))
		for i, bound := range t.Bounds {
			bounds[i] = substituteType(bound, mapping)
		}
		t.Bounds = bounds
	}
	if len(t.Union) > 0 {
		options := make([]Type, len(t.Union))
		for i, option := range t.Union {
			options[i] = substituteType(option, mapping)
		}
		return UnionOf(options...)
	}
	if len(t.Args) > 0 {
		args := make([]Type, len(t.Args))
		for i, arg := range t.Args {
			args[i] = substituteType(arg, mapping)
		}
		t.Args = args
	}
	if len(t.Tuple) > 0 {
		tuple := make([]Type, len(t.Tuple))
		for i, element := range t.Tuple {
			tuple[i] = substituteType(element, mapping)
		}
		t.Tuple = tuple
	}
	if t.Callable != nil {
		params := make([]Type, len(t.Callable.Params))
		for i, param := range t.Callable.Params {
			params[i] = substituteType(param, mapping)
		}
		ret := substituteType(t.Callable.Return, mapping)
		t.Callable = &CallableType{Params: params, Return: ret, Variadic: t.Callable.Variadic}
	}
	if len(t.Members) > 0 {
		members := make(map[string]Type, len(t.Members))
		for name, member := range t.Members {
			members[name] = substituteType(member, mapping)
		}
		t.Members = members
	}
	return t
}

func genericArgMatches(target Type, source Type) bool {
	if target.WildcardKind != "" {
		return target.IsAssignableFrom(source)
	}
	if source.WildcardKind != "" {
		return false
	}
	return target.IsAssignableFrom(source)
}

func (t Type) ExactlyMatches(other Type) bool {
	if len(t.Union) > 0 || len(other.Union) > 0 {
		if len(t.Union) != len(other.Union) {
			return false
		}
		matched := make([]bool, len(other.Union))
		for _, left := range t.Union {
			found := false
			for j, right := range other.Union {
				if matched[j] {
					continue
				}
				if left.ExactlyMatches(right) {
					matched[j] = true
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}
	if t.IsTypeParam || other.IsTypeParam {
		return t.Name == other.Name && t.IsTypeParam == other.IsTypeParam
	}
	if t.WildcardKind != other.WildcardKind {
		return false
	}
	if t.WildcardKind != "" {
		if t.BoundType == nil || other.BoundType == nil {
			return t.BoundType == nil && other.BoundType == nil
		}
		return t.BoundType.ExactlyMatches(*other.BoundType)
	}
	if t.Name != other.Name {
		return false
	}
	if len(t.Args) != len(other.Args) {
		return false
	}
	for i := range t.Args {
		if !t.Args[i].ExactlyMatches(other.Args[i]) {
			return false
		}
	}
	if len(t.Tuple) != len(other.Tuple) {
		return false
	}
	for i := range t.Tuple {
		if !t.Tuple[i].ExactlyMatches(other.Tuple[i]) {
			return false
		}
	}
	return true
}

func ArrayOf(element Type) Type {
	t := Primitive(runtime.TypeArray)
	t.Args = []Type{element}
	if t.Members != nil {
		t.Members = cloneMembers(t.Members)
		getter := t.Members["__get"]
		getter.Callable.Return = element
		t.Members["__get"] = getter
		setter := t.Members["__set"]
		setter.Callable.Params[1] = element
		t.Members["__set"] = setter
		contains := t.Members["__contains"]
		contains.Callable.Params[0] = element
		t.Members["__contains"] = contains
		slice := t.Members["__slice"]
		slice.Callable.Return = t
		t.Members["__slice"] = slice
	}
	return t
}

func MapOf(key Type, value Type) Type {
	t := Primitive(runtime.TypeMap)
	t.Args = []Type{key, value}
	if t.Members != nil {
		t.Members = cloneMembers(t.Members)
		getter := t.Members["__get"]
		getter.Callable.Params[0] = key
		getter.Callable.Return = value
		t.Members["__get"] = getter
		setter := t.Members["__set"]
		setter.Callable.Params[0] = key
		setter.Callable.Params[1] = value
		t.Members["__set"] = setter
		contains := t.Members["__contains"]
		contains.Callable.Params[0] = key
		t.Members["__contains"] = contains
	}
	return t
}

func applyTypeArgs(base Type, args []Type) Type {
	if len(args) == 0 {
		return base
	}
	if len(base.Args) > 0 {
		mapping := make(map[string]Type, len(base.Args))
		for i := range base.Args {
			if i >= len(args) {
				break
			}
			if base.Args[i].IsTypeParam {
				mapping[base.Args[i].Name] = args[i]
			}
		}
		specialized := substituteType(base, mapping)
		specialized.Args = args
		base = specialized
	}
	base.Args = args
	switch base.Name {
	case runtime.TypeArray:
		return ArrayOf(args[0])
	case runtime.TypeMap:
		if len(args) >= 2 {
			return MapOf(args[0], args[1])
		}
	case runtime.TypeTuple:
		return TupleOf(args)
	case "Iterable":
		base.Members = cloneMembers(base.Members)
		getter := base.Members["__get"]
		getter.Callable.Return = args[0]
		base.Members["__get"] = getter
	case "Unstructured":
		base.Members = cloneMembers(base.Members)
		getter := base.Members["__get_piece"]
		if len(args) == 1 {
			getter.Callable.Return = args[0]
		}
		base.Members["__get_piece"] = getter
	case "Sliceable":
		base.Members = cloneMembers(base.Members)
		slice := base.Members["__slice"]
		slice.Callable.Return = args[0]
		base.Members["__slice"] = slice
	case "Indexable":
		if len(args) >= 2 {
			base.Members = cloneMembers(base.Members)
			getter := base.Members["__get"]
			getter.Callable.Params[0] = args[0]
			getter.Callable.Return = args[1]
			base.Members["__get"] = getter
			setter := base.Members["__set"]
			setter.Callable.Params[0] = args[0]
			setter.Callable.Params[1] = args[1]
			base.Members["__set"] = setter
			contains := base.Members["__contains"]
			contains.Callable.Params[0] = args[0]
			base.Members["__contains"] = contains
		}
	case "Collection":
		base.Members = cloneMembers(base.Members)
		item := args[0]
		for _, methodName := range []string{"add", "remove", "contains"} {
			member := base.Members[methodName]
			member.Callable.Params[0] = item
			base.Members[methodName] = member
		}
		asArray := base.Members["asArray"]
		asArray.Callable.Return = ArrayOf(item)
		base.Members["asArray"] = asArray
	}
	return base
}

func builtinMembers(name string) map[string]Type {
	switch name {
	case runtime.TypeRange:
		return map[string]Type{
			"__length": callableMember(nil, Type{Name: runtime.TypeInt}),
			"__get":    callableMember([]Type{{Name: runtime.TypeInt}}, Type{Name: runtime.TypeInt}),
		}
	case runtime.TypeArray:
		return map[string]Type{
			"__get":      callableMember([]Type{{Name: runtime.TypeInt}}, Any()),
			"__set":      callableMember([]Type{{Name: runtime.TypeInt}, Any()}, Type{Name: runtime.TypeVoid}),
			"__contains": callableMember([]Type{Any()}, Type{Name: runtime.TypeBool}),
			"__slice":    callableMember([]Type{{Name: runtime.TypeInt}, {Name: runtime.TypeInt}}, Type{Name: runtime.TypeArray}),
		}
	case runtime.TypeMap:
		return map[string]Type{
			"__get":      callableMember([]Type{{Name: runtime.TypeString}}, Any()),
			"__set":      callableMember([]Type{{Name: runtime.TypeString}, Any()}, Type{Name: runtime.TypeVoid}),
			"__contains": callableMember([]Type{{Name: runtime.TypeString}}, Type{Name: runtime.TypeBool}),
		}
	case runtime.TypeString:
		return map[string]Type{
			"__slice": callableMember([]Type{{Name: runtime.TypeInt}, {Name: runtime.TypeInt}}, Type{Name: runtime.TypeString}),
		}
	case runtime.TypeTuple:
		return map[string]Type{
			"__pieces":    callableMember(nil, Type{Name: runtime.TypeInt}),
			"__get_piece": callableMember([]Type{{Name: runtime.TypeInt}}, Any()),
			"__slice":     callableMember([]Type{{Name: runtime.TypeInt}, {Name: runtime.TypeInt}}, Type{Name: runtime.TypeTuple}),
		}
	default:
		return nil
	}
}

func Primitive(name string) Type {
	var primitive Type
	switch name {
	case runtime.TypeInt:
		primitive = Type{Name: runtime.TypeInt}
	case runtime.TypeFloat:
		primitive = Type{Name: runtime.TypeFloat}
	case runtime.TypeNumber:
		primitive = Type{Name: runtime.TypeNumber}
	case runtime.TypeBool:
		primitive = Type{Name: runtime.TypeBool}
	case runtime.TypeChar:
		primitive = Type{Name: runtime.TypeChar}
	case runtime.TypeString:
		primitive = Type{Name: runtime.TypeString}
	case runtime.TypeNil:
		primitive = Type{Name: runtime.TypeNil}
	case runtime.TypeVoid:
		primitive = Type{Name: runtime.TypeVoid}
	case runtime.TypeRange:
		primitive = Type{Name: runtime.TypeRange}
	case runtime.TypeTuple:
		primitive = Type{Name: runtime.TypeTuple}
	case runtime.TypeArray:
		primitive = Type{Name: runtime.TypeArray}
	case runtime.TypeMap:
		primitive = Type{Name: runtime.TypeMap}
	case runtime.TypeAny, "":
		primitive = Type{Name: runtime.TypeAny}
	default:
		primitive = Type{Name: name}
	}
	if members := builtinMembers(primitive.Name); len(members) > 0 {
		primitive.Members = members
	}
	return primitive
}

func Any() Type {
	return Type{Name: runtime.TypeAny}
}

func Unknown() Type {
	return Type{Name: "Unknown"}
}

func TupleOf(elements []Type) Type {
	t := Primitive(runtime.TypeTuple)
	t.Tuple = elements
	t.Args = append([]Type(nil), elements...)
	return t
}

func (t Type) IsAssignableFrom(other Type) bool {
	if len(t.Union) > 0 {
		if len(other.Union) > 0 {
			for _, source := range other.Union {
				accepted := false
				for _, target := range t.Union {
					if target.IsAssignableFrom(source) {
						accepted = true
						break
					}
				}
				if !accepted {
					return false
				}
			}
			return true
		}
		for _, option := range t.Union {
			if option.IsAssignableFrom(other) {
				return true
			}
		}
		return false
	}
	if len(other.Union) > 0 {
		for _, option := range other.Union {
			if !t.IsAssignableFrom(option) {
				return false
			}
		}
		return true
	}
	if t.IsTypeParam && other.IsTypeParam && t.Name == other.Name {
		return true
	}
	if t.WildcardKind != "" {
		switch t.WildcardKind {
		case "any":
			return true
		case "extends":
			return t.BoundType != nil && t.BoundType.IsAssignableFrom(other)
		case "super":
			return t.BoundType != nil && other.IsAssignableFrom(*t.BoundType)
		}
	}
	if t.IsTypeParam {
		if len(t.Bounds) == 0 {
			return true
		}
		for _, bound := range t.Bounds {
			if !bound.IsAssignableFrom(other) {
				return false
			}
		}
		return true
	}
	if t.Callable != nil && other.Callable != nil {
		if len(t.Callable.Params) != len(other.Callable.Params) {
			return false
		}
		for i := range t.Callable.Params {
			if !t.Callable.Params[i].IsAssignableFrom(other.Callable.Params[i]) {
				return false
			}
		}
		return t.Callable.Return.IsAssignableFrom(other.Callable.Return)
	}
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
	if t.Name == other.Name {
		if len(t.Args) == 0 {
			return true
		}
		if len(t.Args) != len(other.Args) {
			return false
		}
		for i := range t.Args {
			if !genericArgMatches(t.Args[i], other.Args[i]) {
				return false
			}
		}
		return true
	}
	if t.Name == runtime.TypeAny || other.Name == runtime.TypeAny || other.Name == "Unknown" {
		return true
	}
	if t.Name == runtime.TypeNumber {
		switch other.Name {
		case runtime.TypeInt, runtime.TypeFloat, runtime.TypeNumber:
			return true
		}
	}
	if t.Name == runtime.TypeFloat && other.Name == runtime.TypeInt {
		return true
	}
	return false
}

func isNumericCompatibleType(name string) bool {
	switch name {
	case runtime.TypeInt, runtime.TypeFloat, runtime.TypeNumber, "Integer", "Double":
		return true
	default:
		return false
	}
}

func isPrimitiveScalarType(name string) bool {
	switch name {
	case runtime.TypeInt, runtime.TypeFloat, runtime.TypeNumber, runtime.TypeBool, runtime.TypeChar, runtime.TypeNil, runtime.TypeVoid:
		return true
	default:
		return false
	}
}

func isBooleanCompatibleType(name string) bool {
	switch name {
	case runtime.TypeBool, "Boolean":
		return true
	default:
		return false
	}
}

func isTextComparableType(name string) bool {
	switch name {
	case runtime.TypeChar, runtime.TypeString:
		return true
	default:
		return false
	}
}

func (t Type) SupportsIterable() bool {
	switch t.Name {
	case runtime.TypeAny:
		return true
	}
	length, hasLength := t.Members["__length"]
	getter, hasGetter := t.Members["__get"]
	return hasLength && hasGetter && length.Callable != nil && getter.Callable != nil
}

func (t Type) SupportsForIn() bool {
	switch t.Name {
	case runtime.TypeAny, runtime.TypeRange, runtime.TypeTuple, runtime.TypeArray, runtime.TypeMap:
		return true
	default:
		return t.SupportsIterable()
	}
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

func (t Type) SupportsIndexing() bool {
	switch t.Name {
	case runtime.TypeAny, runtime.TypeString, runtime.TypeArray, runtime.TypeTuple, runtime.TypeMap:
		return true
	}
	getter, hasGetter := t.Members["__get"]
	return hasGetter && getter.Callable != nil && len(getter.Callable.Params) == 1
}

func (t Type) SupportsIndexAssignment() bool {
	switch t.Name {
	case runtime.TypeAny, runtime.TypeArray, runtime.TypeMap:
		return true
	}
	setter, hasSetter := t.Members["__set"]
	return hasSetter && setter.Callable != nil && len(setter.Callable.Params) == 2
}

func (t Type) SupportsSliceable() bool {
	switch t.Name {
	case runtime.TypeAny, runtime.TypeString, runtime.TypeArray, runtime.TypeTuple:
		return true
	}
	slicer, hasSlicer := t.Members["__slice"]
	return hasSlicer && slicer.Callable != nil && len(slicer.Callable.Params) == 2
}

func (t Type) SupportsContains() bool {
	switch t.Name {
	case runtime.TypeAny, runtime.TypeString, runtime.TypeArray, runtime.TypeMap, runtime.TypeTuple, runtime.TypeRange:
		return true
	}
	contains, hasContains := t.Members["__contains"]
	return hasContains && contains.Callable != nil && len(contains.Callable.Params) == 1
}

func (t Type) IndexKeyType() Type {
	switch t.Name {
	case runtime.TypeString, runtime.TypeArray, runtime.TypeTuple:
		return Primitive(runtime.TypeInt)
	case runtime.TypeMap:
		if len(t.Args) >= 1 {
			return t.Args[0]
		}
		return Primitive(runtime.TypeString)
	case runtime.TypeAny:
		return Any()
	default:
		getter := t.Members["__get"]
		if getter.Callable != nil && len(getter.Callable.Params) == 1 {
			return getter.Callable.Params[0]
		}
		setter := t.Members["__set"]
		if setter.Callable != nil && len(setter.Callable.Params) >= 1 {
			return setter.Callable.Params[0]
		}
		return Any()
	}
}

func (t Type) IndexValueType() Type {
	if t.WildcardKind == "extends" && t.BoundType != nil {
		return *t.BoundType
	}
	if t.WildcardKind != "" {
		return Any()
	}
	switch t.Name {
	case runtime.TypeString:
		return Primitive(runtime.TypeChar)
	case runtime.TypeArray:
		if len(t.Args) >= 1 {
			return t.Args[0]
		}
		return Any()
	case runtime.TypeMap:
		if len(t.Args) >= 2 {
			return t.Args[1]
		}
		return Any()
	case runtime.TypeTuple, runtime.TypeAny:
		return Any()
	default:
		getter := t.Members["__get"]
		if getter.Callable != nil {
			return getter.Callable.Return.IterableItemType()
		}
		return Any()
	}
}

func (t Type) IndexAssignedValueType() Type {
	switch t.Name {
	case runtime.TypeAny, runtime.TypeArray, runtime.TypeMap:
		if t.Name == runtime.TypeArray && len(t.Args) >= 1 {
			return t.Args[0]
		}
		if t.Name == runtime.TypeMap && len(t.Args) >= 2 {
			return t.Args[1]
		}
		return Any()
	default:
		setter := t.Members["__set"]
		if setter.Callable != nil && len(setter.Callable.Params) == 2 {
			return setter.Callable.Params[1]
		}
		return Any()
	}
}

func (t Type) SliceResultType() Type {
	if t.WildcardKind == "extends" && t.BoundType != nil {
		return *t.BoundType
	}
	if t.WildcardKind != "" {
		return Any()
	}
	switch t.Name {
	case runtime.TypeString:
		return Primitive(runtime.TypeString)
	case runtime.TypeArray:
		if len(t.Args) >= 1 {
			return ArrayOf(t.Args[0])
		}
		return Primitive(runtime.TypeArray)
	case runtime.TypeTuple:
		return Primitive(runtime.TypeTuple)
	case runtime.TypeAny:
		return Any()
	default:
		slicer := t.Members["__slice"]
		if slicer.Callable != nil {
			return slicer.Callable.Return
		}
		return Any()
	}
}

func (t Type) ContainsKeyType() Type {
	switch t.Name {
	case runtime.TypeString:
		return Primitive(runtime.TypeString)
	case runtime.TypeArray, runtime.TypeTuple:
		return t.IndexValueType()
	case runtime.TypeMap:
		return t.IndexKeyType()
	case runtime.TypeRange:
		return Primitive(runtime.TypeInt)
	case runtime.TypeAny:
		return Any()
	default:
		contains := t.Members["__contains"]
		if contains.Callable != nil && len(contains.Callable.Params) == 1 {
			return contains.Callable.Params[0]
		}
		return Any()
	}
}

func (t Type) IterableItemType() Type {
	if t.WildcardKind == "extends" && t.BoundType != nil {
		return *t.BoundType
	}
	if t.WildcardKind != "" {
		return Any()
	}
	switch t.Name {
	case runtime.TypeRange:
		return Primitive(runtime.TypeInt)
	case runtime.TypeArray:
		if len(t.Args) >= 1 {
			return t.Args[0]
		}
		return Any()
	case runtime.TypeMap:
		return Primitive(runtime.TypeString)
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
			return getter.Callable.Return.IterableItemType()
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
