package bytecode

type Op byte

const (
	OpConstant Op = iota
	OpNil
	OpTrue
	OpFalse
	OpPop
	OpDup
	OpDupTwo
	OpGetLocal
	OpSetLocal
	OpGetCapture
	OpSetCapture
	OpDefineGlobal
	OpGetGlobal
	OpSetGlobal
	OpDefineGlobalSlot
	OpGetGlobalSlot
	OpSetGlobalSlot
	OpEqual
	OpGreater
	OpLess
	OpAdd
	OpSub
	OpMul
	OpPow
	OpDiv
	OpMod
	OpNot
	OpNegate
	OpPushHandler
	OpPopHandler
	OpThrow
	OpJump
	OpJumpIfFalse
	OpJumpIfTrue
	OpLoop
	OpAddNum
	OpSubNum
	OpMulNum
	OpPowNum
	OpDivNum
	OpModNum
	OpLessNum
	OpGreaterNum
	OpAddLocalMulThisField
	OpAddLocalMulLocal
	OpAddLocalMulThisFieldAddThisField
	OpAppendLocalString
	OpClosure
	OpCall
	OpCallConst
	OpAddConstLocalInt
	OpCallConstLocalSubInt
	OpCallSelfLocalSubInt
	OpCallGlobalSlot
	OpInvoke
	OpInvokeMethod
	OpCallSuper
	OpInvokeSuper
	OpRange
	OpRangeInitFast
	OpRangeNextFast
	OpJumpIfLocalLessEqualIntConstFalse
	OpJumpIfLocalDivisibleByIntConstFalse
	OpIterInit
	OpIterNext
	OpGetField
	OpSetField
	OpGetThisField
	OpSetThisField
	OpGetProperty
	OpSetProperty
	OpArray
	OpMap
	OpGetIndex
	OpGetIndexArray
	OpGetIndexMap
	OpContains
	OpContainsArray
	OpContainsMap
	OpContainsString
	OpSetIndex
	OpSetIndexArray
	OpSetIndexMap
	OpSlice
	OpCastInt
	OpCastFloat
	OpCastRef
	OpMatchType
	OpWrapInterface
	OpTuple
	OpUnpack
	OpFreeze
	OpSwap
	OpSwapTwo
	OpReturn
	OpArrayAlloc        // pop size(int) → push array of N nils
	OpArrayFill         // pop fill, pop size(int) → push array of N copies of fill
	OpSetLocalArrayBool    // locals[arr][locals[idx]] = bool; args: arr_slot, idx_slot, bool_byte
	OpAddLocalLocal        // locals[dst] += locals[src] (int); args: dst_slot, src_slot
	OpGetLocalArrayField            // push locals[arr_slot][locals[idx_slot].Int].Fields[field_slot]; args: arr_slot, idx_slot, field_slot
	OpJumpIfNotContainsStringConst  // if !strings.Contains(locals[haystack_slot], constants[needle_idx]) jump; args: haystack_slot(1) needle_idx(2) jump(2)
	OpAddToLocal                    // locals[slot] += pop() (numeric); args: slot(1)
	OpSubToLocal                    // locals[slot] -= pop() (numeric); args: slot(1)
	OpMulToLocal                    // locals[slot] *= pop() (numeric); args: slot(1)
	OpJumpIfFalsePop                // pop TOS, then jump if it was falsy; args: offset(2)
	OpJumpIfTruePop                 // pop TOS, then jump if it was truthy; args: offset(2)
	OpIncLocal                      // locals[slot]++ (int); args: slot(1)
	OpDecLocal                      // locals[slot]-- (int); args: slot(1)
	// Fused local-vs-local comparison jumps; args: slot_a(1) slot_b(1) offset(2)
	OpJumpIfLocalGtLocalTrue        // if locals[a] > locals[b] → jump
	OpJumpIfLocalLtLocalFalse       // if locals[a] < locals[b] is false (i.e. >=) → jump
	OpJumpIfLocalGtLocalFalse       // if locals[a] > locals[b] is false (i.e. <=) → jump
	OpJumpIfLocalLtLocalTrue        // if locals[a] < locals[b] → jump
)

