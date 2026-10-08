package policyexec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/MarcosAlves90/polis/v6/internal/commandexec"
	"github.com/MarcosAlves90/polis/v6/internal/policyplan"
	"github.com/MarcosAlves90/polis/v6/spec"
)

const MaxJobs = 16

type Outcome struct {
	Status  spec.Status
	Action  string
	Reason  string
	Command *CommandExecution
}

type Options struct {
	Jobs int
	// Nil selects the full plan. Callers must include prerequisite closure.
	Selected map[string]bool
	// Reuse is called serially, only after all prerequisites pass. Delivery
	// callers leave it nil: incremental evidence is never artifact proof.
	Reuse func(spec.GatePolicy, Result) (Outcome, bool)
	// OnGateStart is called serially immediately before an executable gate is
	// launched. It is presentation-only and must not affect gate semantics.
	OnGateStart func(spec.GatePolicy)
	// OnGateComplete is invoked serially after a launched gate finishes. It is
	// presentation-only and reports observed monotonic elapsed time in milliseconds.
	OnGateComplete func(spec.GatePolicy, Outcome, int64)
}

type completion struct {
	id         string
	outcome    Outcome
	evidence   []byte
	finishedAt time.Time
}

func ValidateJobs(jobs int) error {
	if jobs < 1 || jobs > MaxJobs {
		return fmt.Errorf("jobs must be between 1 and %d", MaxJobs)
	}
	return nil
}

func ExecutePlanWithOptions(plan policyplan.Plan, repo string, evidence io.Writer, opts Options) Result {
	result := Result{Overall: spec.StatusPass, Gates: map[string]spec.Status{}, Outcomes: map[string]Outcome{}}
	if ValidateJobs(opts.Jobs) != nil {
		result.Overall = spec.StatusBlocked
		return result
	}
	gates := plan.GatePolicies()
	pending, running := map[string]bool{}, map[string]bool{}
	startedAt := map[string]time.Time{}
	records := map[string][]byte{}
	finished := make(chan completion, len(gates))
	store := func(c completion) {
		result.Gates[c.id] = c.outcome.Status
		result.Outcomes[c.id] = c.outcome
		records[c.id] = c.evidence
		if c.outcome.Action != "omitted" {
			result.Overall = combine(result.Overall, c.outcome.Status)
		}
		if c.outcome.Status != spec.StatusPass && c.outcome.Command != nil {
			if result.CommandFailures == nil {
				result.CommandFailures = map[string]CommandExecution{}
			}
			result.CommandFailures[c.id] = *c.outcome.Command
		}
	}
	for _, gate := range gates {
		pending[gate.ID] = true
	}
	for len(pending) > 0 || len(running) > 0 {
		progress := false
		for _, gate := range gates {
			if !pending[gate.ID] {
				continue
			}
			var immediate *Outcome
			switch {
			case gate.Mode == spec.GateModeNotApplicable:
				immediate = &Outcome{Status: spec.StatusNotApplicable, Action: "omitted", Reason: *gate.Reason}
			case planGateDeferred(plan, gate.ID):
				immediate = &Outcome{Status: spec.StatusDeferred, Action: "omitted", Reason: spec.DeferredReasonToConsumer}
			case opts.Selected != nil && !opts.Selected[gate.ID]:
				immediate = &Outcome{Status: spec.StatusBlocked, Action: "omitted", Reason: "not selected; no current guarantee provided"}
			}
			if immediate == nil {
				ready, failed := prerequisites(gate, result.Gates)
				if !ready {
					continue
				}
				if failed != "" {
					immediate = &Outcome{Status: spec.StatusBlocked, Action: "blocked", Reason: "prerequisite " + failed + " did not pass"}
				}
			}
			if immediate == nil && (len(running) >= opts.Jobs || !canOverlap(gate, gates, running)) {
				continue
			}
			if immediate == nil && opts.Reuse != nil {
				if cached, ok := opts.Reuse(gate, result); ok {
					immediate = &cached
				}
			}
			if immediate != nil {
				delete(pending, gate.ID)
				store(completion{id: gate.ID, outcome: *immediate, evidence: outcomeEvidence(gate.ID, *immediate)})
				progress = true
				continue
			}
			delete(pending, gate.ID)
			running[gate.ID] = true
			startedAt[gate.ID] = time.Now()
			progress = true
			if opts.OnGateStart != nil {
				opts.OnGateStart(gate)
			}
			go func(gate spec.GatePolicy) {
				var buf bytes.Buffer
				outcome := executeGate(gate, repo, &buf)
				finished <- completion{id: gate.ID, outcome: outcome, evidence: buf.Bytes(), finishedAt: time.Now()}
			}(gate)
		}
		if len(running) > 0 {
			c := <-finished
			delete(running, c.id)
			store(c)
			if opts.OnGateComplete != nil {
				for _, gate := range gates {
					if gate.ID == c.id {
						opts.OnGateComplete(gate, c.outcome, c.finishedAt.Sub(startedAt[c.id]).Milliseconds())
						break
					}
				}
			}
		} else if !progress {
			// Defensive fail-closed handling for an incomplete/corrupt plan.
			for _, gate := range gates {
				if pending[gate.ID] {
					o := Outcome{Status: spec.StatusBlocked, Action: "blocked", Reason: "unresolved prerequisite"}
					store(completion{id: gate.ID, outcome: o, evidence: outcomeEvidence(gate.ID, o)})
					delete(pending, gate.ID)
				}
			}
		}
	}
	enc := json.NewEncoder(evidence)
	_ = enc.Encode(spec.EvidenceEvent{Event: "validation_configured", Gate: "policy", ValidationLevel: plan.ValidationLevel,
		EnabledGates: append([]string{}, plan.EnabledGates...), DisabledGates: append([]string{}, plan.DisabledGates...), DeferredGates: append([]string{}, plan.DeferredGates...)})
	for _, gate := range gates {
		_, _ = evidence.Write(records[gate.ID])
	}
	return result
}

