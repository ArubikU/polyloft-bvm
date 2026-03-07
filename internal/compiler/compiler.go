package compiler

import (
	"fmt"
	"math"

	"github.com/ArubikU/polyloft-bvm/internal/ast"
	"github.com/ArubikU/polyloft-bvm/internal/bytecode"
	"github.com/ArubikU/polyloft-bvm/internal/token"
	"github.com/ArubikU/polyloft-bvm/internal/value"
)

type Compiler struct {
	state           *state
	classes         map[string]*value.Class
	consts          map[string]value.Value
	globalTypes     map[string]string
	capturedGlobals map[string]bool
	globalSlots     map[string]byte
}

type state struct {
	function   *bytecode.Function
	chunk      *bytecode.Chunk
	locals     []local
	depth      int
	parent     *state
	name       string
	ownerClass *value.Class
}

type local struct {
	name  string
	depth int
	slot  byte
	typ   string
}

func Compile(program *ast.Program) (*bytecode.Function, error) {
	chunk := bytecode.NewChunk()
	root := &state{
		function: &bytecode.Function{Name: "<script>", Chunk: chunk},
		chunk:    chunk,
		locals:   make([]local, 0),
		depth:    0,
		name:     "<script>",
	}
	compiler := &Compiler{state: root, classes: make(map[string]*value.Class), consts: make(map[string]value.Value), globalTypes: make(map[string]string), capturedGlobals: collectCapturedGlobals(program), globalSlots: collectGlobalSlots(program)}
	for _, stmt := range program.Statements {
		if err := compiler.compileStmt(stmt); err != nil {
			return nil, err
		}
	}
	root.function.GlobalSlotCount = len(compiler.globalSlots)
	compiler.emit(bytecode.OpNil, 0)
	compiler.emit(bytecode.OpReturn, 0)
	return root.function, nil
}

