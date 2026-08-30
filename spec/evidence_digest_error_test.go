package spec

import (
	"strings"
	"testing"
)

func TestDecodeEvidenceRejectsMalformedDigestedCommandOutput(t *testing.T) {
	hash := strings.Repeat("0", 64)
	cases := []struct {
		name   string
		stdout string
		stderr string
		extra  string
	}{
		{"bad stdout hash", "bad", hash, ``},
		{"bad stderr hash", hash, "bad", ``},
		{"bad environment pass", hash, hash, `,"environment_mode":"clean","environment_pass":1`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(`{"event":"command_finished","gate":"lint","status":"PASS","argv":["go"],"cwd":".","exit_code":0,"duration_ms":1,"stdout_bytes":0,"stderr_bytes":0,"stdout_sha256":"` + tc.stdout + `","stderr_sha256":"` + tc.stderr + `","stdout_truncated":false,"stderr_truncated":false` + tc.extra + `}` + "\n")
			if _, err := DecodeEvidence(raw); err == nil {
				t.Fatalf("expected rejection for %s", raw)
			}
		})
	}

	missing := []byte(`{"event":"command_finished","gate":"lint","status":"PASS","argv":["go"],"cwd":".","exit_code":0,"duration_ms":1,"stdout_bytes":0,"stdout_sha256":"` + hash + `","stderr_sha256":"` + hash + `","stdout_truncated":false,"stderr_truncated":false}` + "\n")
	if _, err := DecodeEvidence(missing); err == nil || !strings.Contains(err.Error(), "missing stderr_bytes") {
		t.Fatalf("missing field error=%v", err)
	}

	badStdoutBytes := []byte(`{"event":"command_finished","gate":"lint","status":"PASS","argv":["go"],"cwd":".","exit_code":0,"duration_ms":1,"stdout_bytes":-1,"stderr_bytes":0,"stdout_sha256":"` + hash + `","stderr_sha256":"` + hash + `","stdout_truncated":false,"stderr_truncated":false}` + "\n")
	if _, err := DecodeEvidence(badStdoutBytes); err == nil || !strings.Contains(err.Error(), "stdout_bytes") {
		t.Fatalf("stdout bytes error=%v", err)
	}

	badStderrBytes := []byte(`{"event":"command_finished","gate":"lint","status":"PASS","argv":["go"],"cwd":".","exit_code":0,"duration_ms":1,"stdout_bytes":0,"stderr_bytes":-1,"stdout_sha256":"` + hash + `","stderr_sha256":"` + hash + `","stdout_truncated":false,"stderr_truncated":false}` + "\n")
	if _, err := DecodeEvidence(badStderrBytes); err == nil || !strings.Contains(err.Error(), "stderr_bytes") {
		t.Fatalf("stderr bytes error=%v", err)
	}

	badStdoutTrunc := []byte(`{"event":"command_finished","gate":"lint","status":"PASS","argv":["go"],"cwd":".","exit_code":0,"duration_ms":1,"stdout_bytes":0,"stderr_bytes":0,"stdout_sha256":"` + hash + `","stderr_sha256":"` + hash + `","stdout_truncated":1,"stderr_truncated":false}` + "\n")
	if _, err := DecodeEvidence(badStdoutTrunc); err == nil || !strings.Contains(err.Error(), "stdout_truncated") {
		t.Fatalf("stdout truncated error=%v", err)
	}

	badStderrTrunc := []byte(`{"event":"command_finished","gate":"lint","status":"PASS","argv":["go"],"cwd":".","exit_code":0,"duration_ms":1,"stdout_bytes":0,"stderr_bytes":0,"stdout_sha256":"` + hash + `","stderr_sha256":"` + hash + `","stdout_truncated":false,"stderr_truncated":1}` + "\n")
	if _, err := DecodeEvidence(badStderrTrunc); err == nil || !strings.Contains(err.Error(), "stderr_truncated") {
		t.Fatalf("stderr truncated error=%v", err)
	}
}

func TestDecodeEvidenceOracleAndCoverageArithmeticErrors(t *testing.T) {
	cases := [][]byte{
		[]byte(`{"event":"oracle_checked","gate":"regression","status":"FAIL","oracle":"baseline_output_contains","oracle_index":0}` + "\n"),
		[]byte(`{"event":"oracle_checked","gate":"regression","status":"PASS","oracle":"other","oracle_index":0}` + "\n"),
		[]byte(`{"event":"oracle_checked","gate":"regression","status":"PASS","oracle":"baseline_output_contains","oracle_index":-1}` + "\n"),
		[]byte(`{"event":"coverage_measured","gate":"coverage","status":"PASS","adapter":"go-coverprofile-v1","report":".polis/coverage.out","metric":"line_coverage_percent","covered_lines":81,"total_lines":100,"value_percent":82,"operator":">","threshold_percent":80}` + "\n"),
	}
	for _, raw := range cases {
		if _, err := DecodeEvidence(raw); err == nil {
			t.Fatalf("expected rejection for %s", raw)
		}
	}
}

func TestDecodeEvidenceRejectsOversizedLine(t *testing.T) {
	raw := append([]byte(`{"event":"gate_started","gate":"lint","padding":"`), []byte(strings.Repeat("x", 1024*1024))...)
	raw = append(raw, []byte(`"}`+"\n")...)
	if _, err := DecodeEvidence(raw); err == nil || !strings.Contains(err.Error(), "read evidence") {
		t.Fatalf("error=%v", err)
	}
}
