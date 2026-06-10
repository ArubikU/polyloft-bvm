package parser

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ArubikU/polyloft-bvm/internal/ast"
	"github.com/ArubikU/polyloft-bvm/internal/token"
)

type Parser struct {
	tokens             []token.Token
	cur                int
	allowRangeShortcut bool
}

func Parse(tokens []token.Token) (*ast.Program, error) {
	p := &Parser{tokens: tokens, allowRangeShortcut: true}
	return p.parseProgram()
}

func (p *Parser) parseProgram() (*ast.Program, error) {
	program := &ast.Program{}
	p.skipNewlines()
	for !p.isAtEnd() {
		stmt, err := p.topLevelDeclaration()
		if err != nil {
			return nil, err
		}
		program.Statements = append(program.Statements, stmt)
		p.skipNewlines()
	}
	return program, nil
}

type topLevelModifiers struct {
	Visibility    ast.Visibility
	HasVisibility bool
	IsAbstract    bool
	IsSealed      bool
}

func (p *Parser) topLevelDeclaration() (ast.Stmt, error) {
	if p.match(token.Import) {
		return p.importDeclaration()
	}
	annotations, err := p.annotations()
	if err != nil {
		return nil, err
	}
	modifiers, err := p.topLevelModifiers()
	if err != nil {
		return nil, err
	}
	if modifiers.Visibility == "" {
		modifiers.Visibility = ast.VisibilityPrivate
	}
	switch {
	case p.match(token.Enum):
		stmt, err := p.enumDeclaration(p.previous(), false, annotations)
		if err != nil {
			return nil, err
		}
		enumStmt := stmt.(*ast.ClassStmt)
		enumStmt.Visibility = modifiers.Visibility
		enumStmt.IsSealed = modifiers.IsSealed
		if modifiers.IsAbstract {
			return nil, fmt.Errorf("line %d:%d: enums cannot be abstract", enumStmt.Name.Line, enumStmt.Name.Column)
		}
		return enumStmt, nil
	case p.match(token.Interface):
		stmt, err := p.interfaceDeclaration(p.previous(), annotations)
		if err != nil {
			return nil, err
		}
		iface := stmt.(*ast.InterfaceStmt)
		iface.Visibility = modifiers.Visibility
		iface.IsSealed = modifiers.IsSealed
		if modifiers.IsAbstract {
			return nil, fmt.Errorf("line %d:%d: interfaces cannot be abstract", iface.Name.Line, iface.Name.Column)
		}
		return iface, nil
	case p.match(token.Class):
		stmt, err := p.classDeclaration(p.previous(), annotations)
		if err != nil {
			return nil, err
		}
		classStmt := stmt.(*ast.ClassStmt)
		classStmt.Visibility = modifiers.Visibility
		classStmt.IsAbstract = modifiers.IsAbstract
		classStmt.IsSealed = modifiers.IsSealed
		return classStmt, nil
	case p.match(token.Record):
		if modifiers.IsAbstract || modifiers.IsSealed {
			current := p.previous()
			return nil, fmt.Errorf("line %d:%d: records do not support abstract or sealed modifiers", current.Line, current.Column)
		}
		stmt, err := p.recordDeclaration(p.previous(), annotations)
		if err != nil {
			return nil, err
		}
		record := stmt.(*ast.ClassStmt)
		record.Visibility = modifiers.Visibility
		return record, nil
	case p.match(token.Native):
		if modifiers.IsAbstract || modifiers.IsSealed {
			current := p.previous()
			return nil, fmt.Errorf("line %d:%d: abstract and sealed modifiers only apply to classes and interfaces", current.Line, current.Column)
		}
		if _, err := p.consume(token.Def, "expected 'def' after 'native'"); err != nil {
			return nil, err
		}
		stmt, err := p.functionDeclaration(p.previous(), annotations, true)
		if err != nil {
			return nil, err
		}
		fnStmt := stmt.(*ast.FunctionStmt)
		fnStmt.Visibility = modifiers.Visibility
		fnStmt.IsNative = true
		return fnStmt, nil
	case p.match(token.Def):
		if modifiers.IsAbstract || modifiers.IsSealed {
			current := p.previous()
			return nil, fmt.Errorf("line %d:%d: abstract and sealed modifiers only apply to classes and interfaces", current.Line, current.Column)
		}
		stmt, err := p.functionDeclaration(p.previous(), annotations, false)
		if err != nil {
			return nil, err
		}
		stmt.(*ast.FunctionStmt).Visibility = modifiers.Visibility
		return stmt, nil
	case p.match(token.TypeKw):
		if modifiers.IsAbstract || modifiers.IsSealed {
			current := p.previous()
			return nil, fmt.Errorf("line %d:%d: abstract and sealed modifiers only apply to classes and interfaces", current.Line, current.Column)
		}
		return p.typeAliasDeclarationWithVisibility(modifiers.Visibility)
	case p.match(token.Let):
		if modifiers.IsAbstract || modifiers.IsSealed {
			current := p.previous()
			return nil, fmt.Errorf("line %d:%d: abstract and sealed modifiers only apply to classes and interfaces", current.Line, current.Column)
		}
		return p.variableDeclarationWithVisibility(ast.VariableLet, modifiers.Visibility)
	case p.match(token.Var):
		if modifiers.IsAbstract || modifiers.IsSealed {
			current := p.previous()
			return nil, fmt.Errorf("line %d:%d: abstract and sealed modifiers only apply to classes and interfaces", current.Line, current.Column)
		}
		return p.variableDeclarationWithVisibility(ast.VariableVar, modifiers.Visibility)
	case p.match(token.Const):
		if modifiers.IsAbstract || modifiers.IsSealed {
			current := p.previous()
			return nil, fmt.Errorf("line %d:%d: abstract and sealed modifiers only apply to classes and interfaces", current.Line, current.Column)
		}
		return p.variableDeclarationWithVisibility(ast.VariableConst, modifiers.Visibility)
	case p.match(token.Final):
		if p.match(token.Enum) {
			stmt, err := p.enumDeclaration(p.previous(), true, annotations)
			if err != nil {
				return nil, err
			}
			enumStmt := stmt.(*ast.ClassStmt)
			enumStmt.Visibility = modifiers.Visibility
			enumStmt.IsSealed = modifiers.IsSealed
			if modifiers.IsAbstract {
				return nil, fmt.Errorf("line %d:%d: enums cannot be abstract", enumStmt.Name.Line, enumStmt.Name.Column)
			}
			return enumStmt, nil
		}
		if modifiers.IsAbstract || modifiers.IsSealed {
			current := p.previous()
			return nil, fmt.Errorf("line %d:%d: abstract and sealed modifiers only apply to classes and interfaces", current.Line, current.Column)
		}
		return p.variableDeclarationWithVisibility(ast.VariableFinal, modifiers.Visibility)
	}
	if len(annotations) > 0 {
		current := p.peek()
		return nil, fmt.Errorf("line %d:%d: annotations can only be applied to top-level functions, classes, records, enums, and interfaces", current.Line, current.Column)
	}
	if modifiers.HasVisibility || modifiers.IsAbstract || modifiers.IsSealed {
		current := p.peek()
		return nil, fmt.Errorf("line %d:%d: invalid top-level modifier target", current.Line, current.Column)
	}
	stmt, err := p.declaration()
	if err != nil {
		return nil, err
	}
	switch node := stmt.(type) {
	case *ast.LetStmt:
		if node.Visibility == "" {
			node.Visibility = ast.VisibilityPrivate
		}
	case *ast.DestructureLetStmt:
		if node.Visibility == "" {
			node.Visibility = ast.VisibilityPrivate
		}
	case *ast.FunctionStmt:
		if node.Visibility == "" {
			node.Visibility = ast.VisibilityPrivate
		}
	case *ast.TypeAliasStmt:
		if node.Visibility == "" {
			node.Visibility = ast.VisibilityPrivate
		}
	case *ast.ClassStmt:
		if node.Visibility == "" {
			node.Visibility = ast.VisibilityPrivate
		}
	case *ast.InterfaceStmt:
		if node.Visibility == "" {
			node.Visibility = ast.VisibilityPrivate
		}
	}
	return stmt, nil
}

func (p *Parser) topLevelModifiers() (topLevelModifiers, error) {
	modifiers := topLevelModifiers{Visibility: ast.VisibilityPrivate}
	for {
		switch {
		case p.match(token.Public):
			if modifiers.HasVisibility {
				return modifiers, fmt.Errorf("line %d:%d: duplicate visibility modifier", p.previous().Line, p.previous().Column)
			}
			modifiers.Visibility = ast.VisibilityPublic
			modifiers.HasVisibility = true
		case p.match(token.Private):
			if modifiers.HasVisibility {
				return modifiers, fmt.Errorf("line %d:%d: duplicate visibility modifier", p.previous().Line, p.previous().Column)
			}
			modifiers.Visibility = ast.VisibilityPrivate
			modifiers.HasVisibility = true
		case p.match(token.Protected):
			if modifiers.HasVisibility {
				return modifiers, fmt.Errorf("line %d:%d: duplicate visibility modifier", p.previous().Line, p.previous().Column)
			}
			modifiers.Visibility = ast.VisibilityProtected
			modifiers.HasVisibility = true
		case p.match(token.Abstract):
			if modifiers.IsAbstract {
				return modifiers, fmt.Errorf("line %d:%d: duplicate abstract modifier", p.previous().Line, p.previous().Column)
			}
			modifiers.IsAbstract = true
		case p.match(token.Sealed):
			if modifiers.IsSealed {
				return modifiers, fmt.Errorf("line %d:%d: duplicate sealed modifier", p.previous().Line, p.previous().Column)
			}
			modifiers.IsSealed = true
		default:
			return modifiers, nil
		}
	}
}

func (p *Parser) declaration() (ast.Stmt, error) {
	if p.match(token.Interface) {
		return p.interfaceDeclaration(p.previous(), nil)
	}
	if p.match(token.Enum) {
		return p.enumDeclaration(p.previous(), false, nil)
	}
	if p.match(token.Record) {
		return p.recordDeclaration(p.previous(), nil)
	}
	if p.match(token.Class) {
		return p.classDeclaration(p.previous(), nil)
	}
	if p.match(token.Def) {
		return p.functionDeclaration(p.previous(), nil, false)
	}
	if p.match(token.TypeKw) {
		return p.typeAliasDeclaration()
	}
	return p.statement()
}

