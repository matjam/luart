// Command chart turns raw benchmark output into the comparison tables and
// charts in bench/README.md.
//
//	go run ./chart -table suite-results-amd64.txt
//	go run ./chart -svg suite-amd64.svg suite-results-amd64.txt
//	go run ./chart -readme README.md,../README.md -name amd64 suite-results-amd64.txt
//	go run ./chart -suite standard -svg standard-amd64.svg suite-results-amd64.txt
//	go run ./chart -summary -readme ../README.md -name summary suite-results-amd64.txt suite-results.txt
//
// -suite picks the benchmark: "suite", BenchmarkSuite, whose interpreters
// are compared with native Go, or "standard", BenchmarkStandard, whose are
// compared with C Lua 5.5, or 5.4 for results without 5.5. -readme replaces the table between the lines
// <!-- suite-table NAME --> and <!-- /suite-table --> in each file.
// -summary makes one table of both benchmarks' geometric means from each
// of several results files, a row per benchmark and machine.
//
// Each timing is the median of the runs in the file.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// A suite is a benchmark function's workloads, in table order, and the
// interpreter or native code the others are compared with.
type suite struct {
	title     string // the benchmark, as the summary names it
	bench     string // the benchmark function's name after Benchmark
	workloads []workload
	base      impl
	column    string // the first column's heading
	against   string // the base, as the chart's title names it
}

type workload struct{ name, label string }

var suites = map[string]suite{
	"suite": {"Embedding workloads", "Suite", []workload{
		{"fib", "fib(25), recursive calls"},
		{"numeric-loop", "numeric loop, 1M iterations"},
		{"array-fill-sum", "array fill and sum, 100k"},
		{"records", "records, 10k tables"},
		{"closures", "closures, 100k"},
		{"sort", "sort 10k with comparator"},
		{"string-build", "string build, 10k pieces"},
		{"string-scan", "string scan, 11k characters"},
		{"go-calls", "calls into Go, 100k"},
		{"plasma", "plasma frame"},
		{"particles", "particles frame"},
	}, impl{"go", "Native Go", -1}, "Workload", "native Go"},
	"standard": {"Standard benchmarks", "Standard", []workload{
		{"bounce", "Bounce"},
		{"cd", "CD"},
		{"deltablue", "DeltaBlue"},
		{"havlak", "Havlak"},
		{"json", "Json"},
		{"list", "List"},
		{"mandelbrot", "Mandelbrot"},
		{"nbody", "NBody"},
		{"permute", "Permute"},
		{"queens", "Queens"},
		{"richards", "Richards"},
		{"sieve", "Sieve"},
		{"storage", "Storage"},
		{"towers", "Towers"},
		{"binary-trees", "binary-trees"},
		{"fannkuch-redux", "fannkuch-redux"},
		{"spectral-norm", "spectral-norm"},
	}, impl{"lua54", "Lua 5.4", 3}, "Benchmark", "C Lua 5.4"},
}

// An impl is an interpreter, by the name of its sub-benchmark. slot is its
// color, the chart's .sN class, which stays with the interpreter whatever
// its order.
type impl struct {
	name, label string
	slot        int
}

// allImpls are the interpreters in column and series order. A results file
// has those it ran: the C ones need cgo and a build tag.
var allImpls = []impl{
	{"luart-jit", "Luart (JIT)", 1},
	{"luart", "Luart (no JIT)", 0},
	{"shopify", "go-lua", 2},
	{"lua54", "Lua 5.4", 3},
	{"luajit", "LuaJIT", 4},
}

// useCLua55 makes C Lua 5.5 the C interpreter, in place of 5.4, for
// results that have it.
func useCLua55() {
	lua55 := impl{"lua55", "Lua 5.5", 3}
	for i, im := range allImpls {
		if im.name == "lua54" {
			allImpls[i] = lua55
		}
	}
	for name, s := range suites {
		if s.base.name == "lua54" {
			s.base, s.against = lua55, "C Lua 5.5"
			suites[name] = s
		}
	}
}

type results struct {
	cpu, goos, goarch string
	ns                map[string][]float64 // by "bench/workload/impl"
}

// impls returns the interpreters r has timings for in s, other than its
// base.
func (r *results) impls(s suite) []impl {
	var in []impl
	for _, im := range allImpls {
		if im.name == s.base.name {
			continue
		}
		for key := range r.ns {
			if strings.HasPrefix(key, s.bench+"/") && strings.HasSuffix(key, "/"+im.name) {
				in = append(in, im)
				break
			}
		}
	}
	return in
}

var line = regexp.MustCompile(`^Benchmark((\w+)/([\w-]+)/([\w-]+)|GoParticles)-\d+\s+\d+\s+([\d.]+) ns/op`)

