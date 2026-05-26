package sema

import (
	"fmt"
	"strings"

	"github.com/ArubikU/polyloft-bvm/internal/ast"
	"github.com/ArubikU/polyloft-bvm/internal/runtime"
	"github.com/ArubikU/polyloft-bvm/internal/token"
)

type Checker struct {
	registry         *runtime.Registry
	scopes           []map[string]symbol
	typeScopes       []map[string]Type
	currentFunc      *CallableType
	currentClass     *ast.ClassStmt
	currentMethod    *ast.MethodDecl
	interfaces       map[string]Type
	interfaceDecls   map[string]*ast.InterfaceStmt
	typeAliasDecls   map[string]*ast.TypeAliasStmt
	classes          map[string]*ast.ClassStmt
	specTypeCache    map[string]*Type
	instanceCache    map[string]*Type
	resolvingTypes   map[string]bool
	resolvingIfaces  map[string]bool
	resolvingClasses map[string]bool
	insideLoop       int
}

type symbol struct {
	Type    Type
	Mutable bool
}

func Check(program *ast.Program, registry *runtime.Registry) error {
	checker := &Checker{registry: registry, scopes: []map[string]symbol{{}}, typeScopes: []map[string]Type{{}}, interfaces: map[string]Type{}, interfaceDecls: map[string]*ast.InterfaceStmt{}, typeAliasDecls: map[string]*ast.TypeAliasStmt{}, classes: map[string]*ast.ClassStmt{}, specTypeCache: map[string]*Type{}, instanceCache: map[string]*Type{}, resolvingTypes: map[string]bool{}, resolvingIfaces: map[string]bool{}, resolvingClasses: map[string]bool{}}
	for _, stmt := range program.Statements {
		switch node := stmt.(type) {
		case *ast.InterfaceStmt:
			checker.interfaceDecls[node.Name.Lexeme] = node
		case *ast.TypeAliasStmt:
			checker.typeAliasDecls[node.Name.Lexeme] = node
		case *ast.ClassStmt:
			checker.classes[node.Name.Lexeme] = node
		}
	}
	checker.installBuiltins()
	for _, stmt := range program.Statements {
		if iface, ok := stmt.(*ast.InterfaceStmt); ok {
			checker.currentScope()[iface.Name.Lexeme] = symbol{Type: checker.interfaceType(iface.Name.Lexeme, map[string]bool{}), Mutable: false}
		}
	}
	for _, stmt := range program.Statements {
		if fn, ok := stmt.(*ast.FunctionStmt); ok {
			checker.currentScope()[fn.Name.Lexeme] = symbol{Type: checker.functionType(fn), Mutable: false}
		}
		if classStmt, ok := stmt.(*ast.ClassStmt); ok {
			checker.currentScope()[classStmt.Name.Lexeme] = symbol{Type: checker.classType(classStmt), Mutable: false}
		}
	}
	for _, stmt := range program.Statements {
		if err := checker.checkStmt(stmt); err != nil {
			return err
		}
	}
	return nil
}

func unwrapInstanceOfCondition(expr ast.Expr) (*ast.InstanceOfExpr, bool) {
	for {
		grouping, ok := expr.(*ast.GroupingExpr)
		if !ok {
			break
		}
		expr = grouping.Expr
	}
	condition, ok := expr.(*ast.InstanceOfExpr)
	return condition, ok
}

func unwrapInstanceOfConditionAndGuard(expr ast.Expr) (*ast.InstanceOfExpr, ast.Expr, bool) {
	for {
		grouping, ok := expr.(*ast.GroupingExpr)
		if !ok {
			break
		}
		expr = grouping.Expr
	}
	binary, ok := expr.(*ast.BinaryExpr)
	if !ok || binary.Operator.Type != token.AndAnd {
		return nil, nil, false
	}
	condition, ok := unwrapInstanceOfCondition(binary.Left)
	if !ok || condition.Binding == nil {
		return nil, nil, false
	}
	return condition, binary.Right, true
}

func (c *Checker) installBuiltins() {
	for name, spec := range c.registry.Specs() {
		c.currentScope()[name] = symbol{Type: c.typeFromSpec(spec), Mutable: false}
	}
	for name, ifaceType := range builtinInterfaceTypes() {
		c.interfaces[name] = ifaceType
		c.currentScope()[name] = symbol{Type: ifaceType, Mutable: false}
	}
}

func builtinInterfaceTypes() map[string]Type {
	intType := Primitive(runtime.TypeInt)
	numberType := Primitive(runtime.TypeNumber)
	boolType := Primitive(runtime.TypeBool)
	voidType := Primitive(runtime.TypeVoid)
	anyType := Any()
	itemType := TypeVariable("T", nil)
	keyType := TypeVariable("K", nil)
	valueType := TypeVariable("V", nil)
	leftType := TypeVariable("L", nil)
	rightType := TypeVariable("R", nil)
	secondType := TypeVariable("U", nil)
	return map[string]Type{
		"Predicate": {
			Name:        "Predicate",
			Args:        []Type{itemType},
			IsInterface: true,
			Members: map[string]Type{
				"test": {Callable: &CallableType{Params: []Type{itemType}, Return: boolType}},
			},
		},
		"Consumer": {
			Name:        "Consumer",
			Args:        []Type{itemType},
			IsInterface: true,
			Members: map[string]Type{
				"accept": {Callable: &CallableType{Params: []Type{itemType}, Return: voidType}},
			},
		},
		"Supplier": {
			Name:        "Supplier",
			Args:        []Type{itemType},
			IsInterface: true,
			Members: map[string]Type{
				"get": {Callable: &CallableType{Return: itemType}},
			},
		},
		"Runnable": {
			Name:        "Runnable",
			IsInterface: true,
			Members: map[string]Type{
				"run": {Callable: &CallableType{Return: voidType}},
			},
		},
		"Function": {
			Name:        "Function",
			Args:        []Type{leftType, rightType},
			IsInterface: true,
			Members: map[string]Type{
				"apply": {Callable: &CallableType{Params: []Type{leftType}, Return: rightType}},
			},
		},
		"BiFunction": {
			Name:        "BiFunction",
			Args:        []Type{leftType, secondType, rightType},
			IsInterface: true,
			Members: map[string]Type{
				"apply": {Callable: &CallableType{Params: []Type{leftType, secondType}, Return: rightType}},
			},
		},
		"UnaryOperator": {
			Name:        "UnaryOperator",
			Args:        []Type{itemType},
			IsInterface: true,
			Members: map[string]Type{
				"apply": {Callable: &CallableType{Params: []Type{itemType}, Return: itemType}},
			},
		},
		"BinaryOperator": {
			Name:        "BinaryOperator",
			Args:        []Type{itemType},
			IsInterface: true,
			Members: map[string]Type{
				"apply": {Callable: &CallableType{Params: []Type{itemType, itemType}, Return: itemType}},
			},
		},
		"Iterable": {
			Name:        "Iterable",
			Args:        []Type{itemType},
			IsInterface: true,
			Members: map[string]Type{
				"__length": {Callable: &CallableType{Return: numberType}},
				"__get":    {Callable: &CallableType{Params: []Type{intType}, Return: anyType}},
			},
		},
		"Unstructured": {
			Name:        "Unstructured",
			IsInterface: true,
			Members: map[string]Type{
				"__pieces":    {Callable: &CallableType{Return: numberType}},
				"__get_piece": {Callable: &CallableType{Params: []Type{intType}, Return: anyType}},
			},
		},
		"Sliceable": {
			Name:        "Sliceable",
			Args:        []Type{itemType},
			IsInterface: true,
			Members: map[string]Type{
				"__slice": {Callable: &CallableType{Params: []Type{intType, intType}, Return: anyType}},
			},
		},
		"Indexable": {
			Name:        "Indexable",
			Args:        []Type{keyType, valueType},
			IsInterface: true,
			Members: map[string]Type{
				"__get":      {Callable: &CallableType{Params: []Type{anyType}, Return: anyType}},
				"__set":      {Callable: &CallableType{Params: []Type{anyType, anyType}, Return: voidType}},
				"__contains": {Callable: &CallableType{Params: []Type{anyType}, Return: boolType}},
			},
		},
		"Collection": {
			Name:        "Collection",
			Args:        []Type{itemType},
			IsInterface: true,
			Members: map[string]Type{
				"size":     {Callable: &CallableType{Return: numberType}},
				"isEmpty":  {Callable: &CallableType{Return: boolType}},
				"add":      {Callable: &CallableType{Params: []Type{anyType}, Return: voidType}},
				"remove":   {Callable: &CallableType{Params: []Type{anyType}, Return: boolType}},
				"contains": {Callable: &CallableType{Params: []Type{anyType}, Return: boolType}},
				"clear":    {Callable: &CallableType{Return: voidType}},
				"asArray":  {Callable: &CallableType{Return: Primitive(runtime.TypeArray)}},
			},
		},
	}
}

func (c *Checker) bindTypeParams(params []ast.TypeParam) []Type {
	if len(params) == 0 {
		return nil
	}
	resolved := make([]Type, len(params))
	for i, param := range params {
		bounds := make([]Type, len(param.Bounds))
		for j, bound := range param.Bounds {
			bounds[j] = c.resolveTypeRef(bound)
		}
		resolved[i] = TypeVariable(param.Name.Lexeme, bounds)
		c.currentScope()[param.Name.Lexeme] = symbol{Type: resolved[i], Mutable: false}
	}
	return resolved
}

func (c *Checker) inferGenericBindings(pattern Type, actual Type, bindings map[string]Type) {
	if pattern.IsTypeParam {
		if existing, ok := bindings[pattern.Name]; ok {
			if c.isAssignable(existing, actual) {
				bindings[pattern.Name] = actual
			}
			return
		}
		bindings[pattern.Name] = actual
		return
	}
	if pattern.Name == actual.Name && len(pattern.Args) == len(actual.Args) {
		for i := range pattern.Args {
			c.inferGenericBindings(pattern.Args[i], actual.Args[i], bindings)
		}
	}
}

func (c *Checker) substituteWithBindings(t Type, bindings map[string]Type) Type {
	return substituteType(t, bindings)
}