func (p *Parser) importDeclaration() (ast.Stmt, error) {
	path := make([]token.Token, 0, 2)
	first, err := p.consume(token.Identifier, "expected module path after import")
	if err != nil {
		return nil, err
	}
	path = append(path, first)
	for p.match(token.Dot) {
		part, err := p.consume(token.Identifier, "expected identifier after '.' in import path")
		if err != nil {
			return nil, err
		}
		path = append(path, part)
	}
	names := make([]token.Token, 0)
	if p.match(token.LeftBrace) {
		for {
			name, err := p.consume(token.Identifier, "expected identifier in import list")
			if err != nil {
				return nil, err
			}
			names = append(names, name)
			if !p.match(token.Comma) {
				break
			}
		}
		if _, err := p.consume(token.RightBrace, "expected '}' after import list"); err != nil {
			return nil, err
		}
	}
	return &ast.ImportStmt{Path: path, Names: names}, nil
}

func (p *Parser) parseTypePrimaryRef(message string) (*ast.TypeRef, error) {
	if p.match(token.Question) {
		wildcard := &ast.TypeRef{Wildcard: true, Name: p.previous()}
		if p.match(token.Extends, token.Super) {
			wildcard.BoundKind = p.previous().Type
			bound, err := p.parseTypeRef("expected wildcard bound")
			if err != nil {
				return nil, err
			}
			wildcard.Bound = bound
		}
		return wildcard, nil
	}
	name, err := p.consume(token.Identifier, message)
	if err != nil {
		return nil, err
	}
	typeRef := &ast.TypeRef{Name: name}
	if p.match(token.Less) {
		args := make([]*ast.TypeRef, 0, 2)
		for {
			arg, err := p.parseTypeRef("expected generic type argument")
			if err != nil {
				return nil, err
			}
			args = append(args, arg)
			if !p.match(token.Comma) {
				break
			}
		}
		if _, err := p.consume(token.Greater, "expected '>' after generic type arguments"); err != nil {
			return nil, err
		}
		typeRef.Args = args
	}
	return typeRef, nil
}

func (p *Parser) parseTypeRef(message string) (*ast.TypeRef, error) {
	first, err := p.parseTypePrimaryRef(message)
	if err != nil {
		return nil, err
	}
	// support Java-style array syntax: T[]  (possibly nested) but only when
	// the brackets are empty; leave `[<expr>]` for the caller to handle (e.g.
	// new T[3] size expressions).
	for p.check(token.LeftBracket) && p.checkNext(token.RightBracket) {
		p.advance() // consume '['
		p.advance() // consume ']'
		// wrap existing type in an array<...>
		first = &ast.TypeRef{
			Name: token.Token{Type: token.Identifier, Lexeme: "array"},
			Args: []*ast.TypeRef{first},
		}
	}
	if !p.match(token.Pipe) {
		return first, nil
	}
	union := []*ast.TypeRef{first}
	for {
		next, err := p.parseTypePrimaryRef("expected type after '|'")
		if err != nil {
			return nil, err
		}
		union = append(union, next)
		if !p.match(token.Pipe) {
			break
		}
	}
	return &ast.TypeRef{Name: first.Name, Union: union}, nil
}

func (p *Parser) typeAliasDeclaration() (ast.Stmt, error) {
	return p.typeAliasDeclarationWithVisibility(ast.VisibilityPrivate)
}

func (p *Parser) typeAliasDeclarationWithVisibility(visibility ast.Visibility) (ast.Stmt, error) {
	name, err := p.consume(token.Identifier, "expected type alias name")
	if err != nil {
		return nil, err
	}
	if _, err := p.consume(token.Equal, "expected '=' after type alias name"); err != nil {
		return nil, err
	}
	target, err := p.parseTypeRef("expected aliased type")
	if err != nil {
		return nil, err
	}
	return &ast.TypeAliasStmt{Visibility: visibility, Name: name, Target: target}, nil
}

func (p *Parser) parseTypeParams() ([]ast.TypeParam, error) {
	if !p.match(token.Less) {
		return nil, nil
	}
	params := make([]ast.TypeParam, 0, 2)
	for {
		name, err := p.consume(token.Identifier, "expected type parameter name")
		if err != nil {
			return nil, err
		}
		typeParam := ast.TypeParam{Name: name}
		if p.match(token.Extends) {
			bounds := make([]*ast.TypeRef, 0, 2)
			for {
				bound, err := p.parseTypeRef("expected upper bound")
				if err != nil {
					return nil, err
				}
				bounds = append(bounds, bound)
				if !p.match(token.Ampersand) {
					break
				}
			}
			typeParam.Bounds = bounds
		}
		params = append(params, typeParam)
		if !p.match(token.Comma) {
			break
		}
	}
	if _, err := p.consume(token.Greater, "expected '>' after type parameters"); err != nil {
		return nil, err
	}
	return params, nil
}

func (p *Parser) maybeTypeParams() ([]ast.TypeParam, bool, error) {
	if !p.check(token.Less) {
		return nil, false, nil
	}
	saved := p.cur
	params, err := p.parseTypeParams()
	if err != nil {
		p.cur = saved
		return nil, false, nil
	}
	return params, true, nil
}

func (p *Parser) interfaceDeclaration(start token.Token, annotations []ast.Annotation) (ast.Stmt, error) {
	name, err := p.consume(token.Identifier, "expected interface name")
	if err != nil {
		return nil, err
	}
	typeParams, _, err := p.maybeTypeParams()
	if err != nil {
		return nil, err
	}
	extends := make([]*ast.TypeRef, 0)
	if p.match(token.Extends) {
		for {
			base, err := p.parseTypeRef("expected interface name after extends")
			if err != nil {
				return nil, err
			}
			extends = append(extends, base)
			if !p.match(token.Comma) {
				break
			}
		}
	}
	permits, err := p.optionalPermitList()
	if err != nil {
		return nil, err
	}
	if _, err := p.consume(token.Colon, "expected ':' after interface name"); err != nil {
		return nil, err
	}
	p.skipNewlines()
	methods := make([]ast.InterfaceMethod, 0)
	for !p.isAtEnd() && !p.check(token.End) {
		methodName, err := p.consumeName("expected interface method name")
		if err != nil {
			return nil, err
		}
		params, err := p.parameterList()
		if err != nil {
			return nil, err
		}
		returnType, err := p.optionalReturnType()
		if err != nil {
			return nil, err
		}
		methods = append(methods, ast.InterfaceMethod{Name: methodName, Params: params, ReturnType: returnType})
		p.skipNewlines()
	}
	if _, err := p.consume(token.End, "expected 'end' after interface body"); err != nil {
		return nil, err
	}
	return &ast.InterfaceStmt{Visibility: ast.VisibilityPrivate, Annotations: annotations, Name: name, TypeParams: typeParams, Extends: extends, Permits: permits, Methods: methods, SourceSpan: ast.SourceSpan{StartLine: sourceStartLine(start, annotations), EndLine: p.previous().Line}}, nil
}

func (p *Parser) classDeclaration(start token.Token, annotations []ast.Annotation) (ast.Stmt, error) {
	name, err := p.consume(token.Identifier, "expected class name")
	if err != nil {
		return nil, err
	}
	typeParams, _, err := p.maybeTypeParams()
	if err != nil {
		return nil, err
	}
	var superclass *ast.TypeRef
	if p.match(token.Extends) {
		base, err := p.parseTypeRef("expected superclass name after extends")
		if err != nil {
			return nil, err
		}
		superclass = base
	}
	implements := make([]*ast.TypeRef, 0)
	if p.match(token.Implements) {
		for {
			iface, err := p.implementedType()
			if err != nil {
				return nil, err
			}
			implements = append(implements, iface)
			if !p.match(token.Comma) {
				break
			}
		}
	}
	permits, err := p.optionalPermitList()
	if err != nil {
		return nil, err
	}
	if _, err := p.consume(token.Colon, "expected ':' after class name"); err != nil {
		return nil, err
	}
	p.skipNewlines()
	classStmt := &ast.ClassStmt{Visibility: ast.VisibilityPrivate, Name: name, TypeParams: typeParams, Superclass: superclass, Implements: implements, Permits: permits, Fields: make([]ast.FieldDecl, 0), Methods: make([]ast.MethodDecl, 0)}
	for !p.isAtEnd() && !p.check(token.End) {
		annotations, err := p.annotations()
		if err != nil {
			return nil, err
		}
		visibility, isStatic, isAbstract, isNative, err := p.memberModifiers()
		if err != nil {
			return nil, err
		}
		if isStatic && p.match(token.Def) {
			method, err := p.methodDeclaration(false, classStmt.IsAbstract, isAbstract, name.Lexeme, annotations)
			if err != nil {
				return nil, err
			}
			method.Static = true
			method.Visibility = visibility
			method.IsNative = isNative
			classStmt.Methods = append(classStmt.Methods, method)
			p.skipNewlines()
			continue
		}
		if isStatic && p.match(token.Let) {
			field, err := p.classFieldDeclaration(ast.VariableLet)
			if err != nil {
				return nil, err
			}
			field.Static = true
			field.Visibility = visibility
			classStmt.Fields = append(classStmt.Fields, field)
			p.skipNewlines()
			continue
		}
		if isStatic && p.match(token.Var) {
			field, err := p.classFieldDeclaration(ast.VariableVar)
			if err != nil {
				return nil, err
			}
			field.Static = true
			field.Visibility = visibility
			classStmt.Fields = append(classStmt.Fields, field)
			p.skipNewlines()
			continue
		}
		if isStatic && p.match(token.Final) {
			field, err := p.classFieldDeclaration(ast.VariableFinal)
			if err != nil {
				return nil, err
			}
			field.Static = true
			field.Visibility = visibility
			classStmt.Fields = append(classStmt.Fields, field)
			p.skipNewlines()
			continue
		}
		if isStatic && p.match(token.Const) {
			field, err := p.classFieldDeclaration(ast.VariableConst)
			if err != nil {
				return nil, err
			}
			field.Static = true
			field.Visibility = visibility
			classStmt.Fields = append(classStmt.Fields, field)
			p.skipNewlines()
			continue
		}
		if isStatic {
			current := p.peek()
			return nil, fmt.Errorf("line %d:%d: invalid static class member", current.Line, current.Column)
		}
		if p.match(token.Def) {
			method, err := p.methodDeclaration(false, classStmt.IsAbstract, isAbstract, name.Lexeme, annotations)
			if err != nil {
				return nil, err
			}
			method.Visibility = visibility
			method.IsNative = isNative
			classStmt.Methods = append(classStmt.Methods, method)
			p.skipNewlines()
			continue
		}
		if p.check(token.Identifier) && p.checkNext(token.Colon) {
			if isStatic {
				current := p.peek()
				return nil, fmt.Errorf("line %d:%d: typed field declarations cannot be static without let/var/final/const", current.Line, current.Column)
			}
			field, err := p.fieldDeclaration()
			if err != nil {
				return nil, err
			}
			field.Visibility = visibility
			classStmt.Fields = append(classStmt.Fields, field)
			p.skipNewlines()
			continue
		}
		if len(annotations) > 0 {
			if p.check(token.Identifier) && p.checkNext(token.LeftParen) {
				if isAbstract {
					current := p.peek()
					return nil, fmt.Errorf("line %d:%d: constructors cannot be abstract", current.Line, current.Column)
				}
				if isNative {
					current := p.peek()
					return nil, fmt.Errorf("line %d:%d: constructors cannot be native", current.Line, current.Column)
				}
				method, err := p.methodDeclaration(true, classStmt.IsAbstract, false, name.Lexeme, annotations)
				if err != nil {
					return nil, err
				}
				method.Visibility = visibility
				classStmt.Methods = append(classStmt.Methods, method)
				p.skipNewlines()
				continue
			}
			current := p.peek()
			return nil, fmt.Errorf("line %d:%d: annotations can only be applied to methods", current.Line, current.Column)
		}
		if p.match(token.Let) {
			field, err := p.classFieldDeclaration(ast.VariableLet)
			if err != nil {
				return nil, err
			}
			field.Visibility = visibility
			classStmt.Fields = append(classStmt.Fields, field)
			p.skipNewlines()
			continue
		}
		if p.match(token.Var) {
			field, err := p.classFieldDeclaration(ast.VariableVar)
			if err != nil {
				return nil, err
			}
			field.Visibility = visibility
			classStmt.Fields = append(classStmt.Fields, field)
			p.skipNewlines()
			continue
		}
		if p.match(token.Final) {
			field, err := p.classFieldDeclaration(ast.VariableFinal)
			if err != nil {
				return nil, err
			}
			field.Visibility = visibility
			classStmt.Fields = append(classStmt.Fields, field)
			p.skipNewlines()
			continue
		}
		if p.match(token.Const) {
			field, err := p.classFieldDeclaration(ast.VariableConst)
			if err != nil {
				return nil, err
			}
			field.Visibility = visibility
			classStmt.Fields = append(classStmt.Fields, field)
			p.skipNewlines()
			continue
		}
		if p.check(token.Identifier) && p.checkNext(token.LeftParen) {
			if isAbstract {
				current := p.peek()
				return nil, fmt.Errorf("line %d:%d: constructors cannot be abstract", current.Line, current.Column)
			}
			if isNative {
				current := p.peek()
				return nil, fmt.Errorf("line %d:%d: constructors cannot be native", current.Line, current.Column)
			}
			method, err := p.methodDeclaration(true, classStmt.IsAbstract, false, name.Lexeme, nil)
			if err != nil {
				return nil, err
			}
			method.Visibility = visibility
			classStmt.Methods = append(classStmt.Methods, method)
			p.skipNewlines()
			continue
		}
		current := p.peek()
		return nil, fmt.Errorf("line %d:%d: invalid class member", current.Line, current.Column)
	}
	if _, err := p.consume(token.End, "expected 'end' after class body"); err != nil {
		return nil, err
	}
	classStmt.Annotations = annotations
	classStmt.SourceSpan = ast.SourceSpan{StartLine: sourceStartLine(start, annotations), EndLine: p.previous().Line}
	return classStmt, nil
}

