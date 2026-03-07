package bytecode

type Op byte

const (
	OpConstant Op = iota
	OpNil
	OpTrue
	OpFalse
	OpPop
	OpGetLocal
	OpSetLocal
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
	OpDiv
	OpNot
	OpNegate
	OpJump
	OpJumpIfFalse
	OpLoop
	OpAddNum
	OpSubNum
	OpMulNum
	OpDivNum
	OpLessNum
	OpGreaterNum
	OpCall
	OpInvoke
	OpInvokeMethod
	OpCallSuper
	OpRange
	OpRangeInitFast
	OpRangeNextFast
	OpIterInit
	OpIterNext
	OpGetField
	OpSetField
	OpGetProperty
	OpSetProperty
	OpTuple
	OpUnpack
	OpFreeze
	OpReturn
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
	case OpGetLocal:
		return "GET_LOCAL"
	case OpSetLocal:
		return "SET_LOCAL"
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
	case OpDiv:
		return "DIV"
	case OpNot:
		return "NOT"
	case OpNegate:
		return "NEGATE"
	case OpJump:
		return "JUMP"
	case OpJumpIfFalse:
		return "JUMP_IF_FALSE"
	case OpLoop:
		return "LOOP"
	case OpAddNum:
		return "ADD_NUM"
	case OpSubNum:
		return "SUB_NUM"
	case OpMulNum:
		return "MUL_NUM"
	case OpDivNum:
		return "DIV_NUM"
	case OpLessNum:
		return "LESS_NUM"
	case OpGreaterNum:
		return "GREATER_NUM"
	case OpCall:
		return "CALL"
	case OpInvoke:
		return "INVOKE"
	case OpInvokeMethod:
		return "INVOKE_METHOD"
	case OpCallSuper:
		return "CALL_SUPER"
	case OpRange:
		return "RANGE"
	case OpRangeInitFast:
		return "RANGE_INIT_FAST"
	case OpRangeNextFast:
		return "RANGE_NEXT_FAST"
	case OpIterInit:
		return "ITER_INIT"
	case OpIterNext:
		return "ITER_NEXT"
	case OpGetField:
		return "GET_FIELD"
	case OpSetField:
		return "SET_FIELD"
	case OpGetProperty:
		return "GET_PROPERTY"
	case OpSetProperty:
		return "SET_PROPERTY"
	case OpTuple:
		return "TUPLE"
	case OpUnpack:
		return "UNPACK"
	case OpFreeze:
		return "FREEZE"
	case OpReturn:
		return "RETURN"
	default:
		return "UNKNOWN"
	}
}
