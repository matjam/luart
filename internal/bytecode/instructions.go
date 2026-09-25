package bytecode

import "fmt"

type OpCode uint

const (
	ModeABC int = iota
	ModeABx
	ModeAsBx
	ModeAx
)

const (
	OpMove OpCode = iota
	OpLoadConstant
	OpLoadConstantEx
	OpLoadBool
	OpLoadNil
	OpGetUpValue
	OpGetTableUp
	OpGetTable
	OpSetTableUp
	OpSetUpValue
	OpSetTable
	OpNewTable
	OpSelf
	OpAdd
	OpSub
	OpMul
	OpDiv
	OpMod
	OpPow
	OpUnaryMinus
	OpNot
	OpLength
	OpConcat
	OpJump
	OpEqual
	OpLessThan
	OpLessOrEqual
	OpTest
	OpTestSet
	OpCall
	OpTailCall
	OpReturn
	OpForLoop
	OpForPrep
	OpTForCall
	OpTForLoop
	OpSetList
	OpClosure
	OpVarArg
	OpExtraArg

	// Lua 5.3's operators, after 5.2's opcodes. OpBitwise is every bitwise
	// operator: the EXTRAARG word after it holds the ArithOp, so that seven
	// operators take one of the few opcodes left.
	OpIDiv
	OpBitwise

	// Lua 5.5's global declarations: ERRNNIL A Bx raises "global 'K[Bx-1]'
	// already defined" unless R(A) is nil ('?' for the name when Bx is 0).
	OpErrNNil
)

var OpNames = []string{
	"MOVE",
	"LOADK",
	"LOADKX",
	"LOADBOOL",
	"LOADNIL",
	"GETUPVAL",
	"GETTABUP",
	"GETTABLE",
	"SETTABUP",
	"SETUPVAL",
	"SETTABLE",
	"NEWTABLE",
	"SELF",
	"ADD",
	"SUB",
	"MUL",
	"DIV",
	"MOD",
	"POW",
	"UNM",
	"NOT",
	"LEN",
	"CONCAT",
	"JMP",
	"EQ",
	"LT",
	"LE",
	"TEST",
	"TESTSET",
	"CALL",
	"TAILCALL",
	"RETURN",
	"FORLOOP",
	"FORPREP",
	"TFORCALL",
	"TFORLOOP",
	"SETLIST",
	"CLOSURE",
	"VARARG",
	"EXTRAARG",
	"IDIV",
	"BITWISE",
	"ERRNNIL",
}

const (
	sizeC             = 9
	sizeB             = 9
	sizeBx            = sizeC + sizeB
	sizeA             = 8
	sizeAx            = sizeC + sizeB + sizeA
	SizeOp            = 6
	posOp             = 0
	posA              = posOp + SizeOp
	posC              = posA + sizeA
	posB              = posC + sizeC
	posBx             = posC
	posAx             = posA
	BitRK             = 1 << (sizeB - 1)
	MaxIndexRK        = BitRK - 1
	MaxArgAx          = 1<<sizeAx - 1
	MaxArgBx          = 1<<sizeBx - 1
	MaxArgSBx         = MaxArgBx >> 1 // sBx is signed
	MaxArgA           = 1<<sizeA - 1
	MaxArgB           = 1<<sizeB - 1
	MaxArgC           = 1<<sizeC - 1
	ListItemsPerFlush = 50 // # list items to accumulate before a setList instruction
)

type Instruction uint32

func IsConstant(x int) bool   { return 0 != x&BitRK }
func ConstantIndex(r int) int { return r & ^BitRK }
func AsConstant(r int) int    { return r | BitRK }

// creates a mask with 'n' 1 bits at position 'p'
func mask1(n, p uint) Instruction { return ^(^Instruction(0) << n) << p }

// creates a mask with 'n' 0 bits at position 'p'
func mask0(n, p uint) Instruction { return ^mask1(n, p) }

