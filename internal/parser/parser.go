package parser

import (
	"fmt"
	"strconv"

	"github.com/ArubikU/polyloft-bvm/internal/ast"
	"github.com/ArubikU/polyloft-bvm/internal/token"
)

type Parser struct {
	tokens []token.Token
	cur    int
}

func Parse(tokens []token.Token) (*ast.Program, error) {
	p := &Parser{tokens: tokens}
	return p.parseProgram()
}

func (p *Parser) parseProgram() (*ast.Program, error) {
	program := &ast.Program{}
	p.skipNewlines()
	for !p.isAtEnd() {
		stmt, err := p.declaration()
		if err != nil {
			return nil, err
		}
		program.Statements = append(program.Statements, stmt)
		p.skipNewlines()
	}
	return program, nil
}

func (p *Parser) declaration() (ast.Stmt, error) {
	if p.match(token.Class) {
		return p.classDeclaration()
	}
	if p.match(token.Def) {
		return p.functionDeclaration()
	}
	return p.statement()
}

func (p *Parser) classDeclaration() (ast.Stmt, error) {
	name, err := p.consume(token.Identifier, "expected class name")
	if err != nil {
		return nil, err
	}
	var superclass *ast.TypeRef
	if p.match(token.Less) {
		base, err := p.consume(token.Identifier, "expected superclass name after '<'")
		if err != nil {
			return nil, err
		}
		superclass = &ast.TypeRef{Name: base}
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
	if _, err := p.consume(token.Colon, "expected ':' after class name"); err != nil {
		return nil, err
	}
	p.skipNewlines()
	classStmt := &ast.ClassStmt{Name: name, Superclass: superclass, Implements: implements, Fields: make([]ast.FieldDecl, 0), Methods: make([]ast.MethodDecl, 0)}
	for !p.isAtEnd() && !p.check(token.End) {
		annotations, err := p.annotations()
		if err != nil {
			return nil, err
		}
		isStatic := p.match(token.Static)
		if isStatic && p.match(token.Def) {
			method, err := p.methodDeclaration(false, name.Lexeme, annotations)
			if err != nil {
				return nil, err
			}
			method.Static = true
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
			classStmt.Fields = append(classStmt.Fields, field)
			p.skipNewlines()
			continue
		}
		if isStatic {
			current := p.peek()
			return nil, fmt.Errorf("line %d:%d: invalid static class member", current.Line, current.Column)
		}
		if p.match(token.Def) {
			method, err := p.methodDeclaration(false, name.Lexeme, nil)
			if err != nil {
				return nil, err
			}
			classStmt.Methods = append(classStmt.Methods, method)
			p.skipNewlines()
			continue
		}
		if p.check(token.Identifier) && p.checkNext(token.Colon) {
			field, err := p.fieldDeclaration()
			if err != nil {
				return nil, err
			}
			classStmt.Fields = append(classStmt.Fields, field)
			p.skipNewlines()
			continue
		}
		if len(annotations) > 0 {
			if p.check(token.Identifier) && p.checkNext(token.LeftParen) {
				method, err := p.methodDeclaration(true, name.Lexeme, annotations)
				if err != nil {
					return nil, err
				}
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
			classStmt.Fields = append(classStmt.Fields, field)
			p.skipNewlines()
			continue
		}
		if p.match(token.Var) {
			field, err := p.classFieldDeclaration(ast.VariableVar)
			if err != nil {
				return nil, err
			}
			classStmt.Fields = append(classStmt.Fields, field)
			p.skipNewlines()
			continue
		}
		if p.match(token.Final) {
			field, err := p.classFieldDeclaration(ast.VariableFinal)
			if err != nil {
				return nil, err
			}
			classStmt.Fields = append(classStmt.Fields, field)
			p.skipNewlines()
			continue
		}
		if p.match(token.Const) {
			field, err := p.classFieldDeclaration(ast.VariableConst)
			if err != nil {
				return nil, err
			}
			classStmt.Fields = append(classStmt.Fields, field)
			p.skipNewlines()
			continue
		}
		if p.check(token.Identifier) && p.checkNext(token.LeftParen) {
			method, err := p.methodDeclaration(true, name.Lexeme, nil)
			if err != nil {
				return nil, err
			}
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
	return classStmt, nil
}

func (p *Parser) fieldDeclaration() (ast.FieldDecl, error) {
	name, err := p.consume(token.Identifier, "expected field name")
	if err != nil {
		return ast.FieldDecl{}, err
	}
	if _, err := p.consume(token.Colon, "expected ':' after field name"); err != nil {
		return ast.FieldDecl{}, err
	}
	typeName, err := p.consume(token.Identifier, "expected field type")
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
	return ast.FieldDecl{Kind: ast.VariableLet, Name: name, Type: &ast.TypeRef{Name: typeName}, Value: valueExpr}, nil
}

func (p *Parser) classFieldDeclaration(kind ast.VariableKind) (ast.FieldDecl, error) {
	name, err := p.consume(token.Identifier, "expected field name")
	if err != nil {
		return ast.FieldDecl{}, err
	}
	var typeRef *ast.TypeRef
	if p.match(token.Colon) {
		typeName, err := p.consume(token.Identifier, "expected field type")
		if err != nil {
			return ast.FieldDecl{}, err
		}
		typeRef = &ast.TypeRef{Name: typeName}
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

func (p *Parser) methodDeclaration(isConstructor bool, className string, annotations []ast.Annotation) (ast.MethodDecl, error) {
	name, err := p.consume(token.Identifier, "expected method name")
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
	return ast.MethodDecl{Name: name, Annotations: annotations, Params: params, ReturnType: returnType, Body: body, IsConstructor: isConstructor}, nil
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
	name, err := p.consume(token.Identifier, "expected interface name after 'implements'")
	if err != nil {
		return nil, err
	}
	if p.match(token.Less) {
		depth := 1
		for depth > 0 {
			if p.isAtEnd() {
				current := p.peek()
				return nil, fmt.Errorf("line %d:%d: unterminated generic interface parameters", current.Line, current.Column)
			}
			if p.match(token.Less) {
				depth++
				continue
			}
			if p.match(token.Greater) {
				depth--
				continue
			}
			p.advance()
		}
	}
	return &ast.TypeRef{Name: name}, nil
}

func (p *Parser) optionalReturnType() (*ast.TypeRef, error) {
	if !p.match(token.Arrow) {
		return nil, nil
	}
	typeName, err := p.consume(token.Identifier, "expected return type after '->'")
	if err != nil {
		return nil, err
	}
	return &ast.TypeRef{Name: typeName}, nil
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
				typeName, err := p.consume(token.Identifier, "expected type name after ':'")
				if err != nil {
					return nil, err
				}
				typeRef = &ast.TypeRef{Name: typeName}
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

func (p *Parser) functionDeclaration() (ast.Stmt, error) {
	name, err := p.consume(token.Identifier, "expected function name")
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
	return &ast.FunctionStmt{Name: name, Params: params, ReturnType: returnType, Body: body}, nil
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
	if p.match(token.Return) {
		return p.returnStatement()
	}
	if p.match(token.For) {
		return p.forStatement()
	}
	return p.expressionStatement()
}

func (p *Parser) variableDeclaration(kind ast.VariableKind) (ast.Stmt, error) {
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
		return &ast.DestructureLetStmt{Kind: kind, Targets: targets, Value: value}, nil
	}
	var typeRef *ast.TypeRef
	if p.match(token.Colon) {
		typeName, err := p.consume(token.Identifier, "expected type name after ':'")
		if err != nil {
			return nil, err
		}
		typeRef = &ast.TypeRef{Name: typeName}
	}
	if _, err := p.consume(token.Equal, "expected '=' after variable name"); err != nil {
		return nil, err
	}
	value, err := p.expression()
	if err != nil {
		return nil, err
	}
	return &ast.LetStmt{Kind: kind, Name: name, Type: typeRef, Value: value}, nil
}

func (p *Parser) ifStatement() (ast.Stmt, error) {
	condition, err := p.expression()
	if err != nil {
		return nil, err
	}
	if _, err := p.consume(token.Colon, "expected ':' after if condition"); err != nil {
		return nil, err
	}
	p.skipNewlines()
	thenBlock, err := p.block(token.Else, token.End)
	if err != nil {
		return nil, err
	}
	var elseBlock *ast.BlockStmt
	if p.match(token.Else) {
		if _, err := p.consume(token.Colon, "expected ':' after else"); err != nil {
			return nil, err
		}
		p.skipNewlines()
		elseBlock, err = p.block(token.End)
		if err != nil {
			return nil, err
		}
	}
	if _, err := p.consume(token.End, "expected 'end' after if block"); err != nil {
		return nil, err
	}
	return &ast.IfStmt{Condition: condition, Then: thenBlock, Else: elseBlock}, nil
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
	p.skipNewlines()
	body, err := p.block(token.End)
	if err != nil {
		return nil, err
	}
	if _, err := p.consume(token.End, "expected 'end' after for block"); err != nil {
		return nil, err
	}
	return &ast.ForStmt{Targets: targets, Iterable: iterable, Condition: condition, Body: body}, nil
}

func (p *Parser) expressionStatement() (ast.Stmt, error) {
	expr, err := p.expression()
	if err != nil {
		return nil, err
	}
	if p.match(token.Equal) {
		value, err := p.expression()
		if err != nil {
			return nil, err
		}
		switch target := expr.(type) {
		case *ast.VariableExpr:
			return &ast.AssignStmt{Name: target.Name, Value: value}, nil
		case *ast.GetExpr:
			return &ast.SetStmt{Object: target.Object, Name: target.Name, Value: value}, nil
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
	return p.equality()
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
	expr, err := p.term()
	if err != nil {
		return nil, err
	}
	for p.match(token.Greater, token.GreaterEqual, token.Less, token.LessEqual) {
		op := p.previous()
		right, err := p.term()
		if err != nil {
			return nil, err
		}
		expr = &ast.BinaryExpr{Left: expr, Operator: op, Right: right}
	}
	return expr, nil
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
	expr, err := p.unary()
	if err != nil {
		return nil, err
	}
	for p.match(token.Star, token.Slash) {
		op := p.previous()
		right, err := p.unary()
		if err != nil {
			return nil, err
		}
		expr = &ast.BinaryExpr{Left: expr, Operator: op, Right: right}
	}
	return expr, nil
}

func (p *Parser) unary() (ast.Expr, error) {
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

func (p *Parser) call() (ast.Expr, error) {
	expr, err := p.primary()
	if err != nil {
		return nil, err
	}
	for {
		if p.match(token.LeftParen) {
			expr, err = p.finishCall(expr)
			if err != nil {
				return nil, err
			}
			continue
		}
		if p.match(token.Dot) {
			name, err := p.consume(token.Identifier, "expected property name after '.'")
			if err != nil {
				return nil, err
			}
			expr = &ast.GetExpr{Object: expr, Name: name}
			continue
		}
		break
	}
	return expr, nil
}

func (p *Parser) finishCall(callee ast.Expr) (ast.Expr, error) {
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
	return &ast.CallExpr{Callee: callee, Paren: paren, Arguments: args}, nil
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
	if p.match(token.Number) {
		value, err := strconv.ParseFloat(p.previous().Lexeme, 64)
		if err != nil {
			return nil, err
		}
		return &ast.LiteralExpr{Value: value}, nil
	}
	if p.match(token.String) {
		return &ast.LiteralExpr{Value: p.previous().Lexeme}, nil
	}
	if p.match(token.Identifier) {
		return &ast.VariableExpr{Name: p.previous()}, nil
	}
	if p.match(token.This) {
		return &ast.ThisExpr{Keyword: p.previous()}, nil
	}
	if p.match(token.New) {
		className, err := p.consume(token.Identifier, "expected class name after 'new'")
		if err != nil {
			return nil, err
		}
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
		return &ast.NewExpr{Class: className, Paren: paren, Arguments: args}, nil
	}
	if p.match(token.Super) {
		return &ast.SuperExpr{Keyword: p.previous()}, nil
	}
	if p.match(token.LeftParen) {
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
