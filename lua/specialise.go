package lua

import "github.com/matjam/apogee/internal/bytecode"

// Opcodes that exist only in a prototype's exec code. They follow the
// compiler's opcodes and fit the 6-bit opcode field. Each is an arithmetic instruction
// with its operand kinds fixed at load time: R reads a register and K a
// constant, whose index has the RK bit cleared.
const (
	opAddRR bytecode.OpCode = bytecode.OpTBC + 1 + iota
	opAddRK
	opAddKR
	opSubRR
	opSubRK
	opSubKR
	opMulRR
	opMulRK
	opMulKR
	opDivRR
	opDivRK
	opDivKR

	// Table access with a constant string key, whose constant index has the
	// RK bit cleared. Each uses the instruction's fieldCache.
	opGetField   // GETTABLE with key C
	opGetFieldUp // GETTABUP with key C
	opSetField   // SETTABLE with key B
	opSetFieldUp // SETTABUP with key B
	opSelfField  // SELF with key C

	// A MULRK whose result is the B operand of the ADDRR that follows it, as
	// in x*0.1 + t. See fuseMulAdd.
	opMulAddRKR

	// Patched into the exec code of a state that compiles: jitCount at
	// function entry and loop latches until the function is compiled, then
	// jitEnter at the places the interpreter hands over to compiled code.
	// Both run jitOrig[pc] afterwards. See jit.go.
	opJITCount
	opJITEnter

	opCount
)

// fuseMulAdd marks each MULRK that feeds the ADDRR after it, so the
// interpreter runs both in one dispatch when every operand is a number. The
// ADDRR stays in place, so jumps to it and the unfused path still run it.
// Fusing arithmetic pairs in general measured slower: decoding the second
// instruction cost more than the dispatch it saved.
func fuseMulAdd(exec []bytecode.Instruction) {
	for pc := 0; pc+1 < len(exec); pc++ {
		if i, next := exec[pc], exec[pc+1]; i.OpCode() == opMulRK && next.OpCode() == opAddRR && next.B() == i.A() {
			exec[pc].SetOpCode(opMulAddRKR)
		}
	}
}

func init() {
	if opCount > 1<<bytecode.SizeOp {
		panic("too many opcodes")
	}
}

// arithInto runs the slow path of arithmetic into register a, which may call
// a metamethod, and returns the frame, which the call may have moved.
func (l *State) arithInto(ci *callInfo, a int, b, c value, op tm) []value {
	v := l.arith(b, c, op)
	ci.frame[a] = v
	return ci.frame
}

// execCode returns the code the interpreter runs for p. It has the same
// length and instruction positions as p.code, so savedPC, line info and
// error naming keep using p.code.
func (p *prototype) execCode() []bytecode.Instruction {
	if p.exec == nil {
		p.buildExec()
	}
	return p.exec
}

// buildExec is execCode's slow path, kept out of line so execCode inlines
// into the interpreter's call and return paths.
func (p *prototype) buildExec() {
	p.exec, p.fields = specialise(p.Code, p.Constants)
	if p.jitOn {
		p.patchJITCounters()
	}
}

func specialise(code []bytecode.Instruction, constants []value) ([]bytecode.Instruction, []fieldCache) {
	exec := make([]bytecode.Instruction, len(code))
	var fields []fieldCache
	stringKey := func(rk int) bool {
		if !bytecode.IsConstant(rk) {
			return false
		}
		_, ok := constants[bytecode.ConstantIndex(rk)].str()
		return ok
	}
	for pc, i := range code {
		exec[pc] = i
		var field bytecode.OpCode
		switch i.OpCode() {
		case bytecode.OpGetTable:
			field = opGetField
		case bytecode.OpGetTableUp:
			field = opGetFieldUp
		case bytecode.OpSelf:
			field = opSelfField
		case bytecode.OpSetTable:
			field = opSetField
		case bytecode.OpSetTableUp:
			field = opSetFieldUp
		case bytecode.OpNewTable: // its fieldCache remembers the shape of its tables
			if fields == nil {
				fields = make([]fieldCache, len(code))
			}
		}
		if field != 0 {
			s := i
			switch field {
			case opSetField, opSetFieldUp:
				if !stringKey(i.B()) {
					continue
				}
				s.SetB(bytecode.ConstantIndex(i.B()))
			default:
				if !stringKey(i.C()) {
					continue
				}
				s.SetC(bytecode.ConstantIndex(i.C()))
			}
			s.SetOpCode(field)
			exec[pc] = s
			if fields == nil {
				fields = make([]fieldCache, len(code))
			}
			continue
		}
		var base bytecode.OpCode
		switch i.OpCode() {
		case bytecode.OpAdd:
			base = opAddRR
		case bytecode.OpSub:
			base = opSubRR
		case bytecode.OpMul:
			base = opMulRR
		case bytecode.OpDiv:
			base = opDivRR
		default:
			continue
		}
		s := i
		switch b, c := bytecode.IsConstant(i.B()), bytecode.IsConstant(i.C()); {
		case !b && !c:
			s.SetOpCode(base)
		case !b && c:
			s.SetOpCode(base + 1)
			s.SetC(bytecode.ConstantIndex(i.C()))
		case b && !c:
			s.SetOpCode(base + 2)
			s.SetB(bytecode.ConstantIndex(i.B()))
		default:
			continue
		}
		exec[pc] = s
	}
	fuseMulAdd(exec)
	return exec, fields
}