func (c *Checker) checkStmt(stmt ast.Stmt) error {
	switch node := stmt.(type) {
	case *ast.LetStmt:
		var declared Type
		var valueType Type
		var err error
		if node.Type != nil {
			declared = c.resolveTypeRef(node.Type)
			valueType, err = c.checkExprWithExpected(node.Value, &declared)
			if err != nil {
				return err
			}
			if !c.isAssignable(declared, valueType) {
				return fmt.Errorf("line %d:%d: cannot assign %s to %s", node.Name.Line, node.Name.Column, valueType.Name, declared.Name)
			}
		} else {
			valueType, err = c.checkExpr(node.Value)
			if err != nil {
				return err
			}
			declared = valueType
		}
		c.currentScope()[node.Name.Lexeme] = symbol{Type: declared, Mutable: node.Kind != ast.VariableConst && node.Kind != ast.VariableFinal}
		return nil
	case *ast.DestructureLetStmt:
		valueType, err := c.checkExpr(node.Value)
		if err != nil {
			return err
		}
		targetTypes, ok := valueType.DestructureTypes(len(node.Targets))
		if !ok {
			return fmt.Errorf("line %d:%d: cannot destructure %s into %d targets", node.Targets[0].Line, node.Targets[0].Column, valueType.Name, len(node.Targets))
		}
		for i, target := range node.Targets {
			c.currentScope()[target.Lexeme] = symbol{Type: targetTypes[i], Mutable: node.Kind != ast.VariableConst && node.Kind != ast.VariableFinal}
		}
		return nil
	case *ast.AssignStmt:
		target, ok := c.lookup(node.Name.Lexeme)
		if !ok {
			return fmt.Errorf("line %d:%d: undefined variable %s", node.Name.Line, node.Name.Column, node.Name.Lexeme)
		}
		if !target.Mutable {
			return fmt.Errorf("line %d:%d: cannot reassign immutable variable %s", node.Name.Line, node.Name.Column, node.Name.Lexeme)
		}
		valueType, err := c.checkExprWithExpected(node.Value, &target.Type)
		if err != nil {
			return err
		}
		if !c.compoundAssignable(target.Type, valueType, node.Operator) {
			return fmt.Errorf("line %d:%d: cannot assign %s to %s", node.Name.Line, node.Name.Column, valueType.Name, target.Type.Name)
		}
		return nil
	case *ast.SetStmt:
		objType, err := c.checkExpr(node.Object)
		if err != nil {
			return err
		}
		member, ok := objType.Members[node.Name.Lexeme]
		if !ok || member.Callable != nil {
			return fmt.Errorf("line %d:%d: %s has no writable field %s", node.Name.Line, node.Name.Column, objType.Name, node.Name.Lexeme)
		}
		if err := c.ensureMemberAccessible(objType, node.Name.Lexeme, false, node.Name.Line, node.Name.Column); err != nil {
			return err
		}
		valueType, err := c.checkExprWithExpected(node.Value, &member)
		if err != nil {
			return err
		}
		if !c.compoundAssignable(member, valueType, node.Operator) {
			return fmt.Errorf("line %d:%d: cannot assign %s to field %s of type %s", node.Name.Line, node.Name.Column, valueType.Name, node.Name.Lexeme, member.Name)
		}
		return nil
	case *ast.SetIndexStmt:
		objType, err := c.checkExpr(node.Object)
		if err != nil {
			return err
		}
		if !objType.SupportsIndexAssignment() {
			return fmt.Errorf("expression of type %s is not index-assignable", objType.Name)
		}
		indexType, err := c.checkExpr(node.Index)
		if err != nil {
			return err
		}
		keyType := objType.IndexKeyType()
		if !keyType.IsAssignableFrom(indexType) {
			return fmt.Errorf("index must be %s, got %s", keyType.Name, indexType.Name)
		}
		assignedType := objType.IndexAssignedValueType()
		valueType, err := c.checkExprWithExpected(node.Value, &assignedType)
		if err != nil {
			return err
		}
		if !c.isAssignable(assignedType, valueType) {
			return fmt.Errorf("cannot assign %s through index of %s", valueType.Name, objType.Name)
		}
		return nil
	case *ast.ExprStmt:
		_, err := c.checkExpr(node.Expr)
		return err
	case *ast.IfStmt:
		if condition, guard, ok := unwrapInstanceOfConditionAndGuard(node.Condition); ok {
			if _, err := c.checkExpr(condition.Expr); err != nil {
				return err
			}
			_ = c.resolveTypeRef(condition.Target)
			c.pushScope()
			c.currentScope()[condition.Binding.Lexeme] = symbol{Type: c.resolveTypeRef(condition.Target), Mutable: false}
			guardType, err := c.checkExpr(guard)
			if err != nil {
				c.popScope()
				return err
			}
			if !isBooleanCompatibleType(guardType.Name) && guardType.Name != runtime.TypeAny {
				c.popScope()
				return fmt.Errorf("if condition must be Bool or Boolean, got %s", guardType.Name)
			}
			if err := c.checkBlock(node.Then); err != nil {
				c.popScope()
				return err
			}
			c.popScope()
			if node.Else != nil {
				return c.checkBlock(node.Else)
			}
			return nil
		}
		condType, err := c.checkExpr(node.Condition)
		if err != nil {
			return err
		}
		if !isBooleanCompatibleType(condType.Name) && condType.Name != runtime.TypeAny {
			return fmt.Errorf("if condition must be Bool or Boolean, got %s", condType.Name)
		}
		if condition, ok := unwrapInstanceOfCondition(node.Condition); ok && condition.Binding != nil {
			c.pushScope()
			c.currentScope()[condition.Binding.Lexeme] = symbol{Type: c.resolveTypeRef(condition.Target), Mutable: false}
			if err := c.checkBlock(node.Then); err != nil {
				c.popScope()
				return err
			}
			c.popScope()
		} else {
			if err := c.checkBlock(node.Then); err != nil {
				return err
			}
		}
		if node.Else != nil {
			return c.checkBlock(node.Else)
		}
		return nil
	case *ast.SwitchStmt:
		if _, err := c.checkExpr(node.Value); err != nil {
			return err
		}
		for _, arm := range node.Arms {
			c.pushScope()
			for _, pattern := range arm.Patterns {
				if pattern.Value != nil {
					if _, err := c.checkExpr(pattern.Value); err != nil {
						c.popScope()
						return err
					}
				}
				if pattern.Type != nil {
					boundType := c.resolveTypeRef(pattern.Type.Type)
					c.currentScope()[pattern.Type.Binding.Lexeme] = symbol{Type: boundType, Mutable: true}
				}
			}
			for _, stmt := range arm.Body.Statements {
				if err := c.checkStmt(stmt); err != nil {
					c.popScope()
					return err
				}
			}
			c.popScope()
		}
		if node.Default != nil {
			return c.checkBlock(node.Default)
		}
		return nil
	case *ast.TryStmt:
		if err := c.checkBlock(node.Body); err != nil {
			return err
		}
		for _, clause := range node.Catches {
			c.pushScope()
			if clause.Binding.Type != "" {
				bindingType := Any()
				if clause.Type != nil {
					bindingType = c.resolveTypeRef(clause.Type)
					if bindingType.Callable != nil {
						bindingType = bindingType.Callable.Return
					}
				}
				c.currentScope()[clause.Binding.Lexeme] = symbol{Type: bindingType, Mutable: true}
			}
			for _, stmt := range clause.Body.Statements {
				if err := c.checkStmt(stmt); err != nil {
					c.popScope()
					return err
				}
			}
			c.popScope()
		}
		return nil
	case *ast.ReturnStmt:
		if c.currentFunc == nil {
			return fmt.Errorf("line %d:%d: return outside function", node.Keyword.Line, node.Keyword.Column)
		}
		if node.Value == nil {
			if !c.currentFunc.Return.IsAssignableFrom(Primitive(runtime.TypeVoid)) {
				return fmt.Errorf("line %d:%d: return value expected", node.Keyword.Line, node.Keyword.Column)
			}
			return nil
		}
		expectedReturn := c.currentFunc.Return
		retType, err := c.checkExprWithExpected(node.Value, &expectedReturn)
		if err != nil {
			return err
		}
		if c.currentFunc.Return.Name == "Unknown" {
			c.currentFunc.Return = retType
			return nil
		}
		if !c.isAssignable(c.currentFunc.Return, retType) {
			return fmt.Errorf("line %d:%d: return type %s does not match %s", node.Keyword.Line, node.Keyword.Column, DisplayName(retType), DisplayName(c.currentFunc.Return))
		}
		return nil
	case *ast.ThrowStmt:
		_, err := c.checkExpr(node.Value)
		return err
	case *ast.InterfaceStmt:
		return c.validateInterfaceDecl(node)
	case *ast.TypeAliasStmt:
		c.currentTypeScope()[node.Name.Lexeme] = c.resolveTypeRef(node.Target)
		return nil
	case *ast.FunctionStmt:
		fnType := c.functionType(node)
		c.currentScope()[node.Name.Lexeme] = symbol{Type: fnType, Mutable: false}
		c.pushScope()
		c.bindTypeParams(node.TypeParams)
		prev := c.currentFunc
		c.currentFunc = fnType.Callable
		for i, param := range node.Params {
			c.currentScope()[param.Name.Lexeme] = symbol{Type: fnType.Callable.Params[i], Mutable: true}
		}
		if node.Body != nil {
			for _, bodyStmt := range node.Body.Statements {
				if err := c.checkStmt(bodyStmt); err != nil {
					c.currentFunc = prev
					c.popScope()
					return err
				}
			}
		}
		c.currentFunc = prev
		c.popScope()
		return nil
	case *ast.ForStmt:
		iterType, err := c.checkExpr(node.Iterable)
		if err != nil {
			return err
		}
		if !iterType.SupportsForIn() {
			return fmt.Errorf("line %d:%d: for-in expects Iterable, got %s", node.Targets[0].Line, node.Targets[0].Column, iterType.Name)
		}
		itemType := iterType.IterableItemType()
		c.pushScope()
		if iterType.Name == runtime.TypeMap && len(node.Targets) == 2 {
			c.currentScope()[node.Targets[0].Lexeme] = symbol{Type: Primitive(runtime.TypeString), Mutable: true}
			c.currentScope()[node.Targets[1].Lexeme] = symbol{Type: Any(), Mutable: true}
		} else if len(node.Targets) == 1 {
			c.currentScope()[node.Targets[0].Lexeme] = symbol{Type: itemType, Mutable: true}
		} else {
			targetTypes, ok := itemType.DestructureTypes(len(node.Targets))
			if !ok {
				c.popScope()
				return fmt.Errorf("line %d:%d: iterable elements of type %s cannot be destructured into %d targets", node.Targets[0].Line, node.Targets[0].Column, itemType.Name, len(node.Targets))
			}
			for i, target := range node.Targets {
				c.currentScope()[target.Lexeme] = symbol{Type: targetTypes[i], Mutable: true}
			}
		}
		whereBindingActive := false
		if node.Condition != nil {
			if condition, guard, ok := unwrapInstanceOfConditionAndGuard(node.Condition); ok {
				if _, err := c.checkExpr(condition.Expr); err != nil {
					c.popScope()
					return err
				}
				boundType := c.resolveTypeRef(condition.Target)
				c.pushScope()
				whereBindingActive = true
				c.currentScope()[condition.Binding.Lexeme] = symbol{Type: boundType, Mutable: false}
				guardType, err := c.checkExpr(guard)
				if err != nil {
					c.popScope()
					c.popScope()
					return err
				}
				if !isBooleanCompatibleType(guardType.Name) && guardType.Name != runtime.TypeAny {
					c.popScope()
					c.popScope()
					return fmt.Errorf("where condition must be Bool or Boolean, got %s", guardType.Name)
				}
			} else if condition, ok := unwrapInstanceOfCondition(node.Condition); ok && condition.Binding != nil {
				if _, err := c.checkExpr(condition.Expr); err != nil {
					c.popScope()
					return err
				}
				boundType := c.resolveTypeRef(condition.Target)
				c.pushScope()
				whereBindingActive = true
				c.currentScope()[condition.Binding.Lexeme] = symbol{Type: boundType, Mutable: false}
			} else {
				condType, err := c.checkExpr(node.Condition)
				if err != nil {
					c.popScope()
					return err
				}
				if !isBooleanCompatibleType(condType.Name) && condType.Name != runtime.TypeAny {
					c.popScope()
					return fmt.Errorf("where condition must be Bool or Boolean, got %s", condType.Name)
				}
			}
		}
		c.insideLoop++
		for _, bodyStmt := range node.Body.Statements {
			if err := c.checkStmt(bodyStmt); err != nil {
				c.insideLoop--
				if whereBindingActive {
					c.popScope()
				}
				c.popScope()
				return err
			}
		}
		c.insideLoop--
		if whereBindingActive {
			c.popScope()
		}
		c.popScope()
		return nil
	case *ast.LoopStmt:
		if node.Condition != nil {
			condType, err := c.checkExpr(node.Condition)
			if err != nil {
				return err
			}
			if !isBooleanCompatibleType(condType.Name) && condType.Name != runtime.TypeAny {
				return fmt.Errorf("line %d:%d: loop condition must be Bool or Boolean, got %s", node.Keyword.Line, node.Keyword.Column, condType.Name)
			}
		}
		c.insideLoop++
		err := c.checkBlock(node.Body)
		c.insideLoop--
		return err
	case *ast.BreakStmt:
		if c.insideLoop == 0 {
			return fmt.Errorf("line %d:%d: break used outside loop", node.Keyword.Line, node.Keyword.Column)
		}
		return nil
	case *ast.ContinueStmt:
		if c.insideLoop == 0 {
			return fmt.Errorf("line %d:%d: continue used outside loop", node.Keyword.Line, node.Keyword.Column)
		}
		return nil
	case *ast.BlockStmt:
		return c.checkBlock(node)
	case *ast.ClassStmt:
		if err := c.validateClassDecl(node); err != nil {
			return err
		}
		classSym, _ := c.lookup(node.Name.Lexeme)
		instanceType := classSym.Type
		if classSym.Type.Callable != nil {
			instanceType = classSym.Type.Callable.Return
		}
		if err := c.validateImplementedInterfaces(node, instanceType); err != nil {
			return err
		}
		prevClass := c.currentClass
		c.currentClass = node
		for _, method := range node.Methods {
			callable := c.methodType(instanceType, method)
			prev := c.currentFunc
			prevMethod := c.currentMethod
			c.currentMethod = &method
			if method.IsAbstract {
				if err := c.validateMethodAnnotations(node, method, classSym.Type, instanceType, callable); err != nil {
					c.currentMethod = prevMethod
					return err
				}
				c.currentMethod = prevMethod
				continue
			}
			c.currentFunc = callable.Callable
			c.pushScope()
			c.bindTypeParams(node.TypeParams)
			c.bindTypeParams(method.TypeParams)
			if !method.Static {
				c.currentScope()["this"] = symbol{Type: instanceType, Mutable: true}
			}
			for i, param := range method.Params {
				c.currentScope()[param.Name.Lexeme] = symbol{Type: callable.Callable.Params[i], Mutable: true}
			}
			if method.Body != nil {
				for _, stmt := range method.Body.Statements {
					if err := c.checkStmt(stmt); err != nil {
						c.popScope()
						c.currentFunc = prev
						return err
					}
				}
			}
			if err := c.validateMethodAnnotations(node, method, classSym.Type, instanceType, callable); err != nil {
				c.popScope()
				c.currentFunc = prev
				c.currentMethod = prevMethod
				return err
			}
			if !method.IsConstructor {
				if method.Static {
					if existing, ok := classSym.Type.Members[method.Name.Lexeme]; ok && existing.Callable != nil {
						callable.Callable.Overloaded = existing.Callable.Overloaded
					}
					classSym.Type.Members[method.Name.Lexeme] = callable
				} else {
					if existing, ok := instanceType.Members[method.Name.Lexeme]; ok && existing.Callable != nil {
						callable.Callable.Overloaded = existing.Callable.Overloaded
					}
					instanceType.Members[method.Name.Lexeme] = callable
				}
			}
			c.popScope()
			c.currentFunc = prev
			c.currentMethod = prevMethod
		}
		c.currentClass = prevClass
		return nil
	default:
		return nil
	}
}

func (c *Checker) compoundAssignable(target Type, valueType Type, operator token.Token) bool {
	if operator.Type == token.Equal || operator.Type == "" {
		return c.isAssignable(target, valueType)
	}
	switch operator.Type {
	case token.PlusEqual:
		if target.Name == runtime.TypeString || valueType.Name == runtime.TypeString {
			return target.Name == runtime.TypeString || target.Name == runtime.TypeAny
		}
		if target.Name == runtime.TypeAny || valueType.Name == runtime.TypeAny {
			return true
		}
		if isNumericCompatibleType(target.Name) && isNumericCompatibleType(valueType.Name) {
			resultType := numericBinaryType(token.Plus, target, valueType)
			return c.isAssignable(target, resultType)
		}
		if resultType, ok := c.resolveBinaryOperatorType(token.Plus, target, valueType); ok {
			return c.isAssignable(target, resultType)
		}
		return false
	case token.MinusEqual, token.StarEqual, token.SlashEqual:
		if target.Name == runtime.TypeAny || valueType.Name == runtime.TypeAny {
			return true
		}
		if isNumericCompatibleType(target.Name) && isNumericCompatibleType(valueType.Name) {
			resultType := numericBinaryType(operator.Type, target, valueType)
			return c.isAssignable(target, resultType)
		}
		binaryOperator := token.Minus
		switch operator.Type {
		case token.StarEqual:
			binaryOperator = token.Star
		case token.SlashEqual:
			binaryOperator = token.Slash
		}
		if resultType, ok := c.resolveBinaryOperatorType(binaryOperator, target, valueType); ok {
			return c.isAssignable(target, resultType)
		}
		return false
	default:
		return false
	}
}