func parse(r io.Reader) (*results, error) {
	res := &results{ns: map[string][]float64{}}
	s := bufio.NewScanner(r)
	for s.Scan() {
		t := s.Text()
		if k, v, ok := strings.Cut(t, ": "); ok {
			switch k {
			case "cpu":
				res.cpu = strings.TrimSpace(v)
			case "goos":
				res.goos = v
			case "goarch":
				res.goarch = v
			}
		}
		m := line.FindStringSubmatch(t)
		if m == nil {
			continue
		}
		key := m[2] + "/" + m[3] + "/" + m[4]
		if m[1] == "GoParticles" { // the suite has no native particles
			key = "Suite/particles/go"
		}
		ns, err := strconv.ParseFloat(m[5], 64)
		if err != nil {
			return nil, err
		}
		res.ns[key] = append(res.ns[key], ns)
	}
	return res, s.Err()
}

// median returns the median time of workload w in s run by im, in
// milliseconds, or NaN.
func (r *results) median(s suite, w workload, im impl) float64 {
	v := slices.Clone(r.ns[s.bench+"/"+w.name+"/"+im.name])
	if len(v) == 0 {
		return math.NaN()
	}
	slices.Sort(v)
	m := v[len(v)/2]
	if len(v)%2 == 0 {
		m = (v[len(v)/2-1] + m) / 2
	}
	return m / 1e6
}

// ratio returns im's median time for w divided by the base's.
func (r *results) ratio(s suite, w workload, im impl) float64 {
	return r.median(s, w, im) / r.median(s, w, s.base)
}

// geomean returns the geometric mean of im's ratios over the workloads it
// ran, or NaN.
func (r *results) geomean(s suite, im impl) float64 {
	sum, n := 0.0, 0
	for _, w := range s.workloads {
		if x := r.ratio(s, w, im); !math.IsNaN(x) {
			sum += math.Log(x)
			n++
		}
	}
	if n == 0 {
		return math.NaN()
	}
	return math.Exp(sum / float64(n))
}

func ms(v float64) string {
	switch {
	case math.IsNaN(v):
		return "–"
	case v < 0.01:
		return fmt.Sprintf("%.3f ms", v)
	case v < 10:
		return fmt.Sprintf("%.2f ms", v)
	case v < 100:
		return fmt.Sprintf("%.1f ms", v)
	}
	return fmt.Sprintf("%.0f ms", v)
}

// times formats how many times slower than the base ratio is.
func times(ratio float64) string {
	switch {
	case math.IsNaN(ratio):
		return "–"
	case ratio < 1:
		return short(1/ratio) + "× faster"
	}
	return short(ratio) + "× slower"
}

func short(x float64) string {
	if x < 9.95 {
		return strconv.FormatFloat(x, 'f', 1, 64)
	}
	return strconv.FormatFloat(x, 'f', 0, 64)
}

// ratio formats a time divided by the base's, with two digits below 1 so
// that 0.95 does not read as 1.
func ratio(r float64) string {
	switch {
	case math.IsNaN(r):
		return "–"
	case r < 0.995:
		return strconv.FormatFloat(r, 'f', 2, 64) + "×"
	}
	return short(r) + "×"
}

func table(w io.Writer, s suite, r *results) {
	impls := r.impls(s)
	fmt.Fprintf(w, "| %s | %s", s.column, s.base.label)
	for _, im := range impls {
		fmt.Fprintf(w, " | %s", im.label)
	}
	fmt.Fprintln(w, " |")
	fmt.Fprintln(w, "|---"+strings.Repeat("|---:", 1+len(impls))+"|")
	for _, wl := range s.workloads {
		fmt.Fprintf(w, "| %s | %s", wl.label, ms(r.median(s, wl, s.base)))
		for _, im := range impls {
			t := r.median(s, wl, im)
			if math.IsNaN(t) {
				fmt.Fprint(w, " | –")
				continue
			}
			fmt.Fprintf(w, " | %s (%s)", ms(t), ratio(r.ratio(s, wl, im)))
		}
		fmt.Fprintln(w, " |")
	}
	fmt.Fprint(w, "| **geometric mean** | ")
	for _, im := range impls {
		fmt.Fprintf(w, " | **%s**", ratio(r.geomean(s, im)))
	}
	fmt.Fprintln(w, " |")
}

// summaryOrder is the benchmarks in the summary's row order.
var summaryOrder = []string{"standard", "suite"}

var coreCount = regexp.MustCompile(`\s+\d+-Core Processor$`)

// machine names the CPU a results file was measured on.
func (r *results) machine() string {
	return coreCount.ReplaceAllString(r.cpu, "")
}

