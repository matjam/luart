// Command chart turns raw BenchmarkSuite output into the comparison table
// and chart in bench/README.md.
//
//	go run ./chart -table suite-results-amd64.txt
//	go run ./chart -svg suite-amd64.svg suite-results-amd64.txt
//
// Each timing is the median of the runs in the file. Every interpreter is
// compared with native Go.
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

// workloads lists the suite's workloads in table order, with their labels.
var workloads = []struct{ name, label string }{
	{"fib", "fib(25), recursive calls"},
	{"numeric-loop", "numeric loop, 1M iterations"},
	{"array-fill-sum", "array fill and sum, 100k"},
	{"records", "records, 10k tables"},
	{"closures", "closures, 100k"},
	{"sort", "sort 10k with comparator"},
	{"string-build", "string build, 10k pieces"},
	{"go-calls", "calls into Go, 100k"},
	{"plasma", "plasma frame"},
	{"particles", "particles frame"},
}

// impls are the interpreters, in column and series order, by the name of
// their sub-benchmark.
var impls = []struct{ name, label string }{
	{"luart", "Luart (no JIT)"},
	{"luart-jit", "Luart (JIT)"},
	{"shopify", "go-lua"},
}

type results struct {
	cpu, goos, goarch string
	ns                map[string][]float64 // by "workload/impl"
}

var line = regexp.MustCompile(`^Benchmark(Suite/([\w-]+)/([\w-]+)|GoParticles)-\d+\s+\d+\s+([\d.]+) ns/op`)

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
		key := m[2] + "/" + m[3]
		if m[1] == "GoParticles" { // the suite has no native particles
			key = "particles/go"
		}
		ns, err := strconv.ParseFloat(m[4], 64)
		if err != nil {
			return nil, err
		}
		res.ns[key] = append(res.ns[key], ns)
	}
	return res, s.Err()
}

// median returns the median time of key in milliseconds, or NaN.
func (r *results) median(key string) float64 {
	v := slices.Clone(r.ns[key])
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

// times formats how many times slower than Go ratio is.
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

func table(w io.Writer, r *results) {
	fmt.Fprint(w, "| Workload | Native Go")
	for _, im := range impls {
		fmt.Fprintf(w, " | %s", im.label)
	}
	for _, im := range impls {
		fmt.Fprintf(w, " | %s vs Go", im.label)
	}
	fmt.Fprintln(w, " |")
	fmt.Fprintln(w, "|---"+strings.Repeat("|---", 1+2*len(impls))+"|")
	for _, wl := range workloads {
		g := r.median(wl.name + "/go")
		fmt.Fprintf(w, "| %s | %s", wl.label, ms(g))
		for _, im := range impls {
			fmt.Fprintf(w, " | %s", ms(r.median(wl.name+"/"+im.name)))
		}
		for _, im := range impls {
			fmt.Fprintf(w, " | %s", times(r.median(wl.name+"/"+im.name)/g))
		}
		fmt.Fprintln(w, " |")
	}
}

func main() {
	tbl := flag.Bool("table", false, "print the Markdown table")
	svg := flag.String("svg", "", "write the chart to this SVG file")
	flag.Parse()
	if flag.NArg() != 1 || !*tbl && *svg == "" {
		fmt.Fprintln(os.Stderr, "usage: chart [-table] [-svg out.svg] results.txt")
		os.Exit(2)
	}
	f, err := os.Open(flag.Arg(0))
	if err != nil {
		fatal(err)
	}
	r, err := parse(f)
	f.Close()
	if err != nil {
		fatal(err)
	}
	if *tbl {
		table(os.Stdout, r)
	}
	if *svg != "" {
		if err := os.WriteFile(*svg, []byte(chart(r)), 0o644); err != nil {
			fatal(err)
		}
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