func (p *Parser) recordDeclaration(start token.Token, annotations []ast.Annotation) (ast.Stmt, error) {
	name, err := p.consume(token.Identifier, "expected record name")
	if err != nil {
		return nil, err
	}
	params, err := p.parameterList()
	if err != nil {
		return nil, err
	}
	fields := make([]ast.FieldDecl, 0, len(params))
	for _, param := range params {
		fields = append(fields, ast.FieldDecl{Kind: ast.VariableFinal, Name: param.Name, Type: param.Type, Visibility: ast.VisibilityPublic})
	}
	methods := make([]ast.MethodDecl, 0, 1)
	methods = append(methods, p.generatedRecordConstructor(name, params))
	p.skipNewlines()
	for !p.isAtEnd() && !p.check(token.End) {
		annotations, err := p.annotations()
		if err != nil {
			return nil, err
		}
		visibility, isStatic, isAbstract, isNative, err := p.memberModifiers()
		if err != nil {
			return nil, err
		}
		if p.match(token.Def) {
			method, err := p.methodDeclaration(false, false, isAbstract, name.Lexeme, annotations)
			if err != nil {
				return nil, err
			}
			method.Static = isStatic
			method.Visibility = visibility
			method.IsNative = isNative
			methods = append(methods, method)
			p.skipNewlines()
			continue
		}
		if p.check(token.Identifier) && p.checkNext(token.LeftParen) {
			current := p.peek()
			return nil, fmt.Errorf("line %d:%d: records cannot declare custom constructors", current.Line, current.Column)
		}
		current := p.peek()
		return nil, fmt.Errorf("line %d:%d: invalid record member", current.Line, current.Column)
	}
	if _, err := p.consume(token.End, "expected 'end' after record body"); err != nil {
		return nil, err
	}
	return &ast.ClassStmt{Visibility: ast.VisibilityPrivate, Annotations: annotations, Name: name, IsRecord: true, Fields: fields, Methods: methods, SourceSpan: ast.SourceSpan{StartLine: sourceStartLine(start, annotations), EndLine: p.previous().Line}}, nil
}

func (p *Parser) enumDeclaration(start token.Token, isFinal bool, annotations []ast.Annotation) (ast.Stmt, error) {
	name, err := p.consume(token.Identifier, "expected enum name")
	if err != nil {
		return nil, err
	}
	p.skipNewlines()
	values := make([]ast.EnumValueDecl, 0)
	fields := make([]ast.FieldDecl, 0)
	methods := make([]ast.MethodDecl, 0)
	parsingMembers := false
	for !p.isAtEnd() && !p.check(token.End) {
		if !parsingMembers && p.check(token.Identifier) && p.peek().Lexeme != name.Lexeme {
			valueName := p.advance()
			args := make([]ast.Expr, 0)
			if p.match(token.LeftParen) {
				args, err = p.argumentList()
				if err != nil {
					return nil, err
				}
			}
			values = append(values, ast.EnumValueDecl{Name: valueName, Arguments: args})
			p.skipNewlines()
			continue
		}
		parsingMembers = true
		annotations, err := p.annotations()
		if err != nil {
			return nil, err
		}
		visibility, isStatic, isAbstract, isNative, err := p.memberModifiers()
		if err != nil {
			return nil, err
		}
		if isStatic && p.match(token.Def) {
			method, err := p.methodDeclaration(false, false, isAbstract, name.Lexeme, annotations)
			if err != nil {
				return nil, err
			}
			method.Static = true
			method.Visibility = visibility
			method.IsNative = isNative
			methods = append(methods, method)
			p.skipNewlines()
			continue
		}
		if isStatic && p.match(token.Let) {
			field, err := p.classFieldDeclaration(ast.VariableLet)
			if err != nil {
				return nil, err
			}
			field.Static = true
			field.Visibility = visibility
			fields = append(fields, field)
			p.skipNewlines()
			continue
		}
		if isStatic && p.match(token.Var) {
			field, err := p.classFieldDeclaration(ast.VariableVar)
			if err != nil {
				return nil, err
			}
			field.Static = true
			field.Visibility = visibility
			fields = append(fields, field)
			p.skipNewlines()
			continue
		}
		if isStatic && p.match(token.Final) {
			field, err := p.classFieldDeclaration(ast.VariableFinal)
			if err != nil {
				return nil, err
			}
			field.Static = true
			field.Visibility = visibility
			fields = append(fields, field)
			p.skipNewlines()
			continue
		}
		if isStatic && p.match(token.Const) {
			field, err := p.classFieldDeclaration(ast.VariableConst)
			if err != nil {
				return nil, err
			}
			field.Static = true
			field.Visibility = visibility
			fields = append(fields, field)
			p.skipNewlines()
			continue
		}
		if isStatic {
			current := p.peek()
			return nil, fmt.Errorf("line %d:%d: invalid static enum member", current.Line, current.Column)
		}
		if p.match(token.Def) {
			method, err := p.methodDeclaration(false, false, isAbstract, name.Lexeme, annotations)
			if err != nil {
				return nil, err
			}
			method.Visibility = visibility
			method.IsNative = isNative
			methods = append(methods, method)
			p.skipNewlines()
			continue
		}
		if p.match(token.Let) {
			field, err := p.classFieldDeclaration(ast.VariableLet)
			if err != nil {
				return nil, err
			}
			field.Visibility = visibility
			fields = append(fields, field)
			p.skipNewlines()
			continue
		}
		if p.match(token.Var) {
			field, err := p.classFieldDeclaration(ast.VariableVar)
			if err != nil {
				return nil, err
			}
			field.Visibility = visibility
			fields = append(fields, field)
			p.skipNewlines()
			continue
		}
		if p.match(token.Final) {
			field, err := p.classFieldDeclaration(ast.VariableFinal)
			if err != nil {
				return nil, err
			}
			field.Visibility = visibility
			fields = append(fields, field)
			p.skipNewlines()
			continue
		}
		if p.match(token.Const) {
			field, err := p.classFieldDeclaration(ast.VariableConst)
			if err != nil {
				return nil, err
			}
			field.Visibility = visibility
			fields = append(fields, field)
			p.skipNewlines()
			continue
		}
		if p.check(token.Identifier) && p.checkNext(token.LeftParen) {
			if isAbstract {
				current := p.peek()
				return nil, fmt.Errorf("line %d:%d: constructors cannot be abstract", current.Line, current.Column)
			}
			if isNative {
				current := p.peek()
				return nil, fmt.Errorf("line %d:%d: constructors cannot be native", current.Line, current.Column)
			}
			if isNative {
				current := p.peek()
				return nil, fmt.Errorf("line %d:%d: constructors cannot be native", current.Line, current.Column)
			}
			method, err := p.methodDeclaration(true, false, false, name.Lexeme, annotations)
			if err != nil {
				return nil, err
			}
			method.Visibility = visibility
			methods = append(methods, method)
			p.skipNewlines()
			continue
		}
		current := p.peek()
		return nil, fmt.Errorf("line %d:%d: invalid enum member", current.Line, current.Column)
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("line %d:%d: enum %s must declare at least one value", name.Line, name.Column, name.Lexeme)
	}
	if _, err := p.consume(token.End, "expected 'end' after enum body"); err != nil {
		return nil, err
	}
	return &ast.ClassStmt{Visibility: ast.VisibilityPrivate, Annotations: annotations, Name: name, IsFinal: isFinal, IsEnum: true, EnumValues: values, Fields: fields, Methods: methods, SourceSpan: ast.SourceSpan{StartLine: sourceStartLine(start, annotations), EndLine: p.previous().Line}}, nil
}

