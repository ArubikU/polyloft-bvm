package compiler

import (
	"fmt"
	"math"

	"github.com/ArubikU/polyloft-bvm/internal/ast"
	"github.com/ArubikU/polyloft-bvm/internal/bytecode"
	bvmruntime "github.com/ArubikU/polyloft-bvm/internal/runtime"
	"github.com/ArubikU/polyloft-bvm/internal/token"
	"github.com/ArubikU/polyloft-bvm/internal/value"
)

type Compiler struct {
	state           *state
	classes         map[string]*value.Class
	consts          map[string]value.Value
	globalTypes     map[string]string
	typeAliases     map[string]*ast.TypeRef
	forceGlobals    bool
	capturedGlobals map[string]bool
	globalSlots     map[string]byte
	lambdaCounter   *int
	interfaceSpecs  map[string]bytecode.InterfaceSpec
	functionSigs    map[string]callableSignature
}

type state struct {
	function   *bytecode.Function
	chunk      *bytecode.Chunk
	locals     []local
	upvalues   []upvalueRef
	depth      int
	parent     *state
	name       string
	ownerClass *value.Class
}

type local struct {
	name        string
	depth       int
	slot        byte
	typ         string
	inlineConst bool
	constValue  value.Value
}

type upvalueRef struct {
	index       byte
	isLocal     bool
	typ         string
	inlineConst bool
	constValue  value.Value
}

type callableSignature struct {
	params []string
	ret    string
}

func Compile(program *ast.Program) (*bytecode.Function, error) {
	return compileProgram(program, false, nil)
}

func CompileWithRegistry(program *ast.Program, registry *bvmruntime.Registry) (*bytecode.Function, error) {
	return compileProgram(program, false, registry)
}

func CompileModule(program *ast.Program) (*bytecode.Function, error) {
	return compileProgram(program, true, nil)
}

func CompileModuleWithRegistry(program *ast.Program, registry *bvmruntime.Registry) (*bytecode.Function, error) {
	return compileProgram(program, true, registry)
}

func compileProgram(program *ast.Program, forceGlobals bool, registry *bvmruntime.Registry) (*bytecode.Function, error) {
	chunk := bytecode.NewChunk()
	globalSlots := map[string]byte{}
	globalSlotNames := []string(nil)
	if !forceGlobals {
		globalSlots, globalSlotNames = collectGlobalSlots(program)
	}
	root := &state{
		function: &bytecode.Function{Name: "<script>", Chunk: chunk},
		chunk:    chunk,
		locals:   make([]local, 0),
		upvalues: make([]upvalueRef, 0),
		depth:    0,
		name:     "<script>",
	}
	lambdaCounter := 0
	aliases := collectTypeAliases(program)
	compiler := &Compiler{state: root, classes: collectRegistryClasses(registry), consts: make(map[string]value.Value), globalTypes: make(map[string]string), typeAliases: aliases, forceGlobals: forceGlobals, capturedGlobals: collectCapturedGlobals(program), globalSlots: globalSlots, lambdaCounter: &lambdaCounter, interfaceSpecs: collectInterfaceSpecs(program, registry), functionSigs: collectFunctionSignatures(program)}
	for _, stmt := range program.Statements {
		if err := compiler.compileStmt(stmt); err != nil {
			return nil, err
		}
	}
	root.function.GlobalSlotCount = len(compiler.globalSlots)
	root.function.GlobalSlotNames = globalSlotNames
	compiler.emit(bytecode.OpNil, 0)
	compiler.emit(bytecode.OpReturn, 0)
	return root.function, nil
}

func collectRegistryClasses(registry *bvmruntime.Registry) map[string]*value.Class {
	classes := make(map[string]*value.Class)
	if registry == nil {
		return classes
	}
	for name, candidate := range registry.Globals() {
		classValue, ok := candidate.AsClass()
		if !ok {
			continue
		}
		classes[name] = classValue
		classes[classValue.Name] = classValue
	}
	return classes
}

