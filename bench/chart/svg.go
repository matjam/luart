package main

import (
	"fmt"
	"html"
	"math"
	"strconv"
	"strings"
)

// The chart: for each workload, one bar per interpreter from 1× (as fast
// as the base) to its time divided by the base's, on a log scale. Colors
// are the reference palette's categorical slots 1 to 4 and 7 (violet, as
// magenta is too close to aqua for deuteranopes in dark mode, adjacent
// when Lua 5.4 is the base), light and dark, which pass its colorblind
// checks in the bars' order in both charts; every bar is labelled with its
// value, so no color is read alone.
//
// Colors are written out per class rather than as custom properties, which
// some SVG renderers ignore; a renderer without media queries gets light.
const style = `
text { font-family: system-ui, -apple-system, "Segoe UI", sans-serif; }
.bg { fill: #fcfcfb; }
.title { fill: #0b0b0b; font-size: 16px; font-weight: 600; }
.sub { fill: #52514e; font-size: 12px; }
.label { fill: #0b0b0b; font-size: 12px; }
.mean { font-weight: 600; }
.value { fill: #52514e; font-size: 11px; font-variant-numeric: tabular-nums; }
.tick { fill: #898781; font-size: 11px; font-variant-numeric: tabular-nums; }
.grid { stroke: #e1e0d9; stroke-width: 1; }
.base { stroke: #c3c2b7; stroke-width: 1; }
.s0 { fill: #2a78d6; } .s1 { fill: #eb6834; } .s2 { fill: #1baf7a; } .s3 { fill: #eda100; } .s4 { fill: #4a3aa7; }
@media (prefers-color-scheme: dark) {
  .bg { fill: #1a1a19; }
  .title, .label { fill: #ffffff; }
  .sub, .value { fill: #c3c2b7; }
  .grid { stroke: #2c2c2a; }
  .base { stroke: #383835; }
  .s0 { fill: #3987e5; } .s1 { fill: #d95926; } .s2 { fill: #199e70; } .s3 { fill: #c98500; } .s4 { fill: #9085e9; }
}
`

const (
	width     = 880.0
	plotLeft  = 220.0
	plotRight = width - 64 // room for the last value label
	plotTop   = 104.0
	barH      = 10.0
	barGap    = 2.0
	groupGap  = 18.0
)

