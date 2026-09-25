package lua

import (
	"fmt"

	"github.com/matjam/luart/internal/bytecode"
	"github.com/matjam/luart/internal/compiler"
)

func (l *State) runtimeError(message string) {
	l.push(stringValue(message))
	if ci := l.callInfo; ci.isLua() {
		line, source := l.currentLine(ci), l.prototype(ci).Source
		if source == "" {
			source = "?"
		} else {
			source = compiler.ChunkID(source)
		}
		l.push(stringValue(fmt.Sprintf("%s:%d: %s", source, line, message)))
	}
	l.errorMessage()
}

// typeError raises "attempt to <operation> a <type> value", with where the
// value came from, as Lua 5.4's luaG_typeerror does.
func (l *State) typeError(v value, operation string) {
	l.runtimeError(fmt.Sprintf("attempt to %s a %s value%s", operation, l.valueToType(v), l.varInfo(v)))
}

// varInfo says where the failing instruction found v, as ldebug.c's
// varinfo does: " (global 'x')", " (local 'x')", " (upvalue 'x')", and so
// on, or "" when it cannot tell.
func (l *State) varInfo(v value) string {
	ci := l.callInfo
	if !ci.isLua() {
		return ""
	}
	p := l.prototype(ci)
	pc := ci.savedPC - 1 // the failing instruction
	if pc > 0 && p.Code[pc].OpCode() == bytecode.OpExtraArg && p.Code[pc-1].OpCode() == bytecode.OpBitwise {
		pc-- // BITWISE has read its operator from the EXTRAARG after it
	}
	var kind, name string
	if up, ok := operandUpValue(p.Code[pc]); ok {
		kind, name = "upvalue", p.upValueName(up)
	} else if reg, ok := operandRegister(p.Code[pc], ci.frame, v); ok {
		name, kind = p.objectName(reg, pc)
	}
	if kind == "" {
		return ""
	}
	return fmt.Sprintf(" (%s '%s')", kind, name)
}

// operandUpValue reports the upvalue that instruction i indexes, if any.
func operandUpValue(i bytecode.Instruction) (int, bool) {
	switch i.OpCode() {
	case bytecode.OpGetTableUp:
		return i.B(), true
	case bytecode.OpSetTableUp:
		return i.A(), true
	}
	return 0, false
}

// operandRegister reports which register operand of instruction i holds v.
// It replaces C Lua's pointer test for whether a value is in the stack.
func operandRegister(i bytecode.Instruction, frame []value, v value) (int, bool) {
	var candidates [2]int
	n := 0
	add := func(r int) {
		if !bytecode.IsConstant(r) && r < len(frame) {
			candidates[n] = r
			n++
		}
	}
	switch i.OpCode() {
	case bytecode.OpGetTable, bytecode.OpSelf, bytecode.OpUnaryMinus, bytecode.OpLength:
		add(i.B())
	case bytecode.OpSetTable, bytecode.OpCall, bytecode.OpTailCall:
		add(i.A())
	case bytecode.OpAdd, bytecode.OpSub, bytecode.OpMul, bytecode.OpDiv, bytecode.OpMod, bytecode.OpPow, bytecode.OpIDiv, bytecode.OpBitwise:
		add(i.B())
		add(i.C())
	case bytecode.OpConcat: // concat fails on the last two operands
		add(i.C() - 1)
		add(i.C())
	}
	for _, r := range candidates[:n] {
		if frame[r].identical(v) {
			return r, true
		}
	}
	return 0, false
}

func (l *State) orderError(left, right value) {
	leftType, rightType := l.valueToType(left).String(), l.valueToType(right).String()
	if leftType == rightType {
		l.runtimeError(fmt.Sprintf("attempt to compare two %s values", leftType))
	}
	l.runtimeError(fmt.Sprintf("attempt to compare %s with %s", leftType, rightType))
}

func (l *State) arithError(v1, v2 value) {
	if _, ok := l.toNumber(v1); !ok {
		v2 = v1
	}
	l.typeError(v2, "perform arithmetic on")
}

// bitwiseError blames the operand of a bitwise operator that is not a
// number.
func (l *State) bitwiseError(v1, v2 value) {
	if !v1.isNumber() {
		v2 = v1
	}
	l.typeError(v2, "perform bitwise operation on")
}

// integerRepresentationError blames the number operand of a bitwise
// operator that has no integer value, as luaG_tointerror does.
func (l *State) integerRepresentationError(v1, v2 value) {
	if _, ok := v1.integer(); !ok {
		v2 = v1
	}
	l.runtimeError("number" + l.varInfo(v2) + " has no integer representation")
}

func (l *State) concatError(v1, v2 value) {
	if _, isString := v1.str(); isString || v1.isNumber() {
		v1 = v2
	}
	_, isString := v1.str()
	l.assert(!isString && !v1.isNumber())
	l.typeError(v1, "concatenate")
}

func (l *State) assert(cond bool) {
	if !cond {
		l.runtimeError("assertion failure")
	}
}

func (l *State) errorMessage() {
	if l.errorFunction != 0 { // is there an error handling function?
		errorFunction := l.stack[l.errorFunction]
		if !errorFunction.isFunction() {
			l.throw(ErrErrorHandler)
		}
		l.stack[l.top] = l.stack[l.top-1] // move argument
		l.stack[l.top-1] = errorFunction  // push function
		l.top++
		l.call(l.top-2, 1, false)
	}
	// The error value stays on the stack, whatever its type; the Go error
	// carries it as text, or says what it is.
	msg, ok := toString(l.stack[l.top-1])
	if !ok {
		msg = fmt.Sprintf("(error object is a %s value)", l.TypeName(-1))
	}
	l.throw(RuntimeError(msg))
}