func (op Op) String() string {
	switch op {
	case OpConstant:
		return "CONSTANT"
	case OpNil:
		return "NIL"
	case OpTrue:
		return "TRUE"
	case OpFalse:
		return "FALSE"
	case OpPop:
		return "POP"
	case OpDup:
		return "DUP"
	case OpDupTwo:
		return "DUP_TWO"
	case OpGetLocal:
		return "GET_LOCAL"
	case OpSetLocal:
		return "SET_LOCAL"
	case OpGetCapture:
		return "GET_CAPTURE"
	case OpSetCapture:
		return "SET_CAPTURE"
	case OpDefineGlobal:
		return "DEFINE_GLOBAL"
	case OpGetGlobal:
		return "GET_GLOBAL"
	case OpSetGlobal:
		return "SET_GLOBAL"
	case OpDefineGlobalSlot:
		return "DEFINE_GLOBAL_SLOT"
	case OpGetGlobalSlot:
		return "GET_GLOBAL_SLOT"
	case OpSetGlobalSlot:
		return "SET_GLOBAL_SLOT"
	case OpEqual:
		return "EQUAL"
	case OpGreater:
		return "GREATER"
	case OpLess:
		return "LESS"
	case OpAdd:
		return "ADD"
	case OpSub:
		return "SUB"
	case OpMul:
		return "MUL"
	case OpPow:
		return "POW"
	case OpDiv:
		return "DIV"
	case OpMod:
		return "MOD"
	case OpNot:
		return "NOT"
	case OpNegate:
		return "NEGATE"
	case OpPushHandler:
		return "PUSH_HANDLER"
	case OpPopHandler:
		return "POP_HANDLER"
	case OpThrow:
		return "THROW"
	case OpJump:
		return "JUMP"
	case OpJumpIfFalse:
		return "JUMP_IF_FALSE"
	case OpJumpIfTrue:
		return "JUMP_IF_TRUE"
	case OpLoop:
		return "LOOP"
	case OpAddNum:
		return "ADD_NUM"
	case OpSubNum:
		return "SUB_NUM"
	case OpMulNum:
		return "MUL_NUM"
	case OpPowNum:
		return "POW_NUM"
	case OpDivNum:
		return "DIV_NUM"
	case OpModNum:
		return "MOD_NUM"
	case OpLessNum:
		return "LESS_NUM"
	case OpGreaterNum:
		return "GREATER_NUM"
	case OpAddLocalMulThisField:
		return "ADD_LOCAL_MUL_THIS_FIELD"
	case OpAddLocalMulLocal:
		return "ADD_LOCAL_MUL_LOCAL"
	case OpAddLocalMulThisFieldAddThisField:
		return "ADD_LOCAL_MUL_THIS_FIELD_ADD_THIS_FIELD"
	case OpAppendLocalString:
		return "APPEND_LOCAL_STRING"
	case OpClosure:
		return "CLOSURE"
	case OpCall:
		return "CALL"
	case OpCallConst:
		return "CALL_CONST"
	case OpAddConstLocalInt:
		return "ADD_CONST_LOCAL_INT"
	case OpCallConstLocalSubInt:
		return "CALL_CONST_LOCAL_SUB_INT"
	case OpCallSelfLocalSubInt:
		return "CALL_SELF_LOCAL_SUB_INT"
	case OpCallGlobalSlot:
		return "CALL_GLOBAL_SLOT"
	case OpInvoke:
		return "INVOKE"
	case OpInvokeMethod:
		return "INVOKE_METHOD"
	case OpCallSuper:
		return "CALL_SUPER"
	case OpInvokeSuper:
		return "INVOKE_SUPER"
	case OpRange:
		return "RANGE"
	case OpRangeInitFast:
		return "RANGE_INIT_FAST"
	case OpRangeNextFast:
		return "RANGE_NEXT_FAST"
	case OpJumpIfLocalLessEqualIntConstFalse:
		return "JUMP_IF_LOCAL_LE_INT_CONST_FALSE"
	case OpJumpIfLocalDivisibleByIntConstFalse:
		return "JUMP_IF_LOCAL_DIVISIBLE_INT_CONST_FALSE"
	case OpIterInit:
		return "ITER_INIT"
	case OpIterNext:
		return "ITER_NEXT"
	case OpGetField:
		return "GET_FIELD"
	case OpSetField:
		return "SET_FIELD"
	case OpGetThisField:
		return "GET_THIS_FIELD"
	case OpSetThisField:
		return "SET_THIS_FIELD"
	case OpGetProperty:
		return "GET_PROPERTY"
	case OpSetProperty:
		return "SET_PROPERTY"
	case OpArray:
		return "ARRAY"
	case OpMap:
		return "MAP"
	case OpGetIndex:
		return "GET_INDEX"
	case OpGetIndexArray:
		return "GET_INDEX_ARRAY"
	case OpGetIndexMap:
		return "GET_INDEX_MAP"
	case OpContains:
		return "CONTAINS"
	case OpContainsArray:
		return "CONTAINS_ARRAY"
	case OpContainsMap:
		return "CONTAINS_MAP"
	case OpContainsString:
		return "CONTAINS_STRING"
	case OpSetIndex:
		return "SET_INDEX"
	case OpSetIndexArray:
		return "SET_INDEX_ARRAY"
	case OpSetIndexMap:
		return "SET_INDEX_MAP"
	case OpSlice:
		return "SLICE"
	case OpCastInt:
		return "CAST_INT"
	case OpCastFloat:
		return "CAST_FLOAT"
	case OpCastRef:
		return "CAST_REF"
	case OpMatchType:
		return "MATCH_TYPE"
	case OpWrapInterface:
		return "WRAP_INTERFACE"
	case OpTuple:
		return "TUPLE"
	case OpUnpack:
		return "UNPACK"
	case OpFreeze:
		return "FREEZE"
	case OpSwap:
		return "SWAP"
	case OpSwapTwo:
		return "SWAP_TWO"
	case OpReturn:
		return "RETURN"
	case OpArrayAlloc:
		return "ARRAY_ALLOC"
	case OpArrayFill:
		return "ARRAY_FILL"
	case OpSetLocalArrayBool:
		return "SET_LOCAL_ARRAY_BOOL"
	case OpAddLocalLocal:
		return "ADD_LOCAL_LOCAL"
	case OpGetLocalArrayField:
		return "GET_LOCAL_ARRAY_FIELD"
	case OpJumpIfNotContainsStringConst:
		return "JUMP_IF_NOT_CONTAINS_STRING_CONST"
	case OpAddToLocal:
		return "ADD_TO_LOCAL"
	case OpSubToLocal:
		return "SUB_TO_LOCAL"
	case OpMulToLocal:
		return "MUL_TO_LOCAL"
	case OpJumpIfFalsePop:
		return "JUMP_IF_FALSE_POP"
	case OpJumpIfTruePop:
		return "JUMP_IF_TRUE_POP"
	case OpIncLocal:
		return "INC_LOCAL"
	case OpDecLocal:
		return "DEC_LOCAL"
	case OpJumpIfLocalGtLocalTrue:
		return "JUMP_IF_LOCAL_GT_LOCAL_TRUE"
	case OpJumpIfLocalLtLocalFalse:
		return "JUMP_IF_LOCAL_LT_LOCAL_FALSE"
	case OpJumpIfLocalGtLocalFalse:
		return "JUMP_IF_LOCAL_GT_LOCAL_FALSE"
	case OpJumpIfLocalLtLocalTrue:
		return "JUMP_IF_LOCAL_LT_LOCAL_TRUE"
	default:
		return "UNKNOWN"
	}
}
