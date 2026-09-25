package luart

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/matjam/luart/internal/bytecode"
	"github.com/matjam/luart/internal/compiler"
)

// parse compiles the source read from r and pushes a closure of its main
// function, raising a syntax error with the compiler's message on failure.
// The parser's nesting counts against the Go calls already running.
func (l *State) parse(r io.ByteReader, name string) *luaClosure {
	bp, err := compiler.Parse(r, name, l.nestedGoCallCount)
	if err != nil {
		l.push(stringValue(err.Error()))
		l.throw(ErrSyntax)
	}
	p := prototypeOf(bp)
	c := l.newLuaClosure(&p)
	l.push(objectValue(c))
	return c
}

// prototypeOf builds the VM's prototype for bp, a compiled function, and
// its nested functions. It shares bp's code and debug information.
func prototypeOf(bp *bytecode.Proto) prototype {
	p := prototype{
		Code:            bp.Code,
		LineInfo:        bp.LineInfo,
		LocalVariables:  bp.LocalVariables,
		UpValues:        bp.UpValues,
		Source:          bp.Source,
		LineDefined:     bp.LineDefined,
		LastLineDefined: bp.LastLineDefined,
		ParameterCount:  bp.ParameterCount,
		MaxStackSize:    bp.MaxStackSize,
		IsVarArg:        bp.IsVarArg,
	}
	if len(bp.Constants) > 0 {
		p.Constants = make([]value, len(bp.Constants))
		for i, k := range bp.Constants {
			p.Constants[i] = constantValue(k)
		}
	}
	if len(bp.Prototypes) > 0 {
		p.Prototypes = make([]prototype, len(bp.Prototypes))
		for i, c := range bp.Prototypes {
			p.Prototypes[i] = prototypeOf(c)
		}
	}
	return p
}

// constantValue converts a compiled constant: nil, a bool, a float64 or a
// string.
func constantValue(k any) value {
	switch k := k.(type) {
	case nil:
		return nilValue
	case bool:
		return boolValue(k)
	case float64:
		return numberValue(k)
	case string:
		return stringValue(k)
	}
	panic(fmt.Sprintf("constant of type %T", k))
}

func (l *State) checkMode(mode, x string) {
	if mode != "" && !strings.Contains(mode, x[:1]) {
		l.push(stringValue(fmt.Sprintf("attempt to load a %s chunk (mode is '%s')", x, mode)))
		l.throw(ErrSyntax)
	}
}

func protectedParser(l *State, r io.Reader, name, chunkMode string) error {
	l.nonYieldableCallCount++
	err := l.protectedCall(func() {
		var closure *luaClosure
		b := bufio.NewReader(r)
		if c, err := b.ReadByte(); err != nil {
			l.checkMode(chunkMode, "text")
			closure = l.parse(b, name)
		} else if c == Signature[0] {
			l.checkMode(chunkMode, "binary")
			b.UnreadByte()
			closure, _ = l.undump(b, name) // TODO handle err
		} else {
			l.checkMode(chunkMode, "text")
			b.UnreadByte()
			closure = l.parse(b, name)
		}
		l.assert(closure.upValueCount() == len(closure.prototype.UpValues))
		for i := range closure.upValues {
			closure.upValues[i] = l.newUpValue()
		}
	}, l.top, l.errorFunction)
	l.nonYieldableCallCount--
	return err
}