func (p *Parser) generatedRecordConstructor(name token.Token, params []ast.Parameter) ast.MethodDecl {
	body := &ast.BlockStmt{Statements: make([]ast.Stmt, 0, len(params))}
	for _, param := range params {
		body.Statements = append(body.Statements, &ast.SetStmt{
			Object:   &ast.ThisExpr{Keyword: token.Token{Type: token.This, Lexeme: "this", Line: param.Name.Line, Column: param.Name.Column}},
			Name:     param.Name,
			Operator: token.Token{Type: token.Equal, Lexeme: "=", Line: param.Name.Line, Column: param.Name.Column},
			Value:    &ast.VariableExpr{Name: param.Name},
		})
	}
	return ast.MethodDecl{Name: name, Params: params, Body: body, IsConstructor: true, Visibility: ast.VisibilityPublic}
}

func (p *Parser) memberModifiers() (ast.Visibility, bool, bool, bool, error) {
	visibility := ast.VisibilityPublic
	isStatic := false
	isAbstract := false
	isNative := false
	seenVisibility := false
	for {
		switch {
		case p.match(token.Static):
			if isStatic {
				return visibility, isStatic, isAbstract, isNative, fmt.Errorf("line %d:%d: duplicate static modifier", p.previous().Line, p.previous().Column)
			}
			isStatic = true
		case p.match(token.Native):
			if isNative {
				return visibility, isStatic, isAbstract, isNative, fmt.Errorf("line %d:%d: duplicate native modifier", p.previous().Line, p.previous().Column)
			}
			isNative = true
		case p.match(token.Abstract):
			if isAbstract {
				return visibility, isStatic, isAbstract, isNative, fmt.Errorf("line %d:%d: duplicate abstract modifier", p.previous().Line, p.previous().Column)
			}
			isAbstract = true
		case p.match(token.Public):
			if seenVisibility {
				return visibility, isStatic, isAbstract, isNative, fmt.Errorf("line %d:%d: duplicate visibility modifier", p.previous().Line, p.previous().Column)
			}
			visibility = ast.VisibilityPublic
			seenVisibility = true
		case p.match(token.Private):
			if seenVisibility {
				return visibility, isStatic, isAbstract, isNative, fmt.Errorf("line %d:%d: duplicate visibility modifier", p.previous().Line, p.previous().Column)
			}
			visibility = ast.VisibilityPrivate
			seenVisibility = true
		case p.match(token.Protected):
			if seenVisibility {
				return visibility, isStatic, isAbstract, isNative, fmt.Errorf("line %d:%d: duplicate visibility modifier", p.previous().Line, p.previous().Column)
			}
			visibility = ast.VisibilityProtected
			seenVisibility = true
		default:
			if isStatic && isAbstract {
				return visibility, isStatic, isAbstract, isNative, fmt.Errorf("line %d:%d: methods cannot be both static and abstract", p.peek().Line, p.peek().Column)
			}
			if isNative && isAbstract {
				return visibility, isStatic, isAbstract, isNative, fmt.Errorf("line %d:%d: methods cannot be both native and abstract", p.peek().Line, p.peek().Column)
			}
			return visibility, isStatic, isAbstract, isNative, nil
		}
	}
}

func (p *Parser) fieldDeclaration() (ast.FieldDecl, error) {
	name, err := p.consume(token.Identifier, "expected field name")
	if err != nil {
		return ast.FieldDecl{}, err
	}
	if _, err := p.consume(token.Colon, "expected ':' after field name"); err != nil {
		return ast.FieldDecl{}, err
	}
	typeRef, err := p.parseTypeRef("expected field type")
	if err != nil {
		return ast.FieldDecl{}, err
	}
	var valueExpr ast.Expr
	if p.match(token.Equal) {
		valueExpr, err = p.expression()
		if err != nil {
			return ast.FieldDecl{}, err
		}
	}
	return ast.FieldDecl{Kind: ast.VariableLet, Name: name, Type: typeRef, Value: valueExpr}, nil
}

func (p *Parser) classFieldDeclaration(kind ast.VariableKind) (ast.FieldDecl, error) {
	name, err := p.consume(token.Identifier, "expected field name")
	if err != nil {
		return ast.FieldDecl{}, err
	}
	var typeRef *ast.TypeRef
	if p.match(token.Colon) {
		typeRef, err = p.parseTypeRef("expected field type")
		if err != nil {
			return ast.FieldDecl{}, err
		}
	}
	var valueExpr ast.Expr
	if p.match(token.Equal) {
		valueExpr, err = p.expression()
		if err != nil {
			return ast.FieldDecl{}, err
		}
	}
	return ast.FieldDecl{Kind: kind, Name: name, Type: typeRef, Value: valueExpr}, nil
}

func (p *Parser) methodDeclaration(isConstructor bool, classIsAbstract bool, isAbstract bool, className string, annotations []ast.Annotation) (ast.MethodDecl, error) {
	name, err := p.consumeName("expected method name")
	if err != nil {
		return ast.MethodDecl{}, err
	}
	typeParams, _, err := p.maybeTypeParams()
	if err != nil {
		return ast.MethodDecl{}, err
	}
	if isConstructor && name.Lexeme != className {
		return ast.MethodDecl{}, fmt.Errorf("line %d:%d: constructor name must match class name %s", name.Line, name.Column, className)
	}
	params, err := p.parameterList()
	if err != nil {
		return ast.MethodDecl{}, err
	}
	returnType, err := p.optionalReturnType()
	if err != nil {
		return ast.MethodDecl{}, err
	}
	if isAbstract {
		if isConstructor {
			return ast.MethodDecl{}, fmt.Errorf("line %d:%d: constructors cannot be abstract", name.Line, name.Column)
		}
		return ast.MethodDecl{Name: name, Annotations: annotations, TypeParams: typeParams, Params: params, ReturnType: returnType, IsAbstract: true}, nil
	}
	if _, err := p.consume(token.Colon, "expected ':' after method signature"); err != nil {
		return ast.MethodDecl{}, err
	}
	p.skipNewlines()
	body, err := p.block(token.End)
	if err != nil {
		return ast.MethodDecl{}, err
	}
	if _, err := p.consume(token.End, "expected 'end' after method body"); err != nil {
		return ast.MethodDecl{}, err
	}
	return ast.MethodDecl{Name: name, Annotations: annotations, TypeParams: typeParams, Params: params, ReturnType: returnType, Body: body, IsConstructor: isConstructor}, nil
}

func (p *Parser) optionalPermitList() ([]*ast.TypeRef, error) {
	if !p.match(token.LeftParen) {
		return nil, nil
	}
	permits := make([]*ast.TypeRef, 0, 2)
	if !p.check(token.RightParen) {
		for {
			name, err := p.parseTypeRef("expected permitted type name")
			if err != nil {
				return nil, err
			}
			permits = append(permits, name)
			if !p.match(token.Comma) {
				break
			}
		}
	}
	if _, err := p.consume(token.RightParen, "expected ')' after permitted type list"); err != nil {
		return nil, err
	}
	return permits, nil
}

func (p *Parser) annotations() ([]ast.Annotation, error) {
	annotations := make([]ast.Annotation, 0)
	for p.match(token.At) {
		name, err := p.consume(token.Identifier, "expected annotation name after '@'")
		if err != nil {
			return nil, err
		}
		annotations = append(annotations, ast.Annotation{Name: name})
		p.skipNewlines()
	}
	return annotations, nil
}

func (p *Parser) implementedType() (*ast.TypeRef, error) {
	return p.parseTypeRef("expected interface name after 'implements'")
}

func (p *Parser) optionalReturnType() (*ast.TypeRef, error) {
	if !p.match(token.Arrow) {
		return nil, nil
	}
	return p.parseTypeRef("expected return type after '->'")
}

func (p *Parser) parameterList() ([]ast.Parameter, error) {
	if _, err := p.consume(token.LeftParen, "expected '('"); err != nil {
		return nil, err
	}
	params := make([]ast.Parameter, 0)
	if !p.check(token.RightParen) {
		for {
			paramName, err := p.consume(token.Identifier, "expected parameter name")
			if err != nil {
				return nil, err
			}
			var typeRef *ast.TypeRef
			if p.match(token.Colon) {
				typeRef, err = p.parseTypeRef("expected type name after ':'")
				if err != nil {
					return nil, err
				}
			}
			params = append(params, ast.Parameter{Name: paramName, Type: typeRef})
			if !p.match(token.Comma) {
				break
			}
		}
	}
	if _, err := p.consume(token.RightParen, "expected ')' after parameters"); err != nil {
		return nil, err
	}
	return params, nil
}

func (p *Parser) functionDeclaration(start token.Token, annotations []ast.Annotation, isNative bool) (ast.Stmt, error) {
	name, err := p.consume(token.Identifier, "expected function name")
	if err != nil {
		return nil, err
	}
	typeParams, _, err := p.maybeTypeParams()
	if err != nil {
		return nil, err
	}
	params, err := p.parameterList()
	if err != nil {
		return nil, err
	}
	returnType, err := p.optionalReturnType()
	if err != nil {
		return nil, err
	}

	if isNative {
		if p.match(token.Colon) {
			// Optional colon for native defs
		}
		p.skipNewlines()
		return &ast.FunctionStmt{Visibility: ast.VisibilityPrivate, Annotations: annotations, Name: name, TypeParams: typeParams, Params: params, ReturnType: returnType, IsNative: true, Body: nil, SourceSpan: ast.SourceSpan{StartLine: sourceStartLine(start, annotations), EndLine: p.previous().Line}}, nil
	}

	if _, err := p.consume(token.Colon, "expected ':' after function signature"); err != nil {
		return nil, err
	}
	p.skipNewlines()
	body, err := p.block(token.End)
	if err != nil {
		return nil, err
	}
	if _, err := p.consume(token.End, "expected 'end' after function body"); err != nil {
		return nil, err
	}
	return &ast.FunctionStmt{Visibility: ast.VisibilityPrivate, Annotations: annotations, Name: name, TypeParams: typeParams, Params: params, ReturnType: returnType, Body: body, SourceSpan: ast.SourceSpan{StartLine: sourceStartLine(start, annotations), EndLine: p.previous().Line}}, nil
}

