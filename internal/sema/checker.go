package sema

import (
	"fmt"

	"github.com/ArubikU/polyloft-bvm/internal/ast"
	"github.com/ArubikU/polyloft-bvm/internal/runtime"
	"github.com/ArubikU/polyloft-bvm/internal/token"
)

type Checker struct {
	registry    *runtime.Registry
	scopes      []map[string]symbol
	currentFunc *CallableType
	insideLoop  int
}

type symbol struct {
	Type Type
}

func Check(program *ast.Program, registry *runtime.Registry) error {
	checker := &Checker{registry: registry, scopes: []map[string]symbol{{}}}
	checker.installBuiltins()
	for _, stmt := range program.Statements {
		if fn, ok := stmt.(*ast.FunctionStmt); ok {
			checker.currentScope()[fn.Name.Lexeme] = symbol{Type: checker.functionType(fn)}
		}
		if classStmt, ok := stmt.(*ast.ClassStmt); ok {
			checker.currentScope()[classStmt.Name.Lexeme] = symbol{Type: checker.classType(classStmt)}
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
		c.currentScope()[name] = symbol{Type: c.typeFromSpec(spec)}
	}
}

func (c *Checker) checkStmt(stmt ast.Stmt) error {
	switch node := stmt.(type) {
	case *ast.LetStmt:
		valueType, err := c.checkExpr(node.Value)
		if err != nil {
			return err
		}
		declared := valueType
		if node.Type != nil {
			declared = Primitive(node.Type.Name.Lexeme)
			if !declared.IsAssignableFrom(valueType) {
				return fmt.Errorf("line %d:%d: cannot assign %s to %s", node.Name.Line, node.Name.Column, valueType.Name, declared.Name)
			}
		}
		c.currentScope()[node.Name.Lexeme] = symbol{Type: declared}
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
			c.currentScope()[target.Lexeme] = symbol{Type: targetTypes[i]}
		}
		return nil
	case *ast.AssignStmt:
		target, ok := c.lookup(node.Name.Lexeme)
		if !ok {
			return fmt.Errorf("line %d:%d: undefined variable %s", node.Name.Line, node.Name.Column, node.Name.Lexeme)
		}
		valueType, err := c.checkExpr(node.Value)
		if err != nil {
			return err
		}
		if !target.Type.IsAssignableFrom(valueType) {
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
		valueType, err := c.checkExpr(node.Value)
		if err != nil {
			return err
		}
		if !member.IsAssignableFrom(valueType) {
			return fmt.Errorf("line %d:%d: cannot assign %s to field %s of type %s", node.Name.Line, node.Name.Column, valueType.Name, node.Name.Lexeme, member.Name)
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
		if condType.Name != runtime.TypeBool && condType.Name != runtime.TypeAny {
			return fmt.Errorf("if condition must be Bool, got %s", condType.Name)
		}
		if err := c.checkBlock(node.Then); err != nil {
			return err
		}
		if node.Else != nil {
			return c.checkBlock(node.Else)
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
		retType, err := c.checkExpr(node.Value)
		if err != nil {
			return err
		}
		if c.currentFunc.Return.Name == "Unknown" {
			c.currentFunc.Return = retType
			return nil
		}
		if !c.currentFunc.Return.IsAssignableFrom(retType) {
			return fmt.Errorf("line %d:%d: return type %s does not match %s", node.Keyword.Line, node.Keyword.Column, retType.Name, c.currentFunc.Return.Name)
		}
		return nil
	case *ast.FunctionStmt:
		fnType := c.functionType(node)
		c.currentScope()[node.Name.Lexeme] = symbol{Type: fnType}
		c.pushScope()
		prev := c.currentFunc
		c.currentFunc = fnType.Callable
		for i, param := range node.Params {
			c.currentScope()[param.Name.Lexeme] = symbol{Type: fnType.Callable.Params[i]}
		}
		for _, bodyStmt := range node.Body.Statements {
			if err := c.checkStmt(bodyStmt); err != nil {
				c.currentFunc = prev
				c.popScope()
				return err
			}
		}
		c.currentScope()[node.Name.Lexeme] = symbol{Type: fnType}
		c.currentFunc = prev
		c.popScope()
		return nil
	case *ast.ForStmt:
		iterType, err := c.checkExpr(node.Iterable)
		if err != nil {
			return err
		}
		if !iterType.SupportsIterable() {
			return fmt.Errorf("line %d:%d: for-in expects Iterable, got %s", node.Targets[0].Line, node.Targets[0].Column, iterType.Name)
		}
		itemType := iterType.IterableItemType()
		c.pushScope()
		if len(node.Targets) == 1 {
			c.currentScope()[node.Targets[0].Lexeme] = symbol{Type: itemType}
		} else {
			targetTypes, ok := itemType.DestructureTypes(len(node.Targets))
			if !ok {
				c.popScope()
				return fmt.Errorf("line %d:%d: iterable elements of type %s cannot be destructured into %d targets", node.Targets[0].Line, node.Targets[0].Column, itemType.Name, len(node.Targets))
			}
			for i, target := range node.Targets {
				c.currentScope()[target.Lexeme] = symbol{Type: targetTypes[i]}
			}
		}
		if node.Condition != nil {
			condType, err := c.checkExpr(node.Condition)
			if err != nil {
				c.popScope()
				return err
			}
			if condType.Name != runtime.TypeBool && condType.Name != runtime.TypeAny {
				c.popScope()
				return fmt.Errorf("where condition must be Bool, got %s", condType.Name)
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
		classSym, _ := c.lookup(node.Name.Lexeme)
		instanceType := classSym.Type
		if classSym.Type.Callable != nil {
			instanceType = classSym.Type.Callable.Return
		}
		if err := c.validateImplementedInterfaces(node, instanceType); err != nil {
			return err
		}
		for _, method := range node.Methods {
			callable := c.methodType(instanceType, method)
			prev := c.currentFunc
			c.currentFunc = callable.Callable
			c.pushScope()
			if !method.Static {
				c.currentScope()["this"] = symbol{Type: instanceType}
			}
			for i, param := range method.Params {
				c.currentScope()[param.Name.Lexeme] = symbol{Type: callable.Callable.Params[i]}
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
		}
		return nil
	default:
		return nil
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
	switch node := expr.(type) {
	case *ast.LiteralExpr:
		switch node.Value.(type) {
		case nil:
			return Primitive(runtime.TypeNil), nil
		case bool:
			return Primitive(runtime.TypeBool), nil
		case float64:
			return Primitive(runtime.TypeNumber), nil
		case string:
			return Primitive(runtime.TypeString), nil
		default:
			return Any(), nil
		}
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
	case *ast.UnaryExpr:
		right, err := c.checkExpr(node.Right)
		if err != nil {
			return Unknown(), err
		}
		switch node.Operator.Type {
		case token.Minus:
			if right.Name != runtime.TypeNumber && right.Name != runtime.TypeAny {
				return Unknown(), fmt.Errorf("line %d:%d: unary '-' expects Number, got %s", node.Operator.Line, node.Operator.Column, right.Name)
			}
			return Primitive(runtime.TypeNumber), nil
		case token.Bang:
			if right.Name != runtime.TypeBool && right.Name != runtime.TypeAny {
				return Unknown(), fmt.Errorf("line %d:%d: unary '!' expects Bool, got %s", node.Operator.Line, node.Operator.Column, right.Name)
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
		for i, arg := range node.Arguments {
			argType, err := c.checkExpr(arg)
			if err != nil {
				return Unknown(), err
			}
			if calleeType.Callable.Variadic {
				continue
			}
			if !calleeType.Callable.Params[i].IsAssignableFrom(argType) {
				return Unknown(), fmt.Errorf("line %d:%d: argument %d expects %s, got %s", node.Paren.Line, node.Paren.Column, i+1, calleeType.Callable.Params[i].Name, argType.Name)
			}
		}
		return calleeType.Callable.Return, nil
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
		for i, arg := range node.Arguments {
			argType, err := c.checkExpr(arg)
			if err != nil {
				return Unknown(), err
			}
			if !calleeType.Callable.Params[i].IsAssignableFrom(argType) {
				return Unknown(), fmt.Errorf("line %d:%d: constructor argument %d expects %s, got %s", node.Paren.Line, node.Paren.Column, i+1, calleeType.Callable.Params[i].Name, argType.Name)
			}
		}
		return calleeType.Callable.Return, nil
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
	default:
		return Unknown(), nil
	}
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
		if (left.Name != runtime.TypeBool && left.Name != runtime.TypeAny) || (right.Name != runtime.TypeBool && right.Name != runtime.TypeAny) {
			return Unknown(), fmt.Errorf("line %d:%d: logical operator expects Bool, got %s and %s", node.Operator.Line, node.Operator.Column, left.Name, right.Name)
		}
		return Primitive(runtime.TypeBool), nil
	case token.Plus:
		if left.Name == runtime.TypeString || right.Name == runtime.TypeString {
			return Primitive(runtime.TypeString), nil
		}
		if (left.Name == runtime.TypeNumber || left.Name == runtime.TypeAny) && (right.Name == runtime.TypeNumber || right.Name == runtime.TypeAny) {
			return Primitive(runtime.TypeNumber), nil
		}
		return Unknown(), fmt.Errorf("line %d:%d: '+' expects Number/Number or String concatenation, got %s and %s", node.Operator.Line, node.Operator.Column, left.Name, right.Name)
	case token.Minus, token.Star, token.Slash, token.Percent:
		if (left.Name != runtime.TypeNumber && left.Name != runtime.TypeAny) || (right.Name != runtime.TypeNumber && right.Name != runtime.TypeAny) {
			return Unknown(), fmt.Errorf("line %d:%d: arithmetic expects Number, got %s and %s", node.Operator.Line, node.Operator.Column, left.Name, right.Name)
		}
		return Primitive(runtime.TypeNumber), nil
	case token.EqualEqual, token.BangEqual, token.Greater, token.GreaterEqual, token.Less, token.LessEqual:
		return Primitive(runtime.TypeBool), nil
	default:
		return Unknown(), nil
	}
}

func (c *Checker) functionType(fn *ast.FunctionStmt) Type {
	params := make([]Type, len(fn.Params))
	for i, param := range fn.Params {
		if param.Type != nil {
			params[i] = Primitive(param.Type.Name.Lexeme)
		} else {
			params[i] = Any()
		}
	}
	ret := Unknown()
	if fn.ReturnType != nil {
		ret = Primitive(fn.ReturnType.Name.Lexeme)
	}
	return Type{Name: runtime.TypeFunction, Callable: &CallableType{Params: params, Return: ret}}
}

func (c *Checker) classType(classStmt *ast.ClassStmt) Type {
	instanceMembers := make(map[string]Type, len(classStmt.Fields)+len(classStmt.Methods))
	classMembers := make(map[string]Type, len(classStmt.Fields)+len(classStmt.Methods))
	instance := Type{Name: classStmt.Name.Lexeme, Members: instanceMembers}
	ctorParams := []Type{}
	for _, field := range classStmt.Fields {
		fieldType := Any()
		if field.Type != nil {
			fieldType = Primitive(field.Type.Name.Lexeme)
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
			ctorParams = methodType.Callable.Params
			continue
		}
		if method.Static {
			classMembers[method.Name.Lexeme] = methodType
		} else {
			instanceMembers[method.Name.Lexeme] = methodType
		}
	}
	return Type{Name: classStmt.Name.Lexeme, Members: classMembers, Callable: &CallableType{Params: ctorParams, Return: instance}}
}

func (c *Checker) methodType(instance Type, method ast.MethodDecl) Type {
	params := make([]Type, len(method.Params))
	for i, param := range method.Params {
		if param.Type != nil {
			params[i] = Primitive(param.Type.Name.Lexeme)
		} else {
			params[i] = Any()
		}
	}
	ret := Unknown()
	if method.IsConstructor {
		ret = instance
	} else if method.ReturnType != nil {
		ret = Primitive(method.ReturnType.Name.Lexeme)
	}
	return Type{Name: runtime.TypeFunction, Callable: &CallableType{Params: params, Return: ret}}
}

func (c *Checker) typeFromSpec(spec runtime.Spec) Type {
	t := Type{Name: spec.TypeName}
	if spec.Module != nil {
		t.Module = spec.Module
	}
	if spec.Callable != nil {
		params := make([]Type, len(spec.Callable.Params))
		for i, param := range spec.Callable.Params {
			params[i] = Primitive(param)
		}
		ret := Primitive(spec.Callable.Return)
		t = Type{Name: runtime.TypeFunction, Callable: &CallableType{Params: params, Return: ret, Variadic: spec.Callable.Variadic}}
	}
	return t
}

func (c *Checker) validateImplementedInterfaces(classStmt *ast.ClassStmt, instanceType Type) error {
	for _, iface := range classStmt.Implements {
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
}

func (c *Checker) popScope() {
	c.scopes = c.scopes[:len(c.scopes)-1]
}

func (c *Checker) currentScope() map[string]symbol {
	return c.scopes[len(c.scopes)-1]
}

func (c *Checker) lookup(name string) (symbol, bool) {
	for i := len(c.scopes) - 1; i >= 0; i-- {
		if t, ok := c.scopes[i][name]; ok {
			return t, true
		}
	}
	return symbol{Type: Unknown()}, false
}
