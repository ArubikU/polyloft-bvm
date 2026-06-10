package compiler

import (
	"fmt"
	"math"
	"strings"

	"github.com/ArubikU/polyloft-bvm/internal/ast"
	"github.com/ArubikU/polyloft-bvm/internal/bytecode"
	bvmruntime "github.com/ArubikU/polyloft-bvm/internal/runtime"
	"github.com/ArubikU/polyloft-bvm/internal/token"
	"github.com/ArubikU/polyloft-bvm/internal/value"
)

type Compiler struct {
	state           *state
	classes         map[string]*value.Class
	classDecls      map[string]*ast.ClassStmt
	consts          map[string]value.Value
	globalTypes     map[string]string
	typeAliases     map[string]*ast.TypeRef
	forceGlobals    bool
	capturedGlobals map[string]bool
	globalSlots     map[string]byte
	lambdaCounter   *int
	interfaceSpecs  map[string]bytecode.InterfaceSpec
	functionSigs    map[string]callableSignature
	functionDecls   map[string]*ast.FunctionStmt
	loops           []loopContext
}

type loopContext struct {
	scopeDepth       int
	continueTarget   int
	continueBackward bool
	breakJumps       []int
	continueJumps    []int
}

type state struct {
	function         *bytecode.Function
	chunk            *bytecode.Chunk
	locals           []local
	upvalues         []upvalueRef
	depth            int
	parent           *state
	name             string
	ownerClass       *value.Class
	lastOpcodeOffset int // offset in chunk.Code of the most recently emitted opcode
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
	typeParams []string
	params     []string
	ret        string
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
	compiler := &Compiler{state: root, classes: collectRegistryClasses(registry), classDecls: collectClassDeclarations(program), consts: make(map[string]value.Value), globalTypes: make(map[string]string), typeAliases: aliases, forceGlobals: forceGlobals, capturedGlobals: collectCapturedGlobals(program), globalSlots: globalSlots, lambdaCounter: &lambdaCounter, interfaceSpecs: collectInterfaceSpecs(program, registry), functionSigs: collectFunctionSignatures(program), functionDecls: collectFunctionDeclarations(program)}
	root.function.Interfaces = compiler.interfaceSpecs
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
			if c.emitFastAddConstLocalAssign(slot, node, targetType) {
				return nil
			}
			if c.emitFastAddLocalLocal(slot, node, targetType) {
				return nil
			}
			if (node.Operator.Type == token.Equal || node.Operator.Type == token.PlusEqual) && c.emitFastStringAppendAssign(slot, node, targetType) {
				return nil
			}
			if node.Operator.Type == token.Equal && c.emitFastLocalMulThisFieldAssign(slot, node, targetType) {
				return nil
			}
			if c.emitFastPlusEqLocalMulThisFieldAddThisField(slot, node) {
				return nil
			}
			if c.emitFastPlusEqLocalMulThisField(slot, node) {
				return nil
			}
			if node.Operator.Type != token.Equal {
				if isNumericLikeType(targetType) && isNumericLikeType(assignedType) {
					var toLocalOp bytecode.Op
					switch node.Operator.Type {
					case token.PlusEqual:
						toLocalOp = bytecode.OpAddToLocal
					case token.MinusEqual:
						toLocalOp = bytecode.OpSubToLocal
					case token.StarEqual:
						toLocalOp = bytecode.OpMulToLocal
					}
					if toLocalOp != 0 {
						if err := c.compileExpr(node.Value); err != nil {
							return err
						}
						c.emit(toLocalOp, node.Name.Line)
						c.emitByte(slot, node.Name.Line)
						return nil
					}
				}
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
			// Detect `a = a op expr` pattern and emit xToLocal fused op
			if c.emitFastNumericSelfAssign(slot, node, targetType, assignedType) {
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
		// Fast path: local_arr[local_idx] = bool_literal → SET_LOCAL_ARRAY_BOOL
		if objVar, ok := node.Object.(*ast.VariableExpr); ok {
			if idxVar, ok := node.Index.(*ast.VariableExpr); ok {
				if lit, ok := node.Value.(*ast.LiteralExpr); ok {
					if boolVal, ok := lit.Value.(bool); ok {
						objType := c.inferExprType(node.Object)
						if isArrayType(objType) {
							if arrSlot, aok := c.resolveLocal(objVar.Name.Lexeme); aok {
								if idxSlot, iok := c.resolveLocal(idxVar.Name.Lexeme); iok {
									var boolByte byte
									if boolVal {
										boolByte = 1
									}
									c.emit(bytecode.OpSetLocalArrayBool, objVar.Name.Line)
									c.emitByte(arrSlot, objVar.Name.Line)
									c.emitByte(idxSlot, objVar.Name.Line)
									c.emitByte(boolByte, objVar.Name.Line)
									return nil
								}
							}
						}
					}
				}
			}
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
		if c.compileFastIfStmt(node) {
			return nil
		}
		if condition, guard, ok := unwrapInstanceOfExprAndGuard(node.Condition); ok {
			return c.compileIfInstanceOf(node, condition, guard)
		}
		if condition, ok := unwrapInstanceOfExpr(node.Condition); ok && condition.Binding != nil {
			return c.compileIfInstanceOf(node, condition, nil)
		}
		if err := c.compileExpr(node.Condition); err != nil {
			return err
		}
		jumpIfFalse := c.emitJump(bytecode.OpJumpIfFalsePop, 0)
		if err := c.compileBlock(node.Then); err != nil {
			return err
		}
		if node.Else != nil {
			jumpOverElse := c.emitJump(bytecode.OpJump, 0)
			c.patchJump(jumpIfFalse)
			if err := c.compileBlock(node.Else); err != nil {
				return err
			}
			c.patchJump(jumpOverElse)
		} else {
			c.patchJump(jumpIfFalse)
		}
		return nil
	case *ast.TryStmt:
		return c.compileTry(node)
	case *ast.SwitchStmt:
		return c.compileSwitch(node)
	case *ast.LoopStmt:
		if node.PostCondition {
			return c.compileDoLoop(node)
		}
		return c.compileLoop(node)
	case *ast.BreakStmt:
		if len(c.loops) == 0 {
			return fmt.Errorf("line %d:%d: break used outside loop", node.Keyword.Line, node.Keyword.Column)
		}
		ctx := &c.loops[len(c.loops)-1]
		c.emitLoopScopePops(ctx.scopeDepth, node.Keyword.Line)
		ctx.breakJumps = append(ctx.breakJumps, c.emitJump(bytecode.OpJump, node.Keyword.Line))
		return nil
	case *ast.ContinueStmt:
		if len(c.loops) == 0 {
			return fmt.Errorf("line %d:%d: continue used outside loop", node.Keyword.Line, node.Keyword.Column)
		}
		ctx := &c.loops[len(c.loops)-1]
		c.emitLoopScopePops(ctx.scopeDepth, node.Keyword.Line)
		if ctx.continueBackward {
			c.emitLoop(ctx.continueTarget, node.Keyword.Line)
		} else {
			ctx.continueJumps = append(ctx.continueJumps, c.emitJump(bytecode.OpJump, node.Keyword.Line))
		}
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
	case *ast.ThrowStmt:
		if err := c.compileExpr(node.Value); err != nil {
			return err
		}
		c.emit(bytecode.OpThrow, node.Keyword.Line)
		return nil
	case *ast.FunctionStmt:
		if node.IsNative {
			// Native functions are provided by the Go runtime registry, not compiled from source.
			// Just register the name as a global type so the type checker knows about it.
			c.globalTypes[node.Name.Lexeme] = "Function"
			return nil
		}
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
		elemType := c.inferIterableElementType(c.inferExprType(node.Iterable))
		if len(node.Targets) == 1 {
			targetSlots[0] = c.declareLocal(node.Targets[0], elemType)
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
		c.loops = append(c.loops, loopContext{scopeDepth: c.state.depth, continueTarget: loopStart, continueBackward: true})
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
			if condition, guard, ok := unwrapInstanceOfExprAndGuard(node.Condition); ok {
				tempName := token.Token{Lexeme: "__where_instanceof_temp", Line: condition.Target.Name.Line}
				tempSlot := c.declareLocal(tempName, c.inferExprType(condition.Expr))
				bindingSlot := c.declareLocal(*condition.Binding, c.typeNameFromRef(condition.Target))
				if err := c.compileExpr(condition.Expr); err != nil {
					return err
				}
				c.emit(bytecode.OpSetLocal, condition.Target.Name.Line)
				c.emitByte(tempSlot, condition.Target.Name.Line)
				c.emit(bytecode.OpGetLocal, condition.Target.Name.Line)
				c.emitByte(tempSlot, condition.Target.Name.Line)
				name := c.constant(c.typeNameFromRef(condition.Target))
				c.emit(bytecode.OpMatchType, condition.Target.Name.Line)
				c.emitUint16(name, condition.Target.Name.Line)
				skipBody := c.emitJump(bytecode.OpJumpIfFalsePop, anchor.Line)
				c.emit(bytecode.OpGetLocal, condition.Target.Name.Line)
				c.emitByte(tempSlot, condition.Target.Name.Line)
				c.emitCastForType(condition.Target, condition.Target.Name.Line)
				c.emit(bytecode.OpSetLocal, condition.Binding.Line)
				c.emitByte(bindingSlot, condition.Binding.Line)
				if err := c.compileExpr(guard); err != nil {
					return err
				}
				skipByGuard := c.emitJump(bytecode.OpJumpIfFalsePop, anchor.Line)
				if err := c.compileBlock(node.Body); err != nil {
					return err
				}
				c.emitLoop(loopStart, anchor.Line)
				c.patchJump(skipByGuard)
				c.emitLoop(loopStart, anchor.Line)
				c.patchJump(skipBody)
				c.emitLoop(loopStart, anchor.Line)
			} else if condition, ok := unwrapInstanceOfExpr(node.Condition); ok && condition.Binding != nil {
				tempName := token.Token{Lexeme: "__where_instanceof_temp", Line: condition.Target.Name.Line}
				tempSlot := c.declareLocal(tempName, c.inferExprType(condition.Expr))
				bindingSlot := c.declareLocal(*condition.Binding, c.typeNameFromRef(condition.Target))
				if err := c.compileExpr(condition.Expr); err != nil {
					return err
				}
				c.emit(bytecode.OpSetLocal, condition.Target.Name.Line)
				c.emitByte(tempSlot, condition.Target.Name.Line)
				c.emit(bytecode.OpGetLocal, condition.Target.Name.Line)
				c.emitByte(tempSlot, condition.Target.Name.Line)
				name := c.constant(c.typeNameFromRef(condition.Target))
				c.emit(bytecode.OpMatchType, condition.Target.Name.Line)
				c.emitUint16(name, condition.Target.Name.Line)
				skipBody := c.emitJump(bytecode.OpJumpIfFalsePop, anchor.Line)
				c.emit(bytecode.OpGetLocal, condition.Target.Name.Line)
				c.emitByte(tempSlot, condition.Target.Name.Line)
				c.emitCastForType(condition.Target, condition.Target.Name.Line)
				c.emit(bytecode.OpSetLocal, condition.Binding.Line)
				c.emitByte(bindingSlot, condition.Binding.Line)
				if err := c.compileBlock(node.Body); err != nil {
					return err
				}
				c.emitLoop(loopStart, anchor.Line)
				c.patchJump(skipBody)
				c.emitLoop(loopStart, anchor.Line)
			} else {
				if err := c.compileExpr(node.Condition); err != nil {
					return err
				}
				skipBody := c.emitJump(bytecode.OpJumpIfFalsePop, anchor.Line)
				if err := c.compileBlock(node.Body); err != nil {
					return err
				}
				c.emitLoop(loopStart, anchor.Line)
				c.patchJump(skipBody)
				c.emitLoop(loopStart, anchor.Line)
			}
		} else {
			if err := c.compileBlock(node.Body); err != nil {
				return err
			}
			c.emitLoop(loopStart, anchor.Line)
		}
		c.patchJump(exitJump)
		c.patchLoopBreaks()
		c.loops = c.loops[:len(c.loops)-1]
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

func (c *Compiler) compileTry(node *ast.TryStmt) error {
	c.beginScope()
	exceptionSlot := c.declareLocal(token.Token{Lexeme: "__exception", Line: node.Keyword.Line}, bvmruntime.TypeAny)
	handler := c.emitJump(bytecode.OpPushHandler, node.Keyword.Line)
	if err := c.compileBlockStatements(node.Body); err != nil {
		return err
	}
	c.emit(bytecode.OpPopHandler, node.Keyword.Line)
	endJump := c.emitJump(bytecode.OpJump, node.Keyword.Line)
	c.patchJump(handler)
	c.emit(bytecode.OpSetLocal, node.Keyword.Line)
	c.emitByte(exceptionSlot, node.Keyword.Line)
	catchJumps := make([]int, 0, len(node.Catches))
	for _, clause := range node.Catches {
		var skipJump int
		if clause.Type != nil {
			c.emit(bytecode.OpGetLocal, clause.Keyword.Line)
			c.emitByte(exceptionSlot, clause.Keyword.Line)
			typeName := c.constant(c.typeNameFromRef(clause.Type))
			c.emit(bytecode.OpMatchType, clause.Keyword.Line)
			c.emitUint16(typeName, clause.Keyword.Line)
			skipJump = c.emitJump(bytecode.OpJumpIfFalsePop, clause.Keyword.Line)
		}
		c.beginScope()
		if clause.Binding.Type != "" {
			bindingType := bvmruntime.TypeAny
			if clause.Type != nil {
				bindingType = c.typeNameFromRef(clause.Type)
			}
			bindingSlot := c.declareLocal(clause.Binding, bindingType)
			c.emit(bytecode.OpGetLocal, clause.Binding.Line)
			c.emitByte(exceptionSlot, clause.Binding.Line)
			c.emit(bytecode.OpSetLocal, clause.Binding.Line)
			c.emitByte(bindingSlot, clause.Binding.Line)
		}
		if err := c.compileBlockStatements(clause.Body); err != nil {
			return err
		}
		c.endScope(clause.Keyword.Line)
		catchJumps = append(catchJumps, c.emitJump(bytecode.OpJump, clause.Keyword.Line))
		if clause.Type != nil {
			c.patchJump(skipJump)
		}
	}
	c.emit(bytecode.OpGetLocal, node.Keyword.Line)
	c.emitByte(exceptionSlot, node.Keyword.Line)
	c.emit(bytecode.OpThrow, node.Keyword.Line)
	for _, jump := range catchJumps {
		c.patchJump(jump)
	}
	c.patchJump(endJump)
	c.endScope(node.Keyword.Line)
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
			jumpIfFalse := c.emitJump(bytecode.OpJumpIfFalsePop, 0)
			matchedJumps = append(matchedJumps, c.emitJump(bytecode.OpJump, 0))
			c.patchJump(jumpIfFalse)
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
		c.emitCastForType(node.Target, node.Target.Name.Line)
		return nil
	case *ast.InstanceOfExpr:
		if err := c.compileExpr(node.Expr); err != nil {
			return err
		}
		name := c.constant(c.typeNameFromRef(node.Target))
		c.emit(bytecode.OpMatchType, node.Target.Name.Line)
		c.emitUint16(name, node.Target.Name.Line)
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
	case *ast.ArrayComprehensionExpr:
		return c.compileArrayComprehension(node)
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
		if node.Size != nil {
			// push the size onto the stack
			if err := c.compileExpr(node.Size); err != nil {
				return err
			}
			switch len(node.Initializer) {
			case 0:
				// new T[N]  →  allocate N nils
				c.emit(bytecode.OpArrayAlloc, 0)
			case 1:
				// new T[N]{fill}  →  N copies of fill
				if err := c.compileExpr(node.Initializer[0]); err != nil {
					return err
				}
				c.emit(bytecode.OpArrayFill, 0)
			default:
				// new T[N]{a,b,c,...}  →  size already bounds-checked; just build explicit array
				c.emit(bytecode.OpPop, 0) // drop size; element count is exact
				for _, element := range node.Initializer {
					if err := c.compileExpr(element); err != nil {
						return err
					}
				}
				c.emit(bytecode.OpArray, 0)
				c.emitByte(byte(len(node.Initializer)), 0)
			}
		} else {
			// new T[]{a,b,c}  or  new T{}  →  bare initializer list
			for _, element := range node.Initializer {
				if err := c.compileExpr(element); err != nil {
					return err
				}
			}
			c.emit(bytecode.OpArray, 0)
			c.emitByte(byte(len(node.Initializer)), 0)
		}
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
		// Constant fold: -(literal) → push negated constant directly, avoids NEGATE opcode
		if node.Operator.Type == token.Minus {
			if folded, ok := c.evalConstExpr(node); ok {
				// Store the raw Go type (int64 or float64) so the VM's constantToValue
				// can handle it correctly. Do NOT store value.Value directly.
				var raw any
				if folded.NumberKind == value.NumberInt {
					raw = folded.Int
				} else {
					raw = folded.Num
				}
				idx := c.state.chunk.AddConstant(raw)
				c.emit(bytecode.OpConstant, node.Operator.Line)
				c.emitUint16(idx, node.Operator.Line)
				return nil
			}
		}
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
		if _, isSuper := node.Object.(*ast.SuperExpr); isSuper {
			return fmt.Errorf("super members must be called as super.%s(...)", node.Name.Lexeme)
		}
		if slot, ok := c.resolveFieldSlot(node.Object, node.Name.Lexeme); ok {
			if _, isThis := node.Object.(*ast.ThisExpr); isThis {
				c.emit(bytecode.OpGetThisField, node.Name.Line)
				c.emitByte(byte(slot), node.Name.Line)
				return nil
			}
			// Fast path: arr[i].field where arr and i are locals in a typed array
			if idxExpr, isIndex := node.Object.(*ast.IndexExpr); isIndex {
				if arrVar, isVar := idxExpr.Object.(*ast.VariableExpr); isVar {
					if idxVar, isIdxVar := idxExpr.Index.(*ast.VariableExpr); isIdxVar {
						if arrSlot, aok := c.resolveLocal(arrVar.Name.Lexeme); aok {
							if isArrayType(c.inferExprType(idxExpr.Object)) {
								if idxSlot, iok := c.resolveLocal(idxVar.Name.Lexeme); iok {
									c.emit(bytecode.OpGetLocalArrayField, node.Name.Line)
									c.emitByte(arrSlot, node.Name.Line)
									c.emitByte(idxSlot, node.Name.Line)
									c.emitByte(byte(slot), node.Name.Line)
									return nil
								}
							}
						}
					}
				}
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
		expectedParams := c.expectedParamTypesForCall(node)
		if variable, ok := node.Callee.(*ast.VariableExpr); ok && variable.Name.Lexeme == "range" {
			if len(node.Arguments) < 1 || len(node.Arguments) > 2 {
				return fmt.Errorf("range expects 1 or 2 arguments")
			}
			for i, arg := range node.Arguments {
				if err := c.compileCallArgument(arg, expectedParams, i, node.Paren.Line); err != nil {
					return err
				}
			}
			c.emit(bytecode.OpRange, variable.Name.Line)
			c.emitByte(byte(len(node.Arguments)), variable.Name.Line)
			return nil
		}
		if variable, ok := node.Callee.(*ast.VariableExpr); ok {
			if localSlot, subValue, ok := c.matchSelfCallableLocalSubInt(variable.Name.Lexeme, node.Arguments); ok {
				subIdx := c.constant(subValue)
				c.emit(bytecode.OpCallSelfLocalSubInt, node.Paren.Line)
				c.emitByte(localSlot, node.Paren.Line)
				c.emitUint16(subIdx, node.Paren.Line)
				return nil
			}
			if constantValue, ok := c.resolveInlineConst(variable.Name.Lexeme); ok {
				if callableConst, localSlot, subValue, ok := c.matchConstCallableLocalSubInt(constantValue, node.Arguments); ok {
					callableIdx := c.constant(callableConst)
					subIdx := c.constant(subValue)
					c.emit(bytecode.OpCallConstLocalSubInt, node.Paren.Line)
					c.emitUint16(callableIdx, node.Paren.Line)
					c.emitByte(localSlot, node.Paren.Line)
					c.emitUint16(subIdx, node.Paren.Line)
					return nil
				}
				if callableConst, ok := constantCallableObject(constantValue); ok {
					for i, arg := range node.Arguments {
						if err := c.compileCallArgument(arg, expectedParams, i, node.Paren.Line); err != nil {
							return err
						}
					}
					idx := c.constant(callableConst)
					c.emit(bytecode.OpCallConst, node.Paren.Line)
					c.emitUint16(idx, node.Paren.Line)
					c.emitByte(byte(len(node.Arguments)), node.Paren.Line)
					return nil
				}
			}
		}
		if variable, ok := node.Callee.(*ast.VariableExpr); ok {
			if _, ok := c.resolveInlineConst(variable.Name.Lexeme); ok {
				// Preserve existing variable resolution when the callee is inlined locally.
			} else if _, ok := c.resolveLocal(variable.Name.Lexeme); ok {
				// Local bindings shadow globals and must keep the generic call path.
			} else if _, ok := c.resolveUpvalue(variable.Name.Lexeme); ok {
				// Captured bindings also need the generic path.
			} else if slot, exists := c.resolveGlobalSlot(variable.Name.Lexeme); exists {
				for i, arg := range node.Arguments {
					if err := c.compileCallArgument(arg, expectedParams, i, node.Paren.Line); err != nil {
						return err
					}
				}
				c.emit(bytecode.OpCallGlobalSlot, node.Paren.Line)
				c.emitByte(slot, node.Paren.Line)
				c.emitByte(byte(len(node.Arguments)), node.Paren.Line)
				return nil
			}
		}
		if _, ok := node.Callee.(*ast.SuperExpr); ok {
			for i, arg := range node.Arguments {
				if err := c.compileCallArgument(arg, expectedParams, i, node.Paren.Line); err != nil {
					return err
				}
			}
			c.emit(bytecode.OpCallSuper, node.Paren.Line)
			c.emitByte(byte(len(node.Arguments)), node.Paren.Line)
			return nil
		}
		if getter, ok := node.Callee.(*ast.GetExpr); ok {
			if _, isSuper := getter.Object.(*ast.SuperExpr); isSuper {
				for i, arg := range node.Arguments {
					if err := c.compileCallArgument(arg, expectedParams, i, node.Paren.Line); err != nil {
						return err
					}
				}
				name := c.constant(getter.Name.Lexeme)
				c.emit(bytecode.OpInvokeSuper, node.Paren.Line)
				c.emitUint16(name, node.Paren.Line)
				c.emitByte(byte(len(node.Arguments)), node.Paren.Line)
				return nil
			}
			if slot, ok := c.resolveMethodSlot(getter.Object, getter.Name.Lexeme); ok {
				if err := c.compileExpr(getter.Object); err != nil {
					return err
				}
				for i, arg := range node.Arguments {
					if err := c.compileCallArgument(arg, expectedParams, i, node.Paren.Line); err != nil {
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
			for i, arg := range node.Arguments {
				if err := c.compileCallArgument(arg, expectedParams, i, node.Paren.Line); err != nil {
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
		for i, arg := range node.Arguments {
			if err := c.compileCallArgument(arg, expectedParams, i, node.Paren.Line); err != nil {
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
		leftFalse := c.emitJump(bytecode.OpJumpIfFalsePop, node.Operator.Line)
		if err := c.compileExpr(node.Right); err != nil {
			return err
		}
		rightFalse := c.emitJump(bytecode.OpJumpIfFalsePop, node.Operator.Line)
		c.emit(bytecode.OpTrue, node.Operator.Line)
		endJump := c.emitJump(bytecode.OpJump, node.Operator.Line)
		c.patchJump(leftFalse)
		c.patchJump(rightFalse)
		c.emit(bytecode.OpFalse, node.Operator.Line)
		c.patchJump(endJump)
		return nil
	}
	if node.Operator.Type == token.OrOr {
		if err := c.compileExpr(node.Left); err != nil {
			return err
		}
		leftFalse := c.emitJump(bytecode.OpJumpIfFalsePop, node.Operator.Line)
		c.emit(bytecode.OpTrue, node.Operator.Line)
		endJump := c.emitJump(bytecode.OpJump, node.Operator.Line)
		c.patchJump(leftFalse)
		if err := c.compileExpr(node.Right); err != nil {
			return err
		}
		rightFalse := c.emitJump(bytecode.OpJumpIfFalsePop, node.Operator.Line)
		c.emit(bytecode.OpTrue, node.Operator.Line)
		endJumpRight := c.emitJump(bytecode.OpJump, node.Operator.Line)
		c.patchJump(rightFalse)
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
	case token.Plus, token.Minus, token.Star, token.StarStar, token.Caret, token.Slash:
		c.emitCompoundOp(node.Operator, c.inferExprType(node.Left), c.inferExprType(node.Right))
	case token.Percent:
		if isNumericLikeType(c.inferExprType(node.Left)) && isNumericLikeType(c.inferExprType(node.Right)) {
			c.emit(bytecode.OpModNum, node.Operator.Line)
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
	case token.StarStar, token.Caret:
		if isNumericLikeType(leftType) && isNumericLikeType(rightType) {
			c.emit(bytecode.OpPowNum, operator.Line)
		} else {
			c.emit(bytecode.OpPow, operator.Line)
		}
	case token.Slash, token.SlashEqual:
		if isNumericLikeType(leftType) && isNumericLikeType(rightType) {
			c.emit(bytecode.OpDivNum, operator.Line)
		} else {
			c.emit(bytecode.OpDiv, operator.Line)
		}
	}
}

func isArrayType(t string) bool {
	return t == bvmruntime.TypeArray || len(t) > 6 && t[:6] == "array<"
}

func isMapType(t string) bool {
	return t == bvmruntime.TypeMap || len(t) > 4 && t[:4] == "map<"
}

func (c *Compiler) emitIndexedGet(object ast.Expr, index ast.Expr, line int) {
	t := c.inferExprType(object)
	if isArrayType(t) {
		c.emit(bytecode.OpGetIndexArray, line)
	} else if isMapType(t) {
		c.emit(bytecode.OpGetIndexMap, line)
	} else {
		c.emit(bytecode.OpGetIndex, line)
	}
}

func (c *Compiler) emitIndexedSet(object ast.Expr, index ast.Expr, line int) {
	t := c.inferExprType(object)
	if isArrayType(t) {
		c.emit(bytecode.OpSetIndexArray, line)
	} else if isMapType(t) {
		c.emit(bytecode.OpSetIndexMap, line)
	} else {
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

// emitFastNumericSelfAssign detects `a = a op expr` and emits xToLocal fused ops.
// Handles: a = a + expr, a = a - expr, a = a * expr, a = a / expr
func (c *Compiler) emitFastNumericSelfAssign(targetSlot byte, node *ast.AssignStmt, targetType string, assignedType string) bool {
	if node.Operator.Type != token.Equal {
		return false
	}
	if !isNumericLikeType(targetType) {
		return false
	}
	bin, ok := node.Value.(*ast.BinaryExpr)
	if !ok {
		return false
	}
	var rhsExpr ast.Expr
	var op bytecode.Op
	switch bin.Operator.Type {
	case token.Plus:
		op = bytecode.OpAddToLocal
	case token.Minus:
		op = bytecode.OpSubToLocal
	case token.Star:
		op = bytecode.OpMulToLocal
	default:
		return false
	}
	// Check that left side references the target variable
	if leftVar, ok := bin.Left.(*ast.VariableExpr); ok && leftVar.Name.Lexeme == node.Name.Lexeme {
		rhsExpr = bin.Right
	} else if op == bytecode.OpAddToLocal || op == bytecode.OpMulToLocal {
		// commutative: check right side too
		if rightVar, ok := bin.Right.(*ast.VariableExpr); ok && rightVar.Name.Lexeme == node.Name.Lexeme {
			rhsExpr = bin.Left
		}
	}
	if rhsExpr == nil {
		return false
	}
	rhsType := c.inferExprType(rhsExpr)
	if !isNumericLikeType(rhsType) {
		return false
	}
	if err := c.compileExpr(rhsExpr); err != nil {
		return false
	}
	c.emit(op, node.Name.Line)
	c.emitByte(targetSlot, node.Name.Line)
	return true
}

// emitFastPlusEqLocalMulThisField handles: target += local * this.field
func (c *Compiler) emitFastPlusEqLocalMulThisField(targetSlot byte, node *ast.AssignStmt) bool {
	if node.Operator.Type != token.PlusEqual {
		return false
	}
	localSlot, fieldSlot, ok := c.matchLocalMulThisField(node.Value)
	if !ok {
		return false
	}
	c.emit(bytecode.OpAddLocalMulThisField, node.Name.Line)
	c.emitByte(targetSlot, node.Name.Line)
	c.emitByte(localSlot, node.Name.Line)
	c.emitByte(fieldSlot, node.Name.Line)
	return true
}

// emitFastPlusEqLocalMulThisFieldAddThisField handles: target += (local * this.fieldA) + this.fieldB
func (c *Compiler) emitFastPlusEqLocalMulThisFieldAddThisField(targetSlot byte, node *ast.AssignStmt) bool {
	if node.Operator.Type != token.PlusEqual {
		return false
	}
	addExpr, ok := node.Value.(*ast.BinaryExpr)
	if !ok || addExpr.Operator.Type != token.Plus {
		return false
	}
	// Try: (local * thisField) + thisField2
	if localSlot, mulFieldSlot, ok := c.matchLocalMulThisField(addExpr.Left); ok {
		if addFieldSlot, ok := c.matchThisFieldSlot(addExpr.Right); ok {
			c.emit(bytecode.OpAddLocalMulThisFieldAddThisField, node.Name.Line)
			c.emitByte(targetSlot, node.Name.Line)
			c.emitByte(localSlot, node.Name.Line)
			c.emitByte(byte(mulFieldSlot), node.Name.Line)
			c.emitByte(byte(addFieldSlot), node.Name.Line)
			return true
		}
	}
	// Try: thisField2 + (local * thisField) — commutative
	if localSlot, mulFieldSlot, ok := c.matchLocalMulThisField(addExpr.Right); ok {
		if addFieldSlot, ok := c.matchThisFieldSlot(addExpr.Left); ok {
			c.emit(bytecode.OpAddLocalMulThisFieldAddThisField, node.Name.Line)
			c.emitByte(targetSlot, node.Name.Line)
			c.emitByte(localSlot, node.Name.Line)
			c.emitByte(byte(mulFieldSlot), node.Name.Line)
			c.emitByte(byte(addFieldSlot), node.Name.Line)
			return true
		}
	}
	return false
}

func (c *Compiler) matchThisFieldSlot(expr ast.Expr) (int, bool) {
	expr = unwrapGrouping(expr)
	getter, ok := expr.(*ast.GetExpr)
	if !ok {
		return 0, false
	}
	if _, isThis := getter.Object.(*ast.ThisExpr); !isThis {
		return 0, false
	}
	slot, ok := c.resolveFieldSlot(getter.Object, getter.Name.Lexeme)
	return slot, ok
}

func (c *Compiler) emitFastStringAppendAssign(targetSlot byte, node *ast.AssignStmt, targetType string) bool {
	if rootTypeName(targetType) != bvmruntime.TypeString {
		return false
	}
	// Handle s += expr
	if node.Operator.Type == token.PlusEqual {
		if err := c.compileExpr(node.Value); err != nil {
			return false
		}
		c.emit(bytecode.OpAppendLocalString, node.Name.Line)
		c.emitByte(targetSlot, node.Name.Line)
		return true
	}
	if node.Operator.Type != token.Equal {
		return false
	}
	// Recursively detect s = s + a + b + ... chains, emitting one APPEND_LOCAL_STRING per term.
	return c.emitStringAppendChain(targetSlot, node.Name.Lexeme, node.Value, node.Name.Line)
}

// emitStringAppendChain peels apart left-associative (s + a + b + c) trees.
// It returns true only when the leftmost leaf is exactly targetName.
// On success every sub-expression is compiled and followed by APPEND_LOCAL_STRING(slot).
func (c *Compiler) emitStringAppendChain(slot byte, targetName string, expr ast.Expr, line int) bool {
	bin, ok := expr.(*ast.BinaryExpr)
	if !ok || bin.Operator.Type != token.Plus {
		return false
	}
	// Base case: targetName + right
	if leftVar, lok := bin.Left.(*ast.VariableExpr); lok && leftVar.Name.Lexeme == targetName {
		if err := c.compileExpr(bin.Right); err != nil {
			return false
		}
		c.emit(bytecode.OpAppendLocalString, line)
		c.emitByte(slot, line)
		return true
	}
	// Recursive case: (deeper_chain) + right
	if c.emitStringAppendChain(slot, targetName, bin.Left, line) {
		if err := c.compileExpr(bin.Right); err != nil {
			return false
		}
		c.emit(bytecode.OpAppendLocalString, line)
		c.emitByte(slot, line)
		return true
	}
	return false
}

func (c *Compiler) emitFastAddConstLocalAssign(targetSlot byte, node *ast.AssignStmt, targetType string) bool {
	if rootTypeName(targetType) != bvmruntime.TypeInt && rootTypeName(targetType) != bvmruntime.TypeNumber {
		return false
	}
	var constExpr ast.Expr
	negate := false
	if node.Operator.Type == token.PlusEqual {
		constExpr = node.Value
	} else if node.Operator.Type == token.MinusEqual {
		constExpr = node.Value
		negate = true
	} else if node.Operator.Type == token.Equal {
		addExpr, ok := node.Value.(*ast.BinaryExpr)
		if !ok {
			return false
		}
		if addExpr.Operator.Type == token.Plus {
			if leftVar, ok := addExpr.Left.(*ast.VariableExpr); ok && leftVar.Name.Lexeme == node.Name.Lexeme {
				constExpr = addExpr.Right
			} else if rightVar, ok := addExpr.Right.(*ast.VariableExpr); ok && rightVar.Name.Lexeme == node.Name.Lexeme {
				constExpr = addExpr.Left
			} else {
				return false
			}
		} else if addExpr.Operator.Type == token.Minus {
			if leftVar, ok := addExpr.Left.(*ast.VariableExpr); ok && leftVar.Name.Lexeme == node.Name.Lexeme {
				constExpr = addExpr.Right
				negate = true
			} else {
				return false
			}
		} else {
			return false
		}
	} else {
		return false
	}
	constantValue, ok := c.evalConstExpr(constExpr)
	if !ok || constantValue.Kind != value.Number || constantValue.NumberKind != value.NumberInt {
		return false
	}
	intVal := constantValue.Int
	if negate {
		intVal = -intVal
	}
	// Fast path: ±1 increments get the compact 2-byte INC_LOCAL / DEC_LOCAL
	if intVal == 1 {
		c.emit(bytecode.OpIncLocal, node.Name.Line)
		c.emitByte(targetSlot, node.Name.Line)
		return true
	}
	if intVal == -1 {
		c.emit(bytecode.OpDecLocal, node.Name.Line)
		c.emitByte(targetSlot, node.Name.Line)
		return true
	}
	idx := c.constant(intVal)
	c.emit(bytecode.OpAddConstLocalInt, node.Name.Line)
	c.emitByte(targetSlot, node.Name.Line)
	c.emitUint16(idx, node.Name.Line)
	return true
}

// emitFastAddLocalLocal emits OpAddLocalLocal for `dst += src` or `dst = dst + src`
// where both are int locals.
func (c *Compiler) emitFastAddLocalLocal(targetSlot byte, node *ast.AssignStmt, targetType string) bool {
	if rootTypeName(targetType) != bvmruntime.TypeInt {
		return false
	}
	var srcExpr ast.Expr
	if node.Operator.Type == token.PlusEqual {
		srcExpr = node.Value
	} else if node.Operator.Type == token.Equal {
		addExpr, ok := node.Value.(*ast.BinaryExpr)
		if !ok || addExpr.Operator.Type != token.Plus {
			return false
		}
		if leftVar, ok := addExpr.Left.(*ast.VariableExpr); ok && leftVar.Name.Lexeme == node.Name.Lexeme {
			srcExpr = addExpr.Right
		} else if rightVar, ok := addExpr.Right.(*ast.VariableExpr); ok && rightVar.Name.Lexeme == node.Name.Lexeme {
			srcExpr = addExpr.Left
		} else {
			return false
		}
	} else {
		return false
	}
	srcVar, ok := srcExpr.(*ast.VariableExpr)
	if !ok {
		return false
	}
	srcSlot, ok := c.resolveLocal(srcVar.Name.Lexeme)
	if !ok {
		return false
	}
	if rootTypeName(c.localType(srcSlot)) != bvmruntime.TypeInt {
		return false
	}
	c.emit(bytecode.OpAddLocalLocal, node.Name.Line)
	c.emitByte(targetSlot, node.Name.Line)
	c.emitByte(srcSlot, node.Name.Line)
	return true
}

func constantCallableObject(candidate value.Value) (any, bool) {
	if candidate.Kind != value.Object || candidate.Object == nil {
		return nil, false
	}
	switch callable := candidate.Object.(type) {
	case *bytecode.Function, *value.Builtin, *value.Class, *value.Closure:
		return callable, true
	default:
		return nil, false
	}
}

func (c *Compiler) compileFastIfStmt(node *ast.IfStmt) bool {
	if _, _, ok := unwrapInstanceOfExprAndGuard(node.Condition); ok {
		return false
	}
	if condition, ok := unwrapInstanceOfExpr(node.Condition); ok && condition.Binding != nil {
		return false
	}
	if slot, constValue, ok := c.matchLocalLessEqualIntConstCondition(node.Condition); ok {
		jumpIfFalse := c.emitJumpLocalConst(bytecode.OpJumpIfLocalLessEqualIntConstFalse, slot, constValue, 0)
		if err := c.compileBlock(node.Then); err != nil {
			return false
		}
		if node.Else != nil {
			jumpOverElse := c.emitJump(bytecode.OpJump, 0)
			c.patchJump(jumpIfFalse)
			if err := c.compileBlock(node.Else); err != nil {
				return false
			}
			c.patchJump(jumpOverElse)
		} else {
			c.patchJump(jumpIfFalse)
		}
		return true
	}
	if slot, divisor, ok := c.matchLocalDivisibleByIntConstCondition(node.Condition); ok {
		jumpIfFalse := c.emitJumpLocalConst(bytecode.OpJumpIfLocalDivisibleByIntConstFalse, slot, divisor, 0)
		if err := c.compileBlock(node.Then); err != nil {
			return false
		}
		if node.Else != nil {
			jumpOverElse := c.emitJump(bytecode.OpJump, 0)
			c.patchJump(jumpIfFalse)
			if err := c.compileBlock(node.Else); err != nil {
				return false
			}
			c.patchJump(jumpOverElse)
		} else {
			c.patchJump(jumpIfFalse)
		}
		return true
	}
	// Pattern: if const_string in local_var:
	if haystackSlot, needle, ok := c.matchContainsStringConstCondition(node.Condition); ok {
		jumpIfFalse := c.emitJumpIfNotContainsStringConst(haystackSlot, needle, 0)
		if err := c.compileBlock(node.Then); err != nil {
			return false
		}
		if node.Else != nil {
			jumpOverElse := c.emitJump(bytecode.OpJump, 0)
			c.patchJump(jumpIfFalse)
			if err := c.compileBlock(node.Else); err != nil {
				return false
			}
			c.patchJump(jumpOverElse)
		} else {
			// No else branch: jump directly to the instruction after the then block.
			c.patchJump(jumpIfFalse)
		}
		return true
	}
	return false
}

func (c *Compiler) emitJumpLocalConst(op bytecode.Op, slot byte, constant int64, line int) int {
	c.emit(op, line)
	c.emitByte(slot, line)
	c.emitUint16(c.constant(constant), line)
	jumpOffset := c.state.chunk.WriteUint16(0, line)
	return jumpOffset - 1
}

func (c *Compiler) matchLocalLessEqualIntConstCondition(expr ast.Expr) (byte, int64, bool) {
	binaryExpr, ok := unwrapGrouping(expr).(*ast.BinaryExpr)
	if !ok || binaryExpr.Operator.Type != token.LessEqual {
		return 0, 0, false
	}
	variable, ok := unwrapGrouping(binaryExpr.Left).(*ast.VariableExpr)
	if !ok {
		return 0, 0, false
	}
	slot, ok := c.resolveLocal(variable.Name.Lexeme)
	if !ok {
		return 0, 0, false
	}
	constantValue, ok := c.evalConstExpr(binaryExpr.Right)
	if !ok || constantValue.Kind != value.Number || constantValue.NumberKind != value.NumberInt {
		return 0, 0, false
	}
	return slot, int64(constantValue.Num), true
}

func (c *Compiler) matchLocalDivisibleByIntConstCondition(expr ast.Expr) (byte, int64, bool) {
	binaryExpr, ok := unwrapGrouping(expr).(*ast.BinaryExpr)
	if !ok || binaryExpr.Operator.Type != token.EqualEqual {
		return 0, 0, false
	}
	modExpr := unwrapGrouping(binaryExpr.Left)
	zeroExpr := unwrapGrouping(binaryExpr.Right)
	if zeroValue, ok := c.evalConstExpr(zeroExpr); !ok || zeroValue.Kind != value.Number || zeroValue.Num != 0 {
		modExpr = unwrapGrouping(binaryExpr.Right)
		zeroExpr = unwrapGrouping(binaryExpr.Left)
		zeroValue, ok := c.evalConstExpr(zeroExpr)
		if !ok || zeroValue.Kind != value.Number || zeroValue.Num != 0 {
			return 0, 0, false
		}
	}
	modBinary, ok := modExpr.(*ast.BinaryExpr)
	if !ok || modBinary.Operator.Type != token.Percent {
		return 0, 0, false
	}
	variable, ok := unwrapGrouping(modBinary.Left).(*ast.VariableExpr)
	if !ok {
		return 0, 0, false
	}
	slot, ok := c.resolveLocal(variable.Name.Lexeme)
	if !ok {
		return 0, 0, false
	}
	constantValue, ok := c.evalConstExpr(modBinary.Right)
	if !ok || constantValue.Kind != value.Number || constantValue.NumberKind != value.NumberInt {
		return 0, 0, false
	}
	return slot, int64(constantValue.Num), true
}

// matchContainsStringConstCondition detects `needle_const in haystack_local`.
// Returns (haystackSlot, needle, true) when matched.
func (c *Compiler) matchContainsStringConstCondition(expr ast.Expr) (byte, string, bool) {
	bin, ok := unwrapGrouping(expr).(*ast.BinaryExpr)
	if !ok || bin.Operator.Type != token.In {
		return 0, "", false
	}
	needleVal, ok := c.evalConstExpr(bin.Left)
	if !ok || needleVal.Kind != value.String {
		return 0, "", false
	}
	haystackVar, ok := unwrapGrouping(bin.Right).(*ast.VariableExpr)
	if !ok {
		return 0, "", false
	}
	slot, ok := c.resolveLocal(haystackVar.Name.Lexeme)
	if !ok {
		return 0, "", false
	}
	return slot, needleVal.Str, true
}

// emitJumpIfNotContainsStringConst emits OpJumpIfNotContainsStringConst and returns
// the offset to patch with the forward jump distance.
func (c *Compiler) emitJumpIfNotContainsStringConst(haystackSlot byte, needle string, line int) int {
	c.emit(bytecode.OpJumpIfNotContainsStringConst, line)
	c.emitByte(haystackSlot, line)
	c.emitUint16(c.constant(needle), line)
	jumpOffset := c.state.chunk.WriteUint16(0, line)
	return jumpOffset - 1
}

func (c *Compiler) matchConstCallableLocalSubInt(constantValue value.Value, args []ast.Expr) (any, byte, int64, bool) {
	if len(args) != 1 {
		return nil, 0, 0, false
	}
	callableConst, ok := constantCallableObject(constantValue)
	if !ok {
		return nil, 0, 0, false
	}
	binaryExpr, ok := unwrapGrouping(args[0]).(*ast.BinaryExpr)
	if !ok || binaryExpr.Operator.Type != token.Minus {
		return nil, 0, 0, false
	}
	variable, ok := unwrapGrouping(binaryExpr.Left).(*ast.VariableExpr)
	if !ok {
		return nil, 0, 0, false
	}
	slot, ok := c.resolveLocal(variable.Name.Lexeme)
	if !ok {
		return nil, 0, 0, false
	}
	constantArg, ok := c.evalConstExpr(binaryExpr.Right)
	if !ok || constantArg.Kind != value.Number || constantArg.NumberKind != value.NumberInt {
		return nil, 0, 0, false
	}
	return callableConst, slot, int64(constantArg.Num), true
}

func (c *Compiler) matchSelfCallableLocalSubInt(name string, args []ast.Expr) (byte, int64, bool) {
	if c.state == nil || c.state.name == "<script>" || c.state.name != name {
		return 0, 0, false
	}
	if len(args) != 1 {
		return 0, 0, false
	}
	binaryExpr, ok := unwrapGrouping(args[0]).(*ast.BinaryExpr)
	if !ok || binaryExpr.Operator.Type != token.Minus {
		return 0, 0, false
	}
	variable, ok := unwrapGrouping(binaryExpr.Left).(*ast.VariableExpr)
	if !ok {
		return 0, 0, false
	}
	slot, ok := c.resolveLocal(variable.Name.Lexeme)
	if !ok {
		return 0, 0, false
	}
	constantArg, ok := c.evalConstExpr(binaryExpr.Right)
	if !ok || constantArg.Kind != value.Number || constantArg.NumberKind != value.NumberInt {
		return 0, 0, false
	}
	return slot, int64(constantArg.Num), true
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

func unwrapInstanceOfExpr(expr ast.Expr) (*ast.InstanceOfExpr, bool) {
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

func unwrapInstanceOfExprAndGuard(expr ast.Expr) (*ast.InstanceOfExpr, ast.Expr, bool) {
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
	condition, ok := unwrapInstanceOfExpr(binary.Left)
	if !ok || condition.Binding == nil {
		return nil, nil, false
	}
	return condition, binary.Right, true
}

func (c *Compiler) emitCastForType(target *ast.TypeRef, line int) {
	targetType := c.typeNameFromRef(target)
	switch targetType {
	case bvmruntime.TypeInt:
		c.emit(bytecode.OpCastInt, line)
	case bvmruntime.TypeFloat:
		c.emit(bytecode.OpCastFloat, line)
	case bvmruntime.TypeNumber:
		// Int and Float already share the runtime numeric representation.
	default:
		name := c.constant(targetType)
		c.emit(bytecode.OpCastRef, line)
		c.emitUint16(name, line)
	}
}

func (c *Compiler) compileIfInstanceOf(node *ast.IfStmt, condition *ast.InstanceOfExpr, guard ast.Expr) error {
	c.beginScope()
	defer c.endScope(0)
	tempName := token.Token{Lexeme: "__instanceof_temp", Line: condition.Target.Name.Line}
	tempSlot := c.declareLocal(tempName, c.inferExprType(condition.Expr))
	if err := c.compileExpr(condition.Expr); err != nil {
		return err
	}
	c.emit(bytecode.OpSetLocal, condition.Target.Name.Line)
	c.emitByte(tempSlot, condition.Target.Name.Line)
	c.emit(bytecode.OpGetLocal, condition.Target.Name.Line)
	c.emitByte(tempSlot, condition.Target.Name.Line)
	name := c.constant(c.typeNameFromRef(condition.Target))
	c.emit(bytecode.OpMatchType, condition.Target.Name.Line)
	c.emitUint16(name, condition.Target.Name.Line)
	jumpIfFalse := c.emitJump(bytecode.OpJumpIfFalsePop, condition.Target.Name.Line)
	c.emit(bytecode.OpGetLocal, condition.Target.Name.Line)
	c.emitByte(tempSlot, condition.Target.Name.Line)
	c.emitCastForType(condition.Target, condition.Target.Name.Line)
	bindingSlot := c.declareLocal(*condition.Binding, c.typeNameFromRef(condition.Target))
	c.emit(bytecode.OpSetLocal, condition.Binding.Line)
	c.emitByte(bindingSlot, condition.Binding.Line)
	if guard != nil {
		if err := c.compileExpr(guard); err != nil {
			return err
		}
		skipThen := c.emitJump(bytecode.OpJumpIfFalsePop, condition.Target.Name.Line)
		if err := c.compileBlockStatements(node.Then); err != nil {
			return err
		}
		if node.Else != nil {
			jumpOverElse := c.emitJump(bytecode.OpJump, 0)
			c.patchJump(skipThen)
			if err := c.compileBlock(node.Else); err != nil {
				return err
			}
			c.patchJump(jumpOverElse)
		} else {
			c.patchJump(skipThen)
		}
		return nil
	}
	if err := c.compileBlockStatements(node.Then); err != nil {
		return err
	}
	if node.Else != nil {
		jumpOverElse := c.emitJump(bytecode.OpJump, 0)
		c.patchJump(jumpIfFalse)
		if err := c.compileBlock(node.Else); err != nil {
			return err
		}
		c.patchJump(jumpOverElse)
	} else {
		c.patchJump(jumpIfFalse)
	}
	return nil
}

func (c *Compiler) compileFunction(stmt *ast.FunctionStmt) (*bytecode.Function, error) {
	return c.compileCallable(stmt.Name.Lexeme, stmt.Params, stmt.ReturnType, stmt.Body, false, nil)
}

func (c *Compiler) compileCallable(name string, params []ast.Parameter, returnType *ast.TypeRef, body *ast.BlockStmt, includeThis bool, ownerClass *value.Class) (*bytecode.Function, error) {
	fnChunk := bytecode.NewChunk()
	child := &state{
		function:   &bytecode.Function{Name: name, Arity: len(params), OwnerClassName: "", ReturnType: c.typeNameFromRef(returnType), Chunk: fnChunk, ParamTypes: make([]string, len(params)), Interfaces: c.interfaceSpecs},
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
		ClassDecl: value.ClassDecl{
			Name:              stmt.Name.Lexeme,
			Superclass:        superclass,
			Implements:        make(map[string]bool),
			Permits:           make(map[string]bool),
			IsAbstract:        stmt.IsAbstract,
			IsEnum:            stmt.IsEnum,
			IsSealed:          stmt.IsSealed || stmt.IsFinal || stmt.IsEnum,
			IsRecord:          stmt.IsRecord,
			EnumOrder:         make([]string, 0, len(stmt.EnumValues)),
			Fields:            make(map[string]value.FieldDef),
			FieldOrder:        make([]string, 0),
			FieldIndex:        make(map[string]int),
			StaticFields:      make(map[string]value.FieldDef),
			MethodVisibility:  make(map[string]string),
			StaticVisibility:  make(map[string]string),
			MethodAnnotations: make(map[string][]string),
		},
		ClassRuntime: value.ClassRuntime{
			FastConstructor:       nil,
			FastMethods:           make([]*value.FastMethodPlan, 0),
			MethodOrder:           make([]string, 0),
			MethodIndex:           make(map[string]int),
			MethodTable:           make([]*bytecode.Function, 0),
			Methods:               make(map[string]*bytecode.Function),
			StaticValues:          make(map[string]value.Value),
			StaticMethods:         make(map[string]*bytecode.Function),
			MethodOverloads:       make(map[string][]*bytecode.Function),
			StaticMethodOverloads: make(map[string][]*bytecode.Function),
			SpecialMethods:        make(map[value.SpecialMethodSlot]*bytecode.Function),
		},
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
		for name, visibility := range superclass.MethodVisibility {
			classValue.MethodVisibility[name] = visibility
		}
		for name, visibility := range superclass.StaticVisibility {
			classValue.StaticVisibility[name] = visibility
		}
		for name, annotations := range superclass.MethodAnnotations {
			classValue.MethodAnnotations[name] = append([]string(nil), annotations...)
		}
		for name, overloads := range superclass.MethodOverloads {
			classValue.MethodOverloads[name] = append(classValue.MethodOverloads[name], overloads...)
		}
		for name, overloads := range superclass.StaticMethodOverloads {
			classValue.StaticMethodOverloads[name] = append(classValue.StaticMethodOverloads[name], overloads...)
		}
		if superclass.Constructor != nil {
			classValue.ConstructorOverloads = append(classValue.ConstructorOverloads, superclass.ConstructorOverloads...)
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

	instanceFieldInitializers := c.buildDynamicInstanceFieldInitializers(stmt.Fields)

	for _, field := range stmt.Fields {
		defaultValue := value.NilValue()
		if field.Value != nil {
			constantValue, ok := c.evalConstExpr(field.Value)
			if !ok {
				if field.Static {
					return nil, fmt.Errorf("static field initializer for %s must be compile-time constant", field.Name.Lexeme)
				}
			} else {
				if field.Kind == ast.VariableConst || field.Kind == ast.VariableFinal {
					constantValue = c.freezeValue(constantValue)
				}
				defaultValue = constantValue
			}
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
	hasConstructor := false

	methods := make([]ast.MethodDecl, 0, len(stmt.Methods)+1)
	for _, method := range stmt.Methods {
		if method.IsConstructor {
			hasConstructor = true
		}
		methods = append(methods, c.injectInstanceFieldInitializers(method, instanceFieldInitializers))
	}
	if !stmt.IsEnum && !hasConstructor && len(instanceFieldInitializers) > 0 {
		methods = append(methods, c.syntheticInitializerConstructor(stmt, instanceFieldInitializers))
	}

	for _, method := range methods {
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
		if method.IsNative {
			// Native method: implementation is provided by the Go runtime registry.
			// Skip bytecode compilation — just register the slot so the method is
			// addressable by the semantic checker and linter.
			continue
		}
		fn, err := c.compileCallable(method.Name.Lexeme, method.Params, method.ReturnType, method.Body, !method.Static, classValue)
		if err != nil {
			return nil, err
		}
		if !method.Static {
			switch method.Name.Lexeme {
			case "__length":
				classValue.SetSpecialMethod(value.SpecialMethodIterableLength, fn)
			case "__get":
				classValue.SetSpecialMethod(value.SpecialMethodIterableGet, fn)
				classValue.SetSpecialMethod(value.SpecialMethodIndexGet, fn)
			case "__set":
				classValue.SetSpecialMethod(value.SpecialMethodIndexSet, fn)
			case "__contains":
				classValue.SetSpecialMethod(value.SpecialMethodContains, fn)
			case "__pieces":
				classValue.SetSpecialMethod(value.SpecialMethodPieces, fn)
			case "__get_piece", "__getPiece":
				classValue.SetSpecialMethod(value.SpecialMethodGetPiece, fn)
			case "__slice":
				classValue.SetSpecialMethod(value.SpecialMethodSlice, fn)
			case "__eq":
				classValue.SetSpecialMethod(value.SpecialMethodEquals, fn)
			case "__hash":
				classValue.SetSpecialMethod(value.SpecialMethodHash, fn)
			}
		}
		for _, annotation := range method.Annotations {
			normalized := normalizeAnnotation(annotation.Name.Lexeme)
			classValue.MethodAnnotations[method.Name.Lexeme] = append(classValue.MethodAnnotations[method.Name.Lexeme], normalized)
			switch normalized {
			case "Equals":
				classValue.SetSpecialMethod(value.SpecialMethodEquals, fn)
			case "Hash":
				classValue.SetSpecialMethod(value.SpecialMethodHash, fn)
			}
		}
		// Register overloads (all same-name variants)
		if method.IsConstructor {
			classValue.ConstructorOverloads = append(classValue.ConstructorOverloads, fn)
			classValue.ConstructorVisibility = string(method.Visibility)
			classValue.Constructor = fn
			continue
		}
		if method.Static {
			classValue.StaticMethods[method.Name.Lexeme] = fn
			classValue.StaticVisibility[method.Name.Lexeme] = string(method.Visibility)
			classValue.StaticMethodOverloads[method.Name.Lexeme] = append(classValue.StaticMethodOverloads[method.Name.Lexeme], fn)
			continue
		}
		classValue.MethodVisibility[method.Name.Lexeme] = string(method.Visibility)
		classValue.MethodOverloads[method.Name.Lexeme] = replaceOrAppendOverload(classValue.MethodOverloads[method.Name.Lexeme], fn)
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

// replaceOrAppendOverload inserts fn into the overload list, replacing any existing
// entry with the same arity and parameter types (virtual override from a parent class).
func replaceOrAppendOverload(overloads []*bytecode.Function, fn *bytecode.Function) []*bytecode.Function {
	for i, existing := range overloads {
		if existing != nil && existing.Arity == fn.Arity && paramTypesEqual(existing.ParamTypes, fn.ParamTypes) {
			overloads[i] = fn
			return overloads
		}
	}
	return append(overloads, fn)
}

func paramTypesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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
	plan := &value.FastMethodPlan{Arity: 0, Expr: expr}
	plan.CompileOps()
	return plan
}

func (c *Compiler) buildDynamicInstanceFieldInitializers(fields []ast.FieldDecl) []ast.Stmt {
	initializers := make([]ast.Stmt, 0)
	for _, field := range fields {
		if field.Static || field.Value == nil {
			continue
		}
		if _, ok := c.evalConstExpr(field.Value); ok {
			continue
		}
		initializers = append(initializers, &ast.SetStmt{
			Object:   &ast.ThisExpr{Keyword: token.Token{Type: token.This, Lexeme: "this", Line: field.Name.Line, Column: field.Name.Column}},
			Name:     field.Name,
			Operator: token.Token{Type: token.Equal, Lexeme: "=", Line: field.Name.Line, Column: field.Name.Column},
			Value:    field.Value,
		})
	}
	return initializers
}

func (c *Compiler) injectInstanceFieldInitializers(method ast.MethodDecl, initializers []ast.Stmt) ast.MethodDecl {
	if !method.IsConstructor || len(initializers) == 0 {
		return method
	}
	statements := make([]ast.Stmt, 0, len(initializers)+len(method.Body.Statements))
	if len(method.Body.Statements) > 0 && isSuperConstructorCall(method.Body.Statements[0]) {
		statements = append(statements, method.Body.Statements[0])
		statements = append(statements, initializers...)
		statements = append(statements, method.Body.Statements[1:]...)
	} else {
		statements = append(statements, initializers...)
		statements = append(statements, method.Body.Statements...)
	}
	method.Body = &ast.BlockStmt{Statements: statements}
	return method
}

func (c *Compiler) syntheticInitializerConstructor(stmt *ast.ClassStmt, initializers []ast.Stmt) ast.MethodDecl {
	statements := make([]ast.Stmt, len(initializers))
	copy(statements, initializers)
	return ast.MethodDecl{
		Name:          stmt.Name,
		Body:          &ast.BlockStmt{Statements: statements},
		IsConstructor: true,
		Visibility:    ast.VisibilityPublic,
	}
}

func isSuperConstructorCall(stmt ast.Stmt) bool {
	exprStmt, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return false
	}
	callExpr, ok := exprStmt.Expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	_, ok = callExpr.Callee.(*ast.SuperExpr)
	return ok
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
	// A superclass is fine as long as no ancestor declares a constructor:
	// the statement scan below guarantees the body is only `this.f = param`
	// assignments (so no super(...) call is being skipped), and NewInstance
	// applies field defaults for the whole chain. Only an ancestor
	// constructor body would make the fast path observably different.
	for super := classValue.Superclass; super != nil; super = super.Superclass {
		if super.Constructor != nil || len(super.ConstructorOverloads) > 0 {
			return nil
		}
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
		case token.StarStar, token.Caret:
			if left.Kind == value.Number && right.Kind == value.Number {
				return foldNumericBinary(token.StarStar, left, right), true
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
		case token.StarStar, token.Caret:
			if left.Kind == value.Number && right.Kind == value.Number {
				return foldNumericBinary(token.StarStar, left, right), true
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
	case *ast.ArrayExpr:
		elements := make([]value.Value, len(node.Elements))
		for i, el := range node.Elements {
			val, ok := c.evalConstExpr(el)
			if !ok {
				return value.NilValue(), false
			}
			elements[i] = val
		}
		return value.ObjectValue(&value.Array{Elements: elements}), true
	case *ast.MapExpr:
		entries := make(map[string]value.Value, len(node.Entries))
		for _, entry := range node.Entries {
			val, ok := c.evalConstExpr(entry.Value)
			if !ok {
				return value.NilValue(), false
			}
			entries[entry.Key] = val
		}
		return value.ObjectValue(&value.Map{Entries: entries}), true
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
		if field.Value != nil {
			return c.inferExprType(field.Value)
		}
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
	case *ast.InstanceOfExpr:
		return bvmruntime.TypeBool
	case *ast.GroupingExpr:
		return c.inferExprType(node.Expr)
	case *ast.TupleExpr:
		return bvmruntime.TypeTuple
	case *ast.ArrayExpr:
		return bvmruntime.TypeArray
	case *ast.ArrayComprehensionExpr:
		return bvmruntime.TypeArray
	case *ast.ArrayNewExpr:
		// Propagate element type so for-loop variables inherit it (e.g. new Shape[N] → array<Shape>)
		if node.Type != nil && node.Type.Name.Lexeme != "" && len(node.Type.Args) == 0 {
			return "array<" + node.Type.Name.Lexeme + ">"
		}
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
		if c.state.ownerClass != nil {
			return c.state.ownerClass.Name
		}
		return ""
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
		if elemType := c.inferIterableElementType(c.inferExprType(node.Object)); elemType != "" {
			return elemType
		}
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
			// Look up the return type from the pre-collected function signatures
			if sig, ok := c.functionSigs[callee.Name.Lexeme]; ok && sig.ret != "" {
				return sig.ret
			}
		}
		return ""
	case *ast.UnaryExpr:
		if node.Operator.Type == token.Minus {
			return c.inferExprType(node.Right)
		}
		if node.Operator.Type == token.Bang {
			return bvmruntime.TypeBool
		}
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
		case token.Minus, token.Star, token.StarStar, token.Caret, token.Slash, token.Percent:
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
	case token.StarStar, token.Caret:
		result := math.Pow(left.Num, right.Num)
		if left.NumberKind == value.NumberInt && right.NumberKind == value.NumberInt && right.Num >= 0 {
			return value.IntValue(int64(result))
		}
		return value.FloatValue(result)
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
	return len(call.Arguments) >= 1 && len(call.Arguments) <= 3
}

// compileArrayComprehension compiles `[expr for var in iterable]` by keeping
// the result array on the stack for the whole loop: each iteration evaluates
// the element and appends it with OP_ARRAY_PUSH (which leaves the array on
// top again), so the comprehension is a well-balanced expression.
func (c *Compiler) compileArrayComprehension(node *ast.ArrayComprehensionExpr) error {
	line := node.For.Line
	c.emit(bytecode.OpArray, line)
	c.emitByte(0, line)
	c.beginScope()
	iterName := token.Token{Lexeme: "__comp_iter_" + node.Variable.Lexeme, Line: line}
	iterSlot := c.declareLocal(iterName, "Iterator")
	elemType := c.inferIterableElementType(c.inferExprType(node.Iterable))
	varSlot := c.declareLocal(node.Variable, elemType)
	if err := c.compileExpr(node.Iterable); err != nil {
		c.endScope(line)
		return err
	}
	c.emit(bytecode.OpIterInit, line)
	c.emitByte(iterSlot, line)
	c.emitByte(0, line)
	loopStart := len(c.state.chunk.Code)
	exitJump := c.emitIterNext(iterSlot, varSlot, line)
	if err := c.compileExpr(node.Element); err != nil {
		c.endScope(line)
		return err
	}
	c.emit(bytecode.OpArrayPush, line)
	c.emitLoop(loopStart, line)
	c.patchJump(exitJump)
	c.endScope(line)
	return nil
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
	c.loops = append(c.loops, loopContext{scopeDepth: c.state.depth, continueTarget: loopStart, continueBackward: true})
	exitJump := c.emitRangeNextFast(currentSlot, endSlot, stepSlot, loopVar, anchor.Line)
	if node.Condition != nil {
		if condition, guard, ok := unwrapInstanceOfExprAndGuard(node.Condition); ok {
			tempName := token.Token{Lexeme: "__where_instanceof_temp", Line: condition.Target.Name.Line}
			tempSlot := c.declareLocal(tempName, c.inferExprType(condition.Expr))
			bindingSlot := c.declareLocal(*condition.Binding, c.typeNameFromRef(condition.Target))
			if err := c.compileExpr(condition.Expr); err != nil {
				return err
			}
			c.emit(bytecode.OpSetLocal, condition.Target.Name.Line)
			c.emitByte(tempSlot, condition.Target.Name.Line)
			c.emit(bytecode.OpGetLocal, condition.Target.Name.Line)
			c.emitByte(tempSlot, condition.Target.Name.Line)
			name := c.constant(c.typeNameFromRef(condition.Target))
			c.emit(bytecode.OpMatchType, condition.Target.Name.Line)
			c.emitUint16(name, condition.Target.Name.Line)
			skipBody := c.emitJump(bytecode.OpJumpIfFalsePop, anchor.Line)
			c.emit(bytecode.OpGetLocal, condition.Target.Name.Line)
			c.emitByte(tempSlot, condition.Target.Name.Line)
			c.emitCastForType(condition.Target, condition.Target.Name.Line)
			c.emit(bytecode.OpSetLocal, condition.Binding.Line)
			c.emitByte(bindingSlot, condition.Binding.Line)
			if err := c.compileExpr(guard); err != nil {
				return err
			}
			skipByGuard := c.emitJump(bytecode.OpJumpIfFalsePop, anchor.Line)
			if err := c.compileBlock(node.Body); err != nil {
				return err
			}
			c.emitLoop(loopStart, anchor.Line)
			c.patchJump(skipByGuard)
			c.emitLoop(loopStart, anchor.Line)
			c.patchJump(skipBody)
			c.emitLoop(loopStart, anchor.Line)
		} else if condition, ok := unwrapInstanceOfExpr(node.Condition); ok && condition.Binding != nil {
			tempName := token.Token{Lexeme: "__where_instanceof_temp", Line: condition.Target.Name.Line}
			tempSlot := c.declareLocal(tempName, c.inferExprType(condition.Expr))
			bindingSlot := c.declareLocal(*condition.Binding, c.typeNameFromRef(condition.Target))
			if err := c.compileExpr(condition.Expr); err != nil {
				return err
			}
			c.emit(bytecode.OpSetLocal, condition.Target.Name.Line)
			c.emitByte(tempSlot, condition.Target.Name.Line)
			c.emit(bytecode.OpGetLocal, condition.Target.Name.Line)
			c.emitByte(tempSlot, condition.Target.Name.Line)
			name := c.constant(c.typeNameFromRef(condition.Target))
			c.emit(bytecode.OpMatchType, condition.Target.Name.Line)
			c.emitUint16(name, condition.Target.Name.Line)
			skipBody := c.emitJump(bytecode.OpJumpIfFalsePop, anchor.Line)
			c.emit(bytecode.OpGetLocal, condition.Target.Name.Line)
			c.emitByte(tempSlot, condition.Target.Name.Line)
			c.emitCastForType(condition.Target, condition.Target.Name.Line)
			c.emit(bytecode.OpSetLocal, condition.Binding.Line)
			c.emitByte(bindingSlot, condition.Binding.Line)
			if err := c.compileBlock(node.Body); err != nil {
				return err
			}
			c.emitLoop(loopStart, anchor.Line)
			c.patchJump(skipBody)
			c.emitLoop(loopStart, anchor.Line)
		} else {
			if err := c.compileExpr(node.Condition); err != nil {
				return err
			}
			skipBody := c.emitJump(bytecode.OpJumpIfFalsePop, anchor.Line)
			if err := c.compileBlock(node.Body); err != nil {
				return err
			}
			c.emitLoop(loopStart, anchor.Line)
			c.patchJump(skipBody)
			c.emitLoop(loopStart, anchor.Line)
		}
	} else {
		if err := c.compileBlock(node.Body); err != nil {
			return err
		}
		c.emitLoop(loopStart, anchor.Line)
	}
	c.patchJump(exitJump)
	c.patchLoopBreaks()
	c.loops = c.loops[:len(c.loops)-1]
	c.endScope(anchor.Line)
	return nil
}

func (c *Compiler) compileLoop(node *ast.LoopStmt) error {
	loopStart := len(c.state.chunk.Code)
	if node.Condition != nil {
		if err := c.compileExpr(node.Condition); err != nil {
			return err
		}
		exitJump := c.emitJump(bytecode.OpJumpIfFalsePop, node.Keyword.Line)
		c.loops = append(c.loops, loopContext{scopeDepth: c.state.depth, continueTarget: loopStart, continueBackward: true})
		if err := c.compileBlock(node.Body); err != nil {
			return err
		}
		c.emitLoop(loopStart, node.Keyword.Line)
		c.patchJump(exitJump)
		c.patchLoopBreaks()
		c.loops = c.loops[:len(c.loops)-1]
		return nil
	}
	c.loops = append(c.loops, loopContext{scopeDepth: c.state.depth, continueTarget: loopStart, continueBackward: true})
	if err := c.compileBlock(node.Body); err != nil {
		return err
	}
	c.emitLoop(loopStart, node.Keyword.Line)
	c.patchLoopBreaks()
	c.loops = c.loops[:len(c.loops)-1]
	return nil
}

func (c *Compiler) compileDoLoop(node *ast.LoopStmt) error {
	loopStart := len(c.state.chunk.Code)
	c.loops = append(c.loops, loopContext{scopeDepth: c.state.depth})
	if err := c.compileBlock(node.Body); err != nil {
		return err
	}
	ctx := &c.loops[len(c.loops)-1]
	ctx.continueTarget = len(c.state.chunk.Code)
	ctx.continueBackward = false
	c.patchLoopContinues()
	if node.Condition != nil {
		if err := c.compileExpr(node.Condition); err != nil {
			return err
		}
		exitJump := c.emitJump(bytecode.OpJumpIfFalsePop, node.Keyword.Line)
		c.emitLoop(loopStart, node.Keyword.Line)
		c.patchJump(exitJump)
	} else {
		c.emitLoop(loopStart, node.Keyword.Line)
	}
	c.patchLoopBreaks()
	c.loops = c.loops[:len(c.loops)-1]
	return nil
}

func (c *Compiler) emitLoopScopePops(scopeDepth int, line int) {
	_ = scopeDepth
	_ = line
}

func (c *Compiler) patchLoopBreaks() {
	if len(c.loops) == 0 {
		return
	}
	ctx := &c.loops[len(c.loops)-1]
	for _, jump := range ctx.breakJumps {
		c.patchJump(jump)
	}
	ctx.breakJumps = nil
}

func (c *Compiler) patchLoopContinues() {
	if len(c.loops) == 0 {
		return
	}
	ctx := &c.loops[len(c.loops)-1]
	for _, jump := range ctx.continueJumps {
		c.patchJump(jump)
	}
	ctx.continueJumps = nil
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

func (c *Compiler) inferIterableElementType(iterableType string) string {
	const prefix = "array<"
	if strings.HasPrefix(iterableType, prefix) && strings.HasSuffix(iterableType, ">") {
		return iterableType[len(prefix) : len(iterableType)-1]
	}
	return ""
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

func (c *Compiler) compileCallArgument(arg ast.Expr, expectedParams []string, index int, line int) error {
	if err := c.compileExpr(arg); err != nil {
		return err
	}
	if index >= len(expectedParams) {
		return nil
	}
	expectedType := expectedParams[index]
	if err := c.maybeWrapInterfaceName(expectedType, arg, line); err != nil {
		return err
	}
	// Skip redundant runtime type check when inferred argument type already matches expected.
	argType := normalizeRuntimeTypeAlias(rootRuntimeTypeName(c.inferExprType(arg)))
	normExpected := normalizeRuntimeTypeAlias(rootRuntimeTypeName(expectedType))
	if argType != "" && argType != bvmruntime.TypeAny && argType == normExpected {
		return nil
	}
	return c.emitRuntimeTypeCheck(expectedType, line)
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

func (c *Compiler) expectedParamTypesForCall(call *ast.CallExpr) []string {
	if call != nil && len(call.TypeArgs) > 0 {
		if params := c.explicitGenericParamTypes(call); len(params) > 0 {
			return params
		}
	}
	if call == nil {
		return nil
	}
	callee := call.Callee
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

func (c *Compiler) explicitGenericParamTypes(call *ast.CallExpr) []string {
	if call == nil || len(call.TypeArgs) == 0 {
		return nil
	}
	switch callee := call.Callee.(type) {
	case *ast.VariableExpr:
		if fn, ok := c.functionDecls[callee.Name.Lexeme]; ok {
			return c.specializedParamTypesFromFunction(fn, call.TypeArgs)
		}
		if classDecl, ok := c.classDecls[callee.Name.Lexeme]; ok {
			return c.specializedParamTypesFromClass(classDecl, call.TypeArgs, len(call.Arguments))
		}
	}
	return nil
}

func (c *Compiler) specializedParamTypesFromFunction(fn *ast.FunctionStmt, typeArgs []*ast.TypeRef) []string {
	if fn == nil || len(fn.TypeParams) == 0 || len(fn.TypeParams) != len(typeArgs) {
		return nil
	}
	mapping := make(map[string]*ast.TypeRef, len(typeArgs))
	for i, param := range fn.TypeParams {
		mapping[param.Name.Lexeme] = typeArgs[i]
	}
	params := make([]string, len(fn.Params))
	for i, param := range fn.Params {
		if param.Type == nil {
			params[i] = bvmruntime.TypeAny
			continue
		}
		params[i] = c.typeNameFromRef(substituteTypeRefParams(param.Type, mapping))
	}
	return params
}

func (c *Compiler) specializedParamTypesFromClass(classDecl *ast.ClassStmt, typeArgs []*ast.TypeRef, arity int) []string {
	if classDecl == nil || len(classDecl.TypeParams) == 0 || len(classDecl.TypeParams) != len(typeArgs) {
		return nil
	}
	mapping := make(map[string]*ast.TypeRef, len(typeArgs))
	for i, param := range classDecl.TypeParams {
		mapping[param.Name.Lexeme] = typeArgs[i]
	}
	for _, method := range classDecl.Methods {
		if !method.IsConstructor || len(method.Params) != arity {
			continue
		}
		params := make([]string, len(method.Params))
		for i, param := range method.Params {
			if param.Type == nil {
				params[i] = bvmruntime.TypeAny
				continue
			}
			params[i] = c.typeNameFromRef(substituteTypeRefParams(param.Type, mapping))
		}
		return params
	}
	return nil
}

func substituteTypeRefParams(typeRef *ast.TypeRef, mapping map[string]*ast.TypeRef) *ast.TypeRef {
	if typeRef == nil {
		return nil
	}
	if replacement, ok := mapping[typeRef.Name.Lexeme]; ok && !typeRef.Wildcard && len(typeRef.Args) == 0 && len(typeRef.Union) == 0 {
		return replacement
	}
	cloned := &ast.TypeRef{Name: typeRef.Name, Wildcard: typeRef.Wildcard, BoundKind: typeRef.BoundKind}
	if typeRef.Bound != nil {
		cloned.Bound = substituteTypeRefParams(typeRef.Bound, mapping)
	}
	if len(typeRef.Args) > 0 {
		cloned.Args = make([]*ast.TypeRef, len(typeRef.Args))
		for i, arg := range typeRef.Args {
			cloned.Args[i] = substituteTypeRefParams(arg, mapping)
		}
	}
	if len(typeRef.Union) > 0 {
		cloned.Union = make([]*ast.TypeRef, len(typeRef.Union))
		for i, option := range typeRef.Union {
			cloned.Union[i] = substituteTypeRefParams(option, mapping)
		}
	}
	return cloned
}

func (c *Compiler) emitRuntimeTypeCheck(typeName string, line int) error {
	typeName = normalizeRuntimeTypeAlias(rootRuntimeTypeName(typeName))
	if typeName == "" || typeName == bvmruntime.TypeAny || isRuntimeGenericTypeParam(typeName) {
		return nil
	}
	switch typeName {
	case bvmruntime.TypeInt:
		c.emit(bytecode.OpCastInt, line)
	case bvmruntime.TypeFloat:
		c.emit(bytecode.OpCastFloat, line)
	case bvmruntime.TypeNumber:
		return nil
	default:
		name := c.constant(typeName)
		c.emit(bytecode.OpCastRef, line)
		c.emitUint16(name, line)
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
		typeParams := make([]string, len(fn.TypeParams))
		for i, param := range fn.TypeParams {
			typeParams[i] = param.Name.Lexeme
		}
		sigs[fn.Name.Lexeme] = callableSignature{typeParams: typeParams, params: params, ret: ret}
	}
	return sigs
}

func collectFunctionDeclarations(program *ast.Program) map[string]*ast.FunctionStmt {
	decls := make(map[string]*ast.FunctionStmt)
	for _, stmt := range program.Statements {
		if fn, ok := stmt.(*ast.FunctionStmt); ok {
			decls[fn.Name.Lexeme] = fn
		}
	}
	return decls
}

func collectClassDeclarations(program *ast.Program) map[string]*ast.ClassStmt {
	decls := make(map[string]*ast.ClassStmt)
	for _, stmt := range program.Statements {
		if classDecl, ok := stmt.(*ast.ClassStmt); ok {
			decls[classDecl.Name.Lexeme] = classDecl
		}
	}
	return decls
}

func normalizeTypeName(name string) string {
	switch name {
	case "int":
		return bvmruntime.TypeInt
	case "Int":
		return bvmruntime.TypeInt
	case "float":
		return bvmruntime.TypeFloat
	case "Float":
		return bvmruntime.TypeFloat
	case "number":
		return bvmruntime.TypeNumber
	case "Number":
		return bvmruntime.TypeNumber
	case "bool":
		return bvmruntime.TypeBool
	case "Bool":
		return bvmruntime.TypeBool
	case "char":
		return bvmruntime.TypeChar
	case "Char":
		return bvmruntime.TypeChar
	case "String":
		return bvmruntime.TypeString
	case "string":
		return bvmruntime.TypeString
	case "Array":
		return bvmruntime.TypeArray
	case "array":
		return bvmruntime.TypeArray
	case "Map":
		return bvmruntime.TypeMap
	case "map":
		return bvmruntime.TypeMap
	case "Tuple":
		return bvmruntime.TypeTuple
	case "tuple":
		return bvmruntime.TypeTuple
	case "Range":
		return bvmruntime.TypeRange
	case "range":
		return bvmruntime.TypeRange
	case "Function":
		return bvmruntime.TypeFunction
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

func trimTypeSpace(input string) string {
	return strings.TrimSpace(input)
}

func parseGenericType(typeName string) (string, []string) {
	typeName = trimTypeSpace(typeName)
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
	return trimTypeSpace(typeName[:start]), splitTopLevelTypeList(typeName[start+1:len(typeName)-1], ',')
}

func splitTopLevelTypeList(input string, separator rune) []string {
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
				parts = append(parts, trimTypeSpace(input[start:i]))
				start = i + 1
			}
		}
	}
	parts = append(parts, trimTypeSpace(input[start:]))
	return parts
}

func rootRuntimeTypeName(typeName string) string {
	base, _ := parseGenericType(typeName)
	base = trimTypeSpace(base)
	if dot := strings.LastIndex(base, "."); dot >= 0 && dot+1 < len(base) {
		base = base[dot+1:]
	}
	return normalizeRuntimeTypeAlias(base)
}

func normalizeRuntimeTypeAlias(typeName string) string {
	switch trimTypeSpace(typeName) {
	case "Int":
		return bvmruntime.TypeInt
	case "Float":
		return bvmruntime.TypeFloat
	case "Number":
		return bvmruntime.TypeNumber
	case "Bool":
		return bvmruntime.TypeBool
	case "Char":
		return bvmruntime.TypeChar
	case "String", "string":
		return bvmruntime.TypeString
	case "Array", "array":
		return bvmruntime.TypeArray
	case "Map", "map":
		return bvmruntime.TypeMap
	case "Tuple", "tuple":
		return bvmruntime.TypeTuple
	case "Range", "range":
		return bvmruntime.TypeRange
	case "Function":
		return bvmruntime.TypeFunction
	case "Any", "any", "":
		return bvmruntime.TypeAny
	default:
		return trimTypeSpace(typeName)
	}
}

func isRuntimeGenericTypeParam(typeName string) bool {
	trimmed := trimTypeSpace(typeName)
	if len(trimmed) == 0 || len(trimmed) > 2 {
		return false
	}
	for index, ch := range trimmed {
		if index == 0 {
			if ch < 'A' || ch > 'Z' {
				return false
			}
			continue
		}
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
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
	case *ast.LoopStmt:
		loopScope := cloneScope(scope)
		if node.Condition != nil {
			collectCapturedGlobalsExpr(node.Condition, captured, inCallable, loopScope)
		}
		collectCapturedGlobalsBlock(node.Body, captured, inCallable, loopScope)
	case *ast.TryStmt:
		collectCapturedGlobalsBlock(node.Body, captured, inCallable, cloneScope(scope))
		for _, clause := range node.Catches {
			catchScope := cloneScope(scope)
			if clause.Binding.Type != "" {
				catchScope[clause.Binding.Lexeme] = true
			}
			collectCapturedGlobalsBlock(clause.Body, captured, inCallable, catchScope)
		}
	case *ast.ThrowStmt:
		collectCapturedGlobalsExpr(node.Value, captured, inCallable, scope)
	case *ast.BreakStmt, *ast.ContinueStmt:
		return
	case *ast.BlockStmt:
		collectCapturedGlobalsBlock(node, captured, inCallable, cloneScope(scope))
	case *ast.FunctionStmt:
		innerScope := make(map[string]bool)
		for _, param := range node.Params {
			innerScope[param.Name.Lexeme] = true
		}
		if !node.IsNative && node.Body != nil {
			collectCapturedGlobalsBlock(node.Body, captured, true, innerScope)
		}
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
	case *ast.ArrayComprehensionExpr:
		collectCapturedGlobalsExpr(node.Iterable, captured, inCallable, scope)
		compScope := cloneScope(scope)
		compScope[node.Variable.Lexeme] = true
		collectCapturedGlobalsExpr(node.Element, captured, inCallable, compScope)
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
	c.state.lastOpcodeOffset = len(c.state.chunk.Code)
	c.state.chunk.WriteOp(op, line)
}

func (c *Compiler) emitByte(v byte, line int) {
	c.state.chunk.EmitByte(v, line)
}

func (c *Compiler) emitUint16(v uint16, line int) {
	c.state.chunk.WriteUint16(v, line)
}

// emitJump emits a conditional or unconditional jump instruction and returns
// the offset of the placeholder uint16 that must be patched later via patchJump.
// It applies up to three peephole optimisations before emitting:
//  1. peepholeNotFold  – NOT + JumpIfFalse[Pop] → JumpIfTrue[Pop]
//  2. peepholeArrayFieldCmp – GET_LOCAL_ARRAY_FIELD … GET_LOCAL CMP JUMP_*_POP → fused opcode
//  3. peepholeLocalLocalCmp – GET_LOCAL … GET_LOCAL CMP JUMP_*_POP → fused opcode
func (c *Compiler) emitJump(op bytecode.Op, line int) int {
	op = c.peepholeNotFold(op)
	if placeholder, ok := c.peepholeArrayFieldCmp(op, line); ok {
		return placeholder
	}
	if placeholder, ok := c.peepholeLocalLocalCmp(op, line); ok {
		return placeholder
	}
	c.emit(op, line)
	offset := len(c.state.chunk.Code)
	c.emitUint16(0, line)
	return offset
}

// peepholeNotFold folds a trailing OpNot into the jump direction:
//
//	NOT + JUMP_IF_FALSE[Pop] → JUMP_IF_TRUE[Pop]  (and vice-versa)
func (c *Compiler) peepholeNotFold(op bytecode.Op) bytecode.Op {
	if op != bytecode.OpJumpIfFalse && op != bytecode.OpJumpIfFalsePop {
		return op
	}
	code := c.state.chunk.Code
	lastOff := c.state.lastOpcodeOffset
	if lastOff >= len(code) || bytecode.Op(code[lastOff]) != bytecode.OpNot {
		return op
	}
	c.state.chunk.Code = code[:lastOff]
	c.state.chunk.Lines = c.state.chunk.Lines[:lastOff]
	if op == bytecode.OpJumpIfFalsePop {
		return bytecode.OpJumpIfTruePop
	}
	return bytecode.OpJumpIfTrue
}

// peepholeArrayFieldCmp fuses:
//
//	GET_LOCAL_ARRAY_FIELD arr idx field; GET_LOCAL cmp; LESS/GREATER_NUM; JUMP_IF_FALSE_POP
//	→ JUMP_IF_ARRAY_FIELD_{GTE,LTE}_LOCAL_TRUE arr idx field cmp offset  (7 bytes)
//
// Returns the placeholder offset and true on success, or 0 and false if the
// pattern is not present.
func (c *Compiler) peepholeArrayFieldCmp(op bytecode.Op, line int) (int, bool) {
	if op != bytecode.OpJumpIfFalsePop {
		return 0, false
	}
	code := c.state.chunk.Code
	n := len(code)
	if n < 7 {
		return 0, false
	}
	compareOp := bytecode.Op(code[n-1])
	if compareOp != bytecode.OpLessNum && compareOp != bytecode.OpGreaterNum {
		return 0, false
	}
	if bytecode.Op(code[n-7]) != bytecode.OpGetLocalArrayField || bytecode.Op(code[n-3]) != bytecode.OpGetLocal {
		return 0, false
	}
	arrSlot, idxSlot, fieldSlot, cmpSlot := code[n-6], code[n-5], code[n-4], code[n-2]
	var fusedOp bytecode.Op
	if compareOp == bytecode.OpLessNum {
		fusedOp = bytecode.OpJumpIfArrayFieldGteLocalTrue // field < cmp is false → field >= cmp
	} else {
		fusedOp = bytecode.OpJumpIfArrayFieldLteLocalTrue // field > cmp is false → field <= cmp
	}
	c.state.chunk.Code = code[:n-7]
	c.state.chunk.Lines = c.state.chunk.Lines[:n-7]
	c.emit(fusedOp, line)
	c.emitByte(arrSlot, line)
	c.emitByte(idxSlot, line)
	c.emitByte(fieldSlot, line)
	c.emitByte(cmpSlot, line)
	placeholder := len(c.state.chunk.Code)
	c.emitUint16(0, line)
	return placeholder, true
}

// peepholeLocalLocalCmp fuses:
//
//	GET_LOCAL a; GET_LOCAL b; LESS/GREATER_NUM; JUMP_IF_{TRUE,FALSE}_POP
//	→ JUMP_IF_LOCAL_{LT,GT}_LOCAL_{TRUE,FALSE} a b offset  (5 bytes)
//
// Uses len(code) rather than lastOpcodeOffset so it fires correctly even
// after peepholeNotFold has already truncated the bytecode stream.
func (c *Compiler) peepholeLocalLocalCmp(op bytecode.Op, line int) (int, bool) {
	if op != bytecode.OpJumpIfTruePop && op != bytecode.OpJumpIfFalsePop {
		return 0, false
	}
	code := c.state.chunk.Code
	n := len(code)
	if n < 5 {
		return 0, false
	}
	compareOp := bytecode.Op(code[n-1])
	if compareOp != bytecode.OpLessNum && compareOp != bytecode.OpGreaterNum {
		return 0, false
	}
	if bytecode.Op(code[n-5]) != bytecode.OpGetLocal || bytecode.Op(code[n-3]) != bytecode.OpGetLocal {
		return 0, false
	}
	slotA, slotB := code[n-4], code[n-2]
	var fusedOp bytecode.Op
	switch {
	case compareOp == bytecode.OpLessNum && op == bytecode.OpJumpIfTruePop:
		fusedOp = bytecode.OpJumpIfLocalLtLocalTrue
	case compareOp == bytecode.OpLessNum && op == bytecode.OpJumpIfFalsePop:
		fusedOp = bytecode.OpJumpIfLocalLtLocalFalse
	case compareOp == bytecode.OpGreaterNum && op == bytecode.OpJumpIfTruePop:
		fusedOp = bytecode.OpJumpIfLocalGtLocalTrue
	default: // GreaterNum + JumpIfFalsePop
		fusedOp = bytecode.OpJumpIfLocalGtLocalFalse
	}
	c.state.chunk.Code = code[:n-5]
	c.state.chunk.Lines = c.state.chunk.Lines[:n-5]
	c.emit(fusedOp, line)
	c.emitByte(slotA, line)
	c.emitByte(slotB, line)
	placeholder := len(c.state.chunk.Code)
	c.emitUint16(0, line)
	return placeholder, true
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