func sourceStartLine(start token.Token, annotations []ast.Annotation) int {
	if len(annotations) > 0 {
		return annotations[0].Name.Line
	}
	return start.Line
}

func (p *Parser) statement() (ast.Stmt, error) {
	if p.match(token.Let) {
		return p.variableDeclaration(ast.VariableLet)
	}
	if p.match(token.Var) {
		return p.variableDeclaration(ast.VariableVar)
	}
	if p.match(token.Const) {
		return p.variableDeclaration(ast.VariableConst)
	}
	if p.match(token.Final) {
		return p.variableDeclaration(ast.VariableFinal)
	}
	if p.match(token.If) {
		return p.ifStatement()
	}
	if p.match(token.Switch) {
		return p.switchStatement()
	}
	if p.match(token.Try) {
		return p.tryStatement()
	}
	if p.match(token.LoopKw) {
		return p.loopStatement(p.previous())
	}
	if p.match(token.DoKw) {
		return p.doLoopStatement(p.previous())
	}
	if p.match(token.BreakKw) {
		return &ast.BreakStmt{Keyword: p.previous()}, nil
	}
	if p.match(token.ContinueKw) {
		return &ast.ContinueStmt{Keyword: p.previous()}, nil
	}
	if p.match(token.Return) {
		return p.returnStatement()
	}
	if p.match(token.Throw) {
		return p.throwStatement()
	}
	if p.match(token.For) {
		return p.forStatement()
	}
	return p.expressionStatement()
}

func (p *Parser) statementSuite(stop ...token.Type) (*ast.BlockStmt, bool, error) {
	if p.match(token.Newline) {
		p.skipNewlines()
		body, err := p.block(stop...)
		return body, false, err
	}
	stmt, err := p.declaration()
	if err != nil {
		return nil, false, err
	}
	return &ast.BlockStmt{Statements: []ast.Stmt{stmt}}, true, nil
}

func (p *Parser) variableDeclaration(kind ast.VariableKind) (ast.Stmt, error) {
	return p.variableDeclarationWithVisibility(kind, ast.VisibilityPrivate)
}

func (p *Parser) variableDeclarationWithVisibility(kind ast.VariableKind, visibility ast.Visibility) (ast.Stmt, error) {
	name, err := p.consume(token.Identifier, "expected variable name")
	if err != nil {
		return nil, err
	}
	if p.match(token.Comma) {
		targets := []token.Token{name}
		for {
			target, err := p.consume(token.Identifier, "expected variable name in destructuring declaration")
			if err != nil {
				return nil, err
			}
			targets = append(targets, target)
			if !p.match(token.Comma) {
				break
			}
		}
		if _, err := p.consume(token.Equal, "expected '=' after destructuring declaration"); err != nil {
			return nil, err
		}
		value, err := p.expression()
		if err != nil {
			return nil, err
		}
		return &ast.DestructureLetStmt{Kind: kind, Visibility: visibility, Targets: targets, Value: value}, nil
	}
	var typeRef *ast.TypeRef
	if p.match(token.Colon) {
		typeRef, err = p.parseTypeRef("expected type name after ':'")
		if err != nil {
			return nil, err
		}
	}
	if _, err := p.consume(token.Equal, "expected '=' after variable name"); err != nil {
		return nil, err
	}
	value, err := p.expression()
	if err != nil {
		return nil, err
	}
	return &ast.LetStmt{Kind: kind, Visibility: visibility, Name: name, Type: typeRef, Value: value}, nil
}

func (p *Parser) ifStatement() (ast.Stmt, error) {
	stmt, requiresEnd, err := p.ifStatementTail()
	if err != nil {
		return nil, err
	}
	if requiresEnd {
		if _, err := p.consume(token.End, "expected 'end' after if block"); err != nil {
			return nil, err
		}
	}
	return stmt, nil
}

func (p *Parser) ifStatementTail() (*ast.IfStmt, bool, error) {
	condition, err := p.expression()
	if err != nil {
		return nil, false, err
	}
	if _, err := p.consume(token.Colon, "expected ':' after if condition"); err != nil {
		return nil, false, err
	}
	thenBlock, thenInline, err := p.statementSuite(token.Elif, token.Else, token.End)
	if err != nil {
		return nil, false, err
	}
	requiresEnd := !thenInline
	p.skipNewlines()
	var elseBlock *ast.BlockStmt
	if p.match(token.Elif) {
		nested, nestedRequiresEnd, err := p.ifStatementTail()
		if err != nil {
			return nil, false, err
		}
		elseBlock = &ast.BlockStmt{Statements: []ast.Stmt{nested}}
		requiresEnd = requiresEnd || nestedRequiresEnd
	} else if p.match(token.Else) {
		// support `else if` as sugar for `elif`
		if p.match(token.If) {
			nested, nestedRequiresEnd, err := p.ifStatementTail()
			if err != nil {
				return nil, false, err
			}
			elseBlock = &ast.BlockStmt{Statements: []ast.Stmt{nested}}
			requiresEnd = requiresEnd || nestedRequiresEnd
		} else {
			if _, err := p.consume(token.Colon, "expected ':' after else"); err != nil {
				return nil, false, err
			}
			var elseInline bool
			elseBlock, elseInline, err = p.statementSuite(token.End)
			if err != nil {
				return nil, false, err
			}
			requiresEnd = requiresEnd || !elseInline
		}
	}
	p.skipNewlines()
	return &ast.IfStmt{Condition: condition, Then: thenBlock, Else: elseBlock}, requiresEnd, nil
}

func (p *Parser) switchStatement() (ast.Stmt, error) {
	valueExpr, err := p.expression()
	if err != nil {
		return nil, err
	}
	if _, err := p.consume(token.Colon, "expected ':' after switch value"); err != nil {
		return nil, err
	}
	p.skipNewlines()
	arms := make([]ast.SwitchArm, 0)
	var defaultBlock *ast.BlockStmt
	for !p.isAtEnd() && !p.check(token.End) {
		if p.match(token.Case) {
			arm, err := p.switchArm()
			if err != nil {
				return nil, err
			}
			arms = append(arms, arm)
			p.skipNewlines()
			continue
		}
		if p.match(token.Default) {
			if defaultBlock != nil {
				current := p.previous()
				return nil, fmt.Errorf("line %d:%d: duplicate default in switch", current.Line, current.Column)
			}
			if _, err := p.consume(token.Colon, "expected ':' after default"); err != nil {
				return nil, err
			}
			defaultBlock, err = p.switchCaseBody()
			if err != nil {
				return nil, err
			}
			p.skipNewlines()
			continue
		}
		current := p.peek()
		return nil, fmt.Errorf("line %d:%d: expected 'case', 'default', or 'end' in switch", current.Line, current.Column)
	}
	if _, err := p.consume(token.End, "expected 'end' after switch block"); err != nil {
		return nil, err
	}
	return &ast.SwitchStmt{Value: valueExpr, Arms: arms, Default: defaultBlock}, nil
}

func (p *Parser) switchArm() (ast.SwitchArm, error) {
	patterns := make([]ast.SwitchPattern, 0, 1)
	for {
		pattern, err := p.switchPattern()
		if err != nil {
			return ast.SwitchArm{}, err
		}
		patterns = append(patterns, pattern)
		if _, err := p.consume(token.Colon, "expected ':' after case pattern"); err != nil {
			return ast.SwitchArm{}, err
		}
		body, emptyBody, err := p.switchCaseBodyOrContinue()
		if err != nil {
			return ast.SwitchArm{}, err
		}
		if !emptyBody {
			return ast.SwitchArm{Patterns: patterns, Body: body}, nil
		}
		if !p.match(token.Case) {
			return ast.SwitchArm{Patterns: patterns, Body: &ast.BlockStmt{}}, nil
		}
	}
}

func (p *Parser) switchPattern() (ast.SwitchPattern, error) {
	if p.match(token.LeftParen) {
		binding, err := p.consume(token.Identifier, "expected binding name in type case")
		if err != nil {
			return ast.SwitchPattern{}, err
		}
		if _, err := p.consume(token.Colon, "expected ':' in type case"); err != nil {
			return ast.SwitchPattern{}, err
		}
		typeName, err := p.consume(token.Identifier, "expected type name in type case")
		if err != nil {
			return ast.SwitchPattern{}, err
		}
		if _, err := p.consume(token.RightParen, "expected ')' after type case"); err != nil {
			return ast.SwitchPattern{}, err
		}
		return ast.SwitchPattern{Type: &ast.TypePattern{Binding: binding, Type: &ast.TypeRef{Name: typeName}}}, nil
	}
	valueExpr, err := p.expression()
	if err != nil {
		return ast.SwitchPattern{}, err
	}
	return ast.SwitchPattern{Value: valueExpr}, nil
}

func (p *Parser) switchCaseBodyOrContinue() (*ast.BlockStmt, bool, error) {
	if p.match(token.Newline) {
		p.skipNewlines()
		if p.check(token.Case) || p.check(token.Default) || p.check(token.End) {
			return &ast.BlockStmt{}, true, nil
		}
		body, err := p.block(token.Case, token.Default, token.End)
		return body, false, err
	}
	body, err := p.inlineCaseBody()
	return body, false, err
}

func (p *Parser) switchCaseBody() (*ast.BlockStmt, error) {
	if p.match(token.Newline) {
		p.skipNewlines()
		return p.block(token.Case, token.Default, token.End)
	}
	return p.inlineCaseBody()
}

func (p *Parser) tryStatement() (ast.Stmt, error) {
	keyword := p.previous()
	if _, err := p.consume(token.Colon, "expected ':' after try"); err != nil {
		return nil, err
	}
	body, inline, err := p.statementSuite(token.Catch, token.End)
	if err != nil {
		return nil, err
	}
	requiresEnd := !inline
	p.skipNewlines()
	catches := make([]ast.CatchClause, 0, 1)
	for p.match(token.Catch) {
		clause, catchInline, err := p.catchClause()
		if err != nil {
			return nil, err
		}
		catches = append(catches, clause)
		requiresEnd = requiresEnd || !catchInline
		p.skipNewlines()
	}
	if len(catches) == 0 {
		return nil, fmt.Errorf("line %d:%d: try requires at least one catch clause", keyword.Line, keyword.Column)
	}
	if requiresEnd {
		if _, err := p.consume(token.End, "expected 'end' after try block"); err != nil {
			return nil, err
		}
	}
	return &ast.TryStmt{Keyword: keyword, Body: body, Catches: catches}, nil
}

