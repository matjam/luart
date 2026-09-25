//go:build clua54 || luajit

package luabench

// #include "clua.h"
import "C"

import (
	"errors"
	"unsafe"
)

type cgoState struct{ l *C.lua_State }

func newCState(src string) (cState, error) {
	s := &cgoState{C.clua_new()}
	cs := C.CString(src)
	defer C.free(unsafe.Pointer(cs))
	if msg := C.clua_do(s.l, cs); msg != nil {
		err := errors.New(C.GoString(msg))
		s.close()
		return nil, err
	}
	return s, nil
}

func (s *cgoState) run() (float64, error) {
	var v C.double
	if msg := C.clua_run(s.l, &v); msg != nil {
		return 0, errors.New(C.GoString(msg))
	}
	return float64(v), nil
}

func (s *cgoState) close() { C.lua_close(s.l) }

func init() { cLuas = append(cLuas, cLua{cName, newCState}) }