// summary writes each benchmark's geometric means from each of rs, a row
// per benchmark and machine, and a column per interpreter any of them ran.
// An interpreter's cell for the benchmark it is the base of is 1×.
func summary(w io.Writer, rs []*results) {
	var impls []impl
	for _, im := range allImpls {
		for _, r := range rs {
			if slices.ContainsFunc(summaryOrder, func(name string) bool {
				s := suites[name]
				return im == s.base || slices.Contains(r.impls(s), im)
			}) {
				impls = append(impls, im)
				break
			}
		}
	}
	fmt.Fprint(w, "| Geometric mean")
	for _, im := range impls {
		fmt.Fprintf(w, " | %s", im.label)
	}
	fmt.Fprintln(w, " |")
	fmt.Fprintln(w, "|---"+strings.Repeat("|---:", len(impls))+"|")
	for _, name := range summaryOrder {
		s := suites[name]
		for _, r := range rs {
			fmt.Fprintf(w, "| %s against %s, %s", s.title, s.against, r.machine())
			for _, im := range impls {
				switch {
				case im == s.base:
					fmt.Fprint(w, " | 1×")
				case im.bold():
					fmt.Fprintf(w, " | **%s**", ratio(r.geomean(s, im)))
				default:
					fmt.Fprintf(w, " | %s", ratio(r.geomean(s, im)))
				}
			}
			fmt.Fprintln(w, " |")
		}
	}
}

// bold reports whether the summary shows im's results in bold.
func (im impl) bold() bool { return im.name == "luart-jit" }

func main() {
	suiteName := flag.String("suite", "suite", `the benchmark: "suite" or "standard"`)
	tbl := flag.Bool("table", false, "print the Markdown table")
	svg := flag.String("svg", "", "write the chart to this SVG file")
	readmes := flag.String("readme", "", "comma-separated Markdown files whose table to replace")
	name := flag.String("name", "", "the table's name in -readme files")
	sum := flag.Bool("summary", false, "the summary table of each results file's geometric means")
	flag.Parse()
	s, ok := suites[*suiteName]
	switch {
	case *sum && flag.NArg() > 0 && *svg == "" && (*tbl || *readmes != "") && (*readmes == "" || *name != ""):
	case !ok || *sum || flag.NArg() != 1 || !*tbl && *svg == "" && *readmes == "" || *readmes != "" && *name == "":
		fmt.Fprintln(os.Stderr, "usage: chart [-suite suite|standard] [-table] [-svg out.svg] [-readme a.md,b.md -name NAME] results.txt")
		fmt.Fprintln(os.Stderr, "       chart -summary [-table] [-readme a.md,b.md -name NAME] results.txt...")
		os.Exit(2)
	}
	var rs []*results
	for _, path := range flag.Args() {
		f, err := os.Open(path)
		if err != nil {
			fatal(err)
		}
		r, err := parse(f)
		f.Close()
		if err != nil {
			fatal(err)
		}
		rs = append(rs, r)
	}
	for _, r := range rs {
		for k := range r.ns {
			if strings.HasSuffix(k, "/lua55") {
				useCLua55()
			}
		}
	}
	s = suites[*suiteName]
	if *sum {
		var t strings.Builder
		summary(&t, rs)
		if *tbl {
			fmt.Print(t.String())
		}
		for _, path := range strings.Split(*readmes, ",") {
			if path == "" {
				continue
			}
			if err := replaceTable(path, *name, t.String()); err != nil {
				fatal(err)
			}
		}
		return
	}
	r := rs[0]
	if *tbl {
		table(os.Stdout, s, r)
	}
	if *svg != "" {
		if err := os.WriteFile(*svg, []byte(chart(s, r)), 0o644); err != nil {
			fatal(err)
		}
	}
	if *readmes != "" {
		var t strings.Builder
		table(&t, s, r)
		for _, path := range strings.Split(*readmes, ",") {
			if err := replaceTable(path, *name, t.String()); err != nil {
				fatal(err)
			}
		}
	}
}

// replaceTable replaces the lines between <!-- suite-table name --> and
// the next <!-- /suite-table --> in the file at path with table.
func replaceTable(path, name, table string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	begin, end := "<!-- suite-table "+name+" -->\n", "<!-- /suite-table -->"
	before, rest, ok := strings.Cut(string(b), begin)
	if !ok {
		return fmt.Errorf("%s: no %q", path, strings.TrimSpace(begin))
	}
	_, after, ok := strings.Cut(rest, end)
	if !ok {
		return fmt.Errorf("%s: no %q after %q", path, end, strings.TrimSpace(begin))
	}
	return os.WriteFile(path, []byte(before+begin+table+end+after), 0o644)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
