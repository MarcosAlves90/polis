package spec

import (
	"strings"
	"testing"
)

type coverageErrorCase struct {
	name string
	raw  string
	want string
}

type coverageParser func([]byte) (CoverageMetric, error)

func TestCoverageAdaptersRejectMalformedInputs(t *testing.T) {
	t.Run("unsupported adapter", func(t *testing.T) {
		if _, err := ParseCoverage("unknown-v1", []byte("x")); err == nil || !strings.Contains(err.Error(), "unsupported coverage adapter") {
			t.Fatalf("error=%v", err)
		}
	})

	assertCoverageErrors(t, "go", ParseGoCoverProfile, []coverageErrorCase{
		{"invalid record", "mode: set\nnot-a-record\n", "invalid syntax"},
		{"zero start", "mode: set\na.go:0.1,1.2 1 1\n", "invalid start line"},
		{"end before start", "mode: set\na.go:3.1,2.2 1 1\n", "invalid end line"},
		{"count overflow", "mode: set\na.go:1.1,1.2 1 18446744073709551616\n", "invalid count"},
	})

	assertCoverageErrors(t, "lcov", ParseLCOV, []coverageErrorCase{
		{"empty source", "SF:\n", "empty source file"},
		{"data before source", "DA:1,1\n", "DA before SF"},
		{"short data", "SF:a.go\nDA:1\n", "invalid DA record"},
		{"bad line", "SF:a.go\nDA:x,1\n", "invalid source line"},
		{"zero line", "SF:a.go\nDA:0,1\n", "invalid source line"},
		{"bad hits", "SF:a.go\nDA:1,x\n", "invalid hit count"},
		{"negative hits", "SF:a.go\nDA:1,-1\n", "invalid hit count"},
		{"zero executable", "SF:a.go\nend_of_record\n", "zero executable lines"},
	})

	assertCoverageErrors(t, "cobertura", ParseCobertura, []coverageErrorCase{
		{"malformed xml", "<coverage>", "decode cobertura"},
		{"bad line", `<coverage><class filename="a.go"><line number="x" hits="1"/></class></coverage>`, "invalid line number"},
		{"zero line", `<coverage><class filename="a.go"><line number="0" hits="1"/></class></coverage>`, "invalid line number"},
		{"bad hits", `<coverage><class filename="a.go"><line number="1" hits="x"/></class></coverage>`, "invalid line hits"},
		{"negative hits", `<coverage><class filename="a.go"><line number="1" hits="-1"/></class></coverage>`, "invalid line hits"},
		{"line outside class", `<coverage><line number="1" hits="1"/></coverage>`, "zero executable lines"},
		{"class without filename", `<coverage><class><line number="1" hits="1"/></class></coverage>`, "zero executable lines"},
	})
}

func assertCoverageErrors(t *testing.T, prefix string, parse coverageParser, cases []coverageErrorCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(prefix+"/"+tc.name, func(t *testing.T) {
			_, err := parse([]byte(tc.raw))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v want=%q", err, tc.want)
			}
		})
	}
}

func TestCoverageAdaptersDeduplicateEquivalentLineRecords(t *testing.T) {
	goProfile := []byte("mode: set\n" +
		"a.go:10.1,12.2 1 0\n" +
		"a.go:10.1,10.9 1 1\n" +
		"a.go:11.1,12.2 1 1\n")
	m, err := ParseGoCoverProfile(goProfile)
	if err != nil {
		t.Fatal(err)
	}
	if m.TotalLines != 3 || m.CoveredLines != 3 || m.Percent != 100 {
		t.Fatalf("metric=%+v", m)
	}

	lcov := []byte("SF:a.go\nDA:10,0\nDA:10,2\nDA:11,1\nend_of_record\n")
	m, err = ParseLCOV(lcov)
	if err != nil {
		t.Fatal(err)
	}
	if m.TotalLines != 2 || m.CoveredLines != 2 || m.Percent != 100 {
		t.Fatalf("metric=%+v", m)
	}
}
