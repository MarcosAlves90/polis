package spec

import "testing"

func validCommand() CommandSpec {
	return CommandSpec{Argv: []string{"go", "test", "./..."}, Cwd: ".", TimeoutSeconds: 60}
}

func TestDecodeChangeContractDefect(t *testing.T) {
	raw := []byte(`{"schema_version":1,"kind":"defect","behavior":{"argv":["go","test","./..."],"cwd":".","timeout_seconds":60},"affected":{"argv":["go","test","./..."],"cwd":".","timeout_seconds":60},"regression":{"mode":"red_green","command":{"argv":["go","test","./...","-run","TestBug"],"cwd":".","timeout_seconds":60},"baseline_exit_code":1,"baseline_output_contains":["FAIL","TestBug"]}}`)
	c, err := DecodeChangeContract(raw)
	if err != nil {
		t.Fatal(err)
	}
	if c.Kind != ChangeKindDefect || c.Regression.Mode != RegressionModeRedGreen {
		t.Fatalf("contract=%+v", c)
	}
}

func TestDecodeChangeContractNonDefect(t *testing.T) {
	raw := []byte(`{"schema_version":1,"kind":"feature","behavior":{"argv":["go","test","./..."],"cwd":".","timeout_seconds":60},"affected":{"argv":["go","test","./..."],"cwd":".","timeout_seconds":60},"regression":{"mode":"not_applicable","reason_code":"not-a-defect"}}`)
	if _, err := DecodeChangeContract(raw); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeChangeContractRejectsAmbiguity(t *testing.T) {
	cases := [][]byte{
		[]byte(`{"schema_version":1,"kind":"defect","behavior":{"argv":["go","test"],"cwd":".","timeout_seconds":60},"affected":{"argv":["go","test"],"cwd":".","timeout_seconds":60},"regression":{"mode":"not_applicable","reason_code":"not-a-defect"}}`),
		[]byte(`{"schema_version":1,"kind":"feature","behavior":{"argv":["go","test"],"cwd":".","timeout_seconds":60},"affected":{"argv":["go","test"],"cwd":".","timeout_seconds":60},"regression":{"mode":"red_green","command":{"argv":["go","test"],"cwd":".","timeout_seconds":60},"baseline_exit_code":1,"baseline_output_contains":["FAIL"]}}`),
		[]byte(`{"schema_version":1,"kind":"defect","behavior":{"argv":["go","test"],"cwd":".","timeout_seconds":60},"affected":{"argv":["go","test"],"cwd":".","timeout_seconds":60},"regression":{"mode":"red_green","command":{"argv":["go","test"],"cwd":".","timeout_seconds":60},"baseline_exit_code":0,"baseline_output_contains":["FAIL"]}}`),
		[]byte(`{"schema_version":1,"kind":"defect","behavior":{"argv":["go","test"],"cwd":".","timeout_seconds":60},"affected":{"argv":["go","test"],"cwd":".","timeout_seconds":60},"regression":{"mode":"red_green","command":{"argv":["go","test"],"cwd":".","timeout_seconds":60},"baseline_exit_code":1,"baseline_output_contains":[]}}`),
		[]byte(`{"schema_version":1,"kind":"feature","behavior":{"argv":["go","test"],"cwd":".","timeout_seconds":60},"affected":{"argv":["go","test"],"cwd":".","timeout_seconds":60},"regression":{"mode":"not_applicable","reason_code":"custom"}}`),
		[]byte(`{"schema_version":1,"kind":"feature","behavior":{"argv":["go","test"],"cwd":".","timeout_seconds":60},"affected":{"argv":["go","test"],"cwd":".","timeout_seconds":60},"regression":{"mode":"not_applicable","reason_code":"not-a-defect"},"extra":true}`),
	}
	for i, raw := range cases {
		if _, err := DecodeChangeContract(raw); err == nil {
			t.Fatalf("case %d expected error", i)
		}
	}
}

func TestChangeContractValidationAdditionalFailures(t *testing.T) {
	cmd := validCommand()
	exit := 1
	validDefect := ChangeContract{SchemaVersion: 1, Kind: ChangeKindDefect, Behavior: cmd, Affected: cmd, Regression: RegressionContract{Mode: RegressionModeRedGreen, Command: &cmd, BaselineExitCode: &exit, BaselineOutputContains: []string{"BUG"}}}
	cases := []ChangeContract{
		func() ChangeContract { c := validDefect; c.SchemaVersion = 2; return c }(),
		func() ChangeContract { c := validDefect; c.Kind = "unknown"; return c }(),
		func() ChangeContract { c := validDefect; c.Behavior = CommandSpec{}; return c }(),
		func() ChangeContract { c := validDefect; c.Affected = CommandSpec{}; return c }(),
		func() ChangeContract { c := validDefect; c.Regression.Command = nil; return c }(),
		func() ChangeContract { c := validDefect; z := 256; c.Regression.BaselineExitCode = &z; return c }(),
		func() ChangeContract {
			c := validDefect
			c.Regression.BaselineOutputContains = []string{"   "}
			return c
		}(),
		func() ChangeContract {
			c := validDefect
			c.Regression.BaselineOutputContains = []string{"BUG", "BUG"}
			return c
		}(),
	}
	for i, c := range cases {
		if err := c.Validate(); err == nil {
			t.Fatalf("case %d expected error", i)
		}
	}
	feature := ChangeContract{SchemaVersion: 1, Kind: ChangeKindFeature, Behavior: cmd, Affected: cmd, Regression: RegressionContract{Mode: RegressionModeNotApplicable, ReasonCode: RegressionReasonNotDefect}}
	badFeature := feature
	badFeature.Regression.Command = &cmd
	if err := badFeature.Validate(); err == nil {
		t.Fatal("expected illegal non-defect command")
	}
}

func TestDecodeChangeContractRejectsTrailingJSON(t *testing.T) {
	raw := []byte(`{"schema_version":1,"kind":"feature","behavior":{"argv":["go","test"],"cwd":".","timeout_seconds":60},"affected":{"argv":["go","test"],"cwd":".","timeout_seconds":60},"regression":{"mode":"not_applicable","reason_code":"not-a-defect"}} {}`)
	if _, err := DecodeChangeContract(raw); err == nil {
		t.Fatal("expected trailing JSON error")
	}
}

func FuzzDecodeChangeContract(f *testing.F) {
	f.Add([]byte(`{"schema_version":2}`))
	f.Add([]byte(`not-json`))
	f.Fuzz(func(t *testing.T, raw []byte) { _, _ = DecodeChangeContract(raw) })
}