func (c *Compiler) compileStmt(stmt ast.Stmt) error {
	switch node := stmt.(type) {
	case *ast.LetStmt:
		declaredType := c.inferDeclaredType(node.Type, node.Value)
		if err := c.compileExpr(node.Value); err != nil {
			return err
		}
		if node.Kind == ast.VariableConst || node.Kind == ast.VariableFinal {
			c.emit(bytecode.OpFreeze, node.Name.Line)
		}
		if node.Kind == ast.VariableConst {
			if constantValue, ok := c.evalConstExpr(node.Value); ok {
				c.consts[node.Name.Lexeme] = constantValue
			}
		}
		if c.shouldUseScriptLocal(node.Name.Lexeme) {
			slot := c.declareLocal(node.Name, declaredType)
			c.emit(bytecode.OpSetLocal, node.Name.Line)
			c.emitByte(slot, node.Name.Line)
			return nil
		}
		if slot, ok := c.resolveGlobalSlot(node.Name.Lexeme); ok {
			c.globalTypes[node.Name.Lexeme] = declaredType
			c.emit(bytecode.OpDefineGlobalSlot, node.Name.Line)
			c.emitByte(slot, node.Name.Line)
			return nil
		}
		if c.state.depth == 0 {
			c.globalTypes[node.Name.Lexeme] = declaredType
			name := c.constant(node.Name.Lexeme)
			c.emit(bytecode.OpDefineGlobal, node.Name.Line)
			c.emitUint16(name, node.Name.Line)
			return nil
		}
		slot := c.declareLocal(node.Name, declaredType)
		c.emit(bytecode.OpSetLocal, node.Name.Line)
		c.emitByte(slot, node.Name.Line)
		return nil
	case *ast.DestructureLetStmt:
		types := c.inferDestructureTypes(node.Value, len(node.Targets))
		if err := c.compileExpr(node.Value); err != nil {
			return err
		}
		c.emit(bytecode.OpUnpack, node.Targets[0].Line)
		c.emitByte(byte(len(node.Targets)), node.Targets[0].Line)
		if c.shouldUseScriptLocals(node.Targets) {
			slots := make([]byte, len(node.Targets))
			for i, target := range node.Targets {
				slots[i] = c.declareLocal(target, types[i])
			}
			for i := len(slots) - 1; i >= 0; i-- {
				if node.Kind == ast.VariableConst || node.Kind == ast.VariableFinal {
					c.emit(bytecode.OpFreeze, node.Targets[i].Line)
				}
				c.emit(bytecode.OpSetLocal, node.Targets[i].Line)
				c.emitByte(slots[i], node.Targets[i].Line)
			}
			return nil
		}
		if c.state.depth == 0 {
			allSlotted := true
			slots := make([]byte, len(node.Targets))
			for i, target := range node.Targets {
				slot, ok := c.resolveGlobalSlot(target.Lexeme)
				if !ok {
					allSlotted = false
					break
				}
				slots[i] = slot
			}
			if allSlotted {
				for i := len(node.Targets) - 1; i >= 0; i-- {
					if node.Kind == ast.VariableConst || node.Kind == ast.VariableFinal {
						c.emit(bytecode.OpFreeze, node.Targets[i].Line)
					}
					if types[i] != "" {
						c.globalTypes[node.Targets[i].Lexeme] = types[i]
					}
					c.emit(bytecode.OpDefineGlobalSlot, node.Targets[i].Line)
					c.emitByte(slots[i], node.Targets[i].Line)
				}
				return nil
			}
		}
		if c.state.depth == 0 {
			for i := len(node.Targets) - 1; i >= 0; i-- {
				if node.Kind == ast.VariableConst || node.Kind == ast.VariableFinal {
					c.emit(bytecode.OpFreeze, node.Targets[i].Line)
				}
				if types[i] != "" {
					c.globalTypes[node.Targets[i].Lexeme] = types[i]
				}
				name := c.constant(node.Targets[i].Lexeme)
				c.emit(bytecode.OpDefineGlobal, node.Targets[i].Line)
				c.emitUint16(name, node.Targets[i].Line)
			}
			return nil
		}
		slots := make([]byte, len(node.Targets))
		for i, target := range node.Targets {
			slots[i] = c.declareLocal(target, types[i])
		}
		for i := len(slots) - 1; i >= 0; i-- {
			if node.Kind == ast.VariableConst || node.Kind == ast.VariableFinal {
				c.emit(bytecode.OpFreeze, node.Targets[i].Line)
			}
			c.emit(bytecode.OpSetLocal, node.Targets[i].Line)
			c.emitByte(slots[i], node.Targets[i].Line)
		}
		return nil
	case *ast.AssignStmt:
		assignedType := c.inferExprType(node.Value)
		if err := c.compileExpr(node.Value); err != nil {
			return err
		}
		if slot, ok := c.resolveLocal(node.Name.Lexeme); ok {
			c.setLocalType(slot, assignedType)
			c.emit(bytecode.OpSetLocal, node.Name.Line)
			c.emitByte(slot, node.Name.Line)
			return nil
		}
		if slot, ok := c.resolveGlobalSlot(node.Name.Lexeme); ok {
			if assignedType != "" {
				c.globalTypes[node.Name.Lexeme] = assignedType
			}
			c.emit(bytecode.OpSetGlobalSlot, node.Name.Line)
			c.emitByte(slot, node.Name.Line)
			return nil
		}
		if assignedType != "" {
			c.globalTypes[node.Name.Lexeme] = assignedType
		}
		name := c.constant(node.Name.Lexeme)
		c.emit(bytecode.OpSetGlobal, node.Name.Line)
		c.emitUint16(name, node.Name.Line)
		return nil
	case *ast.ExprStmt:
		if err := c.compileExpr(node.Expr); err != nil {
			return err
		}
		c.emit(bytecode.OpPop, 0)
		return nil
	case *ast.SetStmt:
		if slot, ok := c.resolveFieldSlot(node.Object, node.Name.Lexeme); ok {
			if err := c.compileExpr(node.Object); err != nil {
				return err
			}
			if err := c.compileExpr(node.Value); err != nil {
				return err
			}
			c.emit(bytecode.OpSetField, node.Name.Line)
			c.emitByte(byte(slot), node.Name.Line)
			return nil
		}
		if err := c.compileExpr(node.Object); err != nil {
			return err
		}
		if err := c.compileExpr(node.Value); err != nil {
			return err
		}
		name := c.constant(node.Name.Lexeme)
		c.emit(bytecode.OpSetProperty, node.Name.Line)
		c.emitUint16(name, node.Name.Line)
		return nil
	case *ast.IfStmt:
		if err := c.compileExpr(node.Condition); err != nil {
			return err
		}
		jumpIfFalse := c.emitJump(bytecode.OpJumpIfFalse, 0)
		c.emit(bytecode.OpPop, 0)
		if err := c.compileBlock(node.Then); err != nil {
			return err
		}
		jumpOverElse := c.emitJump(bytecode.OpJump, 0)
		c.patchJump(jumpIfFalse)
		c.emit(bytecode.OpPop, 0)
		if node.Else != nil {
			if err := c.compileBlock(node.Else); err != nil {
				return err
			}
		}
		c.patchJump(jumpOverElse)
		return nil
	case *ast.ReturnStmt:
		if node.Value != nil {
			if err := c.compileExpr(node.Value); err != nil {
				return err
			}
		} else {
			c.emit(bytecode.OpNil, node.Keyword.Line)
		}
		c.emit(bytecode.OpReturn, node.Keyword.Line)
		return nil
	case *ast.FunctionStmt:
		fn, err := c.compileFunction(node)
		if err != nil {
			return err
		}
		constant := c.constant(fn)
		c.emit(bytecode.OpConstant, node.Name.Line)
		c.emitUint16(constant, node.Name.Line)
		if c.state.depth == 0 {
			if slot, ok := c.resolveGlobalSlot(node.Name.Lexeme); ok {
				c.emit(bytecode.OpDefineGlobalSlot, node.Name.Line)
				c.emitByte(slot, node.Name.Line)
				return nil
			}
			name := c.constant(node.Name.Lexeme)
			c.emit(bytecode.OpDefineGlobal, node.Name.Line)
			c.emitUint16(name, node.Name.Line)
			return nil
		}
		slot := c.declareLocal(node.Name, "Function")
		c.emit(bytecode.OpSetLocal, node.Name.Line)
		c.emitByte(slot, node.Name.Line)
		return nil
	case *ast.ForStmt:
		if len(node.Targets) == 1 && c.isRangeCall(node.Iterable) {
			return c.compileFastRangeFor(node)
		}
		c.beginScope()
		anchor := node.Targets[0]
		iteratorName := token.Token{Lexeme: "__iter_" + anchor.Lexeme, Line: anchor.Line}
		iterSlot := c.declareLocal(iteratorName, "Iterator")
		targetSlots := make([]byte, len(node.Targets))
		itemSlot := byte(0)
		if len(node.Targets) == 1 {
			targetSlots[0] = c.declareLocal(node.Targets[0], "")
			itemSlot = targetSlots[0]
		} else {
			itemSlot = c.declareLocal(token.Token{Lexeme: "__item_" + anchor.Lexeme, Line: anchor.Line}, "")
			for i, target := range node.Targets {
				targetSlots[i] = c.declareLocal(target, "")
			}
		}
		if err := c.compileExpr(node.Iterable); err != nil {
			return err
		}
		c.emit(bytecode.OpIterInit, anchor.Line)
		c.emitByte(iterSlot, anchor.Line)
		loopStart := len(c.state.chunk.Code)
		exitJump := c.emitIterNext(iterSlot, itemSlot, anchor.Line)
		if len(node.Targets) > 1 {
			c.emit(bytecode.OpGetLocal, anchor.Line)
			c.emitByte(itemSlot, anchor.Line)
			c.emit(bytecode.OpUnpack, anchor.Line)
			c.emitByte(byte(len(node.Targets)), anchor.Line)
			for i := len(targetSlots) - 1; i >= 0; i-- {
				c.emit(bytecode.OpSetLocal, node.Targets[i].Line)
				c.emitByte(targetSlots[i], node.Targets[i].Line)
			}
		}
		if node.Condition != nil {
			if err := c.compileExpr(node.Condition); err != nil {
				return err
			}
			skipBody := c.emitJump(bytecode.OpJumpIfFalse, anchor.Line)
			c.emit(bytecode.OpPop, anchor.Line)
			if err := c.compileBlock(node.Body); err != nil {
				return err
			}
			c.emitLoop(loopStart, anchor.Line)
			c.patchJump(skipBody)
			c.emit(bytecode.OpPop, anchor.Line)
		} else {
			if err := c.compileBlock(node.Body); err != nil {
				return err
			}
			c.emitLoop(loopStart, anchor.Line)
		}
		c.patchJump(exitJump)
		c.endScope(anchor.Line)
		return nil
	case *ast.ClassStmt:
		classValue, err := c.compileClass(node)
		if err != nil {
			return err
		}
		constant := c.constant(classValue)
		c.emit(bytecode.OpConstant, node.Name.Line)
		c.emitUint16(constant, node.Name.Line)
		if c.state.depth == 0 {
			if slot, ok := c.resolveGlobalSlot(node.Name.Lexeme); ok {
				c.emit(bytecode.OpDefineGlobalSlot, node.Name.Line)
				c.emitByte(slot, node.Name.Line)
				return nil
			}
			name := c.constant(node.Name.Lexeme)
			c.emit(bytecode.OpDefineGlobal, node.Name.Line)
			c.emitUint16(name, node.Name.Line)
			return nil
		}
		slot := c.declareLocal(node.Name, node.Name.Lexeme)
		c.emit(bytecode.OpSetLocal, node.Name.Line)
		c.emitByte(slot, node.Name.Line)
		return nil
	case *ast.BlockStmt:
		return c.compileBlock(node)
	default:
		return fmt.Errorf("unsupported statement %T", stmt)
	}
}