func chart(s suite, r *results) string {
	type bar struct {
		ratio float64
		impl  int
	}
	impls := r.impls(s)
	// One group per workload, then the geometric mean.
	labels := make([]string, 0, len(s.workloads)+1)
	groups := make([][]bar, len(s.workloads)+1)
	lo, hi := 1.0, 1.0
	add := func(g int, ratio float64, j int) {
		if math.IsNaN(ratio) || math.IsInf(ratio, 0) {
			return
		}
		groups[g] = append(groups[g], bar{ratio, j})
		lo, hi = math.Min(lo, ratio), math.Max(hi, ratio)
	}
	for i, wl := range s.workloads {
		labels = append(labels, wl.label)
		for j, im := range impls {
			add(i, r.ratio(s, wl, im), j)
		}
	}
	labels = append(labels, "geometric mean")
	for j, im := range impls {
		add(len(s.workloads), r.geomean(s, im), j)
	}
	lo, hi = niceBelow(lo), niceAbove(hi)
	x := func(v float64) float64 {
		return plotLeft + (math.Log10(v)-math.Log10(lo))/(math.Log10(hi)-math.Log10(lo))*(plotRight-plotLeft)
	}
	groupH := float64(len(impls))*(barH+barGap) - barGap
	plotBottom := plotTop + float64(len(groups))*(groupH+groupGap) - groupGap
	height := plotBottom + 44

	var b strings.Builder
	title := "Time relative to " + s.against
	sub := fmt.Sprintf("%s, %s/%s. Median time of each workload divided by that of %s; log scale, left of 1× is faster.",
		r.cpu, r.goos, r.goarch, s.against)
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %g %g" width="%g" height="%g" role="img" aria-labelledby="t d">`+"\n",
		width, height, width, height)
	fmt.Fprintf(&b, "<title id=\"t\">%s</title>\n<desc id=\"d\">%s</desc>\n<style>%s</style>\n",
		html.EscapeString(title), html.EscapeString(sub), style)
	fmt.Fprintf(&b, `<rect class="bg" width="%g" height="%g" rx="8"/>`+"\n", width, height)
	fmt.Fprintf(&b, `<text class="title" x="24" y="32">%s</text>`+"\n", html.EscapeString(title))
	fmt.Fprintf(&b, `<text class="sub" x="24" y="52">%s</text>`+"\n", html.EscapeString(sub))

	// Legend, in series order.
	lx := 24.0
	for _, im := range impls {
		fmt.Fprintf(&b, `<rect class="s%d" x="%g" y="70" width="12" height="12" rx="2"/>`, im.slot, lx)
		fmt.Fprintf(&b, `<text class="label" x="%g" y="80">%s</text>`+"\n", lx+18, html.EscapeString(im.label))
		lx += 18 + 6.6*float64(len(im.label)) + 28 // about 6.6px a character
	}

	// Gridlines and ticks at 1, 2 and 5 times each power of ten.
	for _, t := range ticks(lo, hi) {
		cls := "grid"
		if t == 1 {
			cls = "base"
		}
		fmt.Fprintf(&b, `<line class="%s" x1="%.1f" x2="%.1f" y1="%g" y2="%g"/>`, cls, x(t), x(t), plotTop-6, plotBottom+6)
		fmt.Fprintf(&b, `<text class="tick" x="%.1f" y="%g" text-anchor="middle">%s×</text>`+"\n", x(t), plotBottom+24, tickLabel(t))
	}

	// Bars: 2px apart, square at the 1× baseline and rounded at the value.
	for i, label := range labels {
		top := plotTop + float64(i)*(groupH+groupGap)
		cls := "label"
		if i == len(s.workloads) {
			cls = "label mean"
			fmt.Fprintf(&b, `<line class="base" x1="24" x2="%g" y1="%.1f" y2="%.1f"/>`+"\n", width-24, top-groupGap/2, top-groupGap/2)
		}
		fmt.Fprintf(&b, `<text class="%s" x="%g" y="%.1f" text-anchor="end" dominant-baseline="middle">%s</text>`+"\n",
			cls, plotLeft-12, top+groupH/2, html.EscapeString(label))
		ran := make([]bool, len(impls))
		for _, br := range groups[i] {
			ran[br.impl] = true
		}
		for j, ok := range ran {
			if !ok {
				fmt.Fprintf(&b, `<text class="value" x="%.1f" y="%.1f" dominant-baseline="middle">%s: no result</text>`+"\n",
					x(1)+5, top+float64(j)*(barH+barGap)+barH/2, html.EscapeString(impls[j].label))
			}
		}
		for _, br := range groups[i] {
			y := top + float64(br.impl)*(barH+barGap)
			x0, x1 := x(1), x(br.ratio)
			fmt.Fprintf(&b, `<path class="s%d" d="%s"><title>%s: %s</title></path>`,
				impls[br.impl].slot, barPath(x0, x1, y, barH), html.EscapeString(impls[br.impl].label), times(br.ratio))
			anchor, lx := "start", x1+5
			if x1 < x0 {
				anchor, lx = "end", x1-5
			}
			fmt.Fprintf(&b, `<text class="value" x="%.1f" y="%.1f" text-anchor="%s" dominant-baseline="middle">%s</text>`+"\n",
				lx, y+barH/2, anchor, ratio(br.ratio))
		}
	}
	b.WriteString("</svg>\n")
	return b.String()
}

// barPath draws a bar from x0, the baseline, to x1, rounding only the
// corners at x1.
func barPath(x0, x1, y, h float64) string {
	w := math.Abs(x1 - x0)
	r := math.Min(4, math.Min(w, h/2))
	if x1 >= x0 {
		return fmt.Sprintf("M%.1f %.1fH%.1fQ%.1f %.1f %.1f %.1fV%.1fQ%.1f %.1f %.1f %.1fH%.1fZ",
			x0, y, x1-r, x1, y, x1, y+r, y+h-r, x1, y+h, x1-r, y+h, x0)
	}
	return fmt.Sprintf("M%.1f %.1fH%.1fQ%.1f %.1f %.1f %.1fV%.1fQ%.1f %.1f %.1f %.1fH%.1fZ",
		x0, y, x1+r, x1, y, x1, y+r, y+h-r, x1, y+h, x1+r, y+h, x0)
}

// niceAbove returns the smallest 1, 2 or 5 times a power of ten at or
// above v; niceBelow the largest at or below it.
func niceAbove(v float64) float64 {
	p := math.Pow(10, math.Floor(math.Log10(v)))
	for _, m := range []float64{1, 2, 5, 10} {
		if m*p >= v*(1-1e-9) {
			return m * p
		}
	}
	return 10 * p
}

func niceBelow(v float64) float64 {
	p := math.Pow(10, math.Floor(math.Log10(v)))
	for _, m := range []float64{5, 2, 1} {
		if m*p <= v*(1+1e-9) {
			return m * p
		}
	}
	return p
}

func ticks(lo, hi float64) []float64 {
	var t []float64
	for p := math.Pow(10, math.Floor(math.Log10(lo))); p <= hi; p *= 10 {
		for _, m := range []float64{1, 2, 5} {
			if v := m * p; v >= lo*(1-1e-9) && v <= hi*(1+1e-9) {
				t = append(t, v)
			}
		}
	}
	return t
}

func tickLabel(t float64) string {
	return strconv.FormatFloat(t, 'g', -1, 64)
}