func (p *Parser) catchClause() (ast.CatchClause, bool, error) {
	keyword := p.previous()
	var binding token.Token
	var typeRef *ast.TypeRef
	if p.match(token.LeftParen) {
		name, err := p.consume(token.Identifier, "expected catch binding name")
		if err != nil {
			return ast.CatchClause{}, false, err
		}
		binding = name
		if p.match(token.Colon) {
			parsedType, err := p.parseTypeRef("expected exception type in catch clause")
			if err != nil {
				return ast.CatchClause{}, false, err
			}
			typeRef = parsedType
		}
		if _, err := p.consume(token.RightParen, "expected ')' after catch binding"); err != nil {
			return ast.CatchClause{}, false, err
		}
	} else if p.check(token.Identifier) && p.checkNext(token.Colon) {
		binding = p.advance()
	}
	if _, err := p.consume(token.Colon, "expected ':' after catch clause"); err != nil {
		return ast.CatchClause{}, false, err
	}
	body, inline, err := p.statementSuite(token.Catch, token.End)
	if err != nil {
		return ast.CatchClause{}, false, err
	}
	return ast.CatchClause{Keyword: keyword, Binding: binding, Type: typeRef, Body: body}, inline, nil
}

func (p *Parser) inlineCaseBody() (*ast.BlockStmt, error) {
	stmt, err := p.declaration()
	if err != nil {
		return nil, err
	}
	return &ast.BlockStmt{Statements: []ast.Stmt{stmt}}, nil
}

func (p *Parser) returnStatement() (ast.Stmt, error) {
	keyword := p.previous()
	if p.check(token.Newline) || p.check(token.End) || p.check(token.Else) || p.check(token.EOF) {
		return &ast.ReturnStmt{Keyword: keyword}, nil
	}
	value, err := p.expression()
	if err != nil {
		return nil, err
	}
	return &ast.ReturnStmt{Keyword: keyword, Value: value}, nil
}

func (p *Parser) throwStatement() (ast.Stmt, error) {
	keyword := p.previous()
	valueExpr, err := p.expression()
	if err != nil {
		return nil, err
	}
	return &ast.ThrowStmt{Keyword: keyword, Value: valueExpr}, nil
}

