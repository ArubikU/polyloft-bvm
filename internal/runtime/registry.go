package runtime

import "github.com/ArubikU/polyloft-bvm/internal/value"

const (
	TypeAny      = "Any"
	TypeNumber   = "Number"
	TypeBool     = "Bool"
	TypeString   = "String"
	TypeNil      = "Nil"
	TypeVoid     = "Void"
	TypeRange    = "Range"
	TypeTuple    = "Tuple"
	TypeFunction = "Function"
	TypeModule   = "Module"
)

type CallableSpec struct {
	Params   []string
	Return   string
	Variadic bool
}

type Spec struct {
	Name     string
	TypeName string
	Callable *CallableSpec
	Module   *ModuleSpec
}

type ModuleSpec struct {
	Name    string
	Members map[string]Spec
}

type RuntimeModule struct {
	Value *value.Module
	Spec  *ModuleSpec
}

type Registry struct {
	globals map[string]value.Value
	specs   map[string]Spec
}

func NewRegistry() *Registry {
	return &Registry{globals: make(map[string]value.Value), specs: make(map[string]Spec)}
}

func (r *Registry) Globals() map[string]value.Value {
	return r.globals
}

func (r *Registry) Specs() map[string]Spec {
	return r.specs
}

func (r *Registry) Define(name string, val value.Value) {
	r.globals[name] = val
}

func (r *Registry) DefineBuiltin(name string, arity int, fn value.BuiltinFunc) {
	params := make([]string, 0)
	if arity > 0 {
		params = make([]string, arity)
		for i := range params {
			params[i] = TypeAny
		}
	}
	r.DefineTypedBuiltin(name, params, TypeAny, arity < 0, fn)
}

func (r *Registry) DefineTypedBuiltin(name string, params []string, returnType string, variadic bool, fn value.BuiltinFunc) {
	r.Define(name, value.ObjectValue(&value.Builtin{Name: name, Arity: len(params), Fn: fn}))
	if variadic {
		r.globals[name] = value.ObjectValue(&value.Builtin{Name: name, Arity: -1, Fn: fn})
	}
	r.specs[name] = Spec{
		Name:     name,
		TypeName: TypeFunction,
		Callable: &CallableSpec{Params: params, Return: returnType, Variadic: variadic},
	}
}

func (r *Registry) DefineModule(module *RuntimeModule) {
	r.Define(module.Value.Name, value.ObjectValue(module.Value))
	r.specs[module.Value.Name] = Spec{
		Name:     module.Value.Name,
		TypeName: TypeModule,
		Module:   module.Spec,
	}
}

type ModuleBuilder struct {
	module *value.Module
	spec   *ModuleSpec
}

func NewModuleBuilder(name string) *ModuleBuilder {
	return &ModuleBuilder{
		module: &value.Module{
			Name:    name,
			Members: make(map[string]value.Value),
		},
		spec: &ModuleSpec{
			Name:    name,
			Members: make(map[string]Spec),
		},
	}
}

func (b *ModuleBuilder) AddFunction(name string, arity int, fn value.BuiltinFunc) *ModuleBuilder {
	params := make([]string, 0)
	if arity > 0 {
		params = make([]string, arity)
		for i := range params {
			params[i] = TypeAny
		}
	}
	return b.AddTypedFunction(name, params, TypeAny, arity < 0, fn)
}

func (b *ModuleBuilder) AddTypedFunction(name string, params []string, returnType string, variadic bool, fn value.BuiltinFunc) *ModuleBuilder {
	arity := len(params)
	if variadic {
		arity = -1
	}
	b.module.Members[name] = value.ObjectValue(&value.Builtin{
		Name:  b.module.Name + "." + name,
		Arity: arity,
		Fn:    fn,
	})
	b.spec.Members[name] = Spec{
		Name:     b.module.Name + "." + name,
		TypeName: TypeFunction,
		Callable: &CallableSpec{Params: params, Return: returnType, Variadic: variadic},
	}
	return b
}

func (b *ModuleBuilder) AddValue(name string, val value.Value) *ModuleBuilder {
	return b.AddTypedValue(name, val, TypeAny)
}

func (b *ModuleBuilder) AddTypedValue(name string, val value.Value, typeName string) *ModuleBuilder {
	b.module.Members[name] = val
	b.spec.Members[name] = Spec{Name: b.module.Name + "." + name, TypeName: typeName}
	return b
}

func (b *ModuleBuilder) Build() *RuntimeModule {
	return &RuntimeModule{Value: b.module, Spec: b.spec}
}
