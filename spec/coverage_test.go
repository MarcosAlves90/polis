package spec

import (
	"math"
	"strings"
	"testing"
)

func TestParseGoCoverProfileLineCoverage(t *testing.T) {
	raw := "mode: set\n" +
		"example/a.go:10.1,12.2 2 1\n" +
		"example/a.go:12.2,13.9 1 0\n" +
		"example/b.go:10.1,10.9 1 0\n"
	m, err := ParseGoCoverProfile([]byte(raw))
	if err != nil {
		t.Fatalf("ParseGoCoverProfile() error = %v", err)
	}
	if m.TotalLines != 5 || m.CoveredLines != 3 {
		t.Fatalf("metric=%+v", m)
	}
	if math.Abs(m.Percent-60.0) > 1e-9 {
		t.Fatalf("percent=%v", m.Percent)
	}
}

func TestParseGoCoverProfileRejectsMalformedAndZeroExecutable(t *testing.T) {
	cases := []string{
		"",
		"mode: weird\nexample/a.go:1.1,1.2 1 1\n",
		"mode: set\n",
		"mode: set\nnot-a-record\n",
		"mode: set\nexample/a.go:3.1,2.1 1 1\n",
		"mode: set\nexample/a.go:1.1,1.2 1 -1\n",
	}
	for _, raw := range cases {
		if _, err := ParseGoCoverProfile([]byte(raw)); err == nil {
			t.Fatalf("expected rejection for %q", raw)
		}
	}
}

func TestCoveragePassUsesStrictGreaterThan(t *testing.T) {
	if CoveragePass(80.0, 80.0) {
		t.Fatal("exactly 80 must fail threshold 80")
	}
	if !CoveragePass(80.01, 80.0) {
		t.Fatal("80.01 must pass threshold 80")
	}
	if CoveragePass(90.0, 90.0) {
		t.Fatal("exact threshold must fail")
	}
}

func TestDecodePolicyRequiresCoverageModeAndAdapter(t *testing.T) {
	canonical := canonicalPolicyJSON()
	if _, err := DecodePolicy([]byte(canonical)); err != nil {
		t.Fatalf("canonical alpha.5 policy rejected: %v", err)
	}
	old := strings.Replace(canonical,
		`{"id":"coverage","mode":"coverage","command":{"argv":["go","test","./...","-coverprofile=.polis/coverage.out"],"cwd":".","timeout_seconds":1200},"adapter":"go-coverprofile-v1","report":".polis/coverage.out","operator":">","threshold_percent":80}`,
		`{"id":"coverage","mode":"command","command":{"argv":["go","test","./..."],"cwd":".","timeout_seconds":1200}}`, 1)
	if _, err := DecodePolicy([]byte(old)); err == nil {
		t.Fatal("expected alpha.4 coverage command policy rejection")
	}
}
