package luart

// Opcodes that exist only in a prototype's exec code. They follow the Lua 5.2
// opcodes and fit the 6-bit opcode field. Each is an arithmetic instruction
// with its operand kinds fixed at load time: R reads a register and K a
// constant, whose index has the RK bit cleared.
const (
	opAddRR opCode = opExtraArg + 1 + iota
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
func fuseMulAdd(exec []instruction) {
	for pc := 0; pc+1 < len(exec); pc++ {
		if i, next := exec[pc], exec[pc+1]; i.opCode() == opMulRK && next.opCode() == opAddRR && next.b() == i.a() {
			exec[pc].setOpCode(opMulAddRKR)
		}
	}
}

func init() {
	if opCount > 1<<sizeOp {
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
func (p *prototype) execCode() []instruction {
	if p.exec == nil {
		p.buildExec()
	}
	return p.exec
}

// buildExec is execCode's slow path, kept out of line so execCode inlines
// into the interpreter's call and return paths.
func (p *prototype) buildExec() {
	p.exec, p.fields = specialise(p.code, p.constants)
	if p.jitOn {
		p.patchJITCounters()
	}
}

func specialise(code []instruction, constants []value) ([]instruction, []fieldCache) {
	exec := make([]instruction, len(code))
	var fields []fieldCache
	stringKey := func(rk int) bool {
		if !isConstant(rk) {
			return false
		}
		_, ok := constants[constantIndex(rk)].str()
		return ok
	}
	for pc, i := range code {
		exec[pc] = i
		var field opCode
		switch i.opCode() {
		case opGetTable:
			field = opGetField
		case opGetTableUp:
			field = opGetFieldUp
		case opSelf:
			field = opSelfField
		case opSetTable:
			field = opSetField
		case opSetTableUp:
			field = opSetFieldUp
		case opNewTable: // its fieldCache remembers the shape of its tables
			if fields == nil {
				fields = make([]fieldCache, len(code))
			}
		}
		if field != 0 {
			s := i
			switch field {
			case opSetField, opSetFieldUp:
				if !stringKey(i.b()) {
					continue
				}
				s.setB(constantIndex(i.b()))
			default:
				if !stringKey(i.c()) {
					continue
				}
				s.setC(constantIndex(i.c()))
			}
			s.setOpCode(field)
			exec[pc] = s
			if fields == nil {
				fields = make([]fieldCache, len(code))
			}
			continue
		}
		var base opCode
		switch i.opCode() {
		case opAdd:
			base = opAddRR
		case opSub:
			base = opSubRR
		case opMul:
			base = opMulRR
		case opDiv:
			base = opDivRR
		default:
			continue
		}
		s := i
		switch b, c := isConstant(i.b()), isConstant(i.c()); {
		case !b && !c:
			s.setOpCode(base)
		case !b && c:
			s.setOpCode(base + 1)
			s.setC(constantIndex(i.c()))
		case b && !c:
			s.setOpCode(base + 2)
			s.setB(constantIndex(i.b()))
		default:
			continue
		}
		exec[pc] = s
	}
	fuseMulAdd(exec)
	return exec, fields
}
