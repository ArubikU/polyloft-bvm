package sema

import (
	"fmt"

	"github.com/ArubikU/polyloft-bvm/internal/ast"
	"github.com/ArubikU/polyloft-bvm/internal/runtime"
	"github.com/ArubikU/polyloft-bvm/internal/token"
)

type Checker struct {
	registry       *runtime.Registry
	scopes         []map[string]symbol
	typeScopes     []map[string]Type
	currentFunc    *CallableType
	currentClass   *ast.ClassStmt
	currentMethod  *ast.MethodDecl
	interfaces     map[string]Type
	interfaceDecls map[string]*ast.InterfaceStmt
	typeAliasDecls map[string]*ast.TypeAliasStmt
	classes        map[string]*ast.ClassStmt
	specTypeCache  map[string]*Type
	instanceCache  map[string]*Type
	resolvingTypes map[string]bool
	insideLoop     int
}

type symbol struct {
	Type    Type
	Mutable bool
}

func Check(program *ast.Program, registry *runtime.Registry) error {
	checker := &Checker{registry: registry, scopes: []map[string]symbol{{}}, typeScopes: []map[string]Type{{}}, interfaces: map[string]Type{}, interfaceDecls: map[string]*ast.InterfaceStmt{}, typeAliasDecls: map[string]*ast.TypeAliasStmt{}, classes: map[string]*ast.ClassStmt{}, specTypeCache: map[string]*Type{}, instanceCache: map[string]*Type{}, resolvingTypes: map[string]bool{}}
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
	return map[string]Type{
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
		if !assignedType.IsAssignableFrom(valueType) {
			return fmt.Errorf("cannot assign %s through index of %s", valueType.Name, objType.Name)
		}
		return nil
	case *ast.ExprStmt:
		_, err := c.checkExpr(node.Expr)
		return err
	case *ast.IfStmt:
		condType, err := c.checkExpr(node.Condition)
		if err != nil {
			return err
		}
		if !isBooleanCompatibleType(condType.Name) && condType.Name != runtime.TypeAny {
			return fmt.Errorf("if condition must be Bool or Boolean, got %s", condType.Name)
		}
		if err := c.checkBlock(node.Then); err != nil {
			return err
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
					boundType := Primitive(pattern.Type.Type.Name.Lexeme)
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
			return fmt.Errorf("line %d:%d: return type %s does not match %s", node.Keyword.Line, node.Keyword.Column, retType.Name, c.currentFunc.Return.Name)
		}
		return nil
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
		for _, bodyStmt := range node.Body.Statements {
			if err := c.checkStmt(bodyStmt); err != nil {
				c.currentFunc = prev
				c.popScope()
				return err
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
		if node.Condition != nil {
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
		for _, bodyStmt := range node.Body.Statements {
			if err := c.checkStmt(bodyStmt); err != nil {
				c.popScope()
				return err
			}
		}
		c.popScope()
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
			for _, stmt := range method.Body.Statements {
				if err := c.checkStmt(stmt); err != nil {
					c.popScope()
					c.currentFunc = prev
					return err
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
					classSym.Type.Members[method.Name.Lexeme] = callable
				} else {
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
		return false
	case token.MinusEqual, token.StarEqual, token.SlashEqual:
		if target.Name == runtime.TypeAny || valueType.Name == runtime.TypeAny {
			return true
		}
		if isNumericCompatibleType(target.Name) && isNumericCompatibleType(valueType.Name) {
			resultType := numericBinaryType(operator.Type, target, valueType)
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
	if expected.Callable != nil {
		return expected.Callable
	}
	if expected.IsInterface && len(expected.Members) == 1 {
		for _, member := range expected.Members {
			if member.Callable != nil {
				return member.Callable
			}
		}
	}
	return nil
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
		if calleeType.Callable == nil {
			return Unknown(), fmt.Errorf("line %d:%d: expression is not callable", node.Paren.Line, node.Paren.Column)
		}
		if !calleeType.Callable.Variadic && len(node.Arguments) != len(calleeType.Callable.Params) {
			return Unknown(), fmt.Errorf("line %d:%d: expected %d arguments, got %d", node.Paren.Line, node.Paren.Column, len(calleeType.Callable.Params), len(node.Arguments))
		}
		specializedParams := calleeType.Callable.Params
		specializedReturn := calleeType.Callable.Return
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
					argType, err = c.checkExprWithExpected(node.Arguments[i], &specializedParams[i])
					if err != nil {
						return Unknown(), err
					}
				}
				if calleeType.Callable.Variadic {
					continue
				}
				if !c.isAssignable(specializedParams[i], argType) {
					return Unknown(), fmt.Errorf("line %d:%d: argument %d expects %s, got %s", node.Paren.Line, node.Paren.Column, i+1, specializedParams[i].Name, argType.Name)
				}
			}
			return specializedReturn, nil
		}
		for i, arg := range node.Arguments {
			argType, err := c.checkExprWithExpected(arg, &specializedParams[i])
			if err != nil {
				return Unknown(), err
			}
			if calleeType.Callable.Variadic {
				continue
			}
			if !c.isAssignable(specializedParams[i], argType) {
				return Unknown(), fmt.Errorf("line %d:%d: argument %d expects %s, got %s", node.Paren.Line, node.Paren.Column, i+1, specializedParams[i].Name, argType.Name)
			}
		}
		return specializedReturn, nil
	case *ast.NewExpr:
		classSymbol, ok := c.lookup(node.Class.Lexeme)
		if !ok {
			return Unknown(), fmt.Errorf("line %d:%d: undefined class %s", node.Class.Line, node.Class.Column, node.Class.Lexeme)
		}
		calleeType := classSymbol.Type
		if calleeType.Callable == nil {
			return Unknown(), fmt.Errorf("line %d:%d: %s is not constructible", node.Class.Line, node.Class.Column, node.Class.Lexeme)
		}
		if len(node.Arguments) != len(calleeType.Callable.Params) {
			return Unknown(), fmt.Errorf("line %d:%d: expected %d arguments, got %d", node.Paren.Line, node.Paren.Column, len(calleeType.Callable.Params), len(node.Arguments))
		}
		if calleeType.IsAbstract {
			return Unknown(), fmt.Errorf("line %d:%d: cannot instantiate abstract class %s", node.Class.Line, node.Class.Column, node.Class.Lexeme)
		}
		if err := c.ensureConstructorAccessible(node.Class.Lexeme, calleeType.ConstructorVisibility, node.Class.Line, node.Class.Column); err != nil {
			return Unknown(), err
		}
		specializedParams := calleeType.Callable.Params
		specializedReturn := calleeType.Callable.Return
		bindings := make(map[string]Type)
		argTypes := make([]Type, len(node.Arguments))
		for i, arg := range node.Arguments {
			argType, err := c.checkExpr(arg)
			if err != nil {
				return Unknown(), err
			}
			argTypes[i] = argType
			if len(calleeType.Args) > 0 {
				c.inferGenericBindings(calleeType.Callable.Params[i], argType, bindings)
			}
		}
		if len(calleeType.Args) > 0 {
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
		}
		for i, argType := range argTypes {
			if _, isLambda := node.Arguments[i].(*ast.LambdaExpr); isLambda {
				var err error
				argType, err = c.checkExprWithExpected(node.Arguments[i], &specializedParams[i])
				if err != nil {
					return Unknown(), err
				}
			}
			if !c.isAssignable(specializedParams[i], argType) {
				return Unknown(), fmt.Errorf("line %d:%d: constructor argument %d expects %s, got %s", node.Paren.Line, node.Paren.Column, i+1, specializedParams[i].Name, argType.Name)
			}
		}
		return specializedReturn, nil
	case *ast.GetExpr:
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
		if left.Name == runtime.TypeString || right.Name == runtime.TypeString {
			return Primitive(runtime.TypeString), nil
		}
		if (isNumericCompatibleType(left.Name) || left.Name == runtime.TypeAny) && (isNumericCompatibleType(right.Name) || right.Name == runtime.TypeAny) {
			return numericBinaryType(token.Plus, left, right), nil
		}
		return Unknown(), fmt.Errorf("line %d:%d: '+' expects Number/Number or String concatenation, got %s and %s", node.Operator.Line, node.Operator.Column, left.Name, right.Name)
	case token.Minus, token.Star, token.Slash, token.Percent:
		if (!isNumericCompatibleType(left.Name) && left.Name != runtime.TypeAny) || (!isNumericCompatibleType(right.Name) && right.Name != runtime.TypeAny) {
			return Unknown(), fmt.Errorf("line %d:%d: arithmetic expects Number, got %s and %s", node.Operator.Line, node.Operator.Column, left.Name, right.Name)
		}
		return numericBinaryType(node.Operator.Type, left, right), nil
	case token.Greater, token.GreaterEqual, token.Less, token.LessEqual:
		if isTextComparableType(left.Name) && isTextComparableType(right.Name) {
			return Primitive(runtime.TypeBool), nil
		}
		if (!isNumericCompatibleType(left.Name) && left.Name != runtime.TypeAny) || (!isNumericCompatibleType(right.Name) && right.Name != runtime.TypeAny) {
			return Unknown(), fmt.Errorf("line %d:%d: comparison expects Number, got %s and %s", node.Operator.Line, node.Operator.Column, left.Name, right.Name)
		}
		return Primitive(runtime.TypeBool), nil
	case token.In:
		if !right.SupportsContains() {
			return Unknown(), fmt.Errorf("line %d:%d: %s does not support 'in'", node.Operator.Line, node.Operator.Column, right.Name)
		}
		containsType := right.ContainsKeyType()
		if !containsType.IsAssignableFrom(left) {
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
	c.pushScope()
	typeParams := c.bindTypeParams(classStmt.TypeParams)
	defer c.popScope()
	instanceMembers := make(map[string]Type, len(classStmt.Fields)+len(classStmt.Methods))
	classMembers := make(map[string]Type, len(classStmt.Fields)+len(classStmt.Methods))
	instance := Type{Name: classStmt.Name.Lexeme, Args: append([]Type(nil), typeParams...), Members: instanceMembers, IsAbstract: classStmt.IsAbstract, IsEnum: classStmt.IsEnum, IsSealed: classStmt.IsSealed || classStmt.IsFinal || classStmt.IsEnum, IsRecord: classStmt.IsRecord, Permits: permitSet(permitNamesFromRefs(classStmt.Permits))}
	ctorParams := []Type{}
	ctorVisibility := string(ast.VisibilityPublic)
	if classStmt.IsEnum {
		instanceMembers["name"] = Primitive(runtime.TypeString)
		instanceMembers["ordinal"] = Primitive(runtime.TypeNumber)
	}
	if classStmt.Superclass != nil {
		parentType := c.resolveTypeRef(classStmt.Superclass)
		if parent, ok := c.lookup(classStmt.Superclass.Name.Lexeme); ok {
			_ = parent
			for name, member := range parentType.Members {
				classMembers[name] = member
			}
			if parentType.Callable != nil {
				for name, member := range parentType.Callable.Return.Members {
					instanceMembers[name] = member
				}
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
			ctorVisibility = string(method.Visibility)
			continue
		}
		if method.Static {
			classMembers[method.Name.Lexeme] = methodType
		} else {
			instanceMembers[method.Name.Lexeme] = methodType
		}
	}
	if classStmt.IsEnum {
		for _, enumValue := range classStmt.EnumValues {
			classMembers[enumValue.Name.Lexeme] = instance
		}
		classMembers["valueOf"] = Type{Name: runtime.TypeFunction, Callable: &CallableType{Params: []Type{Primitive(runtime.TypeString)}, Return: instance}}
		classMembers["values"] = Type{Name: runtime.TypeFunction, Callable: &CallableType{Params: nil, Return: Primitive(runtime.TypeArray)}}
		classMembers["names"] = Type{Name: runtime.TypeFunction, Callable: &CallableType{Params: nil, Return: Primitive(runtime.TypeArray)}}
		classMembers["size"] = Type{Name: runtime.TypeFunction, Callable: &CallableType{Params: nil, Return: Primitive(runtime.TypeNumber)}}
		return Type{Name: classStmt.Name.Lexeme, Args: append([]Type(nil), typeParams...), Members: classMembers, Callable: &CallableType{Params: nil, Return: instance}, ConstructorVisibility: string(ast.VisibilityPrivate), IsEnum: true, IsSealed: true, IsRecord: classStmt.IsRecord, Permits: permitSet(permitNamesFromRefs(classStmt.Permits))}
	}
	return Type{Name: classStmt.Name.Lexeme, Args: append([]Type(nil), typeParams...), Members: classMembers, Callable: &CallableType{Params: ctorParams, Return: instance}, ConstructorVisibility: ctorVisibility, IsAbstract: classStmt.IsAbstract, IsEnum: classStmt.IsEnum, IsSealed: classStmt.IsSealed || classStmt.IsFinal, IsRecord: classStmt.IsRecord, Permits: permitSet(permitNamesFromRefs(classStmt.Permits))}
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
	if iface, ok := c.interfaces[name]; ok {
		return iface
	}
	if seen[name] {
		return Type{Name: name, Members: map[string]Type{}}
	}
	decl, ok := c.interfaceDecls[name]
	if !ok {
		return Primitive(name)
	}
	seen[name] = true
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
		return applyTypeArgs(alias, args)
	}
	if decl, ok := c.typeAliasDecls[typeRef.Name.Lexeme]; ok {
		if c.resolvingTypes[typeRef.Name.Lexeme] {
			return Unknown()
		}
		c.resolvingTypes[typeRef.Name.Lexeme] = true
		resolved := c.resolveTypeRef(decl.Target)
		delete(c.resolvingTypes, typeRef.Name.Lexeme)
		c.typeScopes[0][typeRef.Name.Lexeme] = resolved
		return applyTypeArgs(resolved, args)
	}
	if iface, ok := c.interfaceDecls[typeRef.Name.Lexeme]; ok && iface != nil {
		return applyTypeArgs(c.interfaceType(typeRef.Name.Lexeme, map[string]bool{}), args)
	}
	if sym, ok := c.lookup(typeRef.Name.Lexeme); ok && (sym.Type.Name == typeRef.Name.Lexeme || sym.Type.IsTypeParam) {
		return applyTypeArgs(sym.Type, args)
	}
	if builtin, ok := sourceBuiltinType(typeRef.Name.Lexeme); ok {
		return applyTypeArgs(Primitive(builtin), args)
	}
	return applyTypeArgs(Primitive(typeRef.Name.Lexeme), args)
}

func sourceBuiltinType(name string) (string, bool) {
	switch name {
	case "int":
		return runtime.TypeInt, true
	case "float":
		return runtime.TypeFloat, true
	case "number":
		return runtime.TypeNumber, true
	case "bool":
		return runtime.TypeBool, true
	case "char":
		return runtime.TypeChar, true
	case "String":
		return runtime.TypeString, true
	case "string":
		return runtime.TypeString, true
	case "nil":
		return runtime.TypeNil, true
	case "void":
		return runtime.TypeVoid, true
	case "array":
		return runtime.TypeArray, true
	case "map":
		return runtime.TypeMap, true
	case "tuple":
		return runtime.TypeTuple, true
	case "Range":
		return runtime.TypeRange, true
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
			if localTypeParams != nil {
				if resolved, ok := localTypeParams[param]; ok {
					params[i] = resolved
					continue
				}
			}
			params[i] = Primitive(param)
		}
		ret := Primitive(spec.Callable.Return)
		if localTypeParams != nil {
			if resolved, ok := localTypeParams[spec.Callable.Return]; ok {
				ret = resolved
			}
		}
		if len(spec.InstanceMembers) > 0 {
			ret = c.instanceTypeFromSpecs(spec.Callable.Return, spec.InstanceMembers)
		}
		t.Callable = &CallableType{Params: params, Return: ret, Variadic: spec.Callable.Variadic}
	}
	return *t
}

func (c *Checker) instanceTypeFromSpecs(typeName string, members map[string]runtime.Spec) Type {
	cacheKey := Primitive(typeName).Name
	if cached, ok := c.instanceCache[cacheKey]; ok {
		return *cached
	}
	instanceType := &Type{Name: cacheKey, Members: make(map[string]Type, len(members))}
	c.instanceCache[cacheKey] = instanceType
	for name, member := range members {
		instanceType.Members[name] = c.shallowTypeFromSpec(member)
	}
	for name, member := range members {
		if member.Callable == nil || len(member.InstanceMembers) == 0 {
			continue
		}
		memberType := instanceType.Members[name]
		memberType.Callable.Return = c.instanceTypeFromSpecs(member.Callable.Return, member.InstanceMembers)
		instanceType.Members[name] = memberType
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
			params[i] = Primitive(param)
		}
		ret := Primitive(spec.Callable.Return)
		if len(spec.InstanceMembers) > 0 {
			ret = Type{Name: Primitive(spec.Callable.Return).Name}
		}
		t.Callable = &CallableType{Params: params, Return: ret, Variadic: spec.Callable.Variadic}
	}
	return t
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
				if !c.isAssignable(member, actual) {
					return fmt.Errorf("line %d:%d: %s.%s does not match interface %s", iface.Name.Line, iface.Name.Column, classStmt.Name.Lexeme, name, iface.Name.Lexeme)
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
				if !c.isAssignable(member, actual) {
					return fmt.Errorf("line %d:%d: %s.%s does not match interface %s", iface.Name.Line, iface.Name.Column, classStmt.Name.Lexeme, name, iface.Name.Lexeme)
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
	if target.Name == source.Name && len(target.Args) > 0 && target.IsInterface == source.IsInterface && ((target.Callable != nil) == (source.Callable != nil)) {
		if len(target.Args) != len(source.Args) {
			return false
		}
		for i := range target.Args {
			if target.Args[i].WildcardKind != "" {
				if !c.isAssignable(target.Args[i], source.Args[i]) {
					return false
				}
				continue
			}
			if !c.isAssignable(target.Args[i], source.Args[i]) {
				return false
			}
		}
		return true
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
		for name, member := range target.Members {
			actual, exists := source.Members[name]
			if !exists || !c.isAssignable(member, actual) {
				return false
			}
		}
		return true
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
