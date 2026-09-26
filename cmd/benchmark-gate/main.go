// Command benchmark-gate fails when the typing benchmarks break their
// budget: more work per keystroke than the committed baseline, or more time
// than any machine should need.
//
//	benchmark-gate -baseline testdata/bench-baseline.txt bench.txt
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/colbytimm/alchemist/internal/benchmark"
)

// budget is docs/plan/23-syntax-highlighting.md's "The budget, enforced",
// as its implementation notes settle it. Allocations are held to the
// baseline because they do not depend on the machine; times only to a
// ceiling with room for a slow runner, and to ratios within one run.
var budget = benchmark.Budget{
	Tolerance: 0.05,
	Ceilings: []benchmark.Ceiling{
		{Benchmark: "BenchmarkDiagnose", NsPerOp: 5e6},
	},
	Growths: []benchmark.Growth{
		{Small: "BenchmarkTypingPlainBuffer/lines=200", Large: "BenchmarkTypingPlainBuffer/lines=2000", MaxRatio: 10},
		{Small: "BenchmarkViewWithoutEdit/lines=200", Large: "BenchmarkViewWithoutEdit/lines=2000", MaxRatio: 2},
	},
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "benchmark-gate:", err)
		os.Exit(1)
	}
}

func run() error {
	baselinePath := flag.String("baseline", "testdata/bench-baseline.txt", "committed benchmark output to hold the run to")
	flag.Parse()
	if flag.NArg() != 1 {
		return errors.New("usage: benchmark-gate -baseline <file> <results>")
	}
	baseline, err := readResults(*baselinePath)
	if err != nil {
		return err
	}
	current, err := readResults(flag.Arg(0))
	if err != nil {
		return err
	}
	if err := benchmark.Report(os.Stdout, baseline, current); err != nil {
		return err
	}
	violations := budget.Check(baseline, current)
	for _, violation := range violations {
		fmt.Fprintln(os.Stderr, violation)
	}
	if len(violations) > 0 {
		return fmt.Errorf("%d over budget", len(violations))
	}
	return nil
}

func readResults(path string) (benchmark.Results, error) {
	file, err := os.Open(path) // #nosec G304 -- a path the caller names on the command line
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }() // read-only: a close failure loses nothing
	return benchmark.Parse(file)
}
