package benchgate_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/benchgate"
)

const output = `goos: linux
pkg: github.com/colbytimm/alchemist/internal/tui/test
BenchmarkTyping/lines=200-4   	      50	   3000000 ns/op	 2000 B/op	   100 allocs/op
BenchmarkTyping/lines=200-4   	      50	   1000000 ns/op	 1000 B/op	   300 allocs/op
BenchmarkTyping/lines=200-4   	      50	   2000000 ns/op	 3000 B/op	   200 allocs/op
BenchmarkView-16              	      50	    500000 ns/op
PASS
ok  	github.com/colbytimm/alchemist/internal/tui/test	15.670s
`

func TestParseTakesTheMedianOfEachMetric(t *testing.T) {
	got, err := benchgate.Parse(strings.NewReader(output))

	require.NoError(t, err)
	require.Equal(t, benchgate.Results{
		"BenchmarkTyping/lines=200": {NsPerOp: 2e6, BytesPerOp: 2000, AllocsPerOp: 200},
		"BenchmarkView":             {NsPerOp: 5e5},
	}, got)
}

func TestParseAveragesTheMiddleTwoOfAnEvenCount(t *testing.T) {
	got, err := benchgate.Parse(strings.NewReader("BenchmarkX-4 1 10 ns/op\nBenchmarkX-4 1 20 ns/op\n"))

	require.NoError(t, err)
	require.InDelta(t, 15.0, got["BenchmarkX"].NsPerOp, 0)
}

func TestCheck(t *testing.T) {
	budget := benchgate.Budget{
		Tolerance: 0.05,
		Ceilings:  []benchgate.Ceiling{{Benchmark: "BenchmarkSmall", NsPerOp: 2e6}},
		Growths:   []benchgate.Growth{{Small: "BenchmarkSmall", Large: "BenchmarkLarge", MaxRatio: 10}},
	}
	baseline := benchgate.Results{
		"BenchmarkSmall": {NsPerOp: 1e6, BytesPerOp: 1000, AllocsPerOp: 100},
		"BenchmarkLarge": {NsPerOp: 5e6, BytesPerOp: 1000, AllocsPerOp: 100},
	}
	tests := []struct {
		name    string
		current benchgate.Results
		want    []string
	}{
		{
			name: "within every budget, however slow the machine against the baseline",
			current: benchgate.Results{
				"BenchmarkSmall": {NsPerOp: 1.9e6, BytesPerOp: 1050, AllocsPerOp: 105},
				"BenchmarkLarge": {NsPerOp: 19e6, BytesPerOp: 900, AllocsPerOp: 90},
			},
		},
		{
			name: "more allocations and bytes than the baseline allows",
			current: benchgate.Results{
				"BenchmarkSmall": {NsPerOp: 1e6, BytesPerOp: 1051, AllocsPerOp: 106},
				"BenchmarkLarge": {NsPerOp: 5e6, BytesPerOp: 1000, AllocsPerOp: 100},
			},
			want: []string{
				"BenchmarkSmall: 106 allocs/op, over 100 by more than 5%",
				"BenchmarkSmall: 1051 B/op, over 1000 by more than 5%",
			},
		},
		{
			name: "over a ceiling",
			current: benchgate.Results{
				"BenchmarkSmall": {NsPerOp: 2.5e6, BytesPerOp: 1000, AllocsPerOp: 100},
				"BenchmarkLarge": {NsPerOp: 5e6, BytesPerOp: 1000, AllocsPerOp: 100},
			},
			want: []string{"BenchmarkSmall: 2.500ms/op, over its ceiling of 2.000ms"},
		},
		{
			name: "growing faster than allowed",
			current: benchgate.Results{
				"BenchmarkSmall": {NsPerOp: 1e6, BytesPerOp: 1000, AllocsPerOp: 100},
				"BenchmarkLarge": {NsPerOp: 11e6, BytesPerOp: 1000, AllocsPerOp: 100},
			},
			want: []string{"BenchmarkLarge: 11.0× BenchmarkSmall, over the 10× allowed"},
		},
		{
			name:    "a benchmark missing from the run",
			current: benchgate.Results{"BenchmarkSmall": {NsPerOp: 1e6, BytesPerOp: 1000, AllocsPerOp: 100}},
			want: []string{
				"BenchmarkLarge: in the baseline but not run",
				"BenchmarkSmall, BenchmarkLarge: bounded against each other but not both run",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, budget.Check(baseline, tt.current))
		})
	}
}

func TestReportShowsTheTimeAgainstTheBaseline(t *testing.T) {
	var out strings.Builder
	baseline := benchgate.Results{"BenchmarkX": {NsPerOp: 2e6, AllocsPerOp: 10}}
	current := benchgate.Results{"BenchmarkX": {NsPerOp: 1e6, AllocsPerOp: 5}, "BenchmarkNew": {NsPerOp: 1e6}}

	require.NoError(t, benchgate.Report(&out, baseline, current))

	require.Regexp(t, `BenchmarkX\s+2\.000ms\s+1\.000ms\s+-50\.0%\s+10\s+5`, out.String())
	require.Regexp(t, `BenchmarkNew\s+-\s+1\.000ms`, out.String())
}
