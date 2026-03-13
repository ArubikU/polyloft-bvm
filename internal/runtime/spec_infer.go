package runtime

import (
	"sort"

	"github.com/ArubikU/polyloft-bvm/internal/bytecode"
	"github.com/ArubikU/polyloft-bvm/internal/value"
)

func InferSpec(name string, val value.Value) Spec {
	switch val.Kind {
	case value.Number:
		if val.NumberKind == value.NumberInt {
			return Spec{Name: name, TypeName: TypeInt}
		}
		return Spec{Name: name, TypeName: TypeFloat}
	case value.Bool:
		return Spec{Name: name, TypeName: TypeBool}
	case value.String:
		return Spec{Name: name, TypeName: TypeString}
	case value.Nil:
		return Spec{Name: name, TypeName: TypeNil}
	}

	if builtin, ok := val.AsBuiltin(); ok {
		params := make([]string, 0)
		variadic := builtin.Arity < 0
		if builtin.Arity > 0 {
			params = make([]string, builtin.Arity)
			for i := range params {
				params[i] = TypeAny
			}
		}
		return Spec{Name: name, TypeName: TypeFunction, Callable: &CallableSpec{Params: params, Return: TypeAny, Variadic: variadic}}
	}
	if closure, ok := val.AsClosure(); ok {
		return callableSpec(name, closure.Function)
	}
	if fn, ok := val.AsFunction(); ok {
		return callableSpec(name, fn)
	}
	if module, ok := val.AsModule(); ok {
		members := make(map[string]Spec, len(module.Members))
		for memberName, memberValue := range module.Members {
			members[memberName] = InferSpec(module.Name+"."+memberName, memberValue)
		}
		return Spec{Name: name, TypeName: TypeModule, Module: &ModuleSpec{Name: module.Name, Members: members}}
	}
	if class, ok := val.AsClass(); ok {
		return classSpec(name, class)
	}
	if _, ok := val.AsArray(); ok {
		return Spec{Name: name, TypeName: TypeArray}
	}
	if _, ok := val.AsMap(); ok {
		return Spec{Name: name, TypeName: TypeMap}
	}
	if _, ok := val.AsTuple(); ok {
		return Spec{Name: name, TypeName: TypeTuple}
	}
	if _, ok := val.AsRange(); ok {
		return Spec{Name: name, TypeName: TypeRange}
	}
	if instance, ok := val.AsInstance(); ok {
		return Spec{Name: name, TypeName: instance.Class.Name}
	}
	return Spec{Name: name, TypeName: TypeAny}
}

func callableSpec(name string, fn *bytecode.Function) Spec {
	params := make([]string, len(fn.ParamTypes))
	copy(params, fn.ParamTypes)
	ret := fn.ReturnType
	if ret == "" {
		ret = TypeAny
	}
	for i, param := range params {
		if param == "" {
			params[i] = TypeAny
		}
	}
	return Spec{Name: name, TypeName: TypeFunction, Callable: &CallableSpec{Params: params, Return: ret}}
}

func callableSignature(fn *bytecode.Function, defaultReturn string) *CallableSpec {
	if fn == nil {
		return nil
	}
	params := make([]string, len(fn.ParamTypes))
	copy(params, fn.ParamTypes)
	for i, param := range params {
		if param == "" {
			params[i] = TypeAny
		}
	}
	ret := fn.ReturnType
	if ret == "" {
		ret = defaultReturn
	}
	return &CallableSpec{Params: params, Return: ret}
}

func callableOverloadSignatures(overloads []*bytecode.Function, defaultReturn string) []*CallableSpec {
	if len(overloads) == 0 {
		return nil
	}
	result := make([]*CallableSpec, 0, len(overloads))
	for _, overload := range overloads {
		signature := callableSignature(overload, defaultReturn)
		if signature == nil {
			continue
		}
		result = append(result, signature)
	}
	if len(result) <= 1 {
		return nil
	}
	for _, signature := range result {
		signature.Overloaded = true
	}
	return result
}

func classSpec(name string, class *value.Class) Spec {
	staticMembers := make(map[string]Spec)
	instanceMembers := make(map[string]Spec)
	collectClassMembers(class, staticMembers, instanceMembers)
	constructor := &CallableSpec{Params: []string{}, Return: class.Name}
	if signature := callableSignature(class.Constructor, class.Name); signature != nil {
		constructor = signature
	}
	constructorOverloads := make([]*CallableSpec, 0, len(class.ConstructorOverloads))
	for _, overload := range class.ConstructorOverloads {
		signature := callableSignature(overload, class.Name)
		if signature != nil {
			constructorOverloads = append(constructorOverloads, signature)
		}
	}
	return Spec{
		Name:                  name,
		TypeName:              class.Name,
		Callable:              constructor,
		ConstructorOverloads:  constructorOverloads,
		ConstructorVisibility: class.ConstructorVisibility,
		Members:               staticMembers,
		InstanceMembers:       instanceMembers,
		IsAbstract:            class.IsAbstract,
		IsSealed:              class.IsSealed,
		IsRecord:              class.IsRecord,
		Permits:               sortedPermitNames(class.Permits),
	}
}

func sortedPermitNames(permits map[string]bool) []string {
	if len(permits) == 0 {
		return nil
	}
	names := make([]string, 0, len(permits))
	for name := range permits {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func collectClassMembers(class *value.Class, staticMembers map[string]Spec, instanceMembers map[string]Spec) {
	if class == nil {
		return
	}
	collectClassMembers(class.Superclass, staticMembers, instanceMembers)
	for name, field := range class.Fields {
		typeName := field.TypeName
		if typeName == "" {
			typeName = TypeAny
		}
		instanceMembers[name] = Spec{Name: class.Name + "." + name, TypeName: typeName}
	}
	for name, method := range class.Methods {
		spec := callableSpec(class.Name+"."+name, method)
		if overloads := callableOverloadSignatures(class.MethodOverloads[name], class.Name); len(overloads) > 0 {
			spec.Callable.Overloaded = true
			spec.Callable.Overloads = overloads
		}
		instanceMembers[name] = spec
	}
	for name, field := range class.StaticFields {
		typeName := field.TypeName
		if typeName == "" {
			typeName = TypeAny
		}
		staticMembers[name] = Spec{Name: class.Name + "." + name, TypeName: typeName}
	}
	for name, method := range class.StaticMethods {
		spec := callableSpec(class.Name+"."+name, method)
		if overloads := callableOverloadSignatures(class.StaticMethodOverloads[name], class.Name); len(overloads) > 0 {
			spec.Callable.Overloaded = true
			spec.Callable.Overloads = overloads
		}
		staticMembers[name] = spec
	}
}
