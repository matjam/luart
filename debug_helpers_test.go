package luart

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"
)

func debugValue(v value) string {
	switch v := v.toAny().(type) {
	case *table:
		entry := func(x value) string {
			if t := x.table(); t != nil {
				return fmt.Sprintf("table %#v", t)
			}
			return debugValue(x)
		}
		var s strings.Builder
		s.WriteString(fmt.Sprintf("table %#v {[", v))
		for _, x := range v.array {
			s.WriteString(entry(x) + ", ")
		}
		s.WriteString("], {")
		for i, x := range v.slots {
			s.WriteString(entry(v.shape.keys[i]) + ": " + entry(x) + ", ")
		}
		for k, x := range v.hash {
			s.WriteString(entry(k.value()) + ": " + entry(x) + ", ")
		}
		return s.String() + "}}"
	case string:
		return "'" + v + "'"
	case float64:
		return fmt.Sprintf("%f", v)
	case *luaClosure:
		return fmt.Sprintf("closure %s:%d %v", v.prototype.source, v.prototype.lineDefined, v)
	case *goClosure:
		return fmt.Sprintf("go closure %#v", v)
	case *goFunction:
		pc := reflect.ValueOf(v.Function).Pointer()
		f := runtime.FuncForPC(pc)
		file, line := f.FileLine(pc)
		return fmt.Sprintf("go function %s %s:%d", f.Name(), file, line)
	case *userData:
		return fmt.Sprintf("userdata %#v", v)
	case nil:
		return "nil"
	case bool:
		return fmt.Sprintf("%#v", v)
	}
	return fmt.Sprintf("unknown %#v %s", v, reflect.TypeOf(v).Name())
}

func stack(s []value) string {
	r := fmt.Sprintf("stack (len: %d, cap: %d):\n", len(s), cap(s))
	for i, v := range s {
		r = fmt.Sprintf("%s %d: %s\n", r, i, debugValue(v))
	}
	return r
}
