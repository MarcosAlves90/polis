package changeexec

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MarcosAlves90/polis/v6/internal/diagnostic"
)

func TestReviewTimeoutHelper(t *testing.T) {
	if os.Getenv("POLIS_REVIEW_TIMEOUT_HELPER") == "1" {
		time.Sleep(5 * time.Second)
	}
}

func TestReviewBaselineNotRun(t *testing.T) {
	contract := strictBehaviorPreservingContract(strictCmd(os.Args[0], "-test.run=^TestChangeExecPrerequisiteHelper$", "--", "assertion"))
	err := ExecuteBaseline(contract, t.TempDir(), &bytes.Buffer{})
	failure, ok := diagnostic.As(err)
	if !ok {
		t.Fatalf("REVIEW-REGRESSION: missing baseline diagnostic: %v", err)
	}
	want := []string{"target regression", "behavior", "affected", "project gate validation"}
	raw, _ := json.Marshal(failure.Report)
	if !reflect.DeepEqual(failure.Report.NotRun, want) || !strings.Contains(string(raw), `"not_run":["target regression","behavior","affected","project gate validation"]`) || !strings.Contains(failure.Report.FormatText(), "Not run: target regression, behavior, affected, project gate validation") {
		t.Fatalf("REVIEW-REGRESSION: baseline skipped stages missing: %s", raw)
	}
}

func TestReviewChangeTimeout(t *testing.T) {
	t.Setenv("POLIS_REVIEW_TIMEOUT_HELPER", "1")
	command := strictCmd(os.Args[0], "-test.run=^TestReviewTimeoutHelper$")
	command.TimeoutSeconds = 1
	contract := strictBehaviorPreservingContract(command)
	err := ExecuteTarget(contract, t.TempDir(), &bytes.Buffer{})
	failure, ok := diagnostic.As(err)
	if !ok {
		t.Fatalf("REVIEW-REGRESSION: missing timeout diagnostic: %v", err)
	}
	raw, _ := json.Marshal(failure.Report)
	if !strings.Contains(string(raw), `"timed_out":true`) || !strings.Contains(failure.Report.FormatText(), "Timed out: true") {
		t.Fatalf("REVIEW-REGRESSION: timeout not identified: %s", raw)
	}
}
