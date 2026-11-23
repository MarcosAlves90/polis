package spec

import (
	"sort"
	"strings"
)

// Stable concurrency reason codes shared by plans, runtime reports and CLI
// output. They are POLIS-owned categories: project command output and
// gate-authored reasons are never copied into these values.
const (
	ConcurrencyDeclaredParallelSafe = "declared_parallel_safe"
	ConcurrencyMissingOptIn         = "parallel_safe_not_declared"
	ConcurrencyCoverageExclusive    = "coverage_report_exclusive"
	ConcurrencyNotApplicable        = "gate_not_applicable"
	ConcurrencyDeferred             = "gate_deferred_to_consumer"
	ConcurrencyPrerequisite         = "mandatory_prerequisite"
	ConcurrencyResourceConflict     = "declared_resource_conflict"
	ConcurrencyWorkerCap            = "worker_cap"
	ConcurrencyJobsOne              = "jobs_equals_one"
	ConcurrencySelection            = "selection_serializes_eligible_gates"
	ConcurrencyNoEligible           = "no_parallel_safe_gates"
	ConcurrencyTooFewEligible       = "fewer_than_two_eligible_gates"
	ConcurrencyNoOverlapGroup       = "no_overlapping_eligible_gates"
	ConcurrencyAllReused            = "eligible_gates_reused"
	ConcurrencyInvalidJobs          = "invalid_jobs"
)

// GateConcurrency records the effective overlap decision for one plan gate.
type GateConcurrency struct {
	Gate      string   `json:"gate"`
	Eligible  bool     `json:"eligible"`
	Reason    string   `json:"reason"`
	DependsOn []string `json:"depends_on,omitempty"`
}

// OverlapGroup is a deterministic set of eligible gates with no dependency
// relationship between any pair. Every gate in a group may run at the same
// time once prerequisites outside the group have passed.
type OverlapGroup struct {
	Gates  []string `json:"gates"`
	Reason string   `json:"reason"`
}

// ConcurrencyFinding explains why a gate or group cannot overlap with others.
type ConcurrencyFinding struct {
	Reason string   `json:"reason"`
	Gates  []string `json:"gates,omitempty"`
	Detail string   `json:"detail,omitempty"`
}

// ConcurrencySummary is the declared, jobs-independent view of a compiled plan.
type ConcurrencySummary struct {
	EligibleGates        []GateConcurrency    `json:"eligible_gates"`
	ExclusiveGates       []GateConcurrency    `json:"exclusive_gates"`
	OverlapGroups        []OverlapGroup       `json:"overlap_groups"`
	SerializationReasons []ConcurrencyFinding `json:"serialization_reasons,omitempty"`
}

// ConcurrencyDiagnosis is the effective, jobs-aware overlap explanation.
type ConcurrencyDiagnosis struct {
	Jobs          int                  `json:"jobs"`
	CanOverlap    bool                 `json:"can_overlap"`
	EligibleGates []string             `json:"eligible_gates"`
	Blockers      []ConcurrencyFinding `json:"blockers,omitempty"`
}

// ConcurrencyObservation records what the bounded scheduler actually observed.
// It is never a substitute for gate evidence and never changes gate outcomes.
type ConcurrencyObservation struct {
	OverlapObserved bool     `json:"overlap_observed"`
	MaxConcurrent   int      `json:"max_concurrent"`
	LaunchedGates   []string `json:"launched_gates,omitempty"`
}

// ConcurrencyReport joins the declared diagnosis with the observed outcome.
// A true overlap_observed field requires measured concurrent execution; a
// --jobs value greater than one is not itself evidence of overlap.
type ConcurrencyReport struct {
	Jobs        int                    `json:"jobs"`
	Observation ConcurrencyObservation `json:"observation"`
	Diagnosis   ConcurrencyDiagnosis   `json:"diagnosis"`
}

// AddReuseBlocker records why an executed plan with no observed overlap stayed
// serial even though its declarations would otherwise permit overlap.
func (d *ConcurrencyDiagnosis) AddReuseBlocker(gates []string) {
	if len(gates) == 0 {
		return
	}
	d.Blockers = append(d.Blockers, ConcurrencyFinding{Reason: ConcurrencyAllReused, Gates: append([]string(nil), gates...),
		Detail: "eligible gates were reused from matching evidence and did not launch"})
	d.Blockers = NormalizeConcurrencyFindings(d.Blockers)
}

// NormalizeConcurrencyFindings sorts and deduplicates findings so text and
// JSON reports are deterministic regardless of scheduler completion timing.
func NormalizeConcurrencyFindings(findings []ConcurrencyFinding) []ConcurrencyFinding {
	seen := map[string]struct{}{}
	normalized := make([]ConcurrencyFinding, 0, len(findings))
	for _, finding := range findings {
		gates := append([]string(nil), finding.Gates...)
		sort.Strings(gates)
		key := finding.Reason + "\x00" + finding.Detail + "\x00" + strings.Join(gates, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, ConcurrencyFinding{Reason: finding.Reason, Gates: gates, Detail: finding.Detail})
	}
	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].Reason != normalized[j].Reason {
			return normalized[i].Reason < normalized[j].Reason
		}
		if normalized[i].Detail != normalized[j].Detail {
			return normalized[i].Detail < normalized[j].Detail
		}
		return strings.Join(normalized[i].Gates, ",") < strings.Join(normalized[j].Gates, ",")
	})
	return normalized
}