func prerequisites(gate spec.GatePolicy, statuses map[string]spec.Status) (bool, string) {
	for _, id := range gate.DependsOn {
		status, done := statuses[id]
		if !done {
			return false, ""
		}
		if status != spec.StatusPass {
			return true, id
		}
	}
	return true, ""
}

func canOverlap(gate spec.GatePolicy, gates []spec.GatePolicy, running map[string]bool) bool {
	if len(running) == 0 {
		return true
	}
	// Coverage owns its report lifecycle and is always exclusive.
	if !gate.ParallelSafe || gate.Mode == spec.GateModeCoverage {
		return false
	}
	for _, other := range gates {
		if running[other.ID] && (!other.ParallelSafe || other.Mode == spec.GateModeCoverage) {
			return false
		}
	}
	return true
}

func outcomeEvidence(id string, o Outcome) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	_ = enc.Encode(spec.EvidenceEvent{Event: "gate_started", Gate: id})
	var reason *string
	if o.Reason != "" {
		reason = &o.Reason
	}
	_ = enc.Encode(spec.EvidenceEvent{Event: "gate_finished", Gate: id, Status: o.Status, Reason: reason})
	return b.Bytes()
}

func executeGate(gate spec.GatePolicy, repo string, evidence io.Writer) Outcome {
	enc := json.NewEncoder(evidence)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(spec.EvidenceEvent{Event: "gate_started", Gate: gate.ID})
	o := Outcome{Status: spec.StatusPass, Action: "executed"}
	if gate.Mode == spec.GateModeCoverage {
		o.Status, o.Command = executeCoverage(enc, gate, repo)
	} else {
		obs := executeCommand(enc, gate.ID, *gate.Command, repo)
		o.Status = obs.Status
		o.Command = &CommandExecution{Argv: append([]string{}, gate.Command.Argv...), Cwd: gate.Command.Cwd, Observation: obs}
	}
	if o.Command != nil {
		o.Reason = safeBlockedReason(o.Command.Observation)
	}
	var reason *string
	if o.Reason != "" {
		reason = &o.Reason
	}
	_ = enc.Encode(spec.EvidenceEvent{Event: "gate_finished", Gate: gate.ID, Status: o.Status, Reason: reason})
	return o
}

// Startup diagnostics may contain runtime environment values expanded into
// executable/module names. Persist categories, never output-derived tokens.
func safeBlockedReason(obs commandexec.Observation) string {
	if obs.Status != spec.StatusBlocked {
		return ""
	}
	category := "command prerequisite unavailable"
	for _, prefix := range []string{"missing executable", "missing dependency", "missing environment condition"} {
		if strings.HasPrefix(obs.Prerequisite, prefix+" ") {
			category = prefix
			break
		}
	}
	return category + "; intended checks did not run"
}