func (p *Parser) forStatement() (ast.Stmt, error) {
	name, err := p.consume(token.Identifier, "expected loop variable name")
	if err != nil {
		return nil, err
	}
	targets := []token.Token{name}
	for p.match(token.Comma) {
		target, err := p.consume(token.Identifier, "expected loop variable name")
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	if _, err := p.consume(token.In, "expected 'in' after loop variable"); err != nil {
		return nil, err
	}
	iterable, err := p.expression()
	if err != nil {
		return nil, err
	}
	var condition ast.Expr
	if p.match(token.Where) {
		condition, err = p.expression()
		if err != nil {
			return nil, err
		}
	}
	if _, err := p.consume(token.Colon, "expected ':' after for header"); err != nil {
		return nil, err
	}
	body, inline, err := p.statementSuite(token.End)
	if err != nil {
		return nil, err
	}
	if !inline {
		if _, err := p.consume(token.End, "expected 'end' after for block"); err != nil {
			return nil, err
		}
	}
	return &ast.ForStmt{Targets: targets, Iterable: iterable, Condition: condition, Body: body}, nil
}

func (p *Parser) loopStatement(keyword token.Token) (ast.Stmt, error) {
	var condition ast.Expr
	if p.match(token.Colon) {
		body, inline, err := p.statementSuite(token.End)
		if err != nil {
			return nil, err
		}
		if !inline {
			if _, err := p.consume(token.End, "expected 'end' after loop block"); err != nil {
				return nil, err
			}
		}
		return &ast.LoopStmt{Keyword: keyword, Condition: condition, Body: body}, nil
	}
	if p.match(token.Newline) {
		p.skipNewlines()
		body, err := p.block(token.End)
		if err != nil {
			return nil, err
		}
		if _, err := p.consume(token.End, "expected 'end' after loop block"); err != nil {
			return nil, err
		}
		return &ast.LoopStmt{Keyword: keyword, Body: body}, nil
	}
	var err error
	condition, err = p.expression()
	if err != nil {
		return nil, err
	}
	if _, err := p.consume(token.Colon, "expected ':' after loop condition"); err != nil {
		return nil, err
	}
	body, inline, err := p.statementSuite(token.End)
	if err != nil {
		return nil, err
	}
	if !inline {
		if _, err := p.consume(token.End, "expected 'end' after loop block"); err != nil {
			return nil, err
		}
	}
	return &ast.LoopStmt{Keyword: keyword, Condition: condition, Body: body}, nil
}

func (p *Parser) doLoopStatement(keyword token.Token) (ast.Stmt, error) {
	if _, err := p.consume(token.Colon, "expected ':' after do"); err != nil {
		return nil, err
	}
	body := &ast.BlockStmt{}
	if p.match(token.Newline) {
		p.skipNewlines()
		for !p.isAtEnd() && !p.checkDoLoopTail() {
			stmt, err := p.declaration()
			if err != nil {
				return nil, err
			}
			body.Statements = append(body.Statements, stmt)
			p.skipNewlines()
		}
	} else {
		stmt, err := p.declaration()
		if err != nil {
			return nil, err
		}
		body.Statements = append(body.Statements, stmt)
	}
	if _, err := p.consume(token.LoopKw, "expected 'loop' after do block"); err != nil {
		return nil, err
	}
	condition, err := p.expression()
	if err != nil {
		return nil, err
	}
	return &ast.LoopStmt{Keyword: keyword, Condition: condition, Body: body, PostCondition: true}, nil
}

func (p *Parser) checkDoLoopTail() bool {
	if !p.check(token.LoopKw) {
		return false
	}
	for i := p.cur + 1; i < len(p.tokens); i++ {
		switch p.tokens[i].Type {
		case token.Newline, token.EOF:
			return true
		case token.Colon:
			return false
		}
	}
	return true
}

func (p *Parser) expressionStatement() (ast.Stmt, error) {
	expr, err := p.expression()
	if err != nil {
		return nil, err
	}
	if p.match(token.Equal, token.PlusEqual, token.MinusEqual, token.StarEqual, token.SlashEqual) {
		operator := p.previous()
		value, err := p.expression()
		if err != nil {
			return nil, err
		}
		switch target := expr.(type) {
		case *ast.VariableExpr:
			return &ast.AssignStmt{Name: target.Name, Operator: operator, Value: value}, nil
		case *ast.GetExpr:
			return &ast.SetStmt{Object: target.Object, Name: target.Name, Operator: operator, Value: value}, nil
		case *ast.IndexExpr:
			return &ast.SetIndexStmt{Object: target.Object, Index: target.Index, Operator: operator, Value: value}, nil
		default:
			current := p.previous()
			return nil, fmt.Errorf("line %d:%d: invalid assignment target", current.Line, current.Column)
		}
	}
	return &ast.ExprStmt{Expr: expr}, nil
}

func (p *Parser) block(stop ...token.Type) (*ast.BlockStmt, error) {
	block := &ast.BlockStmt{}
	p.skipNewlines()
	for !p.isAtEnd() && !p.checkAny(stop...) {
		stmt, err := p.declaration()
		if err != nil {
			return nil, err
		}
		block.Statements = append(block.Statements, stmt)
		p.skipNewlines()
	}
	return block, nil
}

func (p *Parser) expression() (ast.Expr, error) {
	return p.or()
}

func (p *Parser) or() (ast.Expr, error) {
	expr, err := p.and()
	if err != nil {
		return nil, err
	}
	for p.match(token.OrOr) {
		op := p.previous()
		right, err := p.and()
		if err != nil {
			return nil, err
		}
		expr = &ast.BinaryExpr{Left: expr, Operator: op, Right: right}
	}
	return expr, nil
}

func (p *Parser) and() (ast.Expr, error) {
	expr, err := p.equality()
	if err != nil {
		return nil, err
	}
	for p.match(token.AndAnd) {
		op := p.previous()
		right, err := p.equality()
		if err != nil {
			return nil, err
		}
		expr = &ast.BinaryExpr{Left: expr, Operator: op, Right: right}
	}
	return expr, nil
}

func (p *Parser) equality() (ast.Expr, error) {
	expr, err := p.comparison()
	if err != nil {
		return nil, err
	}
	for p.match(token.EqualEqual, token.BangEqual) {
		op := p.previous()
		right, err := p.comparison()
		if err != nil {
			return nil, err
		}
		expr = &ast.BinaryExpr{Left: expr, Operator: op, Right: right}
	}
	return expr, nil
}

func (p *Parser) comparison() (ast.Expr, error) {
	expr, err := p.rangeExpr()
	if err != nil {
		return nil, err
	}
	for p.match(token.Instanceof) {
		target, err := p.parseTypeRef("expected type after 'instanceof'")
		if err != nil {
			return nil, err
		}
		var binding *token.Token
		if p.check(token.Identifier) && !p.checkNext(token.Dot) && !p.checkNext(token.LeftParen) && !p.checkNext(token.LeftBracket) {
			name, err := p.consume(token.Identifier, "expected binding name")
			if err != nil {
				return nil, err
			}
			binding = &name
		}
		expr = &ast.InstanceOfExpr{Expr: expr, Target: target, Binding: binding}
	}
	for p.match(token.Greater, token.GreaterEqual, token.Less, token.LessEqual, token.In) {
		op := p.previous()
		right, err := p.rangeExpr()
		if err != nil {
			return nil, err
		}
		expr = &ast.BinaryExpr{Left: expr, Operator: op, Right: right}
	}
	return expr, nil
}

func (p *Parser) rangeExpr() (ast.Expr, error) {
	expr, err := p.term()
	if err != nil {
		return nil, err
	}
	if !p.allowRangeShortcut {
		return expr, nil
	}
	if !p.match(token.Ellipsis) {
		return expr, nil
	}
	op := p.previous()
	right, err := p.term()
	if err != nil {
		return nil, err
	}
	return &ast.CallExpr{
		Callee: &ast.VariableExpr{Name: token.Token{Type: token.Identifier, Lexeme: "range", Line: op.Line, Column: op.Column}},
		Paren:  token.Token{Type: token.LeftParen, Lexeme: "(", Line: op.Line, Column: op.Column},
		Arguments: []ast.Expr{
			expr,
			right,
		},
	}, nil
}

func (p *Parser) term() (ast.Expr, error) {
	expr, err := p.factor()
	if err != nil {
		return nil, err
	}
	for p.match(token.Plus, token.Minus) {
		op := p.previous()
		right, err := p.factor()
		if err != nil {
			return nil, err
		}
		expr = &ast.BinaryExpr{Left: expr, Operator: op, Right: right}
	}
	return expr, nil
}

func (p *Parser) factor() (ast.Expr, error) {
	expr, err := p.power()
	if err != nil {
		return nil, err
	}
	for p.match(token.Star, token.Slash, token.Percent) {
		op := p.previous()
		right, err := p.power()
		if err != nil {
			return nil, err
		}
		expr = &ast.BinaryExpr{Left: expr, Operator: op, Right: right}
	}
	return expr, nil
}

func (p *Parser) power() (ast.Expr, error) {
	expr, err := p.unary()
	if err != nil {
		return nil, err
	}
	if p.match(token.StarStar, token.Caret) {
		op := p.previous()
		right, err := p.power()
		if err != nil {
			return nil, err
		}
		expr = &ast.BinaryExpr{Left: expr, Operator: op, Right: right}
	}
	return expr, nil
}

func (p *Parser) unary() (ast.Expr, error) {
	if p.isCastStart() {
		if _, err := p.consume(token.LeftParen, "expected '(' before cast"); err != nil {
			return nil, err
		}
		target, err := p.parseTypeRef("expected cast target")
		if err != nil {
			return nil, err
		}
		if _, err := p.consume(token.RightParen, "expected ')' after cast target"); err != nil {
			return nil, err
		}
		right, err := p.unary()
		if err != nil {
			return nil, err
		}
		return &ast.CastExpr{Target: target, Expr: right}, nil
	}
	if p.match(token.Bang, token.Minus) {
		op := p.previous()
		right, err := p.unary()
		if err != nil {
			return nil, err
		}
		return &ast.UnaryExpr{Operator: op, Right: right}, nil
	}
	return p.call()
}

func (p *Parser) isCastStart() bool {
	if !p.check(token.LeftParen) {
		return false
	}
	closing := p.findCastClosingParen()
	if closing < 0 {
		return false
	}
	return p.canStartUnaryAt(closing + 1)
}

func (p *Parser) findCastClosingParen() int {
	if !p.check(token.LeftParen) {
		return -1
	}
	if !p.isCastTypeStart(p.cur + 1) {
		return -1
	}
	depth := 0
	sawTypeToken := false
	for index := p.cur + 1; index < len(p.tokens); index++ {
		current := p.tokens[index]
		if current.Type == token.RightParen {
			if depth != 0 || !sawTypeToken {
				return -1
			}
			return index
		}
		if !p.isCastTypeToken(current.Type) {
			return -1
		}
		sawTypeToken = true
		switch current.Type {
		case token.Less:
			depth++
		case token.Greater:
			if depth == 0 {
				return -1
			}
			depth--
		}
	}
	return -1
}

func (p *Parser) isCastTypeStart(index int) bool {
	if index >= len(p.tokens) {
		return false
	}
	current := p.tokens[index]
	if current.Type == token.Question {
		return true
	}
	if current.Type != token.Identifier {
		return false
	}
	if isPrimitiveCastKeyword(current.Lexeme) {
		return true
	}
	if len(current.Lexeme) == 0 {
		return false
	}
	first := current.Lexeme[0]
	return first >= 'A' && first <= 'Z'
}

func isPrimitiveCastKeyword(lexeme string) bool {
	switch lexeme {
	case "int", "float", "number", "char":
		return true
	default:
		return false
	}
}

func (p *Parser) isCastTypeToken(kind token.Type) bool {
	switch kind {
	case token.Identifier, token.Question, token.Extends, token.Super, token.Less, token.Greater, token.Comma, token.Pipe:
		return true
	default:
		return false
	}
}

func (p *Parser) canStartUnaryAt(index int) bool {
	if index >= len(p.tokens) {
		return false
	}
	switch p.tokens[index].Type {
	case token.Bang, token.Minus, token.False, token.True, token.Nil, token.IntNumber, token.FloatNumber, token.Char, token.String, token.LeftBracket, token.LeftBrace, token.Identifier, token.New, token.This, token.Super, token.LeftParen, token.ThreadKw:
		return true
	default:
		return false
	}
}

func (p *Parser) checkAt(offset int, kind token.Type) bool {
	index := p.cur + offset
	if index >= len(p.tokens) {
		return false
	}
	return p.tokens[index].Type == kind
}

func (p *Parser) call() (ast.Expr, error) {
	expr, err := p.primary()
	if err != nil {
		return nil, err
	}
	for {
		if typeArgs, matched, err := p.tryParseCallTypeArgs(); err != nil {
			return nil, err
		} else if matched {
			if _, err := p.consume(token.LeftParen, "expected '(' after generic type arguments"); err != nil {
				return nil, err
			}
			expr, err = p.finishCall(expr, typeArgs)
			if err != nil {
				return nil, err
			}
			continue
		}
		if p.match(token.LeftParen) {
			expr, err = p.finishCall(expr, nil)
			if err != nil {
				return nil, err
			}
			continue
		}
		if p.match(token.Dot) {
			name, err := p.consumeName("expected property name after '.'")
			if err != nil {
				return nil, err
			}
			expr = &ast.GetExpr{Object: expr, Name: name}
			continue
		}
		if p.match(token.LeftBracket) {
			bracket := p.previous()
			allowRangeShortcut := p.allowRangeShortcut
			p.allowRangeShortcut = false
			index, err := p.expression()
			p.allowRangeShortcut = allowRangeShortcut
			if err != nil {
				return nil, err
			}
			if p.match(token.Ellipsis) {
				end, err := p.expression()
				if err != nil {
					return nil, err
				}
				if _, err := p.consume(token.RightBracket, "expected ']' after slice"); err != nil {
					return nil, err
				}
				expr = &ast.SliceExpr{Object: expr, Start: index, End: end, Bracket: bracket}
				continue
			}
			if _, err := p.consume(token.RightBracket, "expected ']' after index"); err != nil {
				return nil, err
			}
			expr = &ast.IndexExpr{Object: expr, Index: index, Bracket: bracket}
			continue
		}
		break
	}
	return expr, nil
}

func (p *Parser) tryParseCallTypeArgs() ([]*ast.TypeRef, bool, error) {
	if !p.check(token.Less) {
		return nil, false, nil
	}
	save := p.cur
	p.advance()
	args := make([]*ast.TypeRef, 0, 2)
	for {
		arg, err := p.parseTypeRef("expected generic type argument")
		if err != nil {
			p.cur = save
			return nil, false, nil
		}
		args = append(args, arg)
		if !p.match(token.Comma) {
			break
		}
	}
	if _, err := p.consume(token.Greater, "expected '>' after generic type arguments"); err != nil {
		p.cur = save
		return nil, false, nil
	}
	if !p.check(token.LeftParen) {
		p.cur = save
		return nil, false, nil
	}
	return args, true, nil
}

func (p *Parser) finishCall(callee ast.Expr, typeArgs []*ast.TypeRef) (ast.Expr, error) {
	paren := p.previous()
	args, err := p.argumentList()
	if err != nil {
		return nil, err
	}
	return &ast.CallExpr{Callee: callee, TypeArgs: typeArgs, Paren: paren, Arguments: args}, nil
}

func (p *Parser) argumentList() ([]ast.Expr, error) {
	args := make([]ast.Expr, 0)
	if !p.check(token.RightParen) {
		for {
			arg, err := p.expression()
			if err != nil {
				return nil, err
			}
			args = append(args, arg)
			if !p.match(token.Comma) {
				break
			}
		}
	}
	if _, err := p.consume(token.RightParen, "expected ')' after arguments"); err != nil {
		return nil, err
	}
	return args, nil
}

func (p *Parser) primary() (ast.Expr, error) {
	if p.match(token.False) {
		return &ast.LiteralExpr{Value: false}, nil
	}
	if p.match(token.True) {
		return &ast.LiteralExpr{Value: true}, nil
	}
	if p.match(token.Nil) {
		return &ast.LiteralExpr{Value: nil}, nil
	}
	if p.match(token.IntNumber) {
		value, err := strconv.ParseInt(normalizeNumericLexeme(p.previous().Lexeme), 10, 64)
		if err != nil {
			return nil, err
		}
		return &ast.LiteralExpr{Value: value}, nil
	}
	if p.match(token.FloatNumber) {
		value, err := strconv.ParseFloat(normalizeNumericLexeme(p.previous().Lexeme), 64)
		if err != nil {
			return nil, err
		}
		return &ast.LiteralExpr{Value: value}, nil
	}
	if p.match(token.Char) {
		chars := []rune(p.previous().Lexeme)
		if len(chars) != 1 {
			return nil, fmt.Errorf("char literal must contain exactly one character")
		}
		return &ast.LiteralExpr{Value: chars[0]}, nil
	}
	if p.match(token.String) {
		return &ast.LiteralExpr{Value: p.previous().Lexeme}, nil
	}
	if p.match(token.LeftBracket) {
		elements := make([]ast.Expr, 0)
		p.skipNewlines()
		if !p.check(token.RightBracket) {
			for {
				element, err := p.expression()
				if err != nil {
					return nil, err
				}
				// Array comprehension: [expr for var in iterable]
				if len(elements) == 0 && p.check(token.For) {
					forTok := p.advance()
					variable, err := p.consume(token.Identifier, "expected loop variable after 'for' in comprehension")
					if err != nil {
						return nil, err
					}
					if _, err := p.consume(token.In, "expected 'in' after comprehension variable"); err != nil {
						return nil, err
					}
					iterable, err := p.expression()
					if err != nil {
						return nil, err
					}
					p.skipNewlines()
					if _, err := p.consume(token.RightBracket, "expected ']' after comprehension"); err != nil {
						return nil, err
					}
					return &ast.ArrayComprehensionExpr{Element: element, Variable: variable, Iterable: iterable, For: forTok}, nil
				}
				elements = append(elements, element)
				p.skipNewlines()
				if !p.match(token.Comma) {
					break
				}
				p.skipNewlines()
			}
		}
		p.skipNewlines()
		if _, err := p.consume(token.RightBracket, "expected ']' after array literal"); err != nil {
			return nil, err
		}
		return &ast.ArrayExpr{Elements: elements}, nil
	}
	if p.match(token.LeftBrace) {
		entries := make([]ast.MapEntry, 0)
		p.skipNewlines()
		if !p.check(token.RightBrace) {
			for {
				p.skipNewlines()
				var key string
				if p.match(token.Identifier) {
					key = p.previous().Lexeme
				} else if p.match(token.String) {
					key = p.previous().Lexeme
				} else {
					current := p.peek()
					return nil, fmt.Errorf("line %d:%d: expected identifier or string as map key", current.Line, current.Column)
				}
				if _, err := p.consume(token.Colon, "expected ':' after map key"); err != nil {
					return nil, err
				}
				valueExpr, err := p.expression()
				if err != nil {
					return nil, err
				}
				entries = append(entries, ast.MapEntry{Key: key, Value: valueExpr})
				p.skipNewlines()
				if !p.match(token.Comma) {
					break
				}
				p.skipNewlines()
			}
		}
		p.skipNewlines()
		if _, err := p.consume(token.RightBrace, "expected '}' after map literal"); err != nil {
			return nil, err
		}
		return &ast.MapExpr{Entries: entries}, nil
	}
	if p.match(token.Identifier) {
		return &ast.VariableExpr{Name: p.previous()}, nil
	}
	if p.match(token.This) {
		return &ast.ThisExpr{Keyword: p.previous()}, nil
	}
	if p.match(token.New) {
		// parse type reference (could be primitive/array/etc)
		typeRef, err := p.parseTypeRef("expected type after 'new'")
		if err != nil {
			return nil, err
		}
		// optional size bracket: new T[expr] or new T[]
		var sizeExpr ast.Expr
		if p.match(token.LeftBracket) {
			if !p.check(token.RightBracket) {
				expr, err := p.expression()
				if err != nil {
					return nil, err
				}
				sizeExpr = expr
			}
			if _, err := p.consume(token.RightBracket, "expected ']' after array size"); err != nil {
				return nil, err
			}
		}
		// optional initializer
		var initElems []ast.Expr
		var braceTok token.Token
		if p.match(token.LeftBrace) {
			braceTok = p.previous()
			if !p.check(token.RightBrace) {
				for {
					elem, err := p.expression()
					if err != nil {
						return nil, err
					}
					initElems = append(initElems, elem)
					if !p.match(token.Comma) {
						break
					}
				}
			}
			if _, err := p.consume(token.RightBrace, "expected '}' after array initializer"); err != nil {
				return nil, err
			}
		}
		// if there was any array-specific syntax, use ArrayNewExpr
		if sizeExpr != nil || braceTok.Type == token.LeftBrace {
			return &ast.ArrayNewExpr{Type: typeRef, Size: sizeExpr, Brace: braceTok, Initializer: initElems}, nil
		}
		// otherwise treat as normal class new
		if classNameToken := typeRef.Name; classNameToken.Type == token.Identifier && len(typeRef.Args) == 0 && len(typeRef.Union) == 0 {
			if _, err := p.consume(token.LeftParen, "expected '(' after class name"); err != nil {
				return nil, err
			}
			paren := p.previous()
			args := make([]ast.Expr, 0)
			if !p.check(token.RightParen) {
				for {
					arg, err := p.expression()
					if err != nil {
						return nil, err
					}
					args = append(args, arg)
					if !p.match(token.Comma) {
						break
					}
				}
			}
			if _, err := p.consume(token.RightParen, "expected ')' after arguments"); err != nil {
				return nil, err
			}
			return &ast.NewExpr{Class: classNameToken, Paren: paren, Arguments: args}, nil
		}
		return nil, fmt.Errorf("invalid 'new' expression")
	}
	if p.match(token.ThreadKw) {
		return p.threadSpawnExpression(p.previous())
	}
	if p.match(token.Super) {
		return &ast.SuperExpr{Keyword: p.previous()}, nil
	}
	if p.match(token.LeftParen) {
		if p.isLambdaStart() {
			params, err := p.lambdaParameterList()
			if err != nil {
				return nil, err
			}
			if _, err := p.consume(token.FatArrow, "expected '=>' after lambda parameters"); err != nil {
				return nil, err
			}
			if p.match(token.Colon) {
				p.skipNewlines()
				body, err := p.block(token.End)
				if err != nil {
					return nil, err
				}
				if _, err := p.consume(token.End, "expected 'end' after lambda block"); err != nil {
					return nil, err
				}
				return &ast.LambdaExpr{Params: params, Block: body}, nil
			}
			body, err := p.expression()
			if err != nil {
				return nil, err
			}
			return &ast.LambdaExpr{Params: params, Body: body}, nil
		}
		first, err := p.expression()
		if err != nil {
			return nil, err
		}
		if p.match(token.Comma) {
			elements := []ast.Expr{first}
			for {
				next, err := p.expression()
				if err != nil {
					return nil, err
				}
				elements = append(elements, next)
				if !p.match(token.Comma) {
					break
				}
			}
			if _, err := p.consume(token.RightParen, "expected ')' after tuple literal"); err != nil {
				return nil, err
			}
			return &ast.TupleExpr{Elements: elements}, nil
		}
		if _, err := p.consume(token.RightParen, "expected ')' after expression"); err != nil {
			return nil, err
		}
		return &ast.GroupingExpr{Expr: first}, nil
	}
	current := p.peek()
	return nil, fmt.Errorf("line %d:%d: expected expression, got %s", current.Line, current.Column, current.Type)
}

func normalizeNumericLexeme(lexeme string) string {
	return strings.ReplaceAll(lexeme, "_", "")
}

func (p *Parser) threadSpawnExpression(threadToken token.Token) (ast.Expr, error) {
	spawnToken, err := p.consume(token.SpawnKw, "expected 'spawn' after 'thread'")
	if err != nil {
		return nil, err
	}
	if _, err := p.consume(token.Colon, "expected ':' after 'thread spawn'"); err != nil {
		return nil, err
	}
	p.skipNewlines()
	body, err := p.block(token.End)
	if err != nil {
		return nil, err
	}
	if _, err := p.consume(token.End, "expected 'end' after thread body"); err != nil {
		return nil, err
	}
	lambda := &ast.LambdaExpr{Params: nil, Block: body}
	threadClass := &ast.VariableExpr{Name: token.Token{Type: token.Identifier, Lexeme: "Thread", Line: threadToken.Line, Column: threadToken.Column}}
	spawnName := token.Token{Type: token.Identifier, Lexeme: "startThread", Line: spawnToken.Line, Column: spawnToken.Column}
	return &ast.CallExpr{
		Callee:    &ast.GetExpr{Object: threadClass, Name: spawnName},
		Paren:     token.Token{Type: token.LeftParen, Lexeme: "(", Line: spawnToken.Line, Column: spawnToken.Column},
		Arguments: []ast.Expr{lambda},
	}, nil
}

func (p *Parser) isLambdaStart() bool {
	idx := p.cur
	if p.tokens[idx].Type == token.RightParen {
		return idx+1 < len(p.tokens) && p.tokens[idx+1].Type == token.FatArrow
	}
	for idx < len(p.tokens) {
		if p.tokens[idx].Type != token.Identifier {
			return false
		}
		idx++
		if idx < len(p.tokens) && p.tokens[idx].Type == token.Colon {
			idx++
			if idx >= len(p.tokens) || p.tokens[idx].Type != token.Identifier {
				return false
			}
			idx++
		}
		if idx >= len(p.tokens) {
			return false
		}
		if p.tokens[idx].Type == token.Comma {
			idx++
			continue
		}
		if p.tokens[idx].Type == token.RightParen {
			return idx+1 < len(p.tokens) && p.tokens[idx+1].Type == token.FatArrow
		}
		return false
	}
	return false
}

func (p *Parser) lambdaParameterList() ([]ast.Parameter, error) {
	params := make([]ast.Parameter, 0)
	if !p.check(token.RightParen) {
		for {
			paramName, err := p.consume(token.Identifier, "expected lambda parameter name")
			if err != nil {
				return nil, err
			}
			var typeRef *ast.TypeRef
			if p.match(token.Colon) {
				typeRef, err = p.parseTypeRef("expected type name after ':'")
				if err != nil {
					return nil, err
				}
			}
			params = append(params, ast.Parameter{Name: paramName, Type: typeRef})
			if !p.match(token.Comma) {
				break
			}
		}
	}
	if _, err := p.consume(token.RightParen, "expected ')' after lambda parameters"); err != nil {
		return nil, err
	}
	return params, nil
}

func (p *Parser) match(types ...token.Type) bool {
	for _, item := range types {
		if p.check(item) {
			p.advance()
			return true
		}
	}
	return false
}

func (p *Parser) consume(kind token.Type, message string) (token.Token, error) {
	if p.check(kind) {
		return p.advance(), nil
	}
	current := p.peek()
	return token.Token{}, fmt.Errorf("line %d:%d: %s", current.Line, current.Column, message)
}

func (p *Parser) consumeName(message string) (token.Token, error) {
	if p.check(token.Identifier) || p.check(token.Catch) || p.check(token.Try) || p.check(token.Throw) || p.check(token.TypeKw) || p.check(token.Instanceof) {
		return p.advance(), nil
	}
	current := p.peek()
	return token.Token{}, fmt.Errorf("line %d:%d: %s", current.Line, current.Column, message)
}

func (p *Parser) skipNewlines() {
	for p.match(token.Newline) {
	}
}

func (p *Parser) check(kind token.Type) bool {
	if p.isAtEnd() {
		return kind == token.EOF
	}
	return p.peek().Type == kind
}

func (p *Parser) checkNext(kind token.Type) bool {
	if p.cur+1 >= len(p.tokens) {
		return false
	}
	return p.tokens[p.cur+1].Type == kind
}

func (p *Parser) checkAny(types ...token.Type) bool {
	for _, kind := range types {
		if p.check(kind) {
			return true
		}
	}
	return false
}

func (p *Parser) advance() token.Token {
	if !p.isAtEnd() {
		p.cur++
	}
	return p.previous()
}

func (p *Parser) isAtEnd() bool {
	return p.peek().Type == token.EOF
}

func (p *Parser) peek() token.Token {
	return p.tokens[p.cur]
}

func (p *Parser) previous() token.Token {
	return p.tokens[p.cur-1]
}