func (i Instruction) OpCode() OpCode         { return OpCode(i >> posOp & (1<<SizeOp - 1)) }
func (i Instruction) arg(pos, size uint) int { return int(i >> pos & mask1(size, 0)) }
func (i *Instruction) SetOpCode(op OpCode)   { i.setArg(posOp, SizeOp, int(op)) }
func (i *Instruction) setArg(pos, size uint, arg int) {
	*i = *i&mask0(size, pos) | Instruction(arg)<<pos&mask1(size, pos)
}

// Note: the gc optimizer cannot inline through multiple function calls. Manually inline for now.
// func (i instruction) a() int   { return i.arg(posA, sizeA) }
// func (i instruction) b() int   { return i.arg(posB, sizeB) }
// func (i instruction) c() int   { return i.arg(posC, sizeC) }
// func (i instruction) bx() int  { return i.arg(posBx, sizeBx) }
// func (i instruction) ax() int  { return i.arg(posAx, sizeAx) }
// func (i instruction) sbx() int { return i.bx() - maxArgSBx }

func (i Instruction) A() int   { return int(i >> posA & MaxArgA) }
func (i Instruction) B() int   { return int(i >> posB & MaxArgB) }
func (i Instruction) C() int   { return int(i >> posC & MaxArgC) }
func (i Instruction) Bx() int  { return int(i >> posBx & MaxArgBx) }
func (i Instruction) Ax() int  { return int(i >> posAx & MaxArgAx) }
func (i Instruction) SBx() int { return int(i>>posBx&MaxArgBx) - MaxArgSBx }

func (i *Instruction) SetA(arg int)   { i.setArg(posA, sizeA, arg) }
func (i *Instruction) SetB(arg int)   { i.setArg(posB, sizeB, arg) }
func (i *Instruction) SetC(arg int)   { i.setArg(posC, sizeC, arg) }
func (i *Instruction) SetBx(arg int)  { i.setArg(posBx, sizeBx, arg) }
func (i *Instruction) SetAx(arg int)  { i.setArg(posAx, sizeAx, arg) }
func (i *Instruction) SetSBx(arg int) { i.setArg(posBx, sizeBx, arg+MaxArgSBx) }

func CreateABC(op OpCode, a, b, c int) Instruction {
	return Instruction(op)<<posOp |
		Instruction(a)<<posA |
		Instruction(b)<<posB |
		Instruction(c)<<posC
}

func CreateABx(op OpCode, a, bx int) Instruction {
	return Instruction(op)<<posOp |
		Instruction(a)<<posA |
		Instruction(bx)<<posBx
}

func CreateAx(op OpCode, a int) Instruction { return Instruction(op)<<posOp | Instruction(a)<<posAx }

func (i Instruction) String() string {
	op := i.OpCode()
	s := OpNames[op]
	switch OpMode(op) {
	case ModeABC:
		s = fmt.Sprintf("%s %d", s, i.A())
		if BMode(op) == ArgK && IsConstant(i.B()) {
			s = fmt.Sprintf("%s constant %d", s, ConstantIndex(i.B()))
		} else if BMode(op) != ArgN {
			s = fmt.Sprintf("%s %d", s, i.B())
		}
		if CMode(op) == ArgK && IsConstant(i.C()) {
			s = fmt.Sprintf("%s constant %d", s, ConstantIndex(i.C()))
		} else if CMode(op) != ArgN {
			s = fmt.Sprintf("%s %d", s, i.C())
		}
	case ModeAsBx:
		s = fmt.Sprintf("%s %d", s, i.A())
		if BMode(op) != ArgN {
			s = fmt.Sprintf("%s %d", s, i.SBx())
		}
	case ModeABx:
		s = fmt.Sprintf("%s %d", s, i.A())
		if BMode(op) != ArgN {
			s = fmt.Sprintf("%s %d", s, i.Bx())
		}
	case ModeAx:
		s = fmt.Sprintf("%s %d", s, i.Ax())
	}
	return s
}

func opmode(t, a, b, c, m int) byte { return byte(t<<7 | a<<6 | b<<4 | c<<2 | m) }

const (
	ArgN = iota // argument is not used
	ArgU        // argument is used
	ArgR        // argument is a register or a jump offset
	ArgK        // argument is a constant or register/constant
)

