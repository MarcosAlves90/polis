package packageverify

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/polis-dev/polis-v4/spec"
)

func canonicalPolicyBytes(t *testing.T) []byte {
	t.Helper()
	reason := "fixture-not-applicable"
	gates := make([]spec.GatePolicy, 0, len(spec.ProjectGateOrder))
	for _, id := range spec.ProjectGateOrder {
		switch id {
		case "test.complete":
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCommand, Command: &spec.CommandSpec{Argv: []string{"go", "test", "./..."}, Cwd: ".", TimeoutSeconds: 60}})
		case "coverage":
			th := 80.0
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCoverage, Command: &spec.CommandSpec{Argv: []string{"go", "test", "./...", "-coverprofile=.polis/coverage.out"}, Cwd: ".", TimeoutSeconds: 60}, Adapter: spec.CoverageAdapterGoCoverProfileV1, Report: ".polis/coverage.out", Operator: spec.CoverageOperatorGreaterThan, ThresholdPercent: &th})
		default:
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeNotApplicable, Reason: &reason})
		}
	}
	b, err := json.Marshal(spec.Policy{SchemaVersion: spec.PolicySchemaVersion, Gates: gates})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func canonicalChangeBytes(t *testing.T) []byte {
	t.Helper()
	c := spec.ChangeContract{SchemaVersion: 1, Kind: spec.ChangeKindFeature, Behavior: spec.CommandSpec{Argv: []string{"go", "test", "./..."}, Cwd: ".", TimeoutSeconds: 60}, Affected: spec.CommandSpec{Argv: []string{"go", "test", "./..."}, Cwd: ".", TimeoutSeconds: 60}, Regression: spec.RegressionContract{Mode: spec.RegressionModeNotApplicable, ReasonCode: spec.RegressionReasonNotDefect}}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func canonicalEvidence(t *testing.T, policyRaw, changeRaw []byte) []byte {
	t.Helper()
	p, err := spec.DecodePolicy(policyRaw)
	if err != nil {
		t.Fatal(err)
	}
	c, err := spec.DecodeChangeContract(changeRaw)
	if err != nil {
		t.Fatal(err)
	}
	events := []spec.EvidenceEvent{}
	reason := spec.RegressionReasonNotDefect
	events = append(events, spec.EvidenceEvent{Event: "gate_started", Gate: "regression"}, spec.EvidenceEvent{Event: "gate_finished", Gate: "regression", Status: spec.StatusNotApplicable, Reason: &reason})
	addPass := func(g string, cmd spec.CommandSpec) {
		exit := 0
		dur := int64(1)
		out, er := "", ""
		events = append(events, spec.EvidenceEvent{Event: "gate_started", Gate: g}, spec.EvidenceEvent{Event: "command_finished", Gate: g, Status: spec.StatusPass, Argv: cmd.Argv, Cwd: cmd.Cwd, ExitCode: &exit, DurationMS: &dur, Stdout: &out, Stderr: &er}, spec.EvidenceEvent{Event: "gate_finished", Gate: g, Status: spec.StatusPass})
	}
	addPass("behavior", c.Behavior)
	addPass("affected", c.Affected)
	for _, g := range p.Gates {
		events = append(events, spec.EvidenceEvent{Event: "gate_started", Gate: g.ID})
		switch g.Mode {
		case spec.GateModeNotApplicable:
			events = append(events, spec.EvidenceEvent{Event: "gate_finished", Gate: g.ID, Status: spec.StatusNotApplicable, Reason: g.Reason})
		case spec.GateModeCommand:
			exit := 0
			dur := int64(1)
			out, er := "", ""
			events = append(events, spec.EvidenceEvent{Event: "command_finished", Gate: g.ID, Status: spec.StatusPass, Argv: g.Command.Argv, Cwd: g.Command.Cwd, ExitCode: &exit, DurationMS: &dur, Stdout: &out, Stderr: &er}, spec.EvidenceEvent{Event: "gate_finished", Gate: g.ID, Status: spec.StatusPass})
		case spec.GateModeCoverage:
			exit := 0
			dur := int64(1)
			out, er := "", ""
			events = append(events, spec.EvidenceEvent{Event: "command_finished", Gate: g.ID, Status: spec.StatusPass, Argv: g.Command.Argv, Cwd: g.Command.Cwd, ExitCode: &exit, DurationMS: &dur, Stdout: &out, Stderr: &er})
			covered, total := 81, 100
			value := 81.0
			events = append(events, spec.EvidenceEvent{Event: "coverage_measured", Gate: "coverage", Status: spec.StatusPass, Adapter: g.Adapter, Report: g.Report, Metric: spec.CoverageMetricLinePercent, CoveredLines: &covered, TotalLines: &total, ValuePercent: &value, Operator: g.Operator, ThresholdPercent: g.ThresholdPercent}, spec.EvidenceEvent{Event: "gate_finished", Gate: g.ID, Status: spec.StatusPass})
		}
	}
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	for _, e := range events {
		if err := enc.Encode(e); err != nil {
			t.Fatal(err)
		}
	}
	return []byte(b.String())
}

func validManifest(policy, change, regression, payload []byte) []byte {
	ps := sha256.Sum256(policy)
	cs := sha256.Sum256(change)
	rs := sha256.Sum256(regression)
	xs := sha256.Sum256(payload)
	return []byte(`{"format_version":2,"project":"gitrex","change":"test-change","git_object_format":"sha1","base_commit":"1111111111111111111111111111111111111111","target_tree":"2222222222222222222222222222222222222222","policy_sha256":"` + hex.EncodeToString(ps[:]) + `","change_contract_sha256":"` + hex.EncodeToString(cs[:]) + `","regression_patch_sha256":"` + hex.EncodeToString(rs[:]) + `","payload_sha256":"` + hex.EncodeToString(xs[:]) + `"}`)
}

func writePackage(t *testing.T, mutate func(map[string][]byte)) string {
	t.Helper()
	policy := canonicalPolicyBytes(t)
	change := canonicalChangeBytes(t)
	payload := []byte("diff --git a/a b/a\n")
	regression := []byte{}
	members := map[string][]byte{"polis/polis-policy.json": policy, "polis/polis-change.json": change, "polis/polis-regression.patch": regression, "polis/polis-payload.patch": payload, "polis/polis-evidence.ndjson": canonicalEvidence(t, policy, change)}
	if mutate != nil {
		mutate(members)
	}
	if _, ok := members["polis/polis-manifest.json"]; !ok {
		members["polis/polis-manifest.json"] = validManifest(members["polis/polis-policy.json"], members["polis/polis-change.json"], members["polis/polis-regression.patch"], members["polis/polis-payload.patch"])
	}
	if _, ok := members["polis/polis-checksums.sha256"]; !ok {
		members["polis/polis-checksums.sha256"] = checksumBytes(members)
	}
	p := filepath.Join(t.TempDir(), "artifact.polis")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	names := keys(members)
	for _, n := range names {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(members[n]); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}
func keys(m map[string][]byte) []string {
	r := make([]string, 0, len(m))
	for k := range m {
		r = append(r, k)
	}
	sort.Strings(r)
	return r
}
func checksumBytes(m map[string][]byte) []byte {
	var b strings.Builder
	for _, n := range keys(m) {
		if n == "polis/polis-checksums.sha256" {
			continue
		}
		s := sha256.Sum256(m[n])
		b.WriteString(hex.EncodeToString(s[:]))
		b.WriteString("  ")
		b.WriteString(n)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

func TestVerifyAcceptsCanonicalPackage(t *testing.T) {
	r, err := Verify(writePackage(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if r.Project != "gitrex" {
		t.Fatalf("%+v", r)
	}
}
func TestVerifyRejectsExtraMember(t *testing.T) {
	p := writePackage(t, func(m map[string][]byte) { m["polis/extra.txt"] = []byte("x") })
	if _, err := Verify(p); err == nil {
		t.Fatal("expected error")
	}
}
func TestVerifyRejectsChecksumMismatch(t *testing.T) {
	p := writePackage(t, func(m map[string][]byte) {
		m["polis/polis-checksums.sha256"] = []byte(strings.Repeat("0", 64) + "  polis/polis-policy.json\n")
	})
	if _, err := Verify(p); err == nil {
		t.Fatal("expected error")
	}
}
func TestVerifyRejectsPathTraversal(t *testing.T) {
	p := writePackage(t, func(m map[string][]byte) { m["../escape"] = []byte("x") })
	if _, err := Verify(p); err == nil {
		t.Fatal("expected error")
	}
}
func TestVerifyRejectsMalformedEvidence(t *testing.T) {
	p := writePackage(t, func(m map[string][]byte) {
		m["polis/polis-evidence.ndjson"] = []byte(`{"event":"gate_started","gate":"behavior"}\n`)
	})
	if _, err := Verify(p); err == nil {
		t.Fatal("expected error")
	}
}
func TestVerifyRejectsIncompletePolicyEvidence(t *testing.T) {
	p := writePackage(t, func(m map[string][]byte) {
		e := canonicalEvidence(t, m["polis/polis-policy.json"], m["polis/polis-change.json"])
		trimmed := strings.TrimSuffix(string(e), "\n")
		cut := strings.LastIndex(trimmed, "\n")
		m["polis/polis-evidence.ndjson"] = []byte(trimmed[:cut+1])
	})
	if _, err := Verify(p); err == nil {
		t.Fatal("expected error")
	}
}
func TestVerifyRejectsMalformedPolicyOrChange(t *testing.T) {
	for _, member := range []string{"polis/polis-policy.json", "polis/polis-change.json"} {
		p := writePackage(t, func(m map[string][]byte) { m[member] = []byte(`{}`) })
		if _, err := Verify(p); err == nil {
			t.Fatalf("expected %s error", member)
		}
	}
}
func TestVerifyRejectsUnexpectedRegressionPatchForFeature(t *testing.T) {
	p := writePackage(t, func(m map[string][]byte) { m["polis/polis-regression.patch"] = []byte("x") })
	if _, err := Verify(p); err == nil {
		t.Fatal("expected error")
	}
}
func TestValidateMemberPathContract(t *testing.T) {
	if err := validateMemberPath("polis/polis-manifest.json"); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"", `polis\\bad`, "/polis/bad", "C:/bad", "polis/a/../bad", "polis//bad", "outside/file"} {
		if err := validateMemberPath(n); err == nil {
			t.Fatalf("expected %q invalid", n)
		}
	}
}
func TestVerifyManifestDigestsRejectsMismatch(t *testing.T) {
	contents := map[string][]byte{"polis/polis-policy.json": []byte("p"), "polis/polis-change.json": []byte("c"), "polis/polis-regression.patch": {}, "polis/polis-payload.patch": []byte("x")}
	sum := func(n string) string { s := sha256.Sum256(contents[n]); return hex.EncodeToString(s[:]) }
	m := spec.Manifest{PolicySHA256: sum("polis/polis-policy.json"), ChangeContractSHA256: sum("polis/polis-change.json"), RegressionPatchSHA256: sum("polis/polis-regression.patch"), PayloadSHA256: sum("polis/polis-payload.patch")}
	m.PayloadSHA256 = strings.Repeat("0", 64)
	if err := verifyManifestDigests(m, contents); err == nil {
		t.Fatal("expected error")
	}
}
func TestVerifyChecksumFileRejectsMalformedAndDuplicate(t *testing.T) {
	base := map[string][]byte{"polis/polis-manifest.json": []byte("m"), "polis/polis-policy.json": []byte("p"), "polis/polis-change.json": []byte("c"), "polis/polis-regression.patch": {}, "polis/polis-payload.patch": []byte("x"), "polis/polis-evidence.ndjson": []byte("e")}
	for _, v := range []string{"abc\n", strings.Repeat("A", 64) + "  polis/polis-policy.json\n", strings.Repeat("0", 64) + "  polis/polis-checksums.sha256\n"} {
		c := map[string][]byte{}
		for k, b := range base {
			c[k] = b
		}
		c["polis/polis-checksums.sha256"] = []byte(v)
		if err := verifyChecksumFile(c); err == nil {
			t.Fatalf("expected error for %q", v)
		}
	}
	s := sha256.Sum256(base["polis/polis-policy.json"])
	line := hex.EncodeToString(s[:]) + "  polis/polis-policy.json\n"
	c := map[string][]byte{}
	for k, b := range base {
		c[k] = b
	}
	c["polis/polis-checksums.sha256"] = []byte(line + line)
	if err := verifyChecksumFile(c); err == nil {
		t.Fatal("expected duplicate error")
	}
}
