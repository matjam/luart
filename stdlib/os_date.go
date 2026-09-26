package stdlib

import (
	"fmt"
	"strings"
	"time"

	"github.com/matjam/apogee/lua"
)

// The conversions os.date accepts, as loslib.c's LUA_STRFTIMEOPTIONS for
// POSIX: single letters, and E and O with the letters they modify.
var strftimeOptions = []struct{ first, second string }{
	{"aAbBcCdDeFgGhHIjmMnprRStTuUVwWxXyYzZ%", ""},
	{"E", "cCxXyY"},
	{"O", "deHImMSuUVwWy"},
}

// osDate is os.date([format [, time]]), after loslib.c's os_date.
func osDate(l *lua.State) int {
	s := l.OptString(1, "%c")
	t := time.Now()
	if !l.IsNoneOrNil(2) {
		t = time.Unix(l.CheckInteger(2), 0)
	}
	if strings.HasPrefix(s, "!") { // UTC
		t, s = t.UTC(), s[1:]
	} else {
		t = t.Local()
	}
	if !representable(t) {
		l.Errorf("date result cannot be represented in this installation")
	}
	if s == "*t" {
		l.CreateTable(0, 9)
		setDateFields(l, t)
		return 1
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '%' {
			b.WriteByte(s[i])
			i++
			continue
		}
		conversion := checkConversion(l, s[i+1:])
		strftime(&b, conversion[len(conversion)-1], t)
		i += 1 + len(conversion)
	}
	l.PushString(b.String())
	return 1
}

// checkConversion returns the conversion at the start of s, the text after
// a '%', or raises loslib.c's error naming the rest of the format.
func checkConversion(l *lua.State, s string) string {
	for _, o := range strftimeOptions {
		if s == "" || !strings.Contains(o.first, s[:1]) {
			continue
		}
		if o.second == "" {
			return s[:1]
		}
		if len(s) > 1 && strings.Contains(o.second, s[1:2]) {
			return s[:2] // E and O change nothing in the C locale
		}
	}
	l.ArgumentError(1, l.PushFString("invalid conversion specifier '%%%s'", s))
	panic("unreachable")
}

// strftime writes t's conversion c as C's strftime does in the C locale.
func strftime(b *strings.Builder, c byte, t time.Time) {
	hour12 := t.Hour() % 12
	if hour12 == 0 {
		hour12 = 12
	}
	yday, wday := t.YearDay()-1, int(t.Weekday())
	isoYear, isoWeek := t.ISOWeek()
	switch c {
	case 'a':
		b.WriteString(t.Weekday().String()[:3])
	case 'A':
		b.WriteString(t.Weekday().String())
	case 'b', 'h':
		b.WriteString(t.Month().String()[:3])
	case 'B':
		b.WriteString(t.Month().String())
	case 'c':
		fmt.Fprintf(b, "%s %s %2d %02d:%02d:%02d %d", t.Weekday().String()[:3], t.Month().String()[:3], t.Day(), t.Hour(), t.Minute(), t.Second(), t.Year())
	case 'C':
		fmt.Fprintf(b, "%02d", t.Year()/100)
	case 'd':
		fmt.Fprintf(b, "%02d", t.Day())
	case 'D', 'x':
		fmt.Fprintf(b, "%02d/%02d/%02d", int(t.Month()), t.Day(), t.Year()%100)
	case 'e':
		fmt.Fprintf(b, "%2d", t.Day())
	case 'F':
		fmt.Fprintf(b, "%d-%02d-%02d", t.Year(), int(t.Month()), t.Day())
	case 'g':
		fmt.Fprintf(b, "%02d", isoYear%100)
	case 'G':
		fmt.Fprintf(b, "%d", isoYear)
	case 'H':
		fmt.Fprintf(b, "%02d", t.Hour())
	case 'I':
		fmt.Fprintf(b, "%02d", hour12)
	case 'j':
		fmt.Fprintf(b, "%03d", yday+1)
	case 'm':
		fmt.Fprintf(b, "%02d", int(t.Month()))
	case 'M':
		fmt.Fprintf(b, "%02d", t.Minute())
	case 'n':
		b.WriteByte('\n')
	case 'p':
		if t.Hour() < 12 {
			b.WriteString("AM")
		} else {
			b.WriteString("PM")
		}
	case 'r':
		fmt.Fprintf(b, "%02d:%02d:%02d ", hour12, t.Minute(), t.Second())
		strftime(b, 'p', t)
	case 'R':
		fmt.Fprintf(b, "%02d:%02d", t.Hour(), t.Minute())
	case 'S':
		fmt.Fprintf(b, "%02d", t.Second())
	case 't':
		b.WriteByte('\t')
	case 'T', 'X':
		fmt.Fprintf(b, "%02d:%02d:%02d", t.Hour(), t.Minute(), t.Second())
	case 'u': // Monday is 1, Sunday 7
		fmt.Fprintf(b, "%d", (wday+6)%7+1)
	case 'U': // weeks starting on Sunday
		fmt.Fprintf(b, "%02d", (yday+7-wday)/7)
	case 'V':
		fmt.Fprintf(b, "%02d", isoWeek)
	case 'w':
		fmt.Fprintf(b, "%d", wday)
	case 'W': // weeks starting on Monday
		fmt.Fprintf(b, "%02d", (yday+7-(wday+6)%7)/7)
	case 'y':
		fmt.Fprintf(b, "%02d", t.Year()%100)
	case 'Y':
		fmt.Fprintf(b, "%d", t.Year())
	case 'z':
		_, offset := t.Zone()
		sign := '+'
		if offset < 0 {
			sign, offset = '-', -offset
		}
		fmt.Fprintf(b, "%c%02d%02d", sign, offset/3600, offset/60%60)
	case 'Z':
		name, _ := t.Zone()
		b.WriteString(name)
	case '%':
		b.WriteByte('%')
	}
}

var localeCategories = []string{"all", "collate", "ctype", "monetary", "numeric", "time"}

// osSetlocale is os.setlocale([locale [, category]]). apogee has only the
// C locale, also called POSIX; "" asks for the native locale, which is C
// too. Any other locale fails, returning nil.
func osSetlocale(l *lua.State) int {
	name := l.OptString(1, "")
	l.CheckOption(2, "all", localeCategories)
	switch name {
	case "", "C", "POSIX":
		l.PushString("C")
	default:
		l.PushNil()
	}
	return 1
}