func (c *Checker) checkBlock(block *ast.BlockStmt) error {
	c.pushScope()
	defer c.popScope()
	for _, stmt := range block.Statements {
		if err := c.checkStmt(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (c *Checker) checkExpr(expr ast.Expr) (Type, error) {
	return c.checkExprWithExpected(expr, nil)
}

func (c *Checker) expectedCallableType(expected *Type) *CallableType {
	if expected == nil {
		return nil
	}
	resolved := c.resolveKnownInterfaceShape(*expected)
	if resolved.Callable != nil {
		return resolved.Callable
	}
	if resolved.IsInterface && len(resolved.Members) == 1 {
		for _, member := range resolved.Members {
			if member.Callable != nil {
				return member.Callable
			}
		}
	}
	return nil
}

func (c *Checker) resolveKnownInterfaceShape(t Type) Type {
	if t.Callable != nil || len(t.Members) > 0 || len(t.ConstructorOverloads) > 0 {
		return t
	}
	ifaceType, ok := c.interfaces[t.Name]
	if !ok {
		return t
	}
	return c.applyResolvedTypeArgs(ifaceType, t.Args)
}

func (c *Checker) checkExprWithExpected(expr ast.Expr, expected *Type) (Type, error) {
	switch node := expr.(type) {
	case *ast.LiteralExpr:
		switch node.Value.(type) {
		case nil:
			return Primitive(runtime.TypeNil), nil
		case bool:
			return Primitive(runtime.TypeBool), nil
		case int64:
			return Primitive(runtime.TypeInt), nil
		case float64:
			return Primitive(runtime.TypeFloat), nil
		case rune:
			return Primitive(runtime.TypeChar), nil
		case string:
			return Primitive(runtime.TypeString), nil
		default:
			return Any(), nil
		}
	case *ast.CastExpr:
		target := c.resolveTypeRef(node.Target)
		source, err := c.checkExpr(node.Expr)
		if err != nil {
			return Unknown(), err
		}
		if c.isNumericCastType(target) {
			if !c.isNumericCastSourceType(source) && source.Name != runtime.TypeAny && source.Name != "Unknown" {
				return Unknown(), fmt.Errorf("cannot cast %s to %s", source.Name, target.Name)
			}
			return target, nil
		}
		if !c.canReferenceCast(source, target) {
			return Unknown(), fmt.Errorf("cannot cast %s to %s", source.Name, target.Name)
		}
		return target, nil
	case *ast.InstanceOfExpr:
		if _, err := c.checkExpr(node.Expr); err != nil {
			return Unknown(), err
		}
		_ = c.resolveTypeRef(node.Target)
		return Primitive(runtime.TypeBool), nil
	case *ast.VariableExpr:
		if t, ok := c.lookup(node.Name.Lexeme); ok {
			return t.Type, nil
		}
		return Unknown(), fmt.Errorf("line %d:%d: undefined variable %s", node.Name.Line, node.Name.Column, node.Name.Lexeme)
	case *ast.ThisExpr:
		if t, ok := c.lookup(node.Keyword.Lexeme); ok {
			return t.Type, nil
		}
		return Unknown(), fmt.Errorf("line %d:%d: 'this' outside method or constructor", node.Keyword.Line, node.Keyword.Column)
	case *ast.GroupingExpr:
		return c.checkExpr(node.Expr)
	case *ast.TupleExpr:
		elements := make([]Type, len(node.Elements))
		for i, element := range node.Elements {
			elementType, err := c.checkExpr(element)
			if err != nil {
				return Unknown(), err
			}
			elements[i] = elementType
		}
		return TupleOf(elements), nil
	case *ast.ArrayExpr:
		elementType := Any()
		for i, element := range node.Elements {
			candidate, err := c.checkExpr(element)
			if err != nil {
				return Unknown(), err
			}
			if i == 0 {
				elementType = candidate
				continue
			}
			elementType = UnionOf(elementType, candidate)
		}
		return ArrayOf(elementType), nil

	case *ast.ArrayNewExpr:
		// Resolve the declared type.
		// When the user writes `new T[]{...}` parseTypeRef already wraps T into
		// array<T> before the size bracket is consumed, so node.Type comes in as
		// array<T> and node.Size is nil.  Unwrap one level so we always work with
		// the element type during initializer checking.
		declaredType := c.resolveTypeRef(node.Type)
		elemType := declaredType
		if node.Size == nil && declaredType.Name == runtime.TypeArray && len(declaredType.Args) > 0 {
			elemType = declaredType.Args[0]
		}

		if node.Size != nil {
			szType, err := c.checkExpr(node.Size)
			if err != nil {
				return Unknown(), err
			}
			if szType.Name != runtime.TypeInt {
				return Unknown(), fmt.Errorf("array size must be int, got %s", szType.Name)
			}
			// literal-size bounds check: new T[N]{a,b,c,d} where count > N is an error
			if lit, ok := node.Size.(*ast.LiteralExpr); ok {
				num, ok2 := integerLiteralValue(lit)
				if ok2 && len(node.Initializer) > 0 {
					if num < len(node.Initializer) {
						return Unknown(), fmt.Errorf("%d initializer values exceeds specified size %d", len(node.Initializer), num)
					}
				}
			}
		}
		// check initializer value types against the declared element type.
		// When a type is declared (new T[N]{...} or new T[]{...}) the element type
		// is fixed — don't widen it with UnionOf or subtype initializers break.
		// For untyped bare initialisers (no explicit T) widen as before.
		declaredElem := elemType
		for _, elem := range node.Initializer {
			candidate, err := c.checkExpr(elem)
			if err != nil {
				return Unknown(), err
			}
			if !c.isAssignable(declaredElem, candidate) {
				return Unknown(), fmt.Errorf("initializer element %s not assignable to %s", candidate.Name, declaredElem.Name)
			}
		}
		// always return array<declaredElemType>
		return ArrayOf(declaredElem), nil
	case *ast.MapExpr:
		valueType := Any()
		for _, entry := range node.Entries {
			candidate, err := c.checkExpr(entry.Value)
			if err != nil {
				return Unknown(), err
			}
			if valueType.Name == runtime.TypeAny {
				valueType = candidate
				continue
			}
			valueType = UnionOf(valueType, candidate)
		}
		return MapOf(Primitive(runtime.TypeString), valueType), nil
	case *ast.UnaryExpr:
		right, err := c.checkExpr(node.Right)
		if err != nil {
			return Unknown(), err
		}
		switch node.Operator.Type {
		case token.Minus:
			if !isNumericCompatibleType(right.Name) && right.Name != runtime.TypeAny {
				if result, ok := c.resolveUnaryOperatorType(node.Operator.Type, right); ok {
					return result, nil
				}
				return Unknown(), fmt.Errorf("line %d:%d: unary '-' expects Number, got %s", node.Operator.Line, node.Operator.Column, right.Name)
			}
			if right.Name == runtime.TypeInt || right.Name == runtime.TypeFloat {
				return right, nil
			}
			return Primitive(runtime.TypeNumber), nil
		case token.Bang:
			if !isBooleanCompatibleType(right.Name) && right.Name != runtime.TypeAny {
				return Unknown(), fmt.Errorf("line %d:%d: unary '!' expects Bool or Boolean, got %s", node.Operator.Line, node.Operator.Column, right.Name)
			}
			return Primitive(runtime.TypeBool), nil
		}
		return Unknown(), nil
	case *ast.BinaryExpr:
		return c.checkBinary(node)
	case *ast.CallExpr:
		calleeType, err := c.checkExpr(node.Callee)
		if err != nil {
			return Unknown(), err
		}
		if calleeType.Name == runtime.TypeAny {
			// Fast path for dynamic "any" type: assume it's callable and bypass strict structural checks.
			for _, arg := range node.Arguments {
				if _, err := c.checkExpr(arg); err != nil {
					return Unknown(), err
				}
			}
			return Any(), nil
		}
		if calleeType.Callable == nil {
			return Unknown(), fmt.Errorf("line %d:%d: expression is not callable", node.Paren.Line, node.Paren.Column)
		}

		if calleeType.Callable.Overloaded && len(node.TypeArgs) == 0 {
			// Fast path: bypass strict argument checking for overloaded methods.
			for _, arg := range node.Arguments {
				if _, err := c.checkExpr(arg); err != nil {
					return Unknown(), err
				}
			}
			return calleeType.Callable.Return, nil
		}
		if !calleeType.Callable.Variadic && len(node.Arguments) != len(calleeType.Callable.Params) {
			return Unknown(), fmt.Errorf("line %d:%d: expected %d arguments, got %d", node.Paren.Line, node.Paren.Column, len(calleeType.Callable.Params), len(node.Arguments))
		}
		specializedParams := calleeType.Callable.Params
		specializedReturn := calleeType.Callable.Return
		if len(node.TypeArgs) > 0 {
			if len(calleeType.Args) == 0 {
				return Unknown(), fmt.Errorf("line %d:%d: expression does not accept type arguments", node.Paren.Line, node.Paren.Column)
			}
			if len(node.TypeArgs) != len(calleeType.Args) {
				return Unknown(), fmt.Errorf("line %d:%d: expected %d type arguments, got %d", node.Paren.Line, node.Paren.Column, len(calleeType.Args), len(node.TypeArgs))
			}
			explicitArgs := make([]Type, len(node.TypeArgs))
			for i, typeArgRef := range node.TypeArgs {
				resolvedType := c.resolveTypeRef(typeArgRef)
				explicitArgs[i] = resolvedType
				genericArg := calleeType.Args[i]
				if genericArg.IsTypeParam {
					if !c.isAssignable(genericArg, resolvedType) {
						return Unknown(), fmt.Errorf("line %d:%d: type argument %s does not satisfy bounds of %s", node.Paren.Line, node.Paren.Column, resolvedType.Name, genericArg.Name)
					}
				}
			}
			specializedCallee := applyTypeArgs(calleeType, explicitArgs)
			specializedParams = specializedCallee.Callable.Params
			specializedReturn = specializedCallee.Callable.Return
			for i, arg := range node.Arguments {
				expectedIndex := i
				if expectedIndex >= len(specializedParams) {
					expectedIndex = len(specializedParams) - 1
				}
				if expectedIndex < 0 {
					argType, err := c.checkExpr(arg)
					if err != nil {
						return Unknown(), err
					}
					_ = argType
					continue
				}
				argType, err := c.checkExprWithExpected(arg, &specializedParams[expectedIndex])
				if err != nil {
					return Unknown(), err
				}
				if calleeType.Callable.Variadic {
					continue
				}
				if !c.isAssignable(specializedParams[i], argType) {
					return Unknown(), fmt.Errorf("line %d:%d: argument %d expects %s, got %s", node.Paren.Line, node.Paren.Column, i+1, DisplayName(specializedParams[i]), DisplayName(argType))
				}
			}
			return specializedReturn, nil
		}
		if len(calleeType.Args) > 0 {
			bindings := make(map[string]Type)
			argTypes := make([]Type, len(node.Arguments))
			for i, arg := range node.Arguments {
				argType, err := c.checkExpr(arg)
				if err != nil {
					return Unknown(), err
				}
				argTypes[i] = argType
				if i < len(calleeType.Callable.Params) {
					c.inferGenericBindings(calleeType.Callable.Params[i], argType, bindings)
				}
			}
			for _, genericArg := range calleeType.Args {
				if genericArg.IsTypeParam {
					if boundValue, ok := bindings[genericArg.Name]; ok && !c.isAssignable(genericArg, boundValue) {
						return Unknown(), fmt.Errorf("line %d:%d: inferred type %s does not satisfy bounds of %s", node.Paren.Line, node.Paren.Column, boundValue.Name, genericArg.Name)
					}
				}
			}
			specializedParams = make([]Type, len(calleeType.Callable.Params))
			for i, param := range calleeType.Callable.Params {
				specializedParams[i] = c.substituteWithBindings(param, bindings)
			}
			specializedReturn = c.substituteWithBindings(calleeType.Callable.Return, bindings)
			for i, argType := range argTypes {
				if _, isLambda := node.Arguments[i].(*ast.LambdaExpr); isLambda {
					expectedIndex := i
					if expectedIndex >= len(specializedParams) {
						expectedIndex = len(specializedParams) - 1
					}
					if expectedIndex < 0 {
						argType, err = c.checkExpr(node.Arguments[i])
					} else {
						argType, err = c.checkExprWithExpected(node.Arguments[i], &specializedParams[expectedIndex])
					}
					if err != nil {
						return Unknown(), err
					}
				}
				if calleeType.Callable.Variadic {
					continue
				}
				if !c.isAssignable(specializedParams[i], argType) {
					return Unknown(), fmt.Errorf("line %d:%d: argument %d expects %s, got %s", node.Paren.Line, node.Paren.Column, i+1, DisplayName(specializedParams[i]), DisplayName(argType))
				}
			}
			return specializedReturn, nil
		}
		for i, arg := range node.Arguments {
			expectedIndex := i
			if expectedIndex >= len(specializedParams) {
				expectedIndex = len(specializedParams) - 1
			}
			var argType Type
			var err error
			if expectedIndex < 0 {
				argType, err = c.checkExpr(arg)
			} else {
				argType, err = c.checkExprWithExpected(arg, &specializedParams[expectedIndex])
			}
			if err != nil {
				return Unknown(), err
			}
			if calleeType.Callable.Variadic {
				continue
			}
			if !c.isAssignable(specializedParams[i], argType) {
				return Unknown(), fmt.Errorf("line %d:%d: argument %d expects %s, got %s", node.Paren.Line, node.Paren.Column, i+1, DisplayName(specializedParams[i]), DisplayName(argType))
			}
		}
		return specializedReturn, nil
	case *ast.NewExpr:
		classSymbol, ok := c.lookup(node.Class.Lexeme)
		if !ok {
			return Unknown(), fmt.Errorf("line %d:%d: undefined class %s", node.Class.Line, node.Class.Column, node.Class.Lexeme)
		}
		calleeType := classSymbol.Type
		constructorCandidates := c.constructorCandidates(calleeType)
		if len(constructorCandidates) == 0 {
			return Unknown(), fmt.Errorf("line %d:%d: %s is not constructible", node.Class.Line, node.Class.Column, node.Class.Lexeme)
		}
		if calleeType.IsAbstract {
			return Unknown(), fmt.Errorf("line %d:%d: cannot instantiate abstract class %s", node.Class.Line, node.Class.Column, node.Class.Lexeme)
		}
		if err := c.ensureConstructorAccessible(node.Class.Lexeme, calleeType.ConstructorVisibility, node.Class.Line, node.Class.Column); err != nil {
			return Unknown(), err
		}
		argTypes := make([]Type, len(node.Arguments))
		for i, arg := range node.Arguments {
			argType, err := c.checkExpr(arg)
			if err != nil {
				return Unknown(), err
			}
			argTypes[i] = argType
		}
		matchedByArity := false
		var lastTypeError error
		for _, candidate := range constructorCandidates {
			if candidate == nil || (!candidate.Variadic && len(candidate.Params) != len(node.Arguments)) {
				continue
			}
			matchedByArity = true
			specializedParams := candidate.Params
			specializedReturn := candidate.Return
			bindings := make(map[string]Type)
			if len(calleeType.Args) > 0 {
				for i, argType := range argTypes {
					c.inferGenericBindings(candidate.Params[i], argType, bindings)
				}
				for _, genericArg := range calleeType.Args {
					if genericArg.IsTypeParam {
						if boundValue, ok := bindings[genericArg.Name]; ok && !c.isAssignable(genericArg, boundValue) {
							return Unknown(), fmt.Errorf("line %d:%d: inferred type %s does not satisfy bounds of %s", node.Paren.Line, node.Paren.Column, boundValue.Name, genericArg.Name)
						}
					}
				}
				specializedParams = make([]Type, len(candidate.Params))
				for i, param := range candidate.Params {
					specializedParams[i] = c.substituteWithBindings(param, bindings)
				}
				specializedReturn = c.substituteWithBindings(candidate.Return, bindings)
			}
			matched := true
			for i, argType := range argTypes {
				resolvedArgType := argType
				if _, isLambda := node.Arguments[i].(*ast.LambdaExpr); isLambda {
					var err error
					resolvedArgType, err = c.checkExprWithExpected(node.Arguments[i], &specializedParams[i])
					if err != nil {
						return Unknown(), err
					}
				}
				if !c.isAssignable(specializedParams[i], resolvedArgType) {
					lastTypeError = fmt.Errorf("line %d:%d: constructor argument %d expects %s, got %s", node.Paren.Line, node.Paren.Column, i+1, DisplayName(specializedParams[i]), DisplayName(resolvedArgType))
					matched = false
					break
				}
			}
			if matched {
				return specializedReturn, nil
			}
		}
		if !matchedByArity {
			return Unknown(), fmt.Errorf("line %d:%d: no constructor overload of %s accepts %d arguments", node.Paren.Line, node.Paren.Column, node.Class.Lexeme, len(node.Arguments))
		}
		if lastTypeError != nil {
			return Unknown(), lastTypeError
		}
		return Unknown(), fmt.Errorf("line %d:%d: no constructor overload of %s matches the provided arguments", node.Paren.Line, node.Paren.Column, node.Class.Lexeme)
	case *ast.GetExpr:
		if _, isSuper := node.Object.(*ast.SuperExpr); isSuper {
			if c.currentClass == nil || c.currentMethod == nil {
				return Unknown(), fmt.Errorf("line %d:%d: 'super' outside method or constructor", node.Name.Line, node.Name.Column)
			}
			if c.currentClass.Superclass == nil {
				return Unknown(), fmt.Errorf("line %d:%d: class %s has no superclass", node.Name.Line, node.Name.Column, c.currentClass.Name.Lexeme)
			}
			parentSym, ok := c.lookup(c.currentClass.Superclass.Name.Lexeme)
			if !ok || parentSym.Type.Callable == nil {
				return Unknown(), fmt.Errorf("line %d:%d: undefined superclass %s", node.Name.Line, node.Name.Column, c.currentClass.Superclass.Name.Lexeme)
			}
			_, _, isStatic, found := c.lookupClassMember(c.currentClass.Superclass.Name.Lexeme, node.Name.Lexeme)
			if !found {
				return Unknown(), fmt.Errorf("line %d:%d: %s has no member %s", node.Name.Line, node.Name.Column, c.currentClass.Superclass.Name.Lexeme, node.Name.Lexeme)
			}
			if isStatic {
				return Unknown(), fmt.Errorf("line %d:%d: super cannot access static member %s", node.Name.Line, node.Name.Column, node.Name.Lexeme)
			}
			member, ok := parentSym.Type.Callable.Return.Members[node.Name.Lexeme]
			if !ok {
				return Unknown(), fmt.Errorf("line %d:%d: %s has no member %s", node.Name.Line, node.Name.Column, c.currentClass.Superclass.Name.Lexeme, node.Name.Lexeme)
			}
			if err := c.ensureMemberAccessible(parentSym.Type.Callable.Return, node.Name.Lexeme, true, node.Name.Line, node.Name.Column); err != nil {
				return Unknown(), err
			}
			return member, nil
		}
		objType, err := c.checkExpr(node.Object)
		if err != nil {
			return Unknown(), err
		}
		if objType.Members != nil {
			member, ok := objType.Members[node.Name.Lexeme]
			if !ok {
				return Unknown(), fmt.Errorf("line %d:%d: %s has no member %s", node.Name.Line, node.Name.Column, objType.Name, node.Name.Lexeme)
			}
			if err := c.ensureMemberAccessible(objType, node.Name.Lexeme, true, node.Name.Line, node.Name.Column); err != nil {
				return Unknown(), err
			}
			return member, nil
		}
		if objType.Module == nil {
			return Unknown(), fmt.Errorf("line %d:%d: cannot access property on %s", node.Name.Line, node.Name.Column, objType.Name)
		}
		member, ok := objType.Module.Members[node.Name.Lexeme]
		if !ok {
			return Unknown(), fmt.Errorf("line %d:%d: %s has no member %s", node.Name.Line, node.Name.Column, objType.Module.Name, node.Name.Lexeme)
		}
		return c.typeFromSpec(member), nil
	case *ast.LambdaExpr:
		expectedCallable := c.expectedCallableType(expected)
		params := make([]Type, len(node.Params))
		for i, param := range node.Params {
			if param.Type != nil {
				params[i] = c.resolveTypeRef(param.Type)
				continue
			}
			if expectedCallable != nil && len(expectedCallable.Params) == len(node.Params) {
				params[i] = expectedCallable.Params[i]
				continue
			}
			params[i] = Any()
		}
		c.pushScope()
		prevFunc := c.currentFunc
		lambdaReturn := Unknown()
		if expectedCallable != nil {
			lambdaReturn = expectedCallable.Return
		}
		lambdaType := Type{Name: runtime.TypeFunction, Callable: &CallableType{Params: params, Return: lambdaReturn}}
		c.currentFunc = lambdaType.Callable
		for i, param := range node.Params {
			c.currentScope()[param.Name.Lexeme] = symbol{Type: params[i]}
		}
		bodyType := Primitive(runtime.TypeVoid)
		var err error
		if node.Block != nil {
			bodyType, err = c.checkLambdaBlock(node.Block)
		} else {
			if expectedCallable != nil {
				bodyType, err = c.checkExprWithExpected(node.Body, &expectedCallable.Return)
			} else {
				bodyType, err = c.checkExpr(node.Body)
			}
		}
		c.popScope()
		c.currentFunc = prevFunc
		if err != nil {
			return Unknown(), err
		}
		if expectedCallable != nil {
			if expectedCallable.Return.Name != "Unknown" && !c.isAssignable(expectedCallable.Return, bodyType) {
				return Unknown(), fmt.Errorf("lambda return type %s does not match expected %s", bodyType.Name, expectedCallable.Return.Name)
			}
			lambdaType.Callable.Return = expectedCallable.Return
		} else {
			lambdaType.Callable.Return = bodyType
		}
		return lambdaType, nil
	case *ast.IndexExpr:
		objType, err := c.checkExpr(node.Object)
		if err != nil {
			return Unknown(), err
		}
		if !objType.SupportsIndexing() {
			return Unknown(), fmt.Errorf("line %d:%d: cannot index %s", node.Bracket.Line, node.Bracket.Column, objType.Name)
		}
		indexType, err := c.checkExpr(node.Index)
		if err != nil {
			return Unknown(), err
		}
		keyType := objType.IndexKeyType()
		if !keyType.IsAssignableFrom(indexType) {
			return Unknown(), fmt.Errorf("line %d:%d: index must be %s, got %s", node.Bracket.Line, node.Bracket.Column, keyType.Name, indexType.Name)
		}
		if objType.Name == runtime.TypeTuple {
			if literal, ok := node.Index.(*ast.LiteralExpr); ok {
				if idx, ok := integerLiteralValue(literal); ok {
					if idx >= 0 && idx < len(objType.Tuple) {
						return objType.Tuple[idx], nil
					}
				}
			}
		}
		return objType.IndexValueType(), nil
	case *ast.SliceExpr:
		objType, err := c.checkExpr(node.Object)
		if err != nil {
			return Unknown(), err
		}
		if !objType.SupportsSliceable() {
			return Unknown(), fmt.Errorf("line %d:%d: cannot slice %s", node.Bracket.Line, node.Bracket.Column, objType.Name)
		}
		startType, err := c.checkExpr(node.Start)
		if err != nil {
			return Unknown(), err
		}
		endType, err := c.checkExpr(node.End)
		if err != nil {
			return Unknown(), err
		}
		if !isNumericCompatibleType(startType.Name) && startType.Name != runtime.TypeAny {
			return Unknown(), fmt.Errorf("line %d:%d: slice start must be Number, got %s", node.Bracket.Line, node.Bracket.Column, startType.Name)
		}
		if !isNumericCompatibleType(endType.Name) && endType.Name != runtime.TypeAny {
			return Unknown(), fmt.Errorf("line %d:%d: slice end must be Number, got %s", node.Bracket.Line, node.Bracket.Column, endType.Name)
		}
		if objType.Name == runtime.TypeTuple {
			startLiteral, startOk := node.Start.(*ast.LiteralExpr)
			endLiteral, endOk := node.End.(*ast.LiteralExpr)
			if startOk && endOk {
				startNum, startIsNum := integerLiteralValue(startLiteral)
				endNum, endIsNum := integerLiteralValue(endLiteral)
				if startIsNum && endIsNum {
					start := int(startNum)
					end := int(endNum)
					if start >= 0 && end >= start && end < len(objType.Tuple) {
						sliced := append([]Type(nil), objType.Tuple[start:end+1]...)
						return TupleOf(sliced), nil
					}
				}
			}
		}
		return objType.SliceResultType(), nil
	default:
		return Unknown(), nil
	}
}

func (c *Checker) isNumericCastType(t Type) bool {
	if len(t.Union) > 0 {
		if len(t.Union) == 0 {
			return false
		}
		for _, option := range t.Union {
			if !c.isNumericCastType(option) {
				return false
			}
		}
		return true
	}
	switch t.Name {
	case runtime.TypeInt, runtime.TypeFloat, runtime.TypeNumber:
		return true
	default:
		return false
	}
}

func (c *Checker) isNumericCastSourceType(t Type) bool {
	if len(t.Union) > 0 {
		for _, option := range t.Union {
			if !c.isNumericCastSourceType(option) {
				return false
			}
		}
		return true
	}
	return isNumericCompatibleType(t.Name)
}

func (c *Checker) canReferenceCast(source Type, target Type) bool {
	if len(target.Union) > 0 {
		for _, option := range target.Union {
			if c.canReferenceCast(source, option) {
				return true
			}
		}
		return false
	}
	if len(source.Union) > 0 {
		for _, option := range source.Union {
			if !c.canReferenceCast(option, target) {
				return false
			}
		}
		return true
	}
	if source.Name == runtime.TypeNil {
		return !c.isNumericCastType(target) && !isPrimitiveScalarType(target.Name)
	}
	if isWrapperPrimitiveCastPair(source.Name, target.Name) || isWrapperPrimitiveCastPair(target.Name, source.Name) {
		return true
	}
	if target.Name == runtime.TypeAny || source.Name == runtime.TypeAny || source.Name == "Unknown" {
		return true
	}
	if isTextComparableType(source.Name) && isTextComparableType(target.Name) {
		return true
	}
	if isPrimitiveScalarType(source.Name) || isPrimitiveScalarType(target.Name) {
		return false
	}
	if c.isAssignable(target, source) || c.isAssignable(source, target) {
		return true
	}
	if target.IsInterface || source.IsInterface {
		return true
	}
	return len(target.Members) > 0 && len(source.Members) > 0
}

func isWrapperPrimitiveCastPair(sourceName string, targetName string) bool {
	switch sourceName {
	case "Integer":
		return targetName == runtime.TypeInt || targetName == runtime.TypeFloat || targetName == runtime.TypeNumber
	case "Float", "Double":
		return targetName == runtime.TypeFloat || targetName == runtime.TypeNumber || targetName == runtime.TypeInt
	case "Boolean":
		return targetName == runtime.TypeBool
	case "Char":
		return targetName == runtime.TypeChar || targetName == runtime.TypeString
	case runtime.TypeInt:
		return targetName == "Integer" || targetName == "Float" || targetName == "Double"
	case runtime.TypeFloat, runtime.TypeNumber:
		return targetName == "Integer" || targetName == "Float" || targetName == "Double"
	case runtime.TypeBool:
		return targetName == "Boolean"
	case runtime.TypeChar:
		return targetName == "Char"
	default:
		return false
	}
}

func (c *Checker) checkLambdaBlock(block *ast.BlockStmt) (Type, error) {
	result := Primitive(runtime.TypeVoid)
	for i, stmt := range block.Statements {
		if i == len(block.Statements)-1 {
			if exprStmt, ok := stmt.(*ast.ExprStmt); ok {
				if c.currentFunc != nil && c.currentFunc.Return.Name != "Unknown" {
					expected := c.currentFunc.Return
					return c.checkExprWithExpected(exprStmt.Expr, &expected)
				}
				return c.checkExpr(exprStmt.Expr)
			}
		}
		if err := c.checkStmt(stmt); err != nil {
			return Unknown(), err
		}
	}
	if c.currentFunc != nil && c.currentFunc.Return.Name != "Unknown" {
		result = c.currentFunc.Return
	}
	return result, nil
}

func binaryOperatorMethodNames(operator token.Type) (string, string) {
	switch operator {
	case token.Plus:
		return "__add", "__radd"
	case token.Minus:
		return "__sub", "__rsub"
	case token.Star:
		return "__mul", "__rmul"
	case token.Slash:
		return "__div", "__rdiv"
	case token.Percent:
		return "__mod", "__rmod"
	case token.StarStar, token.Caret:
		return "__pow", "__rpow"
	case token.Greater:
		return "__gt", "__rgt"
	case token.Less:
		return "__lt", "__rlt"
	default:
		return "", ""
	}
}

func unaryOperatorMethodName(operator token.Type) string {
	switch operator {
	case token.Minus:
		return "__neg"
	default:
		return ""
	}
}

func (c *Checker) resolveUnaryOperatorType(operator token.Type, operand Type) (Type, bool) {
	methodName := unaryOperatorMethodName(operator)
	if methodName == "" {
		return Unknown(), false
	}
	member, ok := operand.Members[methodName]
	if !ok || member.Callable == nil {
		return Unknown(), false
	}
	if member.Callable.Overloaded {
		if member.Callable.Return.Name == "Unknown" {
			return Any(), true
		}
		return member.Callable.Return, true
	}
	if len(member.Callable.Params) != 0 {
		return Unknown(), false
	}
	if member.Callable.Return.Name == "Unknown" {
		return Any(), true
	}
	return member.Callable.Return, true
}

func (c *Checker) resolveBinaryOperatorType(operator token.Type, left Type, right Type) (Type, bool) {
	leftMethod, rightMethod := binaryOperatorMethodNames(operator)
	if leftMethod != "" {
		if result, ok := c.resolveOperatorMethodReturn(left, leftMethod, right); ok {
			return result, true
		}
	}
	if rightMethod != "" {
		if result, ok := c.resolveOperatorMethodReturn(right, rightMethod, left); ok {
			return result, true
		}
	}
	return Unknown(), false
}

func (c *Checker) resolveOperatorMethodReturn(receiver Type, methodName string, arg Type) (Type, bool) {
	member, ok := receiver.Members[methodName]
	if !ok || member.Callable == nil {
		return Unknown(), false
	}
	if member.Callable.Overloaded {
		if member.Callable.Return.Name == "Unknown" {
			return Any(), true
		}
		return member.Callable.Return, true
	}
	if len(member.Callable.Params) != 1 {
		return Unknown(), false
	}
	if !c.isAssignable(member.Callable.Params[0], arg) {
		return Unknown(), false
	}
	if member.Callable.Return.Name == "Unknown" {
		return Any(), true
	}
	return member.Callable.Return, true
}

func (c *Checker) checkBinary(node *ast.BinaryExpr) (Type, error) {
	left, err := c.checkExpr(node.Left)
	if err != nil {
		return Unknown(), err
	}
	right, err := c.checkExpr(node.Right)
	if err != nil {
		return Unknown(), err
	}
	switch node.Operator.Type {
	case token.AndAnd, token.OrOr:
		if (!isBooleanCompatibleType(left.Name) && left.Name != runtime.TypeAny) || (!isBooleanCompatibleType(right.Name) && right.Name != runtime.TypeAny) {
			return Unknown(), fmt.Errorf("line %d:%d: logical operator expects Bool or Boolean, got %s and %s", node.Operator.Line, node.Operator.Column, left.Name, right.Name)
		}
		return Primitive(runtime.TypeBool), nil
	case token.Plus:
		// string concatenation beats everything else
		if left.Name == runtime.TypeString || right.Name == runtime.TypeString {
			return Primitive(runtime.TypeString), nil
		}
		// array concatenation: both sides must be arrays, result tracks element union
		if left.Name == runtime.TypeArray && right.Name == runtime.TypeArray {
			// element type may be missing (untyped array literal) -> Any
			elemL := Any()
			if len(left.Args) > 0 {
				elemL = left.Args[0]
			}
			elemR := Any()
			if len(right.Args) > 0 {
				elemR = right.Args[0]
			}
			elem := UnionOf(elemL, elemR)
			return Type{Name: runtime.TypeArray, Args: []Type{elem}}, nil
		}
		// numeric addition
		if (isNumericCompatibleType(left.Name) || left.Name == runtime.TypeAny) && (isNumericCompatibleType(right.Name) || right.Name == runtime.TypeAny) {
			return numericBinaryType(token.Plus, left, right), nil
		}
		if result, ok := c.resolveBinaryOperatorType(node.Operator.Type, left, right); ok {
			return result, nil
		}
		return Unknown(), fmt.Errorf("line %d:%d: '+' expects Number/Number, String or Array concatenation, got %s and %s", node.Operator.Line, node.Operator.Column, left.Name, right.Name)
	case token.Minus, token.Star, token.StarStar, token.Caret, token.Slash, token.Percent:
		if (!isNumericCompatibleType(left.Name) && left.Name != runtime.TypeAny) || (!isNumericCompatibleType(right.Name) && right.Name != runtime.TypeAny) {
			if result, ok := c.resolveBinaryOperatorType(node.Operator.Type, left, right); ok {
				return result, nil
			}
			return Unknown(), fmt.Errorf("line %d:%d: arithmetic expects Number, got %s and %s", node.Operator.Line, node.Operator.Column, left.Name, right.Name)
		}
		return numericBinaryType(node.Operator.Type, left, right), nil
	case token.Greater, token.GreaterEqual, token.Less, token.LessEqual:
		if isTextComparableType(left.Name) && isTextComparableType(right.Name) {
			return Primitive(runtime.TypeBool), nil
		}
		if (!isNumericCompatibleType(left.Name) && left.Name != runtime.TypeAny) || (!isNumericCompatibleType(right.Name) && right.Name != runtime.TypeAny) {
			if result, ok := c.resolveBinaryOperatorType(node.Operator.Type, left, right); ok {
				if result.Name == "Unknown" {
					return Primitive(runtime.TypeBool), nil
				}
				return result, nil
			}
			return Unknown(), fmt.Errorf("line %d:%d: comparison expects Number, got %s and %s", node.Operator.Line, node.Operator.Column, left.Name, right.Name)
		}
		return Primitive(runtime.TypeBool), nil
	case token.In:
		if !right.SupportsContains() {
			return Unknown(), fmt.Errorf("line %d:%d: %s does not support 'in'", node.Operator.Line, node.Operator.Column, right.Name)
		}
		containsType := right.ContainsKeyType()
		if !c.isAssignable(containsType, left) {
			return Unknown(), fmt.Errorf("line %d:%d: left side of 'in' must be %s, got %s", node.Operator.Line, node.Operator.Column, containsType.Name, left.Name)
		}
		return Primitive(runtime.TypeBool), nil
	case token.EqualEqual, token.BangEqual:
		return Primitive(runtime.TypeBool), nil
	default:
		return Unknown(), nil
	}
}

func (c *Checker) functionType(fn *ast.FunctionStmt) Type {
	c.pushScope()
	typeParams := c.bindTypeParams(fn.TypeParams)
	defer c.popScope()
	params := make([]Type, len(fn.Params))
	for i, param := range fn.Params {
		if param.Type != nil {
			params[i] = c.resolveTypeRef(param.Type)
		} else {
			params[i] = Any()
		}
	}
	ret := Unknown()
	if fn.ReturnType != nil {
		ret = c.resolveTypeRef(fn.ReturnType)
	}
	return Type{Name: runtime.TypeFunction, Args: typeParams, Callable: &CallableType{Params: params, Return: ret}}
}

func (c *Checker) classType(classStmt *ast.ClassStmt) Type {
	if c.resolvingClasses[classStmt.Name.Lexeme] {
		if self, ok := c.lookup(classStmt.Name.Lexeme); ok {
			return self.Type
		}
	}
	c.resolvingClasses[classStmt.Name.Lexeme] = true
	defer delete(c.resolvingClasses, classStmt.Name.Lexeme)
	c.pushScope()
	typeParams := c.bindTypeParams(classStmt.TypeParams)
	defer c.popScope()
	instanceMembers := make(map[string]Type, len(classStmt.Fields)+len(classStmt.Methods))
	classMembers := make(map[string]Type, len(classStmt.Fields)+len(classStmt.Methods))
	instance := Type{Name: classStmt.Name.Lexeme, Args: append([]Type(nil), typeParams...), Members: instanceMembers, IsAbstract: classStmt.IsAbstract, IsEnum: classStmt.IsEnum, IsSealed: classStmt.IsSealed || classStmt.IsFinal || classStmt.IsEnum, IsRecord: classStmt.IsRecord, Permits: permitSet(permitNamesFromRefs(classStmt.Permits))}
	placeholder := Type{Name: classStmt.Name.Lexeme, Args: append([]Type(nil), typeParams...), Members: classMembers, Callable: &CallableType{Params: nil, Return: instance}, IsAbstract: classStmt.IsAbstract, IsEnum: classStmt.IsEnum, IsSealed: classStmt.IsSealed || classStmt.IsFinal, IsRecord: classStmt.IsRecord, Permits: permitSet(permitNamesFromRefs(classStmt.Permits))}
	c.currentScope()[classStmt.Name.Lexeme] = symbol{Type: placeholder, Mutable: false}
	ctorParams := []Type{}
	ctorOverloads := make([]*CallableType, 0)
	ctorVisibility := string(ast.VisibilityPublic)
	if classStmt.IsEnum {
		instanceMembers["name"] = Primitive(runtime.TypeString)
		instanceMembers["ordinal"] = Primitive(runtime.TypeNumber)
	}
	if classStmt.Superclass != nil {
		if parent, ok := c.lookup(classStmt.Superclass.Name.Lexeme); ok {
			parentType := parent.Type
			parentInstance := parentType
			if parentType.Callable != nil {
				for name, member := range parentType.Members {
					classMembers[name] = member
				}
				parentInstance = parentType.Callable.Return
			}
			for name, member := range parentInstance.Members {
				instanceMembers[name] = member
			}
		}
	}
	for _, field := range classStmt.Fields {
		fieldType := Any()
		if field.Type != nil {
			fieldType = c.resolveTypeRef(field.Type)
		}
		if field.Static {
			classMembers[field.Name.Lexeme] = fieldType
		} else {
			instanceMembers[field.Name.Lexeme] = fieldType
		}
	}
	for _, method := range classStmt.Methods {
		methodType := c.methodType(instance, method)
		if method.IsConstructor {
			if classStmt.IsEnum {
				continue
			}
			ctorParams = methodType.Callable.Params
			ctorCopy := *methodType.Callable
			ctorOverloads = append(ctorOverloads, &ctorCopy)
			ctorVisibility = string(method.Visibility)
			continue
		}
		if method.Static {
			if existing, ok := classMembers[method.Name.Lexeme]; ok {
				if existing.Callable != nil && methodType.Callable != nil {
					existing.Callable.Overloaded = true
					methodType.Callable.Overloaded = true
				}
			}
			classMembers[method.Name.Lexeme] = methodType
			continue
		}
		if existing, ok := instanceMembers[method.Name.Lexeme]; ok {
			if existing.Callable != nil && methodType.Callable != nil {
				existing.Callable.Overloaded = true
				methodType.Callable.Overloaded = true
			}
		}
		instanceMembers[method.Name.Lexeme] = methodType
	}
	if classStmt.IsEnum {
		for _, enumValue := range classStmt.EnumValues {
			classMembers[enumValue.Name.Lexeme] = instance
		}
		classMembers["valueOf"] = Type{Name: runtime.TypeFunction, Callable: &CallableType{Params: []Type{Primitive(runtime.TypeString)}, Return: instance}}
		classMembers["values"] = Type{Name: runtime.TypeFunction, Callable: &CallableType{Params: nil, Return: Primitive(runtime.TypeArray)}}
		classMembers["names"] = Type{Name: runtime.TypeFunction, Callable: &CallableType{Params: nil, Return: Primitive(runtime.TypeArray)}}
		classMembers["size"] = Type{Name: runtime.TypeFunction, Callable: &CallableType{Params: nil, Return: Primitive(runtime.TypeNumber)}}
		finalType := Type{Name: classStmt.Name.Lexeme, Args: append([]Type(nil), typeParams...), Members: classMembers, Callable: &CallableType{Params: nil, Return: instance}, ConstructorVisibility: string(ast.VisibilityPrivate), IsEnum: true, IsSealed: true, IsRecord: classStmt.IsRecord, Permits: permitSet(permitNamesFromRefs(classStmt.Permits))}
		c.currentScope()[classStmt.Name.Lexeme] = symbol{Type: finalType, Mutable: false}
		return finalType
	}
	finalType := Type{Name: classStmt.Name.Lexeme, Args: append([]Type(nil), typeParams...), Members: classMembers, Callable: &CallableType{Params: ctorParams, Return: instance}, ConstructorOverloads: ctorOverloads, ConstructorVisibility: ctorVisibility, IsAbstract: classStmt.IsAbstract, IsEnum: classStmt.IsEnum, IsSealed: classStmt.IsSealed || classStmt.IsFinal, IsRecord: classStmt.IsRecord, Permits: permitSet(permitNamesFromRefs(classStmt.Permits))}
	c.currentScope()[classStmt.Name.Lexeme] = symbol{Type: finalType, Mutable: false}
	return finalType
}

func (c *Checker) constructorCandidates(classType Type) []*CallableType {
	if len(classType.ConstructorOverloads) > 0 {
		return classType.ConstructorOverloads
	}
	if classType.Callable == nil {
		return nil
	}
	return []*CallableType{classType.Callable}
}

func (c *Checker) methodType(instance Type, method ast.MethodDecl) Type {
	c.pushScope()
	for _, arg := range instance.Args {
		if arg.IsTypeParam {
			c.currentScope()[arg.Name] = symbol{Type: arg, Mutable: false}
		}
	}
	typeParams := c.bindTypeParams(method.TypeParams)
	defer c.popScope()
	params := make([]Type, len(method.Params))
	for i, param := range method.Params {
		if param.Type != nil {
			params[i] = c.resolveTypeRef(param.Type)
		} else {
			params[i] = Any()
		}
	}
	ret := Unknown()
	if method.IsConstructor {
		ret = instance
	} else if method.ReturnType != nil {
		ret = c.resolveTypeRef(method.ReturnType)
		if ret.Name == instance.Name {
			ret = instance
		}
	}
	return Type{Name: runtime.TypeFunction, Args: typeParams, Callable: &CallableType{Params: params, Return: ret}}
}

func (c *Checker) interfaceType(name string, seen map[string]bool) Type {
	if iface, ok := c.interfaces[name]; ok && !c.resolvingIfaces[name] {
		return iface
	}
	if seen[name] {
		return Type{Name: name, Members: map[string]Type{}, IsInterface: true}
	}
	if c.resolvingIfaces[name] {
		return Type{Name: name, Members: map[string]Type{}, IsInterface: true}
	}
	decl, ok := c.interfaceDecls[name]
	if !ok {
		return Primitive(name)
	}
	seen[name] = true
	c.resolvingIfaces[name] = true
	c.interfaces[name] = Type{Name: name, Members: map[string]Type{}, IsInterface: true}
	c.pushScope()
	typeParams := c.bindTypeParams(decl.TypeParams)
	members := make(map[string]Type)
	for _, base := range decl.Extends {
		baseType := c.resolveTypeRef(base)
		for memberName, memberType := range baseType.Members {
			members[memberName] = memberType
		}
	}
	for _, method := range decl.Methods {
		params := make([]Type, len(method.Params))
		for i, param := range method.Params {
			if param.Type != nil {
				params[i] = c.resolveTypeRef(param.Type)
			} else {
				params[i] = Any()
			}
		}
		ret := Unknown()
		if method.ReturnType != nil {
			ret = c.resolveTypeRef(method.ReturnType)
		}
		members[method.Name.Lexeme] = Type{Name: runtime.TypeFunction, Callable: &CallableType{Params: params, Return: ret}}
	}
	c.popScope()
	ifaceType := Type{Name: name, Args: typeParams, Members: members, IsInterface: true, IsSealed: decl.IsSealed, Permits: permitSet(permitNamesFromRefs(decl.Permits))}
	c.interfaces[name] = ifaceType
	delete(c.resolvingIfaces, name)
	delete(seen, name)
	return ifaceType
}

func (c *Checker) resolveTypeRef(typeRef *ast.TypeRef) Type {
	if typeRef == nil {
		return Any()
	}
	if len(typeRef.Union) > 0 {
		options := make([]Type, len(typeRef.Union))
		for i, option := range typeRef.Union {
			options[i] = c.resolveTypeRef(option)
		}
		return UnionOf(options...)
	}
	if typeRef.Wildcard {
		if typeRef.Bound == nil {
			return WildcardAny()
		}
		bound := c.resolveTypeRef(typeRef.Bound)
		if typeRef.BoundKind == token.Super {
			return WildcardSuper(bound)
		}
		return WildcardExtends(bound)
	}
	args := make([]Type, len(typeRef.Args))
	for i, arg := range typeRef.Args {
		args[i] = c.resolveTypeRef(arg)
	}
	if alias, ok := c.lookupTypeAlias(typeRef.Name.Lexeme); ok {
		return c.applyResolvedTypeArgs(alias, args)
	}
	if decl, ok := c.typeAliasDecls[typeRef.Name.Lexeme]; ok {
		if c.resolvingTypes[typeRef.Name.Lexeme] {
			return Unknown()
		}
		c.resolvingTypes[typeRef.Name.Lexeme] = true
		resolved := c.resolveTypeRef(decl.Target)
		delete(c.resolvingTypes, typeRef.Name.Lexeme)
		c.typeScopes[0][typeRef.Name.Lexeme] = resolved
		return c.applyResolvedTypeArgs(resolved, args)
	}
	if ifaceType, ok := c.interfaces[typeRef.Name.Lexeme]; ok {
		return c.applyResolvedTypeArgs(ifaceType, args)
	}
	if iface, ok := c.interfaceDecls[typeRef.Name.Lexeme]; ok && iface != nil {
		return c.applyResolvedTypeArgs(c.interfaceType(typeRef.Name.Lexeme, map[string]bool{}), args)
	}
	if _, ok := c.classes[typeRef.Name.Lexeme]; ok {
		if sym, found := c.lookup(typeRef.Name.Lexeme); found {
			if sym.Type.Callable != nil && sym.Type.Callable.Return.Name == typeRef.Name.Lexeme {
				return c.applyResolvedTypeArgs(sym.Type.Callable.Return, args)
			}
			return c.applyResolvedTypeArgs(sym.Type, args)
		}
		classType := c.classType(c.classes[typeRef.Name.Lexeme])
		if classType.Callable != nil {
			return c.applyResolvedTypeArgs(classType.Callable.Return, args)
		}
		return c.applyResolvedTypeArgs(classType, args)
	}
	if sym, ok := c.lookup(typeRef.Name.Lexeme); ok && (sym.Type.Name == typeRef.Name.Lexeme || sym.Type.IsTypeParam || (sym.Type.Callable != nil && sym.Type.Callable.Return.Name == typeRef.Name.Lexeme)) {
		return c.applyResolvedTypeArgs(c.namedTypeValue(sym.Type, typeRef.Name.Lexeme), args)
	}
	if builtin, ok := sourceBuiltinType(typeRef.Name.Lexeme); ok {
		return c.applyResolvedTypeArgs(Primitive(builtin), args)
	}
	return c.applyResolvedTypeArgs(Primitive(typeRef.Name.Lexeme), args)
}

func (c *Checker) applyResolvedTypeArgs(base Type, args []Type) Type {
	if len(base.Args) > 0 && len(args) < len(base.Args) {
		filled := make([]Type, len(base.Args))
		copy(filled, args)
		for i := len(args); i < len(filled); i++ {
			filled[i] = Any()
		}
		args = filled
	}
	if len(args) == 0 {
		return base
	}
	return applyTypeArgs(base, args)
}

func (c *Checker) namedTypeValue(t Type, name string) Type {
	if t.Callable != nil && t.Callable.Return.Name == name && (len(t.Members) > 0 || len(t.ConstructorOverloads) > 0 || t.ConstructorVisibility != "") {
		return t.Callable.Return
	}
	return t
}

func sourceBuiltinType(name string) (string, bool) {
	switch name {
	case "int":
		return runtime.TypeInt, true
	case "Int":
		return runtime.TypeInt, true
	case "float":
		return runtime.TypeFloat, true
	case "Float":
		return runtime.TypeFloat, true
	case "number":
		return runtime.TypeNumber, true
	case "Number":
		return runtime.TypeNumber, true
	case "bool":
		return runtime.TypeBool, true
	case "Bool":
		return runtime.TypeBool, true
	case "char":
		return runtime.TypeChar, true
	case "Char":
		return runtime.TypeChar, true
	case "String":
		return runtime.TypeString, true
	case "string":
		return runtime.TypeString, true
	case "Array":
		return runtime.TypeArray, true
	case "nil":
		return runtime.TypeNil, true
	case "void":
		return runtime.TypeVoid, true
	case "array":
		return runtime.TypeArray, true
	case "Map":
		return runtime.TypeMap, true
	case "map":
		return runtime.TypeMap, true
	case "Tuple":
		return runtime.TypeTuple, true
	case "tuple":
		return runtime.TypeTuple, true
	case "Range":
		return runtime.TypeRange, true
	case "range":
		return runtime.TypeRange, true
	case "Function":
		return runtime.TypeFunction, true
	case "any", "":
		return runtime.TypeAny, true
	case "Any":
		return runtime.TypeAny, true
	default:
		return "", false
	}
}

func numericBinaryType(operator token.Type, left Type, right Type) Type {
	if left.Name == runtime.TypeAny || right.Name == runtime.TypeAny {
		return Any()
	}
	if left.Name == runtime.TypeNumber || right.Name == runtime.TypeNumber {
		if operator == token.Slash {
			return Primitive(runtime.TypeFloat)
		}
		return Primitive(runtime.TypeNumber)
	}
	if left.Name == runtime.TypeInt && right.Name == runtime.TypeInt {
		if operator == token.Slash {
			return Primitive(runtime.TypeFloat)
		}
		return Primitive(runtime.TypeInt)
	}
	if left.Name == runtime.TypeFloat || right.Name == runtime.TypeFloat {
		return Primitive(runtime.TypeFloat)
	}
	return Primitive(runtime.TypeNumber)
}

func integerLiteralValue(literal *ast.LiteralExpr) (int, bool) {
	if numeric, ok := literal.Value.(int64); ok {
		return int(numeric), true
	}
	return 0, false
}

func (c *Checker) typeFromSpec(spec runtime.Spec) Type {
	return c.typeFromSpecWithParams(spec, nil)
}

func (c *Checker) typeFromSpecWithParams(spec runtime.Spec, typeParams map[string]Type) Type {
	if typeParams != nil {
		if resolved, ok := typeParams[spec.TypeName]; ok {
			return resolved
		}
	}
	useCache := typeParams == nil && (spec.Name == "" || spec.Name == spec.TypeName)
	cacheKey := spec.Name
	if cacheKey == "" {
		cacheKey = spec.TypeName
	}
	if useCache {
		if cached, ok := c.specTypeCache[cacheKey]; ok {
			return *cached
		}
	}
	localTypeParams := typeParams
	declaredArgs := make([]Type, 0, len(spec.TypeParams))
	if len(spec.TypeParams) > 0 {
		localTypeParams = make(map[string]Type, len(spec.TypeParams))
		for name, resolved := range typeParams {
			localTypeParams[name] = resolved
		}
		for _, param := range spec.TypeParams {
			resolved := TypeVariable(param, nil)
			localTypeParams[param] = resolved
			declaredArgs = append(declaredArgs, resolved)
		}
	}
	t := &Type{Name: Primitive(spec.TypeName).Name, Args: declaredArgs, ConstructorVisibility: spec.ConstructorVisibility, IsAbstract: spec.IsAbstract, IsSealed: spec.IsSealed, IsInterface: spec.IsInterface, IsRecord: spec.IsRecord, Permits: permitSet(spec.Permits)}
	if useCache {
		c.specTypeCache[cacheKey] = t
	}
	if len(spec.Members) > 0 {
		t.Members = make(map[string]Type, len(spec.Members))
		for name, member := range spec.Members {
			t.Members[name] = c.typeFromSpecWithParams(member, localTypeParams)
		}
	}
	if spec.Module != nil {
		t.Module = spec.Module
	}
	if spec.Callable != nil {
		params := make([]Type, len(spec.Callable.Params))
		for i, param := range spec.Callable.Params {
			params[i] = c.resolveSpecTypeName(param, localTypeParams)
		}
		ret := c.resolveSpecTypeName(spec.Callable.Return, localTypeParams)
		if len(spec.InstanceMembers) > 0 {
			ret = c.instanceTypeFromSpecsWithParams(spec.Callable.Return, spec.InstanceMembers, localTypeParams, declaredArgs)
		}
		t.Callable = &CallableType{Params: params, Return: ret, Variadic: spec.Callable.Variadic, Overloaded: spec.Callable.Overloaded}
	}
	if len(spec.ConstructorOverloads) > 0 {
		t.ConstructorOverloads = make([]*CallableType, 0, len(spec.ConstructorOverloads))
		for _, overload := range spec.ConstructorOverloads {
			if overload == nil {
				continue
			}
			params := make([]Type, len(overload.Params))
			for i, param := range overload.Params {
				params[i] = c.resolveSpecTypeName(param, localTypeParams)
			}
			ret := c.resolveSpecTypeName(overload.Return, localTypeParams)
			if len(spec.InstanceMembers) > 0 {
				ret = c.instanceTypeFromSpecsWithParams(overload.Return, spec.InstanceMembers, localTypeParams, declaredArgs)
			}
			t.ConstructorOverloads = append(t.ConstructorOverloads, &CallableType{Params: params, Return: ret, Variadic: overload.Variadic, Overloaded: overload.Overloaded})
		}
	}
	return *t
}

func (c *Checker) instanceTypeFromSpecs(typeName string, members map[string]runtime.Spec) Type {
	return c.instanceTypeFromSpecsWithParams(typeName, members, nil, nil)
}

func (c *Checker) instanceTypeFromSpecsWithParams(typeName string, members map[string]runtime.Spec, typeParams map[string]Type, declaredArgs []Type) Type {
	cacheKey := Primitive(typeName).Name
	if len(declaredArgs) > 0 {
		cacheKey += "<" + DisplayName(Type{Name: Primitive(typeName).Name, Args: declaredArgs}) + ">"
	}
	if cached, ok := c.instanceCache[cacheKey]; ok {
		return *cached
	}
	instanceType := &Type{Name: Primitive(typeName).Name, Args: append([]Type(nil), declaredArgs...), Members: make(map[string]Type, len(members))}
	c.instanceCache[cacheKey] = instanceType
	for name, member := range members {
		instanceType.Members[name] = c.typeFromSpecWithParams(member, typeParams)
	}
	return *instanceType
}

func (c *Checker) shallowTypeFromSpec(spec runtime.Spec) Type {
	t := Type{Name: Primitive(spec.TypeName).Name, ConstructorVisibility: spec.ConstructorVisibility, IsAbstract: spec.IsAbstract, IsSealed: spec.IsSealed, IsInterface: spec.IsInterface, IsRecord: spec.IsRecord, Permits: permitSet(spec.Permits)}
	if len(spec.Members) > 0 {
		t.Members = make(map[string]Type, len(spec.Members))
		for name, member := range spec.Members {
			t.Members[name] = c.shallowTypeFromSpec(member)
		}
	}
	if spec.Callable != nil {
		params := make([]Type, len(spec.Callable.Params))
		for i, param := range spec.Callable.Params {
			params[i] = c.resolveSpecTypeName(param, nil)
		}
		ret := c.resolveSpecTypeName(spec.Callable.Return, nil)
		if len(spec.InstanceMembers) > 0 {
			ret = Type{Name: Primitive(spec.Callable.Return).Name}
		}
		t.Callable = &CallableType{Params: params, Return: ret, Variadic: spec.Callable.Variadic, Overloaded: spec.Callable.Overloaded}
	}
	if len(spec.ConstructorOverloads) > 0 {
		t.ConstructorOverloads = make([]*CallableType, 0, len(spec.ConstructorOverloads))
		for _, overload := range spec.ConstructorOverloads {
			if overload == nil {
				continue
			}
			params := make([]Type, len(overload.Params))
			for i, param := range overload.Params {
				params[i] = c.resolveSpecTypeName(param, nil)
			}
			ret := c.resolveSpecTypeName(overload.Return, nil)
			if len(spec.InstanceMembers) > 0 {
				ret = Type{Name: Primitive(overload.Return).Name}
			}
			t.ConstructorOverloads = append(t.ConstructorOverloads, &CallableType{Params: params, Return: ret, Variadic: overload.Variadic, Overloaded: overload.Overloaded})
		}
	}
	return t
}

func (c *Checker) resolveSpecTypeName(typeName string, typeParams map[string]Type) Type {
	trimmed := strings.TrimSpace(typeName)
	if trimmed == "" {
		return Any()
	}
	if typeParams != nil {
		if resolved, ok := typeParams[trimmed]; ok {
			return resolved
		}
	}
	unionParts := splitSpecTypeList(trimmed, '|')
	if len(unionParts) > 1 {
		options := make([]Type, len(unionParts))
		for i, part := range unionParts {
			options[i] = c.resolveSpecTypeName(part, typeParams)
		}
		return UnionOf(options...)
	}
	base, args := parseSpecGenericType(trimmed)
	resolvedArgs := make([]Type, len(args))
	for i, arg := range args {
		resolvedArgs[i] = c.resolveSpecTypeName(arg, typeParams)
	}
	if alias, ok := c.lookupTypeAlias(base); ok {
		return c.applyResolvedTypeArgs(alias, resolvedArgs)
	}
	if decl, ok := c.typeAliasDecls[base]; ok {
		if c.resolvingTypes[base] {
			return Unknown()
		}
		c.resolvingTypes[base] = true
		resolved := c.resolveTypeRef(decl.Target)
		delete(c.resolvingTypes, base)
		c.typeScopes[0][base] = resolved
		return c.applyResolvedTypeArgs(resolved, resolvedArgs)
	}
	if ifaceType, ok := c.interfaces[base]; ok {
		return c.applyResolvedTypeArgs(ifaceType, resolvedArgs)
	}
	if iface, ok := c.interfaceDecls[base]; ok && iface != nil {
		return c.applyResolvedTypeArgs(c.interfaceType(base, map[string]bool{}), resolvedArgs)
	}
	if _, ok := c.classes[base]; ok {
		classType := c.classType(c.classes[base])
		if classType.Callable != nil {
			return c.applyResolvedTypeArgs(classType.Callable.Return, resolvedArgs)
		}
		return c.applyResolvedTypeArgs(classType, resolvedArgs)
	}
	if sym, ok := c.lookup(base); ok && (sym.Type.Name == base || sym.Type.IsTypeParam) {
		return c.applyResolvedTypeArgs(c.namedTypeValue(sym.Type, base), resolvedArgs)
	}
	if builtin, ok := sourceBuiltinType(base); ok {
		return c.applyResolvedTypeArgs(Primitive(builtin), resolvedArgs)
	}
	return c.applyResolvedTypeArgs(Primitive(base), resolvedArgs)
}

func parseSpecGenericType(typeName string) (string, []string) {
	typeName = strings.TrimSpace(typeName)
	if !strings.HasSuffix(typeName, ">") {
		return typeName, nil
	}
	depth := 0
	start := -1
	for i, ch := range typeName {
		switch ch {
		case '<':
			if depth == 0 {
				start = i
			}
			depth++
		case '>':
			depth--
		}
	}
	if start < 0 {
		return typeName, nil
	}
	return strings.TrimSpace(typeName[:start]), splitSpecTypeList(typeName[start+1:len(typeName)-1], ',')
}

func splitSpecTypeList(input string, separator rune) []string {
	parts := make([]string, 0)
	depth := 0
	start := 0
	for i, ch := range input {
		switch ch {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		default:
			if ch == separator && depth == 0 {
				parts = append(parts, strings.TrimSpace(input[start:i]))
				start = i + 1
			}
		}
	}
	parts = append(parts, strings.TrimSpace(input[start:]))
	return parts
}

func (c *Checker) validateImplementedInterfaces(classStmt *ast.ClassStmt, instanceType Type) error {
	for _, iface := range classStmt.Implements {
		ifaceType := c.resolveTypeRef(iface)
		if ifaceType.IsInterface || len(ifaceType.Members) > 0 {
			for name, member := range ifaceType.Members {
				actual, exists := instanceType.Members[name]
				if !exists {
					if classStmt.IsAbstract {
						continue
					}
					return fmt.Errorf("line %d:%d: %s declares %s but is missing %s", iface.Name.Line, iface.Name.Column, classStmt.Name.Lexeme, iface.Name.Lexeme, name)
				}
				if !c.interfaceMemberAssignable(member, actual) {
					return fmt.Errorf("line %d:%d: %s.%s does not match interface %s: expected %s, got %s", iface.Name.Line, iface.Name.Column, classStmt.Name.Lexeme, name, iface.Name.Lexeme, DisplayName(member), DisplayName(actual))
				}
			}
			continue
		}
		if ifaceSym, exists := c.lookup(iface.Name.Lexeme); exists && len(ifaceSym.Type.Members) > 0 {
			for name, member := range ifaceSym.Type.Members {
				actual, exists := instanceType.Members[name]
				if !exists {
					if classStmt.IsAbstract {
						continue
					}
					return fmt.Errorf("line %d:%d: %s declares %s but is missing %s", iface.Name.Line, iface.Name.Column, classStmt.Name.Lexeme, iface.Name.Lexeme, name)
				}
				if !c.interfaceMemberAssignable(member, actual) {
					return fmt.Errorf("line %d:%d: %s.%s does not match interface %s: expected %s, got %s", iface.Name.Line, iface.Name.Column, classStmt.Name.Lexeme, name, iface.Name.Lexeme, DisplayName(member), DisplayName(actual))
				}
			}
			continue
		}
		switch iface.Name.Lexeme {
		case "Iterable":
			length, ok := instanceType.Members["__length"]
			if !ok || length.Callable == nil || len(length.Callable.Params) != 0 {
				return fmt.Errorf("line %d:%d: %s declares Iterable but is missing __length()", iface.Name.Line, iface.Name.Column, classStmt.Name.Lexeme)
			}
			getter, ok := instanceType.Members["__get"]
			if !ok || getter.Callable == nil || len(getter.Callable.Params) != 1 {
				return fmt.Errorf("line %d:%d: %s declares Iterable but is missing __get(index)", iface.Name.Line, iface.Name.Column, classStmt.Name.Lexeme)
			}
		case "Unstructured":
			pieces, ok := instanceType.Members["__pieces"]
			if !ok || pieces.Callable == nil || len(pieces.Callable.Params) != 0 {
				return fmt.Errorf("line %d:%d: %s declares Unstructured but is missing __pieces()", iface.Name.Line, iface.Name.Column, classStmt.Name.Lexeme)
			}
			getter, ok := instanceType.Members["__get_piece"]
			if !ok {
				getter, ok = instanceType.Members["__getPiece"]
			}
			if !ok || getter.Callable == nil || len(getter.Callable.Params) != 1 {
				return fmt.Errorf("line %d:%d: %s declares Unstructured but is missing __get_piece(index)", iface.Name.Line, iface.Name.Column, classStmt.Name.Lexeme)
			}
		default:
			return fmt.Errorf("line %d:%d: unsupported interface %s", iface.Name.Line, iface.Name.Column, iface.Name.Lexeme)
		}
	}
	return nil
}


func (c *Checker) interfaceMemberAssignable(expected Type, actual Type) bool {
	if c.isAssignable(expected, actual) {
		return true
	}
	if expected.Callable == nil || actual.Callable == nil {
		return false
	}
	if len(expected.Callable.Params) != len(actual.Callable.Params) {
		return false
	}
	for i := range expected.Callable.Params {
		if !c.isAssignable(expected.Callable.Params[i], actual.Callable.Params[i]) {
			return false
		}
	}
	if c.isAssignable(expected.Callable.Return, actual.Callable.Return) {
		return true
	}
	if c.explicitlyImplementsResolvedInterface(actual.Callable.Return, expected.Callable.Return) {
		return true
	}
	return c.recursiveStructuralAssignable(expected.Callable.Return, actual.Callable.Return, map[string]bool{})
}
func (c *Checker) validateInterfaceDecl(node *ast.InterfaceStmt) error {
	if node.IsSealed {
		if len(node.Permits) == 0 {
			return fmt.Errorf("line %d:%d: sealed interface %s must declare permitted types", node.Name.Line, node.Name.Column, node.Name.Lexeme)
		}
		for _, permit := range node.Permits {
			if c.knowsType(permit.Name.Lexeme) {
				continue
			}
			return fmt.Errorf("line %d:%d: sealed interface %s permits unknown type %s", permit.Name.Line, permit.Name.Column, node.Name.Lexeme, permit.Name.Lexeme)
		}
	}
	for _, base := range node.Extends {
		baseDecl, ok := c.interfaceDecls[base.Name.Lexeme]
		if ok && baseDecl.IsSealed && !c.permitsType(baseDecl.Permits, node.Name.Lexeme) {
			return fmt.Errorf("line %d:%d: %s is not permitted to extend sealed interface %s", base.Name.Line, base.Name.Column, node.Name.Lexeme, base.Name.Lexeme)
		}
		if !ok {
			if baseSym, exists := c.lookup(base.Name.Lexeme); exists && baseSym.Type.IsInterface && baseSym.Type.IsSealed && !c.typePermits(baseSym.Type, node.Name.Lexeme) {
				return fmt.Errorf("line %d:%d: %s is not permitted to extend sealed interface %s", base.Name.Line, base.Name.Column, node.Name.Lexeme, base.Name.Lexeme)
			}
		}
	}
	return nil
}

func (c *Checker) validateClassDecl(node *ast.ClassStmt) error {
	if node.IsEnum {
		if node.IsAbstract {
			return fmt.Errorf("line %d:%d: enum %s cannot be abstract", node.Name.Line, node.Name.Column, node.Name.Lexeme)
		}
		if node.Superclass != nil {
			return fmt.Errorf("line %d:%d: enum %s cannot declare a superclass", node.Name.Line, node.Name.Column, node.Name.Lexeme)
		}
		if len(node.Implements) > 0 {
			return fmt.Errorf("line %d:%d: enum %s cannot implement interfaces yet", node.Name.Line, node.Name.Column, node.Name.Lexeme)
		}
		for _, method := range node.Methods {
			if method.IsAbstract {
				return fmt.Errorf("line %d:%d: enum %s cannot declare abstract methods", method.Name.Line, method.Name.Column, node.Name.Lexeme)
			}
		}
		return nil
	}
	if node.IsSealed {
		if len(node.Permits) == 0 {
			return fmt.Errorf("line %d:%d: sealed class %s must declare permitted types", node.Name.Line, node.Name.Column, node.Name.Lexeme)
		}
		for _, permit := range node.Permits {
			if c.knowsType(permit.Name.Lexeme) {
				continue
			}
			return fmt.Errorf("line %d:%d: sealed class %s permits unknown type %s", permit.Name.Line, permit.Name.Column, node.Name.Lexeme, permit.Name.Lexeme)
		}
	}
	for _, method := range node.Methods {
		if method.IsAbstract && !node.IsAbstract {
			return fmt.Errorf("line %d:%d: class %s must be abstract to declare abstract methods", method.Name.Line, method.Name.Column, node.Name.Lexeme)
		}
	}
	if node.Superclass != nil {
		if parentDecl, ok := c.classes[node.Superclass.Name.Lexeme]; ok {
			if parentDecl.IsSealed && !c.permitsType(parentDecl.Permits, node.Name.Lexeme) {
				return fmt.Errorf("line %d:%d: %s is not permitted to extend sealed class %s", node.Superclass.Name.Line, node.Superclass.Name.Column, node.Name.Lexeme, node.Superclass.Name.Lexeme)
			}
		} else if parentSym, exists := c.lookup(node.Superclass.Name.Lexeme); exists {
			if parentSym.Type.IsSealed && !c.typePermits(parentSym.Type, node.Name.Lexeme) {
				return fmt.Errorf("line %d:%d: %s is not permitted to extend sealed class %s", node.Superclass.Name.Line, node.Superclass.Name.Column, node.Name.Lexeme, node.Superclass.Name.Lexeme)
			}
		}
	}
	for _, iface := range node.Implements {
		if ifaceDecl, ok := c.interfaceDecls[iface.Name.Lexeme]; ok {
			if ifaceDecl.IsSealed && !c.permitsType(ifaceDecl.Permits, node.Name.Lexeme) {
				return fmt.Errorf("line %d:%d: %s is not permitted to implement sealed interface %s", iface.Name.Line, iface.Name.Column, node.Name.Lexeme, iface.Name.Lexeme)
			}
		} else if ifaceSym, exists := c.lookup(iface.Name.Lexeme); exists {
			if ifaceSym.Type.IsInterface && ifaceSym.Type.IsSealed && !c.typePermits(ifaceSym.Type, node.Name.Lexeme) {
				return fmt.Errorf("line %d:%d: %s is not permitted to implement sealed interface %s", iface.Name.Line, iface.Name.Column, node.Name.Lexeme, iface.Name.Lexeme)
			}
		}
	}
	if !node.IsAbstract {
		missing := c.missingAbstractMethods(node.Name.Lexeme)
		if len(missing) > 0 {
			return fmt.Errorf("line %d:%d: concrete class %s must implement abstract methods: %s", node.Name.Line, node.Name.Column, node.Name.Lexeme, missing)
		}
	}
	return nil
}

func (c *Checker) missingAbstractMethods(className string) string {
	required := c.abstractMethodSet(className, map[string]bool{})
	if len(required) == 0 {
		return ""
	}
	parts := make([]string, 0, len(required))
	for name := range required {
		parts = append(parts, name)
	}
	return joinNames(parts)
}

func (c *Checker) abstractMethodSet(className string, seen map[string]bool) map[string]ast.MethodDecl {
	if seen[className] {
		return map[string]ast.MethodDecl{}
	}
	seen[className] = true
	required := make(map[string]ast.MethodDecl)
	classStmt, ok := c.classes[className]
	if !ok {
		if sym, exists := c.lookup(className); exists && sym.Type.Callable != nil {
			for name, member := range sym.Type.Callable.Return.Members {
				if member.Callable != nil && member.IsAbstract {
					required[name] = ast.MethodDecl{Name: token.Token{Lexeme: name}, IsAbstract: true}
				}
			}
		}
		return required
	}
	if classStmt.Superclass != nil {
		for name, method := range c.abstractMethodSet(classStmt.Superclass.Name.Lexeme, seen) {
			required[name] = method
		}
	}
	for _, method := range classStmt.Methods {
		if method.Static || method.IsConstructor {
			continue
		}
		if method.IsAbstract {
			required[method.Name.Lexeme] = method
			continue
		}
		delete(required, method.Name.Lexeme)
	}
	return required
}

func (c *Checker) permitsType(permits []*ast.TypeRef, name string) bool {
	for _, permit := range permits {
		if permit.Name.Lexeme == name {
			return true
		}
	}
	return false
}

func (c *Checker) typePermits(t Type, name string) bool {
	if len(t.Permits) == 0 {
		return false
	}
	return t.Permits[name]
}

func (c *Checker) knowsType(name string) bool {
	if _, ok := c.classes[name]; ok {
		return true
	}
	if _, ok := c.interfaceDecls[name]; ok {
		return true
	}
	_, ok := c.lookup(name)
	return ok
}

func permitSet(names []string) map[string]bool {
	if len(names) == 0 {
		return nil
	}
	permits := make(map[string]bool, len(names))
	for _, name := range names {
		permits[name] = true
	}
	return permits
}

func permitNamesFromRefs(permits []*ast.TypeRef) []string {
	if len(permits) == 0 {
		return nil
	}
	names := make([]string, 0, len(permits))
	for _, permit := range permits {
		names = append(names, permit.Name.Lexeme)
	}
	return names
}

func joinNames(items []string) string {
	if len(items) == 0 {
		return ""
	}
	out := items[0]
	for _, item := range items[1:] {
		out += ", " + item
	}
	return out
}

func (c *Checker) validateMethodAnnotations(classStmt *ast.ClassStmt, method ast.MethodDecl, classType Type, instanceType Type, callable Type) error {
	if !method.Static {
		switch method.Name.Lexeme {
		case "__eq":
			if len(callable.Callable.Params) != 1 {
				return fmt.Errorf("line %d:%d: __eq requires exactly one parameter", method.Name.Line, method.Name.Column)
			}
			if callable.Callable.Return.Name != runtime.TypeBool && callable.Callable.Return.Name != runtime.TypeAny && callable.Callable.Return.Name != "Unknown" {
				return fmt.Errorf("line %d:%d: __eq must return Bool", method.Name.Line, method.Name.Column)
			}
		case "__hash":
			if len(callable.Callable.Params) != 0 {
				return fmt.Errorf("line %d:%d: __hash requires zero parameters", method.Name.Line, method.Name.Column)
			}
		}
	}
	for _, annotation := range method.Annotations {
		switch normalizeAnnotation(annotation.Name.Lexeme) {
		case "Override":
			if !c.hasOverrideTarget(classStmt, method) {
				return fmt.Errorf("line %d:%d: @Override on %s requires a parent or interface method to override", annotation.Name.Line, annotation.Name.Column, method.Name.Lexeme)
			}
		case "Equals":
			if method.Static {
				return fmt.Errorf("line %d:%d: @Equals cannot be applied to static methods", annotation.Name.Line, annotation.Name.Column)
			}
			if len(callable.Callable.Params) != 1 {
				return fmt.Errorf("line %d:%d: @Equals requires exactly one parameter", annotation.Name.Line, annotation.Name.Column)
			}
			if callable.Callable.Return.Name != runtime.TypeBool {
				return fmt.Errorf("line %d:%d: @Equals must return Bool", annotation.Name.Line, annotation.Name.Column)
			}
		case "Hash":
			if method.Static {
				return fmt.Errorf("line %d:%d: @Hash cannot be applied to static methods", annotation.Name.Line, annotation.Name.Column)
			}
			if len(callable.Callable.Params) != 0 {
				return fmt.Errorf("line %d:%d: @Hash requires zero parameters", annotation.Name.Line, annotation.Name.Column)
			}
			if callable.Callable.Return.Name != runtime.TypeNumber {
				return fmt.Errorf("line %d:%d: @Hash must return Number", annotation.Name.Line, annotation.Name.Column)
			}
		}
	}
	_ = classType
	_ = instanceType
	return nil
}

func (c *Checker) hasOverrideTarget(classStmt *ast.ClassStmt, method ast.MethodDecl) bool {
	if classStmt.Superclass != nil {
		parentSym, ok := c.lookup(classStmt.Superclass.Name.Lexeme)
		if ok {
			if method.Static {
				if _, exists := parentSym.Type.Members[method.Name.Lexeme]; exists {
					return true
				}
			} else if parentSym.Type.Callable != nil {
				if _, exists := parentSym.Type.Callable.Return.Members[method.Name.Lexeme]; exists {
					return true
				}
			}
		}
	}
	for _, iface := range classStmt.Implements {
		if ifaceType, ok := c.interfaces[iface.Name.Lexeme]; ok {
			if _, exists := ifaceType.Members[method.Name.Lexeme]; exists {
				return true
			}
			continue
		}
		if ifaceSym, ok := c.lookup(iface.Name.Lexeme); ok {
			if _, exists := ifaceSym.Type.Members[method.Name.Lexeme]; exists {
				return true
			}
			continue
		}
		switch iface.Name.Lexeme {
		case "Iterable":
			if method.Name.Lexeme == "__length" || method.Name.Lexeme == "__get" {
				return true
			}
		case "Unstructured":
			if method.Name.Lexeme == "__pieces" || method.Name.Lexeme == "__get_piece" || method.Name.Lexeme == "__getPiece" {
				return true
			}
		}
	}
	return false
}

func (c *Checker) isAssignable(target Type, source Type) bool {
	target = c.resolveKnownInterfaceShape(target)
	source = c.resolveKnownInterfaceShape(source)
	if target.IsTypeParam && source.IsTypeParam && target.Name == source.Name {
		return true
	}
	if target.WildcardKind != "" {
		switch target.WildcardKind {
		case "any":
			return true
		case "extends":
			return target.BoundType != nil && c.isAssignable(*target.BoundType, source)
		case "super":
			return target.BoundType != nil && c.isAssignable(source, *target.BoundType)
		}
	}
	if target.IsTypeParam {
		if len(target.Bounds) == 0 {
			return true
		}
		for _, bound := range target.Bounds {
			if !c.isAssignable(bound, source) {
				return false
			}
		}
		return true
	}
	if target.Name == source.Name && (len(target.Args) > 0 || len(source.Args) > 0) && target.IsInterface == source.IsInterface && ((target.Callable != nil) == (source.Callable != nil)) {
		argCount := len(target.Args)
		if len(source.Args) > argCount {
			argCount = len(source.Args)
		}
		if argCount == 0 {
			return true
		}
		targetArgs := make([]Type, argCount)
		sourceArgs := make([]Type, argCount)
		for i := 0; i < argCount; i++ {
			if i < len(target.Args) {
				targetArgs[i] = target.Args[i]
			} else {
				targetArgs[i] = Any()
			}
			if i < len(source.Args) {
				sourceArgs[i] = source.Args[i]
			} else {
				sourceArgs[i] = Any()
			}
		}
		for i := 0; i < argCount; i++ {
			if targetArgs[i].WildcardKind != "" {
				if !c.isAssignable(targetArgs[i], sourceArgs[i]) {
					return false
				}
				continue
			}
			if !c.isAssignable(targetArgs[i], sourceArgs[i]) {
				return false
			}
		}
		return true
	}
	if target.IsInterface && c.explicitlyImplementsResolvedInterface(source, target) {
		return true
	}
	if target.Callable != nil && source.Callable != nil {
		if len(target.Callable.Params) != len(source.Callable.Params) {
			return false
		}
		for i := range target.Callable.Params {
			if !c.isAssignable(target.Callable.Params[i], source.Callable.Params[i]) {
				return false
			}
		}
		if c.isAssignable(target.Callable.Return, source.Callable.Return) {
			return true
		}
		return c.recursiveStructuralAssignable(target.Callable.Return, source.Callable.Return, map[string]bool{})
	}
	if target.IsAssignableFrom(source) {
		return true
	}
	if len(target.Args) == 0 && c.isSubclassOf(source.Name, target.Name) {
		return true
	}
	if target.IsInterface && source.Callable != nil {
		if len(target.Members) != 1 {
			return false
		}
		for _, member := range target.Members {
			return member.Callable != nil && c.isAssignable(member, source)
		}
	}
	if len(target.Members) > 0 && len(source.Members) > 0 {
		return c.recursiveStructuralAssignable(target, source, map[string]bool{})
	}
	if _, ok := c.interfaceDecls[target.Name]; ok {
		return c.implementsInterface(source, target.Name)
	}
	if target.IsInterface {
		for name, member := range target.Members {
			actual, exists := source.Members[name]
			if !exists || !c.isAssignable(member, actual) {
				return false
			}
		}
		return len(target.Members) > 0
	}
	return false
}

func (c *Checker) implementsInterface(source Type, ifaceName string) bool {
	ifaceType, ok := c.interfaces[ifaceName]
	if !ok {
		if ifaceSym, exists := c.lookup(ifaceName); exists && len(ifaceSym.Type.Members) > 0 {
			ifaceType = ifaceSym.Type
		} else {
			ifaceType = c.interfaceType(ifaceName, map[string]bool{})
		}
	}
	if source.Callable != nil {
		if len(ifaceType.Members) != 1 {
			return false
		}
		for _, member := range ifaceType.Members {
			return member.Callable != nil && member.IsAssignableFrom(source)
		}
	}
	for name, member := range ifaceType.Members {
		actual, exists := source.Members[name]
		if !exists || !c.isAssignable(member, actual) {
			return false
		}
	}
	return true
}

func (c *Checker) recursiveStructuralAssignable(target Type, source Type, seen map[string]bool) bool {
	if len(target.Members) == 0 || len(source.Members) == 0 {
		return false
	}
	key := DisplayName(target) + "<=" + DisplayName(source)
	if seen[key] {
		return true
	}
	seen[key] = true
	defer delete(seen, key)
	for name, member := range target.Members {
		actual, exists := source.Members[name]
		if !exists {
			return false
		}
		if member.Callable != nil || actual.Callable != nil {
			if member.Callable == nil || actual.Callable == nil {
				return false
			}
			if len(member.Callable.Params) != len(actual.Callable.Params) {
				return false
			}
			for i := range member.Callable.Params {
				if !c.isAssignable(member.Callable.Params[i], actual.Callable.Params[i]) {
					return false
				}
			}
			if !c.isAssignable(member.Callable.Return, actual.Callable.Return) && !c.recursiveStructuralAssignable(member.Callable.Return, actual.Callable.Return, seen) {
				return false
			}
			continue
		}
		if !c.isAssignable(member, actual) && !c.recursiveStructuralAssignable(member, actual, seen) {
			return false
		}
	}
	return true
}

func (c *Checker) explicitlyImplementsResolvedInterface(source Type, target Type) bool {
	classStmt, ok := c.classes[source.Name]
	if !ok || classStmt == nil {
		return false
	}
	bindings := make(map[string]Type, len(classStmt.TypeParams))
	for i, param := range classStmt.TypeParams {
		if i < len(source.Args) {
			bindings[param.Name.Lexeme] = source.Args[i]
		}
	}
	for _, ifaceRef := range classStmt.Implements {
		if ifaceRef.Name.Lexeme != target.Name {
			continue
		}
		resolved := c.resolveTypeRefWithBindings(ifaceRef, bindings)
		if c.isAssignable(target, resolved) {
			return true
		}
	}
	return false
}

func (c *Checker) resolveTypeRefWithBindings(typeRef *ast.TypeRef, bindings map[string]Type) Type {
	if len(bindings) == 0 {
		return c.resolveTypeRef(typeRef)
	}
	c.pushScope()
	defer c.popScope()
	for name, resolved := range bindings {
		c.currentScope()[name] = symbol{Type: resolved, Mutable: false}
	}
	return c.resolveTypeRef(typeRef)
}

func (c *Checker) ensureMemberAccessible(objType Type, memberName string, read bool, line int, col int) error {
	declaringClass, visibility, _, found := c.lookupClassMember(objType.Name, memberName)
	if !found {
		return nil
	}
	switch visibility {
	case ast.VisibilityPublic, "":
		return nil
	case ast.VisibilityPrivate:
		if c.currentClass != nil && c.currentClass.Name.Lexeme == declaringClass {
			return nil
		}
	case ast.VisibilityProtected:
		if c.currentClass != nil && (c.currentClass.Name.Lexeme == declaringClass || c.isSubclassOf(c.currentClass.Name.Lexeme, declaringClass)) {
			return nil
		}
	}
	action := "access"
	if !read {
		action = "assign"
	}
	return fmt.Errorf("line %d:%d: cannot %s %s member %s.%s", line, col, action, visibility, declaringClass, memberName)
}

func (c *Checker) ensureConstructorAccessible(className string, visibility string, line int, col int) error {
	switch visibility {
	case "", string(ast.VisibilityPublic):
		return nil
	case string(ast.VisibilityPrivate):
		if c.currentClass != nil && c.currentClass.Name.Lexeme == className {
			return nil
		}
	case string(ast.VisibilityProtected):
		if c.currentClass != nil && (c.currentClass.Name.Lexeme == className || c.isSubclassOf(c.currentClass.Name.Lexeme, className)) {
			return nil
		}
	}
	return fmt.Errorf("line %d:%d: cannot access %s constructor of %s", line, col, visibility, className)
}

func (c *Checker) lookupClassMember(className string, memberName string) (string, ast.Visibility, bool, bool) {
	classStmt, ok := c.classes[className]
	if !ok {
		return "", "", false, false
	}
	for _, field := range classStmt.Fields {
		if field.Name.Lexeme == memberName {
			return classStmt.Name.Lexeme, field.Visibility, field.Static, true
		}
	}
	for _, method := range classStmt.Methods {
		if method.Name.Lexeme == memberName && !method.IsConstructor {
			return classStmt.Name.Lexeme, method.Visibility, method.Static, true
		}
	}
	if classStmt.Superclass != nil {
		return c.lookupClassMember(classStmt.Superclass.Name.Lexeme, memberName)
	}
	return "", "", false, false
}

func (c *Checker) isSubclassOf(className string, ancestor string) bool {
	current, ok := c.classes[className]
	for ok && current.Superclass != nil {
		if current.Superclass.Name.Lexeme == ancestor {
			return true
		}
		current, ok = c.classes[current.Superclass.Name.Lexeme]
	}
	return false
}

func normalizeAnnotation(name string) string {
	switch name {
	case "override", "Override", "OVERRIDE":
		return "Override"
	case "equals", "Equals", "EQUALS":
		return "Equals"
	case "hash", "Hash", "HASH":
		return "Hash"
	default:
		return name
	}
}

func (c *Checker) pushScope() {
	c.scopes = append(c.scopes, map[string]symbol{})
	c.typeScopes = append(c.typeScopes, map[string]Type{})
}

func (c *Checker) popScope() {
	c.scopes = c.scopes[:len(c.scopes)-1]
	c.typeScopes = c.typeScopes[:len(c.typeScopes)-1]
}

func (c *Checker) currentScope() map[string]symbol {
	return c.scopes[len(c.scopes)-1]
}

func (c *Checker) currentTypeScope() map[string]Type {
	return c.typeScopes[len(c.typeScopes)-1]
}

func (c *Checker) lookupTypeAlias(name string) (Type, bool) {
	for i := len(c.typeScopes) - 1; i >= 0; i-- {
		if resolved, ok := c.typeScopes[i][name]; ok {
			return resolved, true
		}
	}
	return Unknown(), false
}

func (c *Checker) lookup(name string) (symbol, bool) {
	for i := len(c.scopes) - 1; i >= 0; i-- {
		if t, ok := c.scopes[i][name]; ok {
			return t, true
		}
	}
	return symbol{Type: Unknown()}, false
}