func (c *Compiler) compileStmt(stmt ast.Stmt) error {
	switch node := stmt.(type) {
	case *ast.LetStmt:
		declaredType := c.inferDeclaredType(node.Type, node.Value)
		var inlineConst value.Value
		if node.Kind == ast.VariableFinal {
			constantValue, ok := c.evalConstExpr(node.Value)
			if !ok {
				return fmt.Errorf("line %d:%d: final variable %s requires a compile-time constant initializer", node.Name.Line, node.Name.Column, node.Name.Lexeme)
			}
			inlineConst = constantValue
		}
		if err := c.compileExpr(node.Value); err != nil {
			return err
		}
		if err := c.maybeWrapInterfaceType(node.Type, node.Value, node.Name.Line); err != nil {
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
		if node.Kind == ast.VariableFinal {
			c.consts[node.Name.Lexeme] = inlineConst
		}
		if c.shouldUseScriptLocal(node.Name.Lexeme) {
			slot := c.declareLocal(node.Name, declaredType)
			if node.Kind == ast.VariableFinal {
				c.setLocalInlineConst(slot, inlineConst)
			}
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
		if node.Kind == ast.VariableFinal {
			c.setLocalInlineConst(slot, inlineConst)
		}
		c.emit(bytecode.OpSetLocal, node.Name.Line)
		c.emitByte(slot, node.Name.Line)
		return nil
	case *ast.DestructureLetStmt:
		if node.Kind == ast.VariableFinal {
			return fmt.Errorf("line %d:%d: final destructuring requires a compile-time tuple or array initializer", node.Targets[0].Line, node.Targets[0].Column)
		}
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
		targetType := ""
		if slot, ok := c.resolveLocal(node.Name.Lexeme); ok {
			targetType = c.localType(slot)
			if node.Operator.Type == token.Equal && c.emitFastLocalMulThisFieldAssign(slot, node, targetType) {
				return nil
			}
			if node.Operator.Type != token.Equal {
				c.emit(bytecode.OpGetLocal, node.Name.Line)
				c.emitByte(slot, node.Name.Line)
				if err := c.compileExpr(node.Value); err != nil {
					return err
				}
				c.emitCompoundOp(node.Operator, targetType, assignedType)
				c.emit(bytecode.OpSetLocal, node.Name.Line)
				c.emitByte(slot, node.Name.Line)
				return nil
			}
		} else if current, ok := c.globalTypes[node.Name.Lexeme]; ok {
			targetType = current
		}
		if err := c.compileExpr(node.Value); err != nil {
			return err
		}
		if err := c.maybeWrapInterfaceName(targetType, node.Value, node.Name.Line); err != nil {
			return err
		}
		if slot, ok := c.resolveLocal(node.Name.Lexeme); ok {
			if targetType != "" {
				c.setLocalType(slot, targetType)
			} else {
				c.setLocalType(slot, assignedType)
			}
			c.emit(bytecode.OpSetLocal, node.Name.Line)
			c.emitByte(slot, node.Name.Line)
			return nil
		}
		if slot, ok := c.resolveUpvalue(node.Name.Lexeme); ok {
			if node.Operator.Type != token.Equal {
				c.emit(bytecode.OpGetCapture, node.Name.Line)
				c.emitByte(slot, node.Name.Line)
				if err := c.compileExpr(node.Value); err != nil {
					return err
				}
				c.emitCompoundOp(node.Operator, targetType, assignedType)
				c.emit(bytecode.OpSetCapture, node.Name.Line)
				c.emitByte(slot, node.Name.Line)
				return nil
			}
			c.emit(bytecode.OpSetCapture, node.Name.Line)
			c.emitByte(slot, node.Name.Line)
			return nil
		}
		if slot, ok := c.resolveGlobalSlot(node.Name.Lexeme); ok {
			if node.Operator.Type != token.Equal {
				c.emit(bytecode.OpGetGlobalSlot, node.Name.Line)
				c.emitByte(slot, node.Name.Line)
				if err := c.compileExpr(node.Value); err != nil {
					return err
				}
				c.emitCompoundOp(node.Operator, targetType, assignedType)
				c.emit(bytecode.OpSetGlobalSlot, node.Name.Line)
				c.emitByte(slot, node.Name.Line)
				return nil
			}
			if targetType != "" {
				c.globalTypes[node.Name.Lexeme] = targetType
			} else if assignedType != "" {
				c.globalTypes[node.Name.Lexeme] = assignedType
			}
			c.emit(bytecode.OpSetGlobalSlot, node.Name.Line)
			c.emitByte(slot, node.Name.Line)
			return nil
		}
		if assignedType != "" {
			c.globalTypes[node.Name.Lexeme] = assignedType
		}
		if node.Operator.Type != token.Equal {
			name := c.constant(node.Name.Lexeme)
			c.emit(bytecode.OpGetGlobal, node.Name.Line)
			c.emitUint16(name, node.Name.Line)
			if err := c.compileExpr(node.Value); err != nil {
				return err
			}
			c.emitCompoundOp(node.Operator, targetType, assignedType)
			c.emit(bytecode.OpSetGlobal, node.Name.Line)
			c.emitUint16(name, node.Name.Line)
			return nil
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
			if node.Operator.Type != token.Equal {
				if _, isThis := node.Object.(*ast.ThisExpr); isThis {
					c.emit(bytecode.OpGetThisField, node.Name.Line)
					c.emitByte(byte(slot), node.Name.Line)
					if err := c.compileExpr(node.Value); err != nil {
						return err
					}
					c.emitCompoundOp(node.Operator, c.inferExprType(&ast.GetExpr{Object: node.Object, Name: node.Name}), c.inferExprType(node.Value))
					c.emit(bytecode.OpSetThisField, node.Name.Line)
					c.emitByte(byte(slot), node.Name.Line)
					return nil
				}
				if err := c.compileExpr(node.Object); err != nil {
					return err
				}
				c.emit(bytecode.OpDup, node.Name.Line)
				c.emit(bytecode.OpGetField, node.Name.Line)
				c.emitByte(byte(slot), node.Name.Line)
				if err := c.compileExpr(node.Value); err != nil {
					return err
				}
				c.emitCompoundOp(node.Operator, c.inferExprType(&ast.GetExpr{Object: node.Object, Name: node.Name}), c.inferExprType(node.Value))
				c.emit(bytecode.OpSetField, node.Name.Line)
				c.emitByte(byte(slot), node.Name.Line)
				return nil
			}
			if _, isThis := node.Object.(*ast.ThisExpr); isThis {
				if err := c.compileExpr(node.Value); err != nil {
					return err
				}
				c.emit(bytecode.OpSetThisField, node.Name.Line)
				c.emitByte(byte(slot), node.Name.Line)
				return nil
			}
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
		if node.Operator.Type != token.Equal {
			if err := c.compileExpr(node.Object); err != nil {
				return err
			}
			c.emit(bytecode.OpDup, node.Name.Line)
			name := c.constant(node.Name.Lexeme)
			c.emit(bytecode.OpGetProperty, node.Name.Line)
			c.emitUint16(name, node.Name.Line)
			if err := c.compileExpr(node.Value); err != nil {
				return err
			}
			c.emitCompoundOp(node.Operator, c.inferExprType(&ast.GetExpr{Object: node.Object, Name: node.Name}), c.inferExprType(node.Value))
			c.emit(bytecode.OpSetProperty, node.Name.Line)
			c.emitUint16(name, node.Name.Line)
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
	case *ast.SetIndexStmt:
		if node.Operator.Type != token.Equal {
			if err := c.compileExpr(node.Object); err != nil {
				return err
			}
			if err := c.compileExpr(node.Index); err != nil {
				return err
			}
			c.emit(bytecode.OpDupTwo, 0)
			c.emitIndexedGet(node.Object, node.Index, 0)
			if err := c.compileExpr(node.Value); err != nil {
				return err
			}
			c.emitCompoundOp(node.Operator, bvmruntime.TypeAny, c.inferExprType(node.Value))
			c.emitIndexedSet(node.Object, node.Index, 0)
			return nil
		}
		if err := c.compileExpr(node.Object); err != nil {
			return err
		}
		if err := c.compileExpr(node.Index); err != nil {
			return err
		}
		if err := c.compileExpr(node.Value); err != nil {
			return err
		}
		c.emitIndexedSet(node.Object, node.Index, 0)
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
	case *ast.SwitchStmt:
		return c.compileSwitch(node)
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
		c.consts[node.Name.Lexeme] = value.ObjectValue(fn)
		c.globalTypes[node.Name.Lexeme] = "Function"
		c.emitClosure(fn, node.Name.Line)
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
	case *ast.InterfaceStmt:
		return nil
	case *ast.ImportStmt:
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
		if len(node.Targets) > 1 {
			c.emitByte(1, anchor.Line)
		} else {
			c.emitByte(0, anchor.Line)
		}
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
		c.consts[node.Name.Lexeme] = value.ObjectValue(classValue)
		c.globalTypes[node.Name.Lexeme] = node.Name.Lexeme
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
	case *ast.TypeAliasStmt:
		if c.typeAliases != nil {
			c.typeAliases[node.Name.Lexeme] = node.Target
		}
		return nil
	default:
		return fmt.Errorf("unsupported statement %T", stmt)
	}
}

func (c *Compiler) compileBlock(block *ast.BlockStmt) error {
	c.beginScope()
	defer c.endScope(0)
	return c.compileBlockStatements(block)
}

func (c *Compiler) compileBlockStatements(block *ast.BlockStmt) error {
	for _, stmt := range block.Statements {
		if err := c.compileStmt(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (c *Compiler) compileSwitch(node *ast.SwitchStmt) error {
	c.beginScope()
	defer c.endScope(0)
	if err := c.compileExpr(node.Value); err != nil {
		return err
	}
	switchToken := token.Token{Lexeme: fmt.Sprintf("__switch_%d", len(c.state.locals)), Line: 0}
	switchSlot := c.declareLocal(switchToken, c.inferExprType(node.Value))
	c.emit(bytecode.OpSetLocal, 0)
	c.emitByte(switchSlot, 0)
	endJumps := make([]int, 0, len(node.Arms))
	for _, arm := range node.Arms {
		typePatternCount := 0
		for _, pattern := range arm.Patterns {
			if pattern.Type != nil {
				typePatternCount++
			}
		}
		if typePatternCount > 0 && len(arm.Patterns) > 1 {
			return fmt.Errorf("switch arm cannot combine type pattern binding with grouped cases")
		}
		matchedJumps := make([]int, 0, len(arm.Patterns))
		for _, pattern := range arm.Patterns {
			if err := c.emitSwitchPatternCheck(switchSlot, pattern); err != nil {
				return err
			}
			jumpIfFalse := c.emitJump(bytecode.OpJumpIfFalse, 0)
			c.emit(bytecode.OpPop, 0)
			matchedJumps = append(matchedJumps, c.emitJump(bytecode.OpJump, 0))
			c.patchJump(jumpIfFalse)
			c.emit(bytecode.OpPop, 0)
		}
		skipBody := c.emitJump(bytecode.OpJump, 0)
		for _, jump := range matchedJumps {
			c.patchJump(jump)
		}
		c.beginScope()
		if len(arm.Patterns) == 1 && arm.Patterns[0].Type != nil {
			binding := arm.Patterns[0].Type
			bindingSlot := c.declareLocal(binding.Binding, c.typeNameFromRef(binding.Type))
			c.emit(bytecode.OpGetLocal, binding.Binding.Line)
			c.emitByte(switchSlot, binding.Binding.Line)
			c.emit(bytecode.OpSetLocal, binding.Binding.Line)
			c.emitByte(bindingSlot, binding.Binding.Line)
		}
		if err := c.compileBlockStatements(arm.Body); err != nil {
			return err
		}
		c.endScope(0)
		endJumps = append(endJumps, c.emitJump(bytecode.OpJump, 0))
		c.patchJump(skipBody)
	}
	if node.Default != nil {
		if err := c.compileBlock(node.Default); err != nil {
			return err
		}
	}
	for _, jump := range endJumps {
		c.patchJump(jump)
	}
	return nil
}

func (c *Compiler) emitSwitchPatternCheck(switchSlot byte, pattern ast.SwitchPattern) error {
	c.emit(bytecode.OpGetLocal, 0)
	c.emitByte(switchSlot, 0)
	if pattern.Type != nil {
		name := c.constant(c.typeNameFromRef(pattern.Type.Type))
		c.emit(bytecode.OpMatchType, pattern.Type.Binding.Line)
		c.emitUint16(name, pattern.Type.Binding.Line)
		return nil
	}
	if err := c.compileExpr(pattern.Value); err != nil {
		return err
	}
	c.emit(bytecode.OpEqual, 0)
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
		case int64, float64, rune, string:
			constant := c.constant(valueExpr)
			c.emit(bytecode.OpConstant, 0)
			c.emitUint16(constant, 0)
		default:
			return fmt.Errorf("unsupported literal %T", node.Value)
		}
		return nil
	case *ast.CastExpr:
		if err := c.compileExpr(node.Expr); err != nil {
			return err
		}
		targetType := c.typeNameFromRef(node.Target)
		switch targetType {
		case bvmruntime.TypeInt:
			c.emit(bytecode.OpCastInt, node.Target.Name.Line)
		case bvmruntime.TypeFloat:
			c.emit(bytecode.OpCastFloat, node.Target.Name.Line)
		case bvmruntime.TypeNumber:
			// Int and Float already share the runtime numeric representation.
		default:
			name := c.constant(targetType)
			c.emit(bytecode.OpCastRef, node.Target.Name.Line)
			c.emitUint16(name, node.Target.Name.Line)
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
	case *ast.ArrayExpr:
		for _, element := range node.Elements {
			if err := c.compileExpr(element); err != nil {
				return err
			}
		}
		c.emit(bytecode.OpArray, 0)
		c.emitByte(byte(len(node.Elements)), 0)
		return nil

	case *ast.ArrayNewExpr:
		// compile size expression if present (result unused)
		if node.Size != nil {
			if err := c.compileExpr(node.Size); err != nil {
				return err
			}
			// drop size from stack
			c.emit(bytecode.OpPop, 0)
		}
		// compile initializer elements
		for _, element := range node.Initializer {
			if err := c.compileExpr(element); err != nil {
				return err
			}
		}
		c.emit(bytecode.OpArray, 0)
		c.emitByte(byte(len(node.Initializer)), 0)
		return nil
	case *ast.MapExpr:
		for _, entry := range node.Entries {
			key := c.constant(entry.Key)
			c.emit(bytecode.OpConstant, 0)
			c.emitUint16(key, 0)
			if err := c.compileExpr(entry.Value); err != nil {
				return err
			}
		}
		c.emit(bytecode.OpMap, 0)
		c.emitByte(byte(len(node.Entries)), 0)
		return nil
	case *ast.VariableExpr:
		if constantValue, ok := c.resolveInlineConst(node.Name.Lexeme); ok {
			c.emitInlineConst(constantValue, node.Name.Line)
			return nil
		}
		if slot, ok := c.resolveLocal(node.Name.Lexeme); ok {
			c.emit(bytecode.OpGetLocal, node.Name.Line)
			c.emitByte(slot, node.Name.Line)
			return nil
		}
		if slot, ok := c.resolveUpvalue(node.Name.Lexeme); ok {
			c.emit(bytecode.OpGetCapture, node.Name.Line)
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
		if slot, ok := c.resolveUpvalue(node.Keyword.Lexeme); ok {
			c.emit(bytecode.OpGetCapture, node.Keyword.Line)
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
			if _, isThis := node.Object.(*ast.ThisExpr); isThis {
				c.emit(bytecode.OpGetThisField, node.Name.Line)
				c.emitByte(byte(slot), node.Name.Line)
				return nil
			}
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
	case *ast.IndexExpr:
		if err := c.compileExpr(node.Object); err != nil {
			return err
		}
		if err := c.compileExpr(node.Index); err != nil {
			return err
		}
		c.emitIndexedGet(node.Object, node.Index, node.Bracket.Line)
		return nil
	case *ast.SliceExpr:
		if err := c.compileExpr(node.Object); err != nil {
			return err
		}
		if err := c.compileExpr(node.Start); err != nil {
			return err
		}
		if err := c.compileExpr(node.End); err != nil {
			return err
		}
		c.emit(bytecode.OpSlice, node.Bracket.Line)
		return nil
	case *ast.BinaryExpr:
		return c.compileBinary(node)
	case *ast.CallExpr:
		expectedParams := c.expectedParamTypes(node.Callee)
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
				for i, arg := range node.Arguments {
					if err := c.compileExpr(arg); err != nil {
						return err
					}
					if i < len(expectedParams) {
						if err := c.maybeWrapInterfaceName(expectedParams[i], arg, node.Paren.Line); err != nil {
							return err
						}
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
			for i, arg := range node.Arguments {
				if err := c.compileExpr(arg); err != nil {
					return err
				}
				if i < len(expectedParams) {
					if err := c.maybeWrapInterfaceName(expectedParams[i], arg, node.Paren.Line); err != nil {
						return err
					}
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
		for i, arg := range node.Arguments {
			if err := c.compileExpr(arg); err != nil {
				return err
			}
			if i < len(expectedParams) {
				if err := c.maybeWrapInterfaceName(expectedParams[i], arg, node.Paren.Line); err != nil {
					return err
				}
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
	case *ast.LambdaExpr:
		params := append([]ast.Parameter(nil), node.Params...)
		body := c.lambdaBlock(node)
		fn, err := c.compileCallable(c.nextLambdaName(), params, nil, body, false, nil)
		if err != nil {
			return err
		}
		c.emitClosure(fn, 0)
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
		leftFalse := c.emitJump(bytecode.OpJumpIfFalse, node.Operator.Line)
		c.emit(bytecode.OpPop, node.Operator.Line)
		if err := c.compileExpr(node.Right); err != nil {
			return err
		}
		rightFalse := c.emitJump(bytecode.OpJumpIfFalse, node.Operator.Line)
		c.emit(bytecode.OpPop, node.Operator.Line)
		c.emit(bytecode.OpTrue, node.Operator.Line)
		endJump := c.emitJump(bytecode.OpJump, node.Operator.Line)
		c.patchJump(leftFalse)
		c.patchJump(rightFalse)
		c.emit(bytecode.OpPop, node.Operator.Line)
		c.emit(bytecode.OpFalse, node.Operator.Line)
		c.patchJump(endJump)
		return nil
	}
	if node.Operator.Type == token.OrOr {
		if err := c.compileExpr(node.Left); err != nil {
			return err
		}
		leftFalse := c.emitJump(bytecode.OpJumpIfFalse, node.Operator.Line)
		c.emit(bytecode.OpPop, node.Operator.Line)
		c.emit(bytecode.OpTrue, node.Operator.Line)
		endJump := c.emitJump(bytecode.OpJump, node.Operator.Line)
		c.patchJump(leftFalse)
		c.emit(bytecode.OpPop, node.Operator.Line)
		if err := c.compileExpr(node.Right); err != nil {
			return err
		}
		rightFalse := c.emitJump(bytecode.OpJumpIfFalse, node.Operator.Line)
		c.emit(bytecode.OpPop, node.Operator.Line)
		c.emit(bytecode.OpTrue, node.Operator.Line)
		endJumpRight := c.emitJump(bytecode.OpJump, node.Operator.Line)
		c.patchJump(rightFalse)
		c.emit(bytecode.OpPop, node.Operator.Line)
		c.emit(bytecode.OpFalse, node.Operator.Line)
		c.patchJump(endJump)
		c.patchJump(endJumpRight)
		return nil
	}
	if err := c.compileExpr(node.Left); err != nil {
		return err
	}
	if err := c.compileExpr(node.Right); err != nil {
		return err
	}
	switch node.Operator.Type {
	case token.Plus, token.Minus, token.Star, token.Slash:
		c.emitCompoundOp(node.Operator, c.inferExprType(node.Left), c.inferExprType(node.Right))
	case token.Percent:
		if isNumericLikeType(c.inferExprType(node.Left)) && isNumericLikeType(c.inferExprType(node.Right)) {
			c.emit(bytecode.OpMod, node.Operator.Line)
		} else {
			c.emit(bytecode.OpMod, node.Operator.Line)
		}
	case token.EqualEqual:
		c.emit(bytecode.OpEqual, node.Operator.Line)
	case token.BangEqual:
		c.emit(bytecode.OpEqual, node.Operator.Line)
		c.emit(bytecode.OpNot, node.Operator.Line)
	case token.Greater:
		if isNumericLikeType(c.inferExprType(node.Left)) && isNumericLikeType(c.inferExprType(node.Right)) {
			c.emit(bytecode.OpGreaterNum, node.Operator.Line)
		} else {
			c.emit(bytecode.OpGreater, node.Operator.Line)
		}
	case token.Less:
		if isNumericLikeType(c.inferExprType(node.Left)) && isNumericLikeType(c.inferExprType(node.Right)) {
			c.emit(bytecode.OpLessNum, node.Operator.Line)
		} else {
			c.emit(bytecode.OpLess, node.Operator.Line)
		}
	case token.GreaterEqual:
		if isNumericLikeType(c.inferExprType(node.Left)) && isNumericLikeType(c.inferExprType(node.Right)) {
			c.emit(bytecode.OpLessNum, node.Operator.Line)
		} else {
			c.emit(bytecode.OpLess, node.Operator.Line)
		}
		c.emit(bytecode.OpNot, node.Operator.Line)
	case token.LessEqual:
		if isNumericLikeType(c.inferExprType(node.Left)) && isNumericLikeType(c.inferExprType(node.Right)) {
			c.emit(bytecode.OpGreaterNum, node.Operator.Line)
		} else {
			c.emit(bytecode.OpGreater, node.Operator.Line)
		}
		c.emit(bytecode.OpNot, node.Operator.Line)
	case token.In:
		c.emitContains(node.Right, node.Operator.Line)
	default:
		return fmt.Errorf("unsupported binary operator %s", node.Operator.Type)
	}
	return nil
}

func (c *Compiler) emitContains(container ast.Expr, line int) {
	switch c.inferExprType(container) {
	case bvmruntime.TypeArray:
		c.emit(bytecode.OpContainsArray, line)
	case bvmruntime.TypeMap:
		c.emit(bytecode.OpContainsMap, line)
	case bvmruntime.TypeString:
		c.emit(bytecode.OpContainsString, line)
	default:
		c.emit(bytecode.OpContains, line)
	}
}

func (c *Compiler) emitCompoundOp(operator token.Token, leftType string, rightType string) {
	switch operator.Type {
	case token.Plus, token.PlusEqual:
		if isNumericLikeType(leftType) && isNumericLikeType(rightType) {
			c.emit(bytecode.OpAddNum, operator.Line)
		} else {
			c.emit(bytecode.OpAdd, operator.Line)
		}
	case token.Minus, token.MinusEqual:
		if isNumericLikeType(leftType) && isNumericLikeType(rightType) {
			c.emit(bytecode.OpSubNum, operator.Line)
		} else {
			c.emit(bytecode.OpSub, operator.Line)
		}
	case token.Star, token.StarEqual:
		if isNumericLikeType(leftType) && isNumericLikeType(rightType) {
			c.emit(bytecode.OpMulNum, operator.Line)
		} else {
			c.emit(bytecode.OpMul, operator.Line)
		}
	case token.Slash, token.SlashEqual:
		if isNumericLikeType(leftType) && isNumericLikeType(rightType) {
			c.emit(bytecode.OpDivNum, operator.Line)
		} else {
			c.emit(bytecode.OpDiv, operator.Line)
		}
	}
}

func (c *Compiler) emitIndexedGet(object ast.Expr, index ast.Expr, line int) {
	switch c.inferExprType(object) {
	case bvmruntime.TypeArray:
		c.emit(bytecode.OpGetIndexArray, line)
	case bvmruntime.TypeMap:
		c.emit(bytecode.OpGetIndexMap, line)
	default:
		c.emit(bytecode.OpGetIndex, line)
	}
}

func (c *Compiler) emitIndexedSet(object ast.Expr, index ast.Expr, line int) {
	switch c.inferExprType(object) {
	case bvmruntime.TypeArray:
		c.emit(bytecode.OpSetIndexArray, line)
	case bvmruntime.TypeMap:
		c.emit(bytecode.OpSetIndexMap, line)
	default:
		c.emit(bytecode.OpSetIndex, line)
	}
}

func (c *Compiler) emitFastLocalMulThisFieldAssign(targetSlot byte, node *ast.AssignStmt, targetType string) bool {
	_ = targetType
	addExpr, ok := node.Value.(*ast.BinaryExpr)
	if !ok || addExpr.Operator.Type != token.Plus {
		return false
	}
	leftVar, ok := addExpr.Left.(*ast.VariableExpr)
	if !ok || leftVar.Name.Lexeme != node.Name.Lexeme {
		return false
	}
	localSlot, fieldSlot, ok := c.matchLocalMulThisField(addExpr.Right)
	if !ok {
		return false
	}
	c.emit(bytecode.OpAddLocalMulThisField, node.Name.Line)
	c.emitByte(targetSlot, node.Name.Line)
	c.emitByte(localSlot, node.Name.Line)
	c.emitByte(fieldSlot, node.Name.Line)
	return true
}

func (c *Compiler) matchLocalMulThisField(expr ast.Expr) (byte, byte, bool) {
	expr = unwrapGrouping(expr)
	mulExpr, ok := expr.(*ast.BinaryExpr)
	if !ok || mulExpr.Operator.Type != token.Star {
		return 0, 0, false
	}
	if localSlot, fieldSlot, ok := c.matchLocalAndThisField(mulExpr.Left, mulExpr.Right); ok {
		return localSlot, fieldSlot, true
	}
	return c.matchLocalAndThisField(mulExpr.Right, mulExpr.Left)
}

func (c *Compiler) matchLocalAndThisField(localExpr ast.Expr, fieldExpr ast.Expr) (byte, byte, bool) {
	localExpr = unwrapGrouping(localExpr)
	fieldExpr = unwrapGrouping(fieldExpr)
	variable, ok := localExpr.(*ast.VariableExpr)
	if !ok {
		return 0, 0, false
	}
	localSlot, ok := c.resolveLocal(variable.Name.Lexeme)
	if !ok {
		return 0, 0, false
	}
	getter, ok := fieldExpr.(*ast.GetExpr)
	if !ok {
		return 0, 0, false
	}
	if _, isThis := getter.Object.(*ast.ThisExpr); !isThis {
		return 0, 0, false
	}
	fieldSlot, ok := c.resolveFieldSlot(getter.Object, getter.Name.Lexeme)
	if !ok {
		return 0, 0, false
	}
	return localSlot, byte(fieldSlot), true
}

func unwrapGrouping(expr ast.Expr) ast.Expr {
	for {
		grouping, ok := expr.(*ast.GroupingExpr)
		if !ok {
			return expr
		}
		expr = grouping.Expr
	}
}

func (c *Compiler) compileFunction(stmt *ast.FunctionStmt) (*bytecode.Function, error) {
	return c.compileCallable(stmt.Name.Lexeme, stmt.Params, stmt.ReturnType, stmt.Body, false, nil)
}

func (c *Compiler) compileCallable(name string, params []ast.Parameter, returnType *ast.TypeRef, body *ast.BlockStmt, includeThis bool, ownerClass *value.Class) (*bytecode.Function, error) {
	fnChunk := bytecode.NewChunk()
	child := &state{
		function:   &bytecode.Function{Name: name, Arity: len(params), OwnerClassName: "", ReturnType: c.typeNameFromRef(returnType), Chunk: fnChunk, ParamTypes: make([]string, len(params))},
		chunk:      fnChunk,
		locals:     make([]local, 0),
		upvalues:   make([]upvalueRef, 0),
		depth:      1,
		parent:     c.state,
		name:       name,
		ownerClass: ownerClass,
	}
	childCompiler := &Compiler{state: child, classes: c.classes, consts: c.consts, globalTypes: c.globalTypes, typeAliases: c.typeAliases, capturedGlobals: c.capturedGlobals, globalSlots: c.globalSlots, lambdaCounter: c.lambdaCounter, interfaceSpecs: c.interfaceSpecs, functionSigs: c.functionSigs}
	if ownerClass != nil {
		child.function.OwnerClassName = ownerClass.Name
	}
	if includeThis {
		child.declareParam(token.Token{Lexeme: "this"}, ownerClass.Name)
	}
	for i, param := range params {
		typeName := c.typeNameFromRef(param.Type)
		child.function.ParamTypes[i] = typeName
		child.declareParam(param.Name, typeName)
	}
	for _, bodyStmt := range body.Statements {
		if err := childCompiler.compileStmt(bodyStmt); err != nil {
			return nil, err
		}
	}
	childCompiler.emit(bytecode.OpNil, 0)
	childCompiler.emit(bytecode.OpReturn, 0)
	child.function.Upvalues = make([]bytecode.UpvalueSpec, len(child.upvalues))
	for i, upvalue := range child.upvalues {
		child.function.Upvalues[i] = bytecode.UpvalueSpec{Index: upvalue.index, IsLocal: upvalue.isLocal}
	}
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
		Permits:           make(map[string]bool),
		IsAbstract:        stmt.IsAbstract,
		IsEnum:            stmt.IsEnum,
		IsSealed:          stmt.IsSealed || stmt.IsFinal || stmt.IsEnum,
		IsRecord:          stmt.IsRecord,
		EnumOrder:         make([]string, 0, len(stmt.EnumValues)),
		FastConstructor:   nil,
		FastMethods:       make([]*value.FastMethodPlan, 0),
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
		MethodVisibility:  make(map[string]string),
		StaticVisibility:  make(map[string]string),
		MethodAnnotations: make(map[string][]string),
	}
	for _, iface := range stmt.Implements {
		classValue.Implements[iface.Name.Lexeme] = true
	}
	for _, permit := range stmt.Permits {
		classValue.Permits[permit.Name.Lexeme] = true
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
		classValue.FastMethods = append(classValue.FastMethods, superclass.FastMethods...)
		for name, fn := range superclass.Methods {
			classValue.Methods[name] = fn
		}
	}
	c.classes[stmt.Name.Lexeme] = classValue

	if stmt.IsEnum {
		classValue.FieldIndex["name"] = len(classValue.FieldOrder)
		classValue.FieldOrder = append(classValue.FieldOrder, "name")
		classValue.Fields["name"] = value.FieldDef{Default: value.StringValue(""), Mutable: false, TypeName: bvmruntime.TypeString, Visibility: string(ast.VisibilityPublic)}
		classValue.FieldIndex["ordinal"] = len(classValue.FieldOrder)
		classValue.FieldOrder = append(classValue.FieldOrder, "ordinal")
		classValue.Fields["ordinal"] = value.FieldDef{Default: value.NumberValue(0), Mutable: false, TypeName: bvmruntime.TypeNumber, Visibility: string(ast.VisibilityPublic)}
	}

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
		def.Visibility = string(field.Visibility)
		if field.Static {
			classValue.StaticFields[field.Name.Lexeme] = def
			classValue.StaticValues[field.Name.Lexeme] = defaultValue
			classValue.StaticVisibility[field.Name.Lexeme] = string(field.Visibility)
			continue
		}
		if _, exists := classValue.FieldIndex[field.Name.Lexeme]; !exists {
			classValue.FieldIndex[field.Name.Lexeme] = len(classValue.FieldOrder)
			classValue.FieldOrder = append(classValue.FieldOrder, field.Name.Lexeme)
		}
		classValue.Fields[field.Name.Lexeme] = def
	}

	var enumConstructor *ast.MethodDecl

	for _, method := range stmt.Methods {
		if method.IsAbstract {
			continue
		}
		if method.IsConstructor {
			if stmt.IsEnum {
				methodCopy := method
				enumConstructor = &methodCopy
				continue
			}
			classValue.FastConstructor = c.detectFastConstructor(classValue, method)
		}
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
				classValue.IndexGetMethod = fn
			case "__set":
				classValue.IndexSetMethod = fn
			case "__contains":
				classValue.ContainsMethod = fn
			case "__pieces":
				classValue.PiecesMethod = fn
			case "__get_piece", "__getPiece":
				classValue.GetPieceMethod = fn
			case "__slice":
				classValue.SliceMethod = fn
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
			classValue.ConstructorVisibility = string(method.Visibility)
			classValue.Constructor = fn
			continue
		}
		if method.Static {
			classValue.StaticMethods[method.Name.Lexeme] = fn
			classValue.StaticVisibility[method.Name.Lexeme] = string(method.Visibility)
			continue
		}
		classValue.MethodVisibility[method.Name.Lexeme] = string(method.Visibility)
		fastMethod := c.detectFastMethod(classValue, method)
		if slot, exists := classValue.MethodIndex[method.Name.Lexeme]; exists {
			classValue.MethodTable[slot] = fn
			if slot < len(classValue.FastMethods) {
				classValue.FastMethods[slot] = fastMethod
			}
		} else {
			classValue.MethodIndex[method.Name.Lexeme] = len(classValue.MethodOrder)
			classValue.MethodOrder = append(classValue.MethodOrder, method.Name.Lexeme)
			classValue.MethodTable = append(classValue.MethodTable, fn)
			classValue.FastMethods = append(classValue.FastMethods, fastMethod)
		}
		classValue.Methods[method.Name.Lexeme] = fn
	}
	if stmt.IsEnum {
		if err := c.initializeEnumClass(classValue, stmt, enumConstructor); err != nil {
			return nil, err
		}
	}
	return classValue, nil
}

func (c *Compiler) detectFastMethod(classValue *value.Class, method ast.MethodDecl) *value.FastMethodPlan {
	if method.Static || method.IsConstructor || len(method.Params) != 0 {
		return nil
	}
	if len(method.Body.Statements) != 1 {
		return nil
	}
	ret, ok := method.Body.Statements[0].(*ast.ReturnStmt)
	if !ok || ret.Value == nil {
		return nil
	}
	expr := c.detectFastMethodExpr(classValue, ret.Value)
	if expr == nil {
		return nil
	}
	return &value.FastMethodPlan{Arity: 0, Expr: expr}
}

func (c *Compiler) detectFastMethodExpr(classValue *value.Class, expr ast.Expr) *value.FastMethodExpr {
	expr = unwrapGrouping(expr)
	switch node := expr.(type) {
	case *ast.LiteralExpr:
		if number, ok := node.Value.(float64); ok {
			return &value.FastMethodExpr{Kind: value.FastMethodExprNumber, Number: number}
		}
		if number, ok := node.Value.(int64); ok {
			return &value.FastMethodExpr{Kind: value.FastMethodExprNumber, Number: float64(number)}
		}
		return nil
	case *ast.GetExpr:
		if _, isThis := node.Object.(*ast.ThisExpr); !isThis {
			return nil
		}
		fieldSlot, _, ok := classValue.LookupFieldSlot(node.Name.Lexeme)
		if !ok {
			return nil
		}
		return &value.FastMethodExpr{Kind: value.FastMethodExprField, FieldSlot: fieldSlot}
	case *ast.UnaryExpr:
		if node.Operator.Type != token.Minus {
			return nil
		}
		right := c.detectFastMethodExpr(classValue, node.Right)
		if right == nil {
			return nil
		}
		return &value.FastMethodExpr{Kind: value.FastMethodExprNegate, Left: right}
	case *ast.BinaryExpr:
		left := c.detectFastMethodExpr(classValue, node.Left)
		right := c.detectFastMethodExpr(classValue, node.Right)
		if left == nil || right == nil {
			return nil
		}
		kind := value.FastMethodExprAdd
		switch node.Operator.Type {
		case token.Plus:
			kind = value.FastMethodExprAdd
		case token.Minus:
			kind = value.FastMethodExprSub
		case token.Star:
			kind = value.FastMethodExprMul
		case token.Slash:
			kind = value.FastMethodExprDiv
		default:
			return nil
		}
		return &value.FastMethodExpr{Kind: kind, Left: left, Right: right}
	default:
		return nil
	}
}

func (c *Compiler) detectFastConstructor(classValue *value.Class, method ast.MethodDecl) *value.FastConstructorPlan {
	if classValue.Superclass != nil {
		return nil
	}
	if method.Static || !method.IsConstructor {
		return nil
	}
	if len(method.Body.Statements) == 0 {
		return &value.FastConstructorPlan{Arity: len(method.Params)}
	}
	paramIndex := make(map[string]int, len(method.Params))
	for i, param := range method.Params {
		paramIndex[param.Name.Lexeme] = i
	}
	fieldSlots := make([]int, 0, len(method.Body.Statements))
	argIndexes := make([]int, 0, len(method.Body.Statements))
	seenField := make(map[int]bool, len(method.Body.Statements))
	for _, stmt := range method.Body.Statements {
		setStmt, ok := stmt.(*ast.SetStmt)
		if !ok {
			return nil
		}
		thisExpr, ok := setStmt.Object.(*ast.ThisExpr)
		if !ok || thisExpr.Keyword.Lexeme != "this" {
			return nil
		}
		valueExpr, ok := setStmt.Value.(*ast.VariableExpr)
		if !ok {
			return nil
		}
		argIndex, ok := paramIndex[valueExpr.Name.Lexeme]
		if !ok {
			return nil
		}
		fieldSlot, _, ok := classValue.LookupFieldSlot(setStmt.Name.Lexeme)
		if !ok || seenField[fieldSlot] {
			return nil
		}
		seenField[fieldSlot] = true
		fieldSlots = append(fieldSlots, fieldSlot)
		argIndexes = append(argIndexes, argIndex)
	}
	return &value.FastConstructorPlan{Arity: len(method.Params), FieldSlots: fieldSlots, ArgIndexes: argIndexes}
}

func (c *Compiler) initializeEnumClass(classValue *value.Class, stmt *ast.ClassStmt, enumConstructor *ast.MethodDecl) error {
	enumInstances := make([]value.Value, 0, len(stmt.EnumValues))
	enumNames := make([]value.Value, 0, len(stmt.EnumValues))
	for ordinal, enumValue := range stmt.EnumValues {
		args := make([]value.Value, len(enumValue.Arguments))
		for i, argument := range enumValue.Arguments {
			constantValue, ok := c.evalConstExpr(argument)
			if !ok {
				return fmt.Errorf("enum value %s.%s requires compile-time constant arguments", stmt.Name.Lexeme, enumValue.Name.Lexeme)
			}
			args[i] = constantValue
		}
		instance, err := c.buildEnumInstance(classValue, enumValue.Name.Lexeme, ordinal, args, enumConstructor)
		if err != nil {
			return err
		}
		wrapped := value.ObjectValue(instance)
		classValue.EnumOrder = append(classValue.EnumOrder, enumValue.Name.Lexeme)
		classValue.StaticFields[enumValue.Name.Lexeme] = value.FieldDef{Default: wrapped, Mutable: false, TypeName: classValue.Name, Visibility: string(ast.VisibilityPublic)}
		classValue.StaticValues[enumValue.Name.Lexeme] = wrapped
		classValue.StaticVisibility[enumValue.Name.Lexeme] = string(ast.VisibilityPublic)
		enumInstances = append(enumInstances, wrapped)
		enumNames = append(enumNames, value.StringValue(enumValue.Name.Lexeme))
	}
	if _, exists := classValue.StaticValues["valueOf"]; !exists {
		classValue.StaticValues["valueOf"] = value.ObjectValue(&value.Builtin{Name: classValue.Name + ".valueOf", Arity: 1, Fn: func(args []value.Value) (value.Value, error) {
			if len(args) != 1 || args[0].Kind != value.String {
				return value.NilValue(), fmt.Errorf("%s.valueOf expects a string", classValue.Name)
			}
			member, ok := classValue.StaticValues[args[0].Str]
			if !ok {
				return value.NilValue(), fmt.Errorf("enum value %s.%s not found", classValue.Name, args[0].Str)
			}
			return member, nil
		}})
		classValue.StaticVisibility["valueOf"] = string(ast.VisibilityPublic)
	}
	if _, exists := classValue.StaticValues["values"]; !exists {
		classValue.StaticValues["values"] = value.ObjectValue(&value.Builtin{Name: classValue.Name + ".values", Arity: 0, Fn: func(args []value.Value) (value.Value, error) {
			items := make([]value.Value, len(enumInstances))
			copy(items, enumInstances)
			return value.ObjectValue(&value.Array{Elements: items}), nil
		}})
		classValue.StaticVisibility["values"] = string(ast.VisibilityPublic)
	}
	if _, exists := classValue.StaticValues["names"]; !exists {
		classValue.StaticValues["names"] = value.ObjectValue(&value.Builtin{Name: classValue.Name + ".names", Arity: 0, Fn: func(args []value.Value) (value.Value, error) {
			items := make([]value.Value, len(enumNames))
			copy(items, enumNames)
			return value.ObjectValue(&value.Array{Elements: items}), nil
		}})
		classValue.StaticVisibility["names"] = string(ast.VisibilityPublic)
	}
	if _, exists := classValue.StaticValues["size"]; !exists {
		classValue.StaticValues["size"] = value.ObjectValue(&value.Builtin{Name: classValue.Name + ".size", Arity: 0, Fn: func(args []value.Value) (value.Value, error) {
			return value.NumberValue(float64(len(enumInstances))), nil
		}})
		classValue.StaticVisibility["size"] = string(ast.VisibilityPublic)
	}
	return nil
}

func (c *Compiler) buildEnumInstance(classValue *value.Class, name string, ordinal int, args []value.Value, enumConstructor *ast.MethodDecl) (*value.Instance, error) {
	instance := classValue.NewInstance()
	if nameSlot, _, ok := classValue.LookupFieldSlot("name"); ok {
		instance.Fields[nameSlot] = value.StringValue(name)
	}
	if ordinalSlot, _, ok := classValue.LookupFieldSlot("ordinal"); ok {
		instance.Fields[ordinalSlot] = value.NumberValue(float64(ordinal))
	}
	if enumConstructor == nil {
		if len(args) > 0 {
			return nil, fmt.Errorf("enum value %s.%s has constructor arguments but enum has no constructor", classValue.Name, name)
		}
		instance.Frozen = true
		return instance, nil
	}
	if len(enumConstructor.Params) != len(args) {
		return nil, fmt.Errorf("enum constructor %s expects %d args, got %d", classValue.Name, len(enumConstructor.Params), len(args))
	}
	bindings := make(map[string]value.Value, len(enumConstructor.Params))
	for i, param := range enumConstructor.Params {
		bindings[param.Name.Lexeme] = args[i]
	}
	for _, stmt := range enumConstructor.Body.Statements {
		setStmt, ok := stmt.(*ast.SetStmt)
		if !ok {
			return nil, fmt.Errorf("enum constructor %s only supports field assignments", classValue.Name)
		}
		if _, ok := setStmt.Object.(*ast.ThisExpr); !ok {
			return nil, fmt.Errorf("enum constructor %s only supports assignments to this", classValue.Name)
		}
		fieldValue, ok := c.evalEnumInitExpr(setStmt.Value, bindings)
		if !ok {
			return nil, fmt.Errorf("enum constructor %s requires compile-time evaluable assignments", classValue.Name)
		}
		fieldSlot, _, ok := classValue.LookupFieldSlot(setStmt.Name.Lexeme)
		if !ok {
			return nil, fmt.Errorf("enum constructor %s assigns unknown field %s", classValue.Name, setStmt.Name.Lexeme)
		}
		instance.Fields[fieldSlot] = fieldValue
	}
	instance.Frozen = true
	return instance, nil
}

func (c *Compiler) evalEnumInitExpr(expr ast.Expr, bindings map[string]value.Value) (value.Value, bool) {
	switch node := expr.(type) {
	case *ast.LiteralExpr:
		switch v := node.Value.(type) {
		case nil:
			return value.NilValue(), true
		case bool:
			return value.BoolValue(v), true
		case int64:
			return value.IntValue(v), true
		case float64:
			return value.FloatValue(v), true
		case rune:
			return value.CharValue(v), true
		case string:
			return value.StringValue(v), true
		default:
			return value.NilValue(), false
		}
	case *ast.GroupingExpr:
		return c.evalEnumInitExpr(node.Expr, bindings)
	case *ast.VariableExpr:
		if bound, ok := bindings[node.Name.Lexeme]; ok {
			return bound, true
		}
		if constantValue, ok := c.resolveInlineConst(node.Name.Lexeme); ok {
			return constantValue, true
		}
		return value.NilValue(), false
	case *ast.UnaryExpr:
		right, ok := c.evalEnumInitExpr(node.Right, bindings)
		if !ok {
			return value.NilValue(), false
		}
		switch node.Operator.Type {
		case token.Minus:
			if right.Kind != value.Number {
				return value.NilValue(), false
			}
			if right.NumberKind == value.NumberInt {
				return value.IntValue(-int64(right.Num)), true
			}
			return value.FloatValue(-right.Num), true
		case token.Bang:
			return value.BoolValue(!right.IsTruthy()), true
		default:
			return value.NilValue(), false
		}
	case *ast.BinaryExpr:
		left, ok := c.evalEnumInitExpr(node.Left, bindings)
		if !ok {
			return value.NilValue(), false
		}
		right, ok := c.evalEnumInitExpr(node.Right, bindings)
		if !ok {
			return value.NilValue(), false
		}
		switch node.Operator.Type {
		case token.Plus:
			if left.Kind == value.Number && right.Kind == value.Number {
				return foldNumericBinary(token.Plus, left, right), true
			}
			if left.Kind == value.String || right.Kind == value.String || left.Kind == value.Char || right.Kind == value.Char {
				return value.StringValue(left.String() + right.String()), true
			}
		case token.Minus:
			if left.Kind == value.Number && right.Kind == value.Number {
				return foldNumericBinary(token.Minus, left, right), true
			}
		case token.Star:
			if left.Kind == value.Number && right.Kind == value.Number {
				return foldNumericBinary(token.Star, left, right), true
			}
		case token.Slash:
			if left.Kind == value.Number && right.Kind == value.Number {
				return value.FloatValue(left.Num / right.Num), true
			}
		}
	}
	return value.NilValue(), false
}

func (c *Compiler) nextLambdaName() string {
	if c.lambdaCounter == nil {
		counter := 0
		c.lambdaCounter = &counter
	}
	*c.lambdaCounter = *c.lambdaCounter + 1
	return fmt.Sprintf("<lambda:%d>", *c.lambdaCounter)
}

func (c *Compiler) evalConstExpr(expr ast.Expr) (value.Value, bool) {
	switch node := expr.(type) {
	case *ast.LiteralExpr:
		switch v := node.Value.(type) {
		case nil:
			return value.NilValue(), true
		case bool:
			return value.BoolValue(v), true
		case int64:
			return value.IntValue(v), true
		case float64:
			return value.FloatValue(v), true
		case string:
			return value.StringValue(v), true
		default:
			return value.NilValue(), false
		}
	case *ast.GroupingExpr:
		return c.evalConstExpr(node.Expr)
	case *ast.VariableExpr:
		if constantValue, ok := c.resolveInlineConst(node.Name.Lexeme); ok {
			return constantValue, true
		}
		v, ok := c.consts[node.Name.Lexeme]
		return v, ok
	case *ast.GetExpr:
		variable, ok := node.Object.(*ast.VariableExpr)
		if !ok {
			return value.NilValue(), false
		}
		classValue, ok := c.classes[variable.Name.Lexeme]
		if !ok {
			return value.NilValue(), false
		}
		if member, ok := classValue.StaticValues[node.Name.Lexeme]; ok {
			return member, true
		}
		if method, ok := classValue.StaticMethods[node.Name.Lexeme]; ok {
			return value.ObjectValue(method), true
		}
		return value.NilValue(), false
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
			if right.NumberKind == value.NumberInt {
				return value.IntValue(-int64(right.Num)), true
			}
			return value.FloatValue(-right.Num), true
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
				return foldNumericBinary(token.Plus, left, right), true
			}
			if left.Kind == value.String || right.Kind == value.String || left.Kind == value.Char || right.Kind == value.Char {
				return value.StringValue(left.String() + right.String()), true
			}
		case token.Minus:
			if left.Kind == value.Number && right.Kind == value.Number {
				return foldNumericBinary(token.Minus, left, right), true
			}
		case token.Star:
			if left.Kind == value.Number && right.Kind == value.Number {
				return foldNumericBinary(token.Star, left, right), true
			}
		case token.Slash:
			if left.Kind == value.Number && right.Kind == value.Number {
				return value.FloatValue(left.Num / right.Num), true
			}
		case token.Percent:
			if left.Kind == value.Number && right.Kind == value.Number {
				return foldNumericBinary(token.Percent, left, right), true
			}
		case token.AndAnd:
			return value.BoolValue(left.IsTruthy() && right.IsTruthy()), true
		case token.OrOr:
			return value.BoolValue(left.IsTruthy() || right.IsTruthy()), true
		case token.EqualEqual:
			if compare, ok := compareTextConstants(left, right); ok {
				return value.BoolValue(compare == 0), true
			}
			return value.BoolValue(value.Equal(left, right)), true
		case token.BangEqual:
			if compare, ok := compareTextConstants(left, right); ok {
				return value.BoolValue(compare != 0), true
			}
			return value.BoolValue(!value.Equal(left, right)), true
		case token.Greater:
			if compare, ok := compareTextConstants(left, right); ok {
				return value.BoolValue(compare > 0), true
			}
			if left.Kind == value.Number && right.Kind == value.Number {
				return value.BoolValue(left.Num > right.Num), true
			}
		case token.GreaterEqual:
			if compare, ok := compareTextConstants(left, right); ok {
				return value.BoolValue(compare >= 0), true
			}
			if left.Kind == value.Number && right.Kind == value.Number {
				return value.BoolValue(left.Num >= right.Num), true
			}
		case token.Less:
			if compare, ok := compareTextConstants(left, right); ok {
				return value.BoolValue(compare < 0), true
			}
			if left.Kind == value.Number && right.Kind == value.Number {
				return value.BoolValue(left.Num < right.Num), true
			}
		case token.LessEqual:
			if compare, ok := compareTextConstants(left, right); ok {
				return value.BoolValue(compare <= 0), true
			}
			if left.Kind == value.Number && right.Kind == value.Number {
				return value.BoolValue(left.Num <= right.Num), true
			}
		}
	}
	return value.NilValue(), false
}

func compareTextConstants(left value.Value, right value.Value) (int, bool) {
	leftText, ok := textConstantValue(left)
	if !ok {
		return 0, false
	}
	rightText, ok := textConstantValue(right)
	if !ok {
		return 0, false
	}
	if leftText < rightText {
		return -1, true
	}
	if leftText > rightText {
		return 1, true
	}
	return 0, true
}

func textConstantValue(candidate value.Value) (string, bool) {
	switch candidate.Kind {
	case value.Char, value.String:
		return candidate.Str, true
	default:
		return "", false
	}
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

func (c *Compiler) localType(slot byte) string {
	for i := range c.state.locals {
		if c.state.locals[i].slot == slot {
			return c.state.locals[i].typ
		}
	}
	return ""
}

func (c *Compiler) setLocalInlineConst(slot byte, constantValue value.Value) {
	for i := range c.state.locals {
		if c.state.locals[i].slot == slot {
			c.state.locals[i].inlineConst = true
			c.state.locals[i].constValue = constantValue
			return
		}
	}
}

func (c *Compiler) localInlineConst(slot byte) (value.Value, bool) {
	for i := range c.state.locals {
		if c.state.locals[i].slot == slot {
			return c.state.locals[i].constValue, c.state.locals[i].inlineConst
		}
	}
	return value.NilValue(), false
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

func (s *state) resolveLocal(name string) (byte, string, value.Value, bool, bool) {
	for i := len(s.locals) - 1; i >= 0; i-- {
		if s.locals[i].name == name {
			return s.locals[i].slot, s.locals[i].typ, s.locals[i].constValue, s.locals[i].inlineConst, true
		}
	}
	return 0, "", value.NilValue(), false, false
}

func (c *Compiler) resolveUpvalue(name string) (byte, bool) {
	index, _, _, _, ok := c.resolveUpvalueFrom(c.state, name)
	return index, ok
}

func (c *Compiler) resolveUpvalueFrom(current *state, name string) (byte, string, value.Value, bool, bool) {
	if current.parent == nil {
		return 0, "", value.NilValue(), false, false
	}
	if slot, typ, constValue, inlineConst, ok := current.parent.resolveLocal(name); ok {
		return c.addUpvalue(current, slot, true, typ, constValue, inlineConst), typ, constValue, inlineConst, true
	}
	if slot, typ, constValue, inlineConst, ok := c.resolveUpvalueFrom(current.parent, name); ok {
		return c.addUpvalue(current, slot, false, typ, constValue, inlineConst), typ, constValue, inlineConst, true
	}
	return 0, "", value.NilValue(), false, false
}

func (c *Compiler) addUpvalue(current *state, slot byte, isLocal bool, typ string, constValue value.Value, inlineConst bool) byte {
	for i, upvalue := range current.upvalues {
		if upvalue.index == slot && upvalue.isLocal == isLocal {
			return byte(i)
		}
	}
	current.upvalues = append(current.upvalues, upvalueRef{index: slot, isLocal: isLocal, typ: typ, inlineConst: inlineConst, constValue: constValue})
	return byte(len(current.upvalues) - 1)
}

func (c *Compiler) upvalueInlineConst(slot byte) (value.Value, bool) {
	if int(slot) >= len(c.state.upvalues) {
		return value.NilValue(), false
	}
	upvalue := c.state.upvalues[slot]
	return upvalue.constValue, upvalue.inlineConst
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

func (c *Compiler) resolveInlineConst(name string) (value.Value, bool) {
	if slot, ok := c.resolveLocal(name); ok {
		if constantValue, ok := c.localInlineConst(slot); ok {
			return constantValue, true
		}
	}
	if slot, ok := c.resolveUpvalue(name); ok {
		if constantValue, ok := c.upvalueInlineConst(slot); ok {
			return constantValue, true
		}
	}
	constantValue, ok := c.consts[name]
	return constantValue, ok
}

func (c *Compiler) emitInlineConst(v value.Value, line int) {
	switch v.Kind {
	case value.Nil:
		c.emit(bytecode.OpNil, line)
	case value.Bool:
		if v.Bool {
			c.emit(bytecode.OpTrue, line)
		} else {
			c.emit(bytecode.OpFalse, line)
		}
	case value.Number:
		constant := c.constant(v.Num)
		if v.NumberKind == value.NumberInt {
			constant = c.constant(int64(v.Num))
		}
		c.emit(bytecode.OpConstant, line)
		c.emitUint16(constant, line)
	case value.Char:
		constant := c.constant([]rune(v.Str)[0])
		c.emit(bytecode.OpConstant, line)
		c.emitUint16(constant, line)
	case value.String:
		constant := c.constant(v.Str)
		c.emit(bytecode.OpConstant, line)
		c.emitUint16(constant, line)
	default:
		constant := any(v.Object)
		if fn, ok := v.AsFunction(); ok {
			constant = fn
		}
		idx := c.constant(constant)
		c.emit(bytecode.OpConstant, line)
		c.emitUint16(idx, line)
	}
}

func (c *Compiler) inferDeclaredType(typeRef *ast.TypeRef, valueExpr ast.Expr) string {
	if typeRef != nil {
		return c.typeNameFromRef(typeRef)
	}
	return c.inferExprType(valueExpr)
}

func (c *Compiler) typeNameFromRef(typeRef *ast.TypeRef) string {
	return typeNameFromRefWithAliases(typeRef, c.typeAliases, map[string]bool{})
}

func typeNameFromRefWithAliases(typeRef *ast.TypeRef, aliases map[string]*ast.TypeRef, seen map[string]bool) string {
	if typeRef == nil {
		return ""
	}
	if len(typeRef.Union) > 0 {
		parts := make([]string, len(typeRef.Union))
		for i, option := range typeRef.Union {
			parts[i] = typeNameFromRefWithAliases(option, aliases, seen)
		}
		return joinSerializedTypes(parts, " | ")
	}
	if typeRef.Wildcard {
		return bvmruntime.TypeAny
	}
	if aliases != nil {
		if alias, ok := aliases[typeRef.Name.Lexeme]; ok {
			if seen[typeRef.Name.Lexeme] {
				return typeRef.Name.Lexeme
			}
			nextSeen := cloneSeenTypes(seen)
			nextSeen[typeRef.Name.Lexeme] = true
			return typeNameFromRefWithAliases(alias, aliases, nextSeen)
		}
	}
	base := normalizeTypeName(typeRef.Name.Lexeme)
	if len(typeRef.Args) == 0 {
		return base
	}
	args := make([]string, len(typeRef.Args))
	for i, arg := range typeRef.Args {
		args[i] = typeNameFromRefWithAliases(arg, aliases, seen)
	}
	return base + "<" + joinSerializedTypes(args, ", ") + ">"
}

func cloneSeenTypes(seen map[string]bool) map[string]bool {
	cloned := make(map[string]bool, len(seen)+1)
	for name, active := range seen {
		cloned[name] = active
	}
	return cloned
}

func joinSerializedTypes(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	joined := parts[0]
	for i := 1; i < len(parts); i++ {
		joined += sep + parts[i]
	}
	return joined
}

func collectTypeAliases(program *ast.Program) map[string]*ast.TypeRef {
	aliases := make(map[string]*ast.TypeRef)
	for _, stmt := range program.Statements {
		if alias, ok := stmt.(*ast.TypeAliasStmt); ok {
			aliases[alias.Name.Lexeme] = alias.Target
		}
	}
	return aliases
}

func (c *Compiler) inferFieldType(field ast.FieldDecl, defaultValue value.Value) string {
	if field.Type != nil {
		return c.typeNameFromRef(field.Type)
	}
	switch defaultValue.Kind {
	case value.Number:
		return bvmruntime.TypeNumber
	case value.Char:
		return bvmruntime.TypeChar
	case value.String:
		return bvmruntime.TypeString
	case value.Bool:
		return bvmruntime.TypeBool
	default:
		return ""
	}
}

func (c *Compiler) inferExprType(expr ast.Expr) string {
	switch node := expr.(type) {
	case *ast.LiteralExpr:
		switch node.Value.(type) {
		case int64:
			return bvmruntime.TypeInt
		case float64:
			return bvmruntime.TypeFloat
		case rune:
			return bvmruntime.TypeChar
		case string:
			return bvmruntime.TypeString
		case bool:
			return bvmruntime.TypeBool
		default:
			return ""
		}
	case *ast.CastExpr:
		return c.typeNameFromRef(node.Target)
	case *ast.GroupingExpr:
		return c.inferExprType(node.Expr)
	case *ast.TupleExpr:
		return bvmruntime.TypeTuple
	case *ast.ArrayExpr:
		return bvmruntime.TypeArray
	case *ast.MapExpr:
		return bvmruntime.TypeMap
	case *ast.VariableExpr:
		for i := len(c.state.locals) - 1; i >= 0; i-- {
			if c.state.locals[i].name == node.Name.Lexeme {
				return c.state.locals[i].typ
			}
		}
		if _, typ, _, _, ok := c.resolveUpvalueFrom(c.state, node.Name.Lexeme); ok {
			return typ
		}
		return c.globalTypes[node.Name.Lexeme]
	case *ast.ThisExpr:
		return c.state.ownerClass.Name
	case *ast.GetExpr:
		if class := c.resolveExprClass(node.Object); class != nil {
			if field, ok := class.LookupField(node.Name.Lexeme); ok {
				return field.TypeName
			}
			if field, ok := class.StaticFields[node.Name.Lexeme]; ok {
				return field.TypeName
			}
			if _, ok := class.StaticMethods[node.Name.Lexeme]; ok {
				return bvmruntime.TypeFunction
			}
			return ""
		}
		return ""
	case *ast.IndexExpr:
		return bvmruntime.TypeAny
	case *ast.SliceExpr:
		return c.inferExprType(node.Object)
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
				return bvmruntime.TypeRange
			}
		}
		return ""
	case *ast.LambdaExpr:
		return bvmruntime.TypeFunction
	case *ast.BinaryExpr:
		left := c.inferExprType(node.Left)
		right := c.inferExprType(node.Right)
		switch node.Operator.Type {
		case token.AndAnd, token.OrOr:
			return bvmruntime.TypeBool
		case token.Plus:
			if isNumericLikeType(left) && isNumericLikeType(right) {
				return inferNumericTypeName(node.Operator.Type, left, right)
			}
			if left == bvmruntime.TypeString || right == bvmruntime.TypeString {
				return bvmruntime.TypeString
			}
		case token.Minus, token.Star, token.Slash, token.Percent:
			if isNumericLikeType(left) && isNumericLikeType(right) {
				return inferNumericTypeName(node.Operator.Type, left, right)
			}
		case token.EqualEqual, token.BangEqual, token.Greater, token.GreaterEqual, token.Less, token.LessEqual:
			return bvmruntime.TypeBool
		}
	}
	return ""
}

func isNumericLikeType(typeName string) bool {
	switch typeName {
	case bvmruntime.TypeInt, bvmruntime.TypeFloat, bvmruntime.TypeNumber, "Integer", "Double":
		return true
	default:
		return false
	}
}

func inferNumericTypeName(operator token.Type, left string, right string) string {
	if operator == token.Slash {
		return bvmruntime.TypeFloat
	}
	if left == bvmruntime.TypeInt && right == bvmruntime.TypeInt {
		return bvmruntime.TypeInt
	}
	if left == bvmruntime.TypeFloat || right == bvmruntime.TypeFloat {
		return bvmruntime.TypeFloat
	}
	if left == bvmruntime.TypeNumber || right == bvmruntime.TypeNumber {
		return bvmruntime.TypeNumber
	}
	return bvmruntime.TypeNumber
}

func foldNumericBinary(operator token.Type, left value.Value, right value.Value) value.Value {
	switch operator {
	case token.Plus:
		if left.NumberKind == value.NumberInt && right.NumberKind == value.NumberInt {
			return value.IntValue(int64(left.Num + right.Num))
		}
		return value.FloatValue(left.Num + right.Num)
	case token.Minus:
		if left.NumberKind == value.NumberInt && right.NumberKind == value.NumberInt {
			return value.IntValue(int64(left.Num - right.Num))
		}
		return value.FloatValue(left.Num - right.Num)
	case token.Star:
		if left.NumberKind == value.NumberInt && right.NumberKind == value.NumberInt {
			return value.IntValue(int64(left.Num * right.Num))
		}
		return value.FloatValue(left.Num * right.Num)
	case token.Percent:
		if left.NumberKind == value.NumberInt && right.NumberKind == value.NumberInt {
			return value.IntValue(int64(math.Mod(left.Num, right.Num)))
		}
		return value.FloatValue(math.Mod(left.Num, right.Num))
	default:
		return value.FloatValue(left.Num)
	}
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
	loopVar := c.declareLocal(anchor, bvmruntime.TypeNumber)
	currentSlot := c.declareLocal(token.Token{Lexeme: "__range_current_" + anchor.Lexeme, Line: anchor.Line}, bvmruntime.TypeNumber)
	endSlot := c.declareLocal(token.Token{Lexeme: "__range_end_" + anchor.Lexeme, Line: anchor.Line}, bvmruntime.TypeNumber)
	stepSlot := c.declareLocal(token.Token{Lexeme: "__range_step_" + anchor.Lexeme, Line: anchor.Line}, bvmruntime.TypeNumber)
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

func (c *Compiler) emitClosure(fn *bytecode.Function, line int) {
	constant := c.constant(fn)
	c.emit(bytecode.OpClosure, line)
	c.emitUint16(constant, line)
}

func (c *Compiler) lambdaBlock(node *ast.LambdaExpr) *ast.BlockStmt {
	if node.Block == nil {
		return &ast.BlockStmt{Statements: []ast.Stmt{&ast.ReturnStmt{Value: node.Body}}}
	}
	stmts := append([]ast.Stmt(nil), node.Block.Statements...)
	if len(stmts) > 0 {
		if exprStmt, ok := stmts[len(stmts)-1].(*ast.ExprStmt); ok {
			stmts[len(stmts)-1] = &ast.ReturnStmt{Value: exprStmt.Expr}
		}
	}
	return &ast.BlockStmt{Statements: stmts}
}

func (c *Compiler) maybeWrapInterfaceType(typeRef *ast.TypeRef, expr ast.Expr, line int) error {
	if typeRef == nil {
		return nil
	}
	return c.maybeWrapInterfaceName(c.typeNameFromRef(typeRef), expr, line)
}

func (c *Compiler) maybeWrapInterfaceName(typeName string, expr ast.Expr, line int) error {
	typeName = rootTypeName(typeName)
	if !c.exprIsCallableish(expr) {
		return nil
	}
	iface, ok := c.interfaceSpecs[typeName]
	if !ok || iface.FunctionalMethod == "" {
		return nil
	}
	ifaceConst := c.constant(iface.Name)
	methodConst := c.constant(iface.FunctionalMethod)
	c.emit(bytecode.OpWrapInterface, line)
	c.emitUint16(ifaceConst, line)
	c.emitUint16(methodConst, line)
	return nil
}

func (c *Compiler) exprIsCallableish(expr ast.Expr) bool {
	switch node := expr.(type) {
	case *ast.LambdaExpr:
		return true
	case *ast.VariableExpr:
		if sig, ok := c.functionSigs[node.Name.Lexeme]; ok {
			return len(sig.params) >= 0
		}
		return c.inferExprType(expr) == "Function"
	case *ast.GetExpr:
		return c.inferExprType(expr) == "Function"
	default:
		return c.inferExprType(expr) == "Function"
	}
}

func (c *Compiler) expectedParamTypes(callee ast.Expr) []string {
	switch node := callee.(type) {
	case *ast.VariableExpr:
		if sig, ok := c.functionSigs[node.Name.Lexeme]; ok {
			return sig.params
		}
	case *ast.GetExpr:
		if class := c.resolveExprClass(node.Object); class != nil {
			if method, _, ok := class.LookupMethod(node.Name.Lexeme); ok {
				return method.ParamTypes
			}
		}
	}
	return nil
}

func collectInterfaceSpecs(program *ast.Program, registry *bvmruntime.Registry) map[string]bytecode.InterfaceSpec {
	specs := make(map[string]bytecode.InterfaceSpec)
	aliases := collectTypeAliases(program)
	for _, stmt := range program.Statements {
		iface, ok := stmt.(*ast.InterfaceStmt)
		if !ok {
			continue
		}
		methods := make(map[string]bytecode.InterfaceMethodSpec, len(iface.Methods))
		functional := ""
		for _, method := range iface.Methods {
			params := make([]string, len(method.Params))
			for i, param := range method.Params {
				if param.Type != nil {
					params[i] = typeNameFromRefWithAliases(param.Type, aliases, map[string]bool{})
				} else {
					params[i] = bvmruntime.TypeAny
				}
			}
			ret := ""
			if method.ReturnType != nil {
				ret = typeNameFromRefWithAliases(method.ReturnType, aliases, map[string]bool{})
			}
			methods[method.Name.Lexeme] = bytecode.InterfaceMethodSpec{Name: method.Name.Lexeme, Params: params, Return: ret}
			if functional == "" {
				functional = method.Name.Lexeme
			} else {
				functional = ""
			}
		}
		specs[iface.Name.Lexeme] = bytecode.InterfaceSpec{Name: iface.Name.Lexeme, Methods: methods, FunctionalMethod: functional}
	}
	if registry != nil {
		for name, spec := range registry.Specs() {
			if !spec.IsInterface || len(spec.Members) == 0 {
				continue
			}
			if _, exists := specs[name]; exists {
				continue
			}
			methods := make(map[string]bytecode.InterfaceMethodSpec, len(spec.Members))
			functional := ""
			for memberName, member := range spec.Members {
				if member.Callable == nil {
					functional = ""
					continue
				}
				methods[memberName] = bytecode.InterfaceMethodSpec{Name: memberName, Params: append([]string(nil), member.Callable.Params...), Return: member.Callable.Return}
				if functional == "" {
					functional = memberName
				} else {
					functional = ""
				}
			}
			specs[name] = bytecode.InterfaceSpec{Name: name, Methods: methods, FunctionalMethod: functional}
		}
	}
	return specs
}

func collectFunctionSignatures(program *ast.Program) map[string]callableSignature {
	sigs := make(map[string]callableSignature)
	aliases := collectTypeAliases(program)
	for _, stmt := range program.Statements {
		fn, ok := stmt.(*ast.FunctionStmt)
		if !ok {
			continue
		}
		params := make([]string, len(fn.Params))
		for i, param := range fn.Params {
			if param.Type != nil {
				params[i] = typeNameFromRefWithAliases(param.Type, aliases, map[string]bool{})
			} else {
				params[i] = bvmruntime.TypeAny
			}
		}
		ret := ""
		if fn.ReturnType != nil {
			ret = typeNameFromRefWithAliases(fn.ReturnType, aliases, map[string]bool{})
		}
		sigs[fn.Name.Lexeme] = callableSignature{params: params, ret: ret}
	}
	return sigs
}

func normalizeTypeName(name string) string {
	switch name {
	case "int":
		return bvmruntime.TypeInt
	case "float":
		return bvmruntime.TypeFloat
	case "number":
		return bvmruntime.TypeNumber
	case "bool":
		return bvmruntime.TypeBool
	case "char":
		return bvmruntime.TypeChar
	case "String":
		return bvmruntime.TypeString
	case "string":
		return bvmruntime.TypeString
	case "array":
		return bvmruntime.TypeArray
	case "map":
		return bvmruntime.TypeMap
	case "tuple":
		return bvmruntime.TypeTuple
	case "range":
		return bvmruntime.TypeRange
	case "nil":
		return bvmruntime.TypeNil
	case "void":
		return bvmruntime.TypeVoid
	case "any":
		return bvmruntime.TypeAny
	case "Any":
		return bvmruntime.TypeAny
	default:
		if name == "" {
			return bvmruntime.TypeAny
		}
		return name
	}
}

func rootTypeName(typeName string) string {
	for i, r := range typeName {
		if r == '<' || r == ' ' {
			return typeName[:i]
		}
	}
	return typeName
}

func (c *Compiler) shouldUseScriptLocal(name string) bool {
	if c.forceGlobals {
		return false
	}
	if c.state.parent != nil || c.state.name != "<script>" {
		return false
	}
	return !c.capturedGlobals[name]
}

func (c *Compiler) shouldUseScriptLocals(targets []token.Token) bool {
	if c.forceGlobals {
		return false
	}
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

func collectGlobalSlots(program *ast.Program) (map[string]byte, []string) {
	slots := make(map[string]byte)
	names := make([]string, 0)
	var next byte
	reserve := func(name string) {
		if _, exists := slots[name]; exists {
			return
		}
		slots[name] = next
		names = append(names, name)
		next++
	}
	for _, name := range coreGlobalSlotCandidates() {
		reserve(name)
	}
	for _, stmt := range program.Statements {
		switch node := stmt.(type) {
		case *ast.LetStmt:
			reserve(node.Name.Lexeme)
		case *ast.DestructureLetStmt:
			for _, target := range node.Targets {
				reserve(target.Lexeme)
			}
		case *ast.FunctionStmt:
			reserve(node.Name.Lexeme)
		case *ast.ClassStmt:
			reserve(node.Name.Lexeme)
		case *ast.TypeAliasStmt:
			continue
		}
	}
	return slots, names
}

func coreGlobalSlotCandidates() []string {
	registry := bvmruntime.NewRegistry()
	bvmruntime.InstallCoreGlobals(registry, nil)
	names := make([]string, 0, len(registry.Globals()))
	for name := range registry.Globals() {
		names = append(names, name)
	}
	return names
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
	case *ast.SetIndexStmt:
		collectCapturedGlobalsExpr(node.Object, captured, inCallable, scope)
		collectCapturedGlobalsExpr(node.Index, captured, inCallable, scope)
		collectCapturedGlobalsExpr(node.Value, captured, inCallable, scope)
	case *ast.ExprStmt:
		collectCapturedGlobalsExpr(node.Expr, captured, inCallable, scope)
	case *ast.TypeAliasStmt:
		return
	case *ast.IfStmt:
		collectCapturedGlobalsExpr(node.Condition, captured, inCallable, scope)
		collectCapturedGlobalsBlock(node.Then, captured, inCallable, cloneScope(scope))
		if node.Else != nil {
			collectCapturedGlobalsBlock(node.Else, captured, inCallable, cloneScope(scope))
		}
	case *ast.SwitchStmt:
		collectCapturedGlobalsExpr(node.Value, captured, inCallable, scope)
		for _, arm := range node.Arms {
			armScope := cloneScope(scope)
			for _, pattern := range arm.Patterns {
				if pattern.Value != nil {
					collectCapturedGlobalsExpr(pattern.Value, captured, inCallable, armScope)
				}
				if pattern.Type != nil {
					armScope[pattern.Type.Binding.Lexeme] = true
				}
			}
			collectCapturedGlobalsBlock(arm.Body, captured, inCallable, armScope)
		}
		if node.Default != nil {
			collectCapturedGlobalsBlock(node.Default, captured, inCallable, cloneScope(scope))
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
			if method.Body == nil {
				continue
			}
			innerScope := map[string]bool{}
			if !method.Static {
				innerScope["this"] = true
			}
			for _, param := range method.Params {
				innerScope[param.Name.Lexeme] = true
			}
			collectCapturedGlobalsBlock(method.Body, captured, true, innerScope)
		}
	case *ast.InterfaceStmt:
		return
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
	case *ast.ArrayExpr:
		for _, element := range node.Elements {
			collectCapturedGlobalsExpr(element, captured, inCallable, scope)
		}
	case *ast.MapExpr:
		for _, entry := range node.Entries {
			collectCapturedGlobalsExpr(entry.Value, captured, inCallable, scope)
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
	case *ast.IndexExpr:
		collectCapturedGlobalsExpr(node.Object, captured, inCallable, scope)
		collectCapturedGlobalsExpr(node.Index, captured, inCallable, scope)
	case *ast.SliceExpr:
		collectCapturedGlobalsExpr(node.Object, captured, inCallable, scope)
		collectCapturedGlobalsExpr(node.Start, captured, inCallable, scope)
		collectCapturedGlobalsExpr(node.End, captured, inCallable, scope)
	case *ast.LambdaExpr:
		innerScope := cloneScope(scope)
		for _, param := range node.Params {
			innerScope[param.Name.Lexeme] = true
		}
		collectCapturedGlobalsExpr(node.Body, captured, true, innerScope)
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