func (c *Compiler) compileBlock(block *ast.BlockStmt) error {
	c.beginScope()
	defer c.endScope(0)
	for _, stmt := range block.Statements {
		if err := c.compileStmt(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (c *Compiler) compileExpr(expr ast.Expr) error {
	switch node := expr.(type) {
	case *ast.LiteralExpr:
		switch valueExpr := node.Value.(type) {
		case nil:
			c.emit(bytecode.OpNil, 0)
		case bool:
			if valueExpr {
				c.emit(bytecode.OpTrue, 0)
			} else {
				c.emit(bytecode.OpFalse, 0)
			}
		case float64, string:
			constant := c.constant(valueExpr)
			c.emit(bytecode.OpConstant, 0)
			c.emitUint16(constant, 0)
		default:
			return fmt.Errorf("unsupported literal %T", node.Value)
		}
		return nil
	case *ast.GroupingExpr:
		return c.compileExpr(node.Expr)
	case *ast.TupleExpr:
		for _, element := range node.Elements {
			if err := c.compileExpr(element); err != nil {
				return err
			}
		}
		c.emit(bytecode.OpTuple, 0)
		c.emitByte(byte(len(node.Elements)), 0)
		return nil
	case *ast.VariableExpr:
		if slot, ok := c.resolveLocal(node.Name.Lexeme); ok {
			c.emit(bytecode.OpGetLocal, node.Name.Line)
			c.emitByte(slot, node.Name.Line)
			return nil
		}
		if slot, ok := c.resolveGlobalSlot(node.Name.Lexeme); ok {
			c.emit(bytecode.OpGetGlobalSlot, node.Name.Line)
			c.emitByte(slot, node.Name.Line)
			return nil
		}
		name := c.constant(node.Name.Lexeme)
		c.emit(bytecode.OpGetGlobal, node.Name.Line)
		c.emitUint16(name, node.Name.Line)
		return nil
	case *ast.ThisExpr:
		if slot, ok := c.resolveLocal(node.Keyword.Lexeme); ok {
			c.emit(bytecode.OpGetLocal, node.Keyword.Line)
			c.emitByte(slot, node.Keyword.Line)
			return nil
		}
		return fmt.Errorf("'this' used outside method or constructor")
	case *ast.SuperExpr:
		return fmt.Errorf("super must be called as super(...)")
	case *ast.UnaryExpr:
		if err := c.compileExpr(node.Right); err != nil {
			return err
		}
		switch node.Operator.Type {
		case token.Minus:
			c.emit(bytecode.OpNegate, node.Operator.Line)
		case token.Bang:
			c.emit(bytecode.OpNot, node.Operator.Line)
		default:
			return fmt.Errorf("unsupported unary operator %s", node.Operator.Type)
		}
		return nil
	case *ast.GetExpr:
		if slot, ok := c.resolveFieldSlot(node.Object, node.Name.Lexeme); ok {
			if err := c.compileExpr(node.Object); err != nil {
				return err
			}
			c.emit(bytecode.OpGetField, node.Name.Line)
			c.emitByte(byte(slot), node.Name.Line)
			return nil
		}
		if err := c.compileExpr(node.Object); err != nil {
			return err
		}
		name := c.constant(node.Name.Lexeme)
		c.emit(bytecode.OpGetProperty, node.Name.Line)
		c.emitUint16(name, node.Name.Line)
		return nil
	case *ast.BinaryExpr:
		return c.compileBinary(node)
	case *ast.CallExpr:
		if variable, ok := node.Callee.(*ast.VariableExpr); ok && variable.Name.Lexeme == "range" {
			if len(node.Arguments) < 1 || len(node.Arguments) > 2 {
				return fmt.Errorf("range expects 1 or 2 arguments")
			}
			for _, arg := range node.Arguments {
				if err := c.compileExpr(arg); err != nil {
					return err
				}
			}
			c.emit(bytecode.OpRange, variable.Name.Line)
			c.emitByte(byte(len(node.Arguments)), variable.Name.Line)
			return nil
		}
		if _, ok := node.Callee.(*ast.SuperExpr); ok {
			for _, arg := range node.Arguments {
				if err := c.compileExpr(arg); err != nil {
					return err
				}
			}
			c.emit(bytecode.OpCallSuper, node.Paren.Line)
			c.emitByte(byte(len(node.Arguments)), node.Paren.Line)
			return nil
		}
		if getter, ok := node.Callee.(*ast.GetExpr); ok {
			if slot, ok := c.resolveMethodSlot(getter.Object, getter.Name.Lexeme); ok {
				if err := c.compileExpr(getter.Object); err != nil {
					return err
				}
				for _, arg := range node.Arguments {
					if err := c.compileExpr(arg); err != nil {
						return err
					}
				}
				c.emit(bytecode.OpInvokeMethod, node.Paren.Line)
				c.emitByte(byte(slot), node.Paren.Line)
				c.emitByte(byte(len(node.Arguments)), node.Paren.Line)
				return nil
			}
			if err := c.compileExpr(getter.Object); err != nil {
				return err
			}
			for _, arg := range node.Arguments {
				if err := c.compileExpr(arg); err != nil {
					return err
				}
			}
			name := c.constant(getter.Name.Lexeme)
			c.emit(bytecode.OpInvoke, node.Paren.Line)
			c.emitUint16(name, node.Paren.Line)
			c.emitByte(byte(len(node.Arguments)), node.Paren.Line)
			return nil
		}
		if err := c.compileExpr(node.Callee); err != nil {
			return err
		}
		for _, arg := range node.Arguments {
			if err := c.compileExpr(arg); err != nil {
				return err
			}
		}
		c.emit(bytecode.OpCall, node.Paren.Line)
		c.emitByte(byte(len(node.Arguments)), node.Paren.Line)
		return nil
	case *ast.NewExpr:
		if slot, ok := c.resolveGlobalSlot(node.Class.Lexeme); ok {
			c.emit(bytecode.OpGetGlobalSlot, node.Class.Line)
			c.emitByte(slot, node.Class.Line)
		} else {
			name := c.constant(node.Class.Lexeme)
			c.emit(bytecode.OpGetGlobal, node.Class.Line)
			c.emitUint16(name, node.Class.Line)
		}
		for _, arg := range node.Arguments {
			if err := c.compileExpr(arg); err != nil {
				return err
			}
		}
		c.emit(bytecode.OpCall, node.Paren.Line)
		c.emitByte(byte(len(node.Arguments)), node.Paren.Line)
		return nil
	default:
		return fmt.Errorf("unsupported expression %T", expr)
	}
}

func (c *Compiler) compileBinary(node *ast.BinaryExpr) error {
	if node.Operator.Type == token.AndAnd {
		if err := c.compileExpr(node.Left); err != nil {
			return err
		}
		endJump := c.emitJump(bytecode.OpJumpIfFalse, node.Operator.Line)
		c.emit(bytecode.OpPop, node.Operator.Line)
		if err := c.compileExpr(node.Right); err != nil {
			return err
		}
		c.patchJump(endJump)
		return nil
	}
	if node.Operator.Type == token.OrOr {
		if err := c.compileExpr(node.Left); err != nil {
			return err
		}
		elseJump := c.emitJump(bytecode.OpJumpIfFalse, node.Operator.Line)
		endJump := c.emitJump(bytecode.OpJump, node.Operator.Line)
		c.patchJump(elseJump)
		c.emit(bytecode.OpPop, node.Operator.Line)
		if err := c.compileExpr(node.Right); err != nil {
			return err
		}
		c.patchJump(endJump)
		return nil
	}
	if err := c.compileExpr(node.Left); err != nil {
		return err
	}
	if err := c.compileExpr(node.Right); err != nil {
		return err
	}
	switch node.Operator.Type {
	case token.Plus:
		if c.inferExprType(node.Left) == "Number" && c.inferExprType(node.Right) == "Number" {
			c.emit(bytecode.OpAddNum, node.Operator.Line)
		} else {
			c.emit(bytecode.OpAdd, node.Operator.Line)
		}
	case token.Minus:
		if c.inferExprType(node.Left) == "Number" && c.inferExprType(node.Right) == "Number" {
			c.emit(bytecode.OpSubNum, node.Operator.Line)
		} else {
			c.emit(bytecode.OpSub, node.Operator.Line)
		}
	case token.Star:
		if c.inferExprType(node.Left) == "Number" && c.inferExprType(node.Right) == "Number" {
			c.emit(bytecode.OpMulNum, node.Operator.Line)
		} else {
			c.emit(bytecode.OpMul, node.Operator.Line)
		}
	case token.Slash:
		if c.inferExprType(node.Left) == "Number" && c.inferExprType(node.Right) == "Number" {
			c.emit(bytecode.OpDivNum, node.Operator.Line)
		} else {
			c.emit(bytecode.OpDiv, node.Operator.Line)
		}
	case token.Percent:
		c.emit(bytecode.OpMod, node.Operator.Line)
	case token.EqualEqual:
		c.emit(bytecode.OpEqual, node.Operator.Line)
	case token.BangEqual:
		c.emit(bytecode.OpEqual, node.Operator.Line)
		c.emit(bytecode.OpNot, node.Operator.Line)
	case token.Greater:
		if c.inferExprType(node.Left) == "Number" && c.inferExprType(node.Right) == "Number" {
			c.emit(bytecode.OpGreaterNum, node.Operator.Line)
		} else {
			c.emit(bytecode.OpGreater, node.Operator.Line)
		}
	case token.Less:
		if c.inferExprType(node.Left) == "Number" && c.inferExprType(node.Right) == "Number" {
			c.emit(bytecode.OpLessNum, node.Operator.Line)
		} else {
			c.emit(bytecode.OpLess, node.Operator.Line)
		}
	case token.GreaterEqual:
		if c.inferExprType(node.Left) == "Number" && c.inferExprType(node.Right) == "Number" {
			c.emit(bytecode.OpLessNum, node.Operator.Line)
		} else {
			c.emit(bytecode.OpLess, node.Operator.Line)
		}
		c.emit(bytecode.OpNot, node.Operator.Line)
	case token.LessEqual:
		if c.inferExprType(node.Left) == "Number" && c.inferExprType(node.Right) == "Number" {
			c.emit(bytecode.OpGreaterNum, node.Operator.Line)
		} else {
			c.emit(bytecode.OpGreater, node.Operator.Line)
		}
		c.emit(bytecode.OpNot, node.Operator.Line)
	default:
		return fmt.Errorf("unsupported binary operator %s", node.Operator.Type)
	}
	return nil
}

func (c *Compiler) compileFunction(stmt *ast.FunctionStmt) (*bytecode.Function, error) {
	return c.compileCallable(stmt.Name.Lexeme, stmt.Params, stmt.ReturnType, stmt.Body, false, nil)
}

func (c *Compiler) compileCallable(name string, params []ast.Parameter, returnType *ast.TypeRef, body *ast.BlockStmt, includeThis bool, ownerClass *value.Class) (*bytecode.Function, error) {
	fnChunk := bytecode.NewChunk()
	child := &state{
		function:   &bytecode.Function{Name: name, Arity: len(params), ReturnType: c.typeNameFromRef(returnType), Chunk: fnChunk},
		chunk:      fnChunk,
		locals:     make([]local, 0),
		depth:      1,
		parent:     c.state,
		name:       name,
		ownerClass: ownerClass,
	}
	childCompiler := &Compiler{state: child, classes: c.classes, consts: c.consts, globalTypes: c.globalTypes, capturedGlobals: c.capturedGlobals, globalSlots: c.globalSlots}
	if includeThis {
		child.declareParam(token.Token{Lexeme: "this"}, ownerClass.Name)
	}
	for _, param := range params {
		child.declareParam(param.Name, c.typeNameFromRef(param.Type))
	}
	for _, bodyStmt := range body.Statements {
		if err := childCompiler.compileStmt(bodyStmt); err != nil {
			return nil, err
		}
	}
	childCompiler.emit(bytecode.OpNil, 0)
	childCompiler.emit(bytecode.OpReturn, 0)
	return child.function, nil
}

func (c *Compiler) compileClass(stmt *ast.ClassStmt) (*value.Class, error) {
	var superclass *value.Class
	if stmt.Superclass != nil {
		resolved, ok := c.classes[stmt.Superclass.Name.Lexeme]
		if !ok {
			return nil, fmt.Errorf("unknown superclass %s", stmt.Superclass.Name.Lexeme)
		}
		superclass = resolved
	}

	classValue := &value.Class{
		Name:              stmt.Name.Lexeme,
		Superclass:        superclass,
		Implements:        make(map[string]bool),
		Fields:            make(map[string]value.FieldDef),
		FieldOrder:        make([]string, 0),
		FieldIndex:        make(map[string]int),
		MethodOrder:       make([]string, 0),
		MethodIndex:       make(map[string]int),
		MethodTable:       make([]*bytecode.Function, 0),
		Methods:           make(map[string]*bytecode.Function),
		StaticFields:      make(map[string]value.FieldDef),
		StaticValues:      make(map[string]value.Value),
		StaticMethods:     make(map[string]*bytecode.Function),
		MethodAnnotations: make(map[string][]string),
	}
	for _, iface := range stmt.Implements {
		classValue.Implements[iface.Name.Lexeme] = true
	}
	if superclass != nil {
		classValue.FieldOrder = append(classValue.FieldOrder, superclass.FieldOrder...)
		for name, idx := range superclass.FieldIndex {
			classValue.FieldIndex[name] = idx
		}
		for name, def := range superclass.Fields {
			classValue.Fields[name] = def
		}
		classValue.MethodOrder = append(classValue.MethodOrder, superclass.MethodOrder...)
		for name, idx := range superclass.MethodIndex {
			classValue.MethodIndex[name] = idx
		}
		classValue.MethodTable = append(classValue.MethodTable, superclass.MethodTable...)
		for name, fn := range superclass.Methods {
			classValue.Methods[name] = fn
		}
	}
	c.classes[stmt.Name.Lexeme] = classValue

	for _, field := range stmt.Fields {
		defaultValue := value.NilValue()
		if field.Value != nil {
			constantValue, ok := c.evalConstExpr(field.Value)
			if !ok {
				return nil, fmt.Errorf("field initializer for %s must be compile-time constant for now", field.Name.Lexeme)
			}
			if field.Kind == ast.VariableConst || field.Kind == ast.VariableFinal {
				constantValue = c.freezeValue(constantValue)
			}
			defaultValue = constantValue
		}
		def := value.FieldDef{Default: defaultValue, Mutable: field.Kind == ast.VariableLet || field.Kind == ast.VariableVar, TypeName: c.inferFieldType(field, defaultValue)}
		if field.Static {
			classValue.StaticFields[field.Name.Lexeme] = def
			classValue.StaticValues[field.Name.Lexeme] = defaultValue
			continue
		}
		if _, exists := classValue.FieldIndex[field.Name.Lexeme]; !exists {
			classValue.FieldIndex[field.Name.Lexeme] = len(classValue.FieldOrder)
			classValue.FieldOrder = append(classValue.FieldOrder, field.Name.Lexeme)
		}
		classValue.Fields[field.Name.Lexeme] = def
	}

	for _, method := range stmt.Methods {
		fn, err := c.compileCallable(method.Name.Lexeme, method.Params, method.ReturnType, method.Body, !method.Static, classValue)
		if err != nil {
			return nil, err
		}
		if !method.Static {
			switch method.Name.Lexeme {
			case "__length":
				classValue.IterableLength = fn
			case "__get":
				classValue.IterableGet = fn
			case "__pieces":
				classValue.PiecesMethod = fn
			case "__get_piece", "__getPiece":
				classValue.GetPieceMethod = fn
			}
		}
		for _, annotation := range method.Annotations {
			normalized := normalizeAnnotation(annotation.Name.Lexeme)
			classValue.MethodAnnotations[method.Name.Lexeme] = append(classValue.MethodAnnotations[method.Name.Lexeme], normalized)
			switch normalized {
			case "Equals":
				classValue.EqualMethod = fn
			case "Hash":
				classValue.HashMethod = fn
			}
		}
		if method.IsConstructor {
			classValue.Constructor = fn
			continue
		}
		if method.Static {
			classValue.StaticMethods[method.Name.Lexeme] = fn
			continue
		}
		if slot, exists := classValue.MethodIndex[method.Name.Lexeme]; exists {
			classValue.MethodTable[slot] = fn
		} else {
			classValue.MethodIndex[method.Name.Lexeme] = len(classValue.MethodOrder)
			classValue.MethodOrder = append(classValue.MethodOrder, method.Name.Lexeme)
			classValue.MethodTable = append(classValue.MethodTable, fn)
		}
		classValue.Methods[method.Name.Lexeme] = fn
	}
	return classValue, nil
}

func (c *Compiler) evalConstExpr(expr ast.Expr) (value.Value, bool) {
	switch node := expr.(type) {
	case *ast.LiteralExpr:
		switch v := node.Value.(type) {
		case nil:
			return value.NilValue(), true
		case bool:
			return value.BoolValue(v), true
		case float64:
			return value.NumberValue(v), true
		case string:
			return value.StringValue(v), true
		default:
			return value.NilValue(), false
		}
	case *ast.GroupingExpr:
		return c.evalConstExpr(node.Expr)
	case *ast.VariableExpr:
		v, ok := c.consts[node.Name.Lexeme]
		return v, ok
	case *ast.UnaryExpr:
		right, ok := c.evalConstExpr(node.Right)
		if !ok {
			return value.NilValue(), false
		}
		switch node.Operator.Type {
		case token.Minus:
			if right.Kind != value.Number {
				return value.NilValue(), false
			}
			return value.NumberValue(-right.Num), true
		case token.Bang:
			return value.BoolValue(!right.IsTruthy()), true
		default:
			return value.NilValue(), false
		}
	case *ast.BinaryExpr:
		left, ok := c.evalConstExpr(node.Left)
		if !ok {
			return value.NilValue(), false
		}
		right, ok := c.evalConstExpr(node.Right)
		if !ok {
			return value.NilValue(), false
		}
		switch node.Operator.Type {
		case token.Plus:
			if left.Kind == value.Number && right.Kind == value.Number {
				return value.NumberValue(left.Num + right.Num), true
			}
			if left.Kind == value.String || right.Kind == value.String {
				return value.StringValue(left.String() + right.String()), true
			}
		case token.Minus:
			if left.Kind == value.Number && right.Kind == value.Number {
				return value.NumberValue(left.Num - right.Num), true
			}
		case token.Star:
			if left.Kind == value.Number && right.Kind == value.Number {
				return value.NumberValue(left.Num * right.Num), true
			}
		case token.Slash:
			if left.Kind == value.Number && right.Kind == value.Number {
				return value.NumberValue(left.Num / right.Num), true
			}
		case token.Percent:
			if left.Kind == value.Number && right.Kind == value.Number {
				return value.NumberValue(math.Mod(left.Num, right.Num)), true
			}
		case token.AndAnd:
			return value.BoolValue(left.IsTruthy() && right.IsTruthy()), true
		case token.OrOr:
			return value.BoolValue(left.IsTruthy() || right.IsTruthy()), true
		case token.EqualEqual:
			return value.BoolValue(value.Equal(left, right)), true
		case token.BangEqual:
			return value.BoolValue(!value.Equal(left, right)), true
		case token.Greater:
			if left.Kind == value.Number && right.Kind == value.Number {
				return value.BoolValue(left.Num > right.Num), true
			}
		case token.GreaterEqual:
			if left.Kind == value.Number && right.Kind == value.Number {
				return value.BoolValue(left.Num >= right.Num), true
			}
		case token.Less:
			if left.Kind == value.Number && right.Kind == value.Number {
				return value.BoolValue(left.Num < right.Num), true
			}
		case token.LessEqual:
			if left.Kind == value.Number && right.Kind == value.Number {
				return value.BoolValue(left.Num <= right.Num), true
			}
		}
	}
	return value.NilValue(), false
}

func (c *Compiler) freezeValue(v value.Value) value.Value {
	if instance, ok := v.AsInstance(); ok {
		instance.Frozen = true
	}
	return v
}

func (c *Compiler) beginScope() {
	c.state.depth++
}

func (c *Compiler) endScope(line int) {
	for len(c.state.locals) > 0 && c.state.locals[len(c.state.locals)-1].depth >= c.state.depth {
		c.state.locals = c.state.locals[:len(c.state.locals)-1]
	}
	c.state.depth--
}

func (c *Compiler) declareLocal(name token.Token, typ string) byte {
	slot := byte(len(c.state.locals))
	c.state.locals = append(c.state.locals, local{name: name.Lexeme, depth: c.state.depth, slot: slot, typ: typ})
	if len(c.state.locals) > c.state.function.MaxLocals {
		c.state.function.MaxLocals = len(c.state.locals)
	}
	return slot
}

func (s *state) declareParam(name token.Token, typ string) {
	slot := byte(len(s.locals))
	s.locals = append(s.locals, local{name: name.Lexeme, depth: 1, slot: slot, typ: typ})
	if len(s.locals) > s.function.MaxLocals {
		s.function.MaxLocals = len(s.locals)
	}
}

func (c *Compiler) resolveLocal(name string) (byte, bool) {
	for i := len(c.state.locals) - 1; i >= 0; i-- {
		if c.state.locals[i].name == name {
			return c.state.locals[i].slot, true
		}
	}
	return 0, false
}

func (c *Compiler) setLocalType(slot byte, typ string) {
	if typ == "" {
		return
	}
	for i := range c.state.locals {
		if c.state.locals[i].slot == slot {
			c.state.locals[i].typ = typ
			return
		}
	}
}

func (c *Compiler) inferDeclaredType(typeRef *ast.TypeRef, valueExpr ast.Expr) string {
	if typeRef != nil {
		return c.typeNameFromRef(typeRef)
	}
	return c.inferExprType(valueExpr)
}

func (c *Compiler) typeNameFromRef(typeRef *ast.TypeRef) string {
	if typeRef == nil {
		return ""
	}
	if typeRef.Name.Lexeme == "Number" || typeRef.Name.Lexeme == "Float" || typeRef.Name.Lexeme == "Int" {
		return "Number"
	}
	return typeRef.Name.Lexeme
}

func (c *Compiler) inferFieldType(field ast.FieldDecl, defaultValue value.Value) string {
	if field.Type != nil {
		return c.typeNameFromRef(field.Type)
	}
	switch defaultValue.Kind {
	case value.Number:
		return "Number"
	case value.String:
		return "String"
	case value.Bool:
		return "Bool"
	default:
		return ""
	}
}

func (c *Compiler) inferExprType(expr ast.Expr) string {
	switch node := expr.(type) {
	case *ast.LiteralExpr:
		switch node.Value.(type) {
		case float64:
			return "Number"
		case string:
			return "String"
		case bool:
			return "Bool"
		default:
			return ""
		}
	case *ast.GroupingExpr:
		return c.inferExprType(node.Expr)
	case *ast.TupleExpr:
		return "Tuple"
	case *ast.VariableExpr:
		for i := len(c.state.locals) - 1; i >= 0; i-- {
			if c.state.locals[i].name == node.Name.Lexeme {
				return c.state.locals[i].typ
			}
		}
		return c.globalTypes[node.Name.Lexeme]
	case *ast.ThisExpr:
		return c.state.ownerClass.Name
	case *ast.GetExpr:
		if class := c.resolveExprClass(node.Object); class != nil {
			if field, ok := class.LookupField(node.Name.Lexeme); ok {
				return field.TypeName
			}
			return ""
		}
		return ""
	case *ast.NewExpr:
		return node.Class.Lexeme
	case *ast.CallExpr:
		switch callee := node.Callee.(type) {
		case *ast.GetExpr:
			if class := c.resolveExprClass(callee.Object); class != nil {
				if method, _, ok := class.LookupMethod(callee.Name.Lexeme); ok {
					return method.ReturnType
				}
			}
		case *ast.VariableExpr:
			if callee.Name.Lexeme == "range" {
				return "Range"
			}
		}
		return ""
	case *ast.BinaryExpr:
		left := c.inferExprType(node.Left)
		right := c.inferExprType(node.Right)
		switch node.Operator.Type {
		case token.AndAnd, token.OrOr:
			return "Bool"
		case token.Plus:
			if left == "Number" && right == "Number" {
				return "Number"
			}
			if left == "String" || right == "String" {
				return "String"
			}
		case token.Minus, token.Star, token.Slash, token.Percent:
			if left == "Number" && right == "Number" {
				return "Number"
			}
		case token.EqualEqual, token.BangEqual, token.Greater, token.GreaterEqual, token.Less, token.LessEqual:
			return "Bool"
		}
	}
	return ""
}

func (c *Compiler) isRangeCall(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	callee, ok := call.Callee.(*ast.VariableExpr)
	if !ok || callee.Name.Lexeme != "range" {
		return false
	}
	return len(call.Arguments) == 1 || len(call.Arguments) == 2
}

func (c *Compiler) compileFastRangeFor(node *ast.ForStmt) error {
	call := node.Iterable.(*ast.CallExpr)
	c.beginScope()
	anchor := node.Targets[0]
	loopVar := c.declareLocal(anchor, "Number")
	currentSlot := c.declareLocal(token.Token{Lexeme: "__range_current_" + anchor.Lexeme, Line: anchor.Line}, "Number")
	endSlot := c.declareLocal(token.Token{Lexeme: "__range_end_" + anchor.Lexeme, Line: anchor.Line}, "Number")
	stepSlot := c.declareLocal(token.Token{Lexeme: "__range_step_" + anchor.Lexeme, Line: anchor.Line}, "Number")
	for _, arg := range call.Arguments {
		if err := c.compileExpr(arg); err != nil {
			return err
		}
	}
	c.emit(bytecode.OpRangeInitFast, anchor.Line)
	c.emitByte(currentSlot, anchor.Line)
	c.emitByte(endSlot, anchor.Line)
	c.emitByte(stepSlot, anchor.Line)
	c.emitByte(byte(len(call.Arguments)), anchor.Line)
	loopStart := len(c.state.chunk.Code)
	exitJump := c.emitRangeNextFast(currentSlot, endSlot, stepSlot, loopVar, anchor.Line)
	if node.Condition != nil {
		if err := c.compileExpr(node.Condition); err != nil {
			return err
		}
		skipBody := c.emitJump(bytecode.OpJumpIfFalse, anchor.Line)
		c.emit(bytecode.OpPop, anchor.Line)
		if err := c.compileBlock(node.Body); err != nil {
			return err
		}
		c.emitLoop(loopStart, anchor.Line)
		c.patchJump(skipBody)
		c.emit(bytecode.OpPop, anchor.Line)
	} else {
		if err := c.compileBlock(node.Body); err != nil {
			return err
		}
		c.emitLoop(loopStart, anchor.Line)
	}
	c.patchJump(exitJump)
	c.endScope(anchor.Line)
	return nil
}

func (c *Compiler) inferDestructureTypes(expr ast.Expr, count int) []string {
	types := make([]string, count)
	tuple, ok := expr.(*ast.TupleExpr)
	if !ok || len(tuple.Elements) != count {
		return types
	}
	for i, element := range tuple.Elements {
		types[i] = c.inferExprType(element)
	}
	return types
}

func (c *Compiler) resolveExprClass(expr ast.Expr) *value.Class {
	className := c.inferExprType(expr)
	if className == "" {
		return nil
	}
	classValue, ok := c.classes[className]
	if !ok {
		return nil
	}
	return classValue
}

func (c *Compiler) resolveFieldSlot(expr ast.Expr, fieldName string) (int, bool) {
	classValue := c.resolveExprClass(expr)
	if classValue == nil {
		return 0, false
	}
	slot, _, ok := classValue.LookupFieldSlot(fieldName)
	return slot, ok
}

func (c *Compiler) resolveMethodSlot(expr ast.Expr, methodName string) (int, bool) {
	classValue := c.resolveExprClass(expr)
	if classValue == nil {
		return 0, false
	}
	slot, _, ok := classValue.LookupMethodSlot(methodName)
	return slot, ok
}

func (c *Compiler) resolveGlobalSlot(name string) (byte, bool) {
	slot, ok := c.globalSlots[name]
	return slot, ok
}

func (c *Compiler) shouldUseScriptLocal(name string) bool {
	if c.state.parent != nil || c.state.name != "<script>" {
		return false
	}
	return !c.capturedGlobals[name]
}

func (c *Compiler) shouldUseScriptLocals(targets []token.Token) bool {
	if c.state.parent != nil || c.state.name != "<script>" {
		return false
	}
	for _, target := range targets {
		if c.capturedGlobals[target.Lexeme] {
			return false
		}
	}
	return true
}

func collectCapturedGlobals(program *ast.Program) map[string]bool {
	captured := make(map[string]bool)
	for _, stmt := range program.Statements {
		collectCapturedGlobalsStmt(stmt, captured, false, nil)
	}
	return captured
}

func collectGlobalSlots(program *ast.Program) map[string]byte {
	slots := make(map[string]byte)
	var next byte
	for _, stmt := range program.Statements {
		switch node := stmt.(type) {
		case *ast.LetStmt:
			if _, exists := slots[node.Name.Lexeme]; !exists {
				slots[node.Name.Lexeme] = next
				next++
			}
		case *ast.DestructureLetStmt:
			for _, target := range node.Targets {
				if _, exists := slots[target.Lexeme]; !exists {
					slots[target.Lexeme] = next
					next++
				}
			}
		case *ast.FunctionStmt:
			if _, exists := slots[node.Name.Lexeme]; !exists {
				slots[node.Name.Lexeme] = next
				next++
			}
		case *ast.ClassStmt:
			if _, exists := slots[node.Name.Lexeme]; !exists {
				slots[node.Name.Lexeme] = next
				next++
			}
		}
	}
	return slots
}

func collectCapturedGlobalsStmt(stmt ast.Stmt, captured map[string]bool, inCallable bool, scope map[string]bool) {
	switch node := stmt.(type) {
	case *ast.LetStmt:
		if scope != nil {
			scope[node.Name.Lexeme] = true
		}
		collectCapturedGlobalsExpr(node.Value, captured, inCallable, scope)
	case *ast.DestructureLetStmt:
		if scope != nil {
			for _, target := range node.Targets {
				scope[target.Lexeme] = true
			}
		}
		collectCapturedGlobalsExpr(node.Value, captured, inCallable, scope)
	case *ast.AssignStmt:
		if inCallable && (scope == nil || !scope[node.Name.Lexeme]) {
			captured[node.Name.Lexeme] = true
		}
		collectCapturedGlobalsExpr(node.Value, captured, inCallable, scope)
	case *ast.SetStmt:
		collectCapturedGlobalsExpr(node.Object, captured, inCallable, scope)
		collectCapturedGlobalsExpr(node.Value, captured, inCallable, scope)
	case *ast.ExprStmt:
		collectCapturedGlobalsExpr(node.Expr, captured, inCallable, scope)
	case *ast.IfStmt:
		collectCapturedGlobalsExpr(node.Condition, captured, inCallable, scope)
		collectCapturedGlobalsBlock(node.Then, captured, inCallable, cloneScope(scope))
		if node.Else != nil {
			collectCapturedGlobalsBlock(node.Else, captured, inCallable, cloneScope(scope))
		}
	case *ast.ReturnStmt:
		if node.Value != nil {
			collectCapturedGlobalsExpr(node.Value, captured, inCallable, scope)
		}
	case *ast.ForStmt:
		collectCapturedGlobalsExpr(node.Iterable, captured, inCallable, scope)
		loopScope := cloneScope(scope)
		for _, target := range node.Targets {
			loopScope[target.Lexeme] = true
		}
		if node.Condition != nil {
			collectCapturedGlobalsExpr(node.Condition, captured, inCallable, loopScope)
		}
		collectCapturedGlobalsBlock(node.Body, captured, inCallable, loopScope)
	case *ast.BlockStmt:
		collectCapturedGlobalsBlock(node, captured, inCallable, cloneScope(scope))
	case *ast.FunctionStmt:
		innerScope := make(map[string]bool)
		for _, param := range node.Params {
			innerScope[param.Name.Lexeme] = true
		}
		collectCapturedGlobalsBlock(node.Body, captured, true, innerScope)
	case *ast.ClassStmt:
		for _, method := range node.Methods {
			innerScope := map[string]bool{}
			if !method.Static {
				innerScope["this"] = true
			}
			for _, param := range method.Params {
				innerScope[param.Name.Lexeme] = true
			}
			collectCapturedGlobalsBlock(method.Body, captured, true, innerScope)
		}
	}
}

func collectCapturedGlobalsBlock(block *ast.BlockStmt, captured map[string]bool, inCallable bool, scope map[string]bool) {
	for _, stmt := range block.Statements {
		collectCapturedGlobalsStmt(stmt, captured, inCallable, scope)
	}
}

func collectCapturedGlobalsExpr(expr ast.Expr, captured map[string]bool, inCallable bool, scope map[string]bool) {
	switch node := expr.(type) {
	case *ast.VariableExpr:
		if inCallable && (scope == nil || !scope[node.Name.Lexeme]) {
			captured[node.Name.Lexeme] = true
		}
	case *ast.GroupingExpr:
		collectCapturedGlobalsExpr(node.Expr, captured, inCallable, scope)
	case *ast.TupleExpr:
		for _, element := range node.Elements {
			collectCapturedGlobalsExpr(element, captured, inCallable, scope)
		}
	case *ast.UnaryExpr:
		collectCapturedGlobalsExpr(node.Right, captured, inCallable, scope)
	case *ast.BinaryExpr:
		collectCapturedGlobalsExpr(node.Left, captured, inCallable, scope)
		collectCapturedGlobalsExpr(node.Right, captured, inCallable, scope)
	case *ast.CallExpr:
		collectCapturedGlobalsExpr(node.Callee, captured, inCallable, scope)
		for _, arg := range node.Arguments {
			collectCapturedGlobalsExpr(arg, captured, inCallable, scope)
		}
	case *ast.NewExpr:
		if inCallable && (scope == nil || !scope[node.Class.Lexeme]) {
			captured[node.Class.Lexeme] = true
		}
		for _, arg := range node.Arguments {
			collectCapturedGlobalsExpr(arg, captured, inCallable, scope)
		}
	case *ast.GetExpr:
		collectCapturedGlobalsExpr(node.Object, captured, inCallable, scope)
	}
}

func cloneScope(scope map[string]bool) map[string]bool {
	if scope == nil {
		return map[string]bool{}
	}
	copyScope := make(map[string]bool, len(scope))
	for name, declared := range scope {
		copyScope[name] = declared
	}
	return copyScope
}

func normalizeAnnotation(name string) string {
	switch name {
	case "equals", "Equals", "EQUALS":
		return "Equals"
	case "hash", "Hash", "HASH":
		return "Hash"
	case "override", "Override", "OVERRIDE":
		return "Override"
	default:
		return name
	}
}

func (c *Compiler) emit(op bytecode.Op, line int) {
	c.state.chunk.WriteOp(op, line)
}

func (c *Compiler) emitByte(v byte, line int) {
	c.state.chunk.EmitByte(v, line)
}

func (c *Compiler) emitUint16(v uint16, line int) {
	c.state.chunk.WriteUint16(v, line)
}

func (c *Compiler) emitJump(op bytecode.Op, line int) int {
	c.emit(op, line)
	offset := len(c.state.chunk.Code)
	c.emitUint16(0, line)
	return offset
}

func (c *Compiler) emitLoop(loopStart int, line int) {
	c.emit(bytecode.OpLoop, line)
	distance := len(c.state.chunk.Code) - loopStart + 2
	c.emitUint16(uint16(distance), line)
}

func (c *Compiler) patchJump(offset int) {
	distance := len(c.state.chunk.Code) - offset - 2
	c.state.chunk.PatchUint16(offset, uint16(distance))
}

func (c *Compiler) emitIterNext(iterSlot, valueSlot byte, line int) int {
	c.emit(bytecode.OpIterNext, line)
	c.emitByte(iterSlot, line)
	c.emitByte(valueSlot, line)
	offset := len(c.state.chunk.Code)
	c.emitUint16(0, line)
	return offset
}

func (c *Compiler) emitRangeNextFast(currentSlot, endSlot, stepSlot, valueSlot byte, line int) int {
	c.emit(bytecode.OpRangeNextFast, line)
	c.emitByte(currentSlot, line)
	c.emitByte(endSlot, line)
	c.emitByte(stepSlot, line)
	c.emitByte(valueSlot, line)
	offset := len(c.state.chunk.Code)
	c.emitUint16(0, line)
	return offset
}

func (c *Compiler) constant(v any) uint16 {
	return c.state.chunk.AddConstant(v)
}
