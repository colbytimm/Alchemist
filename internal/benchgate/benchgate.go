// Package benchgate holds benchmark results to a committed baseline and to
// budgets of their own, for CI to fail on.
package benchgate

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Result is one benchmark's median over every run of it in the output.
type Result struct {
	NsPerOp     float64
	BytesPerOp  float64
	AllocsPerOp float64
}

// Results are keyed by benchmark name, without the GOMAXPROCS suffix, so
// runs on machines of different sizes compare.
type Results map[string]Result

var procsSuffix = regexp.MustCompile(`-\d+$`)

// Parse reads `go test -bench -benchmem` output. Lines that are not results
// are skipped, as the output interleaves them with package headers.
func Parse(r io.Reader) (Results, error) {
	samples := map[string][]Result{}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		name, result, ok := parseLine(scanner.Text())
		if ok {
			samples[name] = append(samples[name], result)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("benchgate: read results: %w", err)
	}
	results := Results{}
	for name, runs := range samples {
		results[name] = median(runs)
	}
	return results, nil
}

// parseLine reads "BenchmarkX-4  50  1234 ns/op  56 B/op  7 allocs/op".
func parseLine(line string) (string, Result, bool) {
	fields := strings.Fields(line)
	if len(fields) < 4 || !strings.HasPrefix(fields[0], "Benchmark") {
		return "", Result{}, false
	}
	var result Result
	seen := false
	for i := 2; i+1 < len(fields); i += 2 {
		value, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			return "", Result{}, false
		}
		switch fields[i+1] {
		case "ns/op":
			result.NsPerOp, seen = value, true
		case "B/op":
			result.BytesPerOp = value
		case "allocs/op":
			result.AllocsPerOp = value
		}
	}
	return procsSuffix.ReplaceAllString(fields[0], ""), result, seen
}

func median(runs []Result) Result {
	return Result{
		NsPerOp:     medianOf(runs, func(r Result) float64 { return r.NsPerOp }),
		BytesPerOp:  medianOf(runs, func(r Result) float64 { return r.BytesPerOp }),
		AllocsPerOp: medianOf(runs, func(r Result) float64 { return r.AllocsPerOp }),
	}
}

func medianOf(runs []Result, metric func(Result) float64) float64 {
	values := make([]float64, 0, len(runs))
	for _, run := range runs {
		values = append(values, metric(run))
	}
	slices.Sort(values)
	middle := len(values) / 2
	if len(values)%2 == 1 {
		return values[middle]
	}
	return (values[middle-1] + values[middle]) / 2
}

// Ceiling is a time a benchmark may not exceed on any machine.
type Ceiling struct {
	Benchmark string
	NsPerOp   float64
}

// Growth bounds how much slower Large may be than Small in the same run.
type Growth struct {
	Small, Large string
	MaxRatio     float64
}

// Budget is what a run is held to. Tolerance is the relative growth over
// the baseline allowed in bytes and allocations per op.
type Budget struct {
	Tolerance float64
	Ceilings  []Ceiling
	Growths   []Growth
}

// Check lists every way current breaks the budget, in a stable order.
func (b Budget) Check(baseline, current Results) []string {
	var violations []string
	for _, name := range sortedNames(baseline) {
		violations = append(violations, b.checkAgainstBaseline(name, baseline[name], current)...)
	}
	for _, ceiling := range b.Ceilings {
		violations = append(violations, checkCeiling(ceiling, current)...)
	}
	for _, growth := range b.Growths {
		violations = append(violations, checkGrowth(growth, current)...)
	}
	return violations
}

func (b Budget) checkAgainstBaseline(name string, want Result, current Results) []string {
	got, ok := current[name]
	if !ok {
		return []string{name + ": in the baseline but not run"}
	}
	var violations []string
	if exceeds(got.AllocsPerOp, want.AllocsPerOp, b.Tolerance) {
		violations = append(violations, fmt.Sprintf("%s: %.0f allocs/op, over %.0f by more than %.0f%%",
			name, got.AllocsPerOp, want.AllocsPerOp, b.Tolerance*100))
	}
	if exceeds(got.BytesPerOp, want.BytesPerOp, b.Tolerance) {
		violations = append(violations, fmt.Sprintf("%s: %.0f B/op, over %.0f by more than %.0f%%",
			name, got.BytesPerOp, want.BytesPerOp, b.Tolerance*100))
	}
	return violations
}

func exceeds(got, want, tolerance float64) bool {
	return got > want*(1+tolerance)
}

func checkCeiling(c Ceiling, current Results) []string {
	got, ok := current[c.Benchmark]
	switch {
	case !ok:
		return []string{c.Benchmark + ": has a ceiling but was not run"}
	case got.NsPerOp > c.NsPerOp:
		return []string{fmt.Sprintf("%s: %s/op, over its ceiling of %s", c.Benchmark, duration(got.NsPerOp), duration(c.NsPerOp))}
	}
	return nil
}

func checkGrowth(g Growth, current Results) []string {
	small, okSmall := current[g.Small]
	large, okLarge := current[g.Large]
	switch {
	case !okSmall || !okLarge:
		return []string{fmt.Sprintf("%s, %s: bounded against each other but not both run", g.Small, g.Large)}
	case large.NsPerOp > small.NsPerOp*g.MaxRatio:
		return []string{fmt.Sprintf("%s: %.1f× %s, over the %.0f× allowed",
			g.Large, large.NsPerOp/small.NsPerOp, g.Small, g.MaxRatio)}
	}
	return nil
}

// Report tabulates current against baseline, time included: a time on
// another machine is shown for reference, never judged.
func Report(w io.Writer, baseline, current Results) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%-44s %12s %12s %8s %12s %12s\n", "benchmark", "base time", "time", "Δ", "base allocs", "allocs")
	for _, name := range sortedNames(current) {
		got := current[name]
		want, ok := baseline[name]
		if !ok {
			fmt.Fprintf(&b, "%-44s %12s %12s %8s %12s %12.0f\n", name, "-", duration(got.NsPerOp), "-", "-", got.AllocsPerOp)
			continue
		}
		fmt.Fprintf(&b, "%-44s %12s %12s %+7.1f%% %12.0f %12.0f\n", name, duration(want.NsPerOp), duration(got.NsPerOp),
			(got.NsPerOp/want.NsPerOp-1)*100, want.AllocsPerOp, got.AllocsPerOp)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func duration(ns float64) string {
	return strconv.FormatFloat(ns/1e6, 'f', 3, 64) + "ms"
}

func sortedNames(results Results) []string {
	names := make([]string, 0, len(results))
	for name := range results {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
