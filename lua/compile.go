package lua

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/matjam/luart/internal/bytecode"
	"github.com/matjam/luart/internal/chunk"
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

// constantValue converts a compiled constant: nil, a bool, an int64, a
// float64 or a string.
func constantValue(k any) value {
	switch k := k.(type) {
	case nil:
		return nilValue
	case bool:
		return boolValue(k)
	case int64:
		return integerValue(k)
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
			closure = l.undump(b, name)
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

// undump loads the binary chunk read from r and pushes a closure of its
// main function, raising a syntax error with the loader's message on
// failure.
func (l *State) undump(r io.Reader, name string) *luaClosure {
	bp, err := chunk.Load(r, name)
	if err != nil {
		l.push(stringValue(err.Error()))
		l.throw(ErrSyntax)
	}
	p := prototypeOf(bp)
	c := l.newLuaClosure(&p)
	l.push(objectValue(c))
	return c
}

// protoOf is the compiled function p runs: the inverse of prototypeOf.
func protoOf(p *prototype) *bytecode.Proto {
	bp := &bytecode.Proto{
		Code:            p.Code,
		LineInfo:        p.LineInfo,
		LocalVariables:  p.LocalVariables,
		UpValues:        p.UpValues,
		Source:          p.Source,
		LineDefined:     p.LineDefined,
		LastLineDefined: p.LastLineDefined,
		ParameterCount:  p.ParameterCount,
		MaxStackSize:    p.MaxStackSize,
		IsVarArg:        p.IsVarArg,
	}
	if len(p.Constants) > 0 {
		bp.Constants = make([]any, len(p.Constants))
		for i, k := range p.Constants {
			bp.Constants[i] = k.toAny()
		}
	}
	if len(p.Prototypes) > 0 {
		bp.Prototypes = make([]*bytecode.Proto, len(p.Prototypes))
		for i := range p.Prototypes {
			bp.Prototypes[i] = protoOf(&p.Prototypes[i])
		}
	}
	return bp
}