func OpMode(m OpCode) int     { return int(opModes[m] & 3) }
func BMode(m OpCode) byte     { return (opModes[m] >> 4) & 3 }
func CMode(m OpCode) byte     { return (opModes[m] >> 2) & 3 }
func TestAMode(m OpCode) bool { return opModes[m]&(1<<6) != 0 }
func TestTMode(m OpCode) bool { return opModes[m]&(1<<7) != 0 }

var opModes []byte = []byte{
	//     T  A    B       C     mode		    opcode
	opmode(0, 1, ArgR, ArgN, ModeABC),  // opMove
	opmode(0, 1, ArgK, ArgN, ModeABx),  // opLoadConstant
	opmode(0, 1, ArgN, ArgN, ModeABx),  // opLoadConstantEx
	opmode(0, 1, ArgU, ArgU, ModeABC),  // opLoadBool
	opmode(0, 1, ArgU, ArgN, ModeABC),  // opLoadNil
	opmode(0, 1, ArgU, ArgN, ModeABC),  // opGetUpValue
	opmode(0, 1, ArgU, ArgK, ModeABC),  // opGetTableUp
	opmode(0, 1, ArgR, ArgK, ModeABC),  // opGetTable
	opmode(0, 0, ArgK, ArgK, ModeABC),  // opSetTableUp
	opmode(0, 0, ArgU, ArgN, ModeABC),  // opSetUpValue
	opmode(0, 0, ArgK, ArgK, ModeABC),  // opSetTable
	opmode(0, 1, ArgU, ArgU, ModeABC),  // opNewTable
	opmode(0, 1, ArgR, ArgK, ModeABC),  // opSelf
	opmode(0, 1, ArgK, ArgK, ModeABC),  // opAdd
	opmode(0, 1, ArgK, ArgK, ModeABC),  // opSub
	opmode(0, 1, ArgK, ArgK, ModeABC),  // opMul
	opmode(0, 1, ArgK, ArgK, ModeABC),  // opDiv
	opmode(0, 1, ArgK, ArgK, ModeABC),  // opMod
	opmode(0, 1, ArgK, ArgK, ModeABC),  // opPow
	opmode(0, 1, ArgR, ArgN, ModeABC),  // opUnaryMinus
	opmode(0, 1, ArgR, ArgN, ModeABC),  // opNot
	opmode(0, 1, ArgR, ArgN, ModeABC),  // opLength
	opmode(0, 1, ArgR, ArgR, ModeABC),  // opConcat
	opmode(0, 0, ArgR, ArgN, ModeAsBx), // opJump
	opmode(1, 0, ArgK, ArgK, ModeABC),  // opEqual
	opmode(1, 0, ArgK, ArgK, ModeABC),  // opLessThan
	opmode(1, 0, ArgK, ArgK, ModeABC),  // opLessOrEqual
	opmode(1, 0, ArgN, ArgU, ModeABC),  // opTest
	opmode(1, 1, ArgR, ArgU, ModeABC),  // opTestSet
	opmode(0, 1, ArgU, ArgU, ModeABC),  // opCall
	opmode(0, 1, ArgU, ArgU, ModeABC),  // opTailCall
	opmode(0, 0, ArgU, ArgN, ModeABC),  // opReturn
	opmode(0, 1, ArgR, ArgN, ModeAsBx), // opForLoop
	opmode(0, 1, ArgR, ArgN, ModeAsBx), // opForPrep
	opmode(0, 0, ArgN, ArgU, ModeABC),  // opTForCall
	opmode(0, 1, ArgR, ArgN, ModeAsBx), // opTForLoop
	opmode(0, 0, ArgU, ArgU, ModeABC),  // opSetList
	opmode(0, 1, ArgU, ArgN, ModeABx),  // opClosure
	opmode(0, 1, ArgU, ArgN, ModeABC),  // opVarArg
	opmode(0, 0, ArgU, ArgU, ModeAx),   // opExtraArg
	opmode(0, 1, ArgK, ArgK, ModeABC),  // opIDiv
	opmode(0, 1, ArgK, ArgK, ModeABC),  // opBitwise; C is unused for ~x
	opmode(0, 0, ArgU, ArgN, ModeABx),  // opErrNNil
}
