package policyplan

import (
	"sort"
	"strings"

	"github.com/MarcosAlves90/polis/v6/spec"
)

// Concurrency types and reason codes are owned by spec so plans, gate runs,
// build reports and CLI output share one stable vocabulary.
type (
	GateConcurrency      = spec.GateConcurrency
	OverlapGroup         = spec.OverlapGroup
	ConcurrencyFinding   = spec.ConcurrencyFinding
	ConcurrencySummary   = spec.ConcurrencySummary
	ConcurrencyDiagnosis = spec.ConcurrencyDiagnosis
)

const (
	ConcurrencyDeclaredParallelSafe = spec.ConcurrencyDeclaredParallelSafe
	ConcurrencyMissingOptIn         = spec.ConcurrencyMissingOptIn
	ConcurrencyCoverageExclusive    = spec.ConcurrencyCoverageExclusive
	ConcurrencyNotApplicable        = spec.ConcurrencyNotApplicable
	ConcurrencyDeferred             = spec.ConcurrencyDeferred
	ConcurrencyPrerequisite         = spec.ConcurrencyPrerequisite
	ConcurrencyResourceConflict     = spec.ConcurrencyResourceConflict
	ConcurrencyWorkerCap            = spec.ConcurrencyWorkerCap
	ConcurrencyJobsOne              = spec.ConcurrencyJobsOne
	ConcurrencySelection            = spec.ConcurrencySelection
	ConcurrencyNoEligible           = spec.ConcurrencyNoEligible
	ConcurrencyTooFewEligible       = spec.ConcurrencyTooFewEligible
	ConcurrencyNoOverlapGroup       = spec.ConcurrencyNoOverlapGroup
	ConcurrencyAllReused            = spec.ConcurrencyAllReused
	ConcurrencyInvalidJobs          = spec.ConcurrencyInvalidJobs
)

// ConcurrencyOptions binds the declared plan to a concrete --jobs bound and
// optional incremental selection.
type ConcurrencyOptions struct {
	Jobs     int
	Selected map[string]bool
}

// ConcurrencySummary derives overlap eligibility solely from validated plan
// declarations. It never infers safety from a gate's name, command, or cwd.
func (p Plan) ConcurrencySummary() ConcurrencySummary {
	order := p.orderedGateIDs()
	deferred := map[string]bool{}
	for _, id := range p.DeferredGates {
		deferred[id] = true
	}
	byID := make(map[string]Gate, len(p.Gates))
	for _, gate := range p.Gates {
		byID[gate.ID] = gate
	}
	summary := ConcurrencySummary{EligibleGates: []GateConcurrency{}, ExclusiveGates: []GateConcurrency{}, OverlapGroups: []OverlapGroup{}}
	eligible := make([]string, 0, len(order))
	missingOptIn := []string{}
	coverage := []string{}
	prerequisites := []string{}
	for _, id := range order {
		gate, ok := byID[id]
		if !ok {
			continue
		}
		entry := GateConcurrency{Gate: id, DependsOn: append([]string(nil), gate.DependsOn...)}
		switch {
		case gate.State != GateStateEnabled || gate.Mode == spec.GateModeNotApplicable:
			entry.Reason = ConcurrencyNotApplicable
		case deferred[id]:
			entry.Reason = ConcurrencyDeferred
		case gate.Mode == spec.GateModeCoverage:
			entry.Reason = ConcurrencyCoverageExclusive
			coverage = append(coverage, id)
		case gate.Mode != spec.GateModeCommand:
			entry.Reason = ConcurrencyNotApplicable
		case !gate.ParallelSafe:
			entry.Reason = ConcurrencyMissingOptIn
			missingOptIn = append(missingOptIn, id)
		default:
			entry.Eligible = true
			entry.Reason = ConcurrencyDeclaredParallelSafe
			eligible = append(eligible, id)
			if len(gate.DependsOn) != 0 {
				prerequisites = append(prerequisites, id)
			}
		}
		if entry.Eligible {
			summary.EligibleGates = append(summary.EligibleGates, entry)
		} else {
			summary.ExclusiveGates = append(summary.ExclusiveGates, entry)
		}
	}
	dependencies := make(map[string][]string, len(byID))
	for id, gate := range byID {
		dependencies[id] = append([]string(nil), gate.DependsOn...)
	}
	summary.OverlapGroups = overlapGroups(eligible, dependencies)
	if len(eligible) == 0 {
		summary.SerializationReasons = append(summary.SerializationReasons, ConcurrencyFinding{Reason: ConcurrencyNoEligible,
			Detail: "no enabled command gate declares parallel_safe"})
	} else if len(eligible) < 2 {
		summary.SerializationReasons = append(summary.SerializationReasons, ConcurrencyFinding{Reason: ConcurrencyTooFewEligible,
			Gates: append([]string(nil), eligible...)})
	} else if !hasOverlapGroup(summary.OverlapGroups, eligible...) {
		summary.SerializationReasons = append(summary.SerializationReasons, ConcurrencyFinding{Reason: ConcurrencyNoOverlapGroup,
			Gates: append([]string(nil), eligible...)})
	}
	if len(missingOptIn) != 0 {
		summary.SerializationReasons = append(summary.SerializationReasons, ConcurrencyFinding{Reason: ConcurrencyMissingOptIn,
			Gates: missingOptIn, Detail: "review the real commands and outputs before declaring parallel_safe"})
	}
	if len(coverage) != 0 {
		summary.SerializationReasons = append(summary.SerializationReasons, ConcurrencyFinding{Reason: ConcurrencyCoverageExclusive,
			Gates: coverage, Detail: "coverage owns its report lifecycle and cannot overlap another gate"})
	}
	if len(prerequisites) != 0 {
		summary.SerializationReasons = append(summary.SerializationReasons, ConcurrencyFinding{Reason: ConcurrencyPrerequisite,
			Gates: prerequisites, Detail: "eligible gates wait for every declared prerequisite to pass"})
	}
	summary.SerializationReasons = spec.NormalizeConcurrencyFindings(summary.SerializationReasons)
	return summary
}

// ConcurrencyDiagnosis applies the runtime --jobs bound and incremental
// selection without changing the declared eligibility rules.
func (p Plan) ConcurrencyDiagnosis(opts ConcurrencyOptions) ConcurrencyDiagnosis {
	jobs := opts.Jobs
	if jobs < 1 {
		jobs = 1
	}
	summary := p.ConcurrencySummary()
	eligible := make([]string, 0, len(summary.EligibleGates))
	for _, gate := range summary.EligibleGates {
		eligible = append(eligible, gate.Gate)
	}
	selected := make([]string, 0, len(eligible))
	if opts.Selected == nil {
		selected = append(selected, eligible...)
	} else {
		for _, id := range eligible {
			if opts.Selected[id] {
				selected = append(selected, id)
			}
		}
	}
	diagnosis := ConcurrencyDiagnosis{Jobs: jobs, EligibleGates: selected}
	groups := make([]OverlapGroup, 0, len(summary.OverlapGroups))
	for _, group := range summary.OverlapGroups {
		filtered := make([]string, 0, len(group.Gates))
		for _, id := range group.Gates {
			if opts.Selected == nil || opts.Selected[id] {
				filtered = append(filtered, id)
			}
		}
		if len(filtered) > 1 {
			groups = append(groups, OverlapGroup{Gates: filtered, Reason: ConcurrencyDeclaredParallelSafe})
		}
	}
	diagnosis.CanOverlap = jobs > 1 && len(groups) != 0
	if jobs <= 1 {
		diagnosis.Blockers = append(diagnosis.Blockers,
			ConcurrencyFinding{Reason: ConcurrencyJobsOne, Detail: "jobs=1 keeps every run serial"},
			ConcurrencyFinding{Reason: ConcurrencyWorkerCap, Gates: append([]string(nil), selected...), Detail: "jobs=1"})
	}
	if opts.Selected != nil && !diagnosis.CanOverlap && len(summary.EligibleGates) >= 2 && len(selected) < 2 {
		diagnosis.Blockers = append(diagnosis.Blockers, ConcurrencyFinding{Reason: ConcurrencySelection,
			Gates: append([]string(nil), selected...), Detail: "fewer than two eligible gates are selected"})
	}
	if !diagnosis.CanOverlap && len(selected) >= 2 && len(groups) == 0 {
		diagnosis.Blockers = append(diagnosis.Blockers, ConcurrencyFinding{Reason: ConcurrencyNoOverlapGroup,
			Gates: append([]string(nil), selected...), Detail: "eligible gates are ordered by prerequisites"})
	}
	if !diagnosis.CanOverlap {
		diagnosis.Blockers = append(diagnosis.Blockers, summary.SerializationReasons...)
	}
	diagnosis.Blockers = spec.NormalizeConcurrencyFindings(diagnosis.Blockers)
	return diagnosis
}

func (p Plan) orderedGateIDs() []string {
	ids := make([]string, 0, len(p.ExecutionOrder))
	seen := map[string]struct{}{}
	for _, id := range p.ExecutionOrder {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	for _, gate := range p.Gates {
		if _, ok := seen[gate.ID]; ok {
			continue
		}
		seen[gate.ID] = struct{}{}
		ids = append(ids, gate.ID)
	}
	return ids
}

func overlapGroups(eligible []string, dependencies map[string][]string) []OverlapGroup {
	groups := make([]OverlapGroup, 0, len(eligible))
	for _, id := range eligible {
		placed := false
		for i := range groups {
			if groupAccepts(groups[i].Gates, id, dependencies) {
				groups[i].Gates = append(groups[i].Gates, id)
				placed = true
				break
			}
		}
		if !placed {
			groups = append(groups, OverlapGroup{Gates: []string{id}, Reason: ConcurrencyDeclaredParallelSafe})
		}
	}
	sort.Slice(groups, func(i, j int) bool {
		if len(groups[i].Gates) == len(groups[j].Gates) {
			return strings.Join(groups[i].Gates, ",") < strings.Join(groups[j].Gates, ",")
		}
		return len(groups[i].Gates) > len(groups[j].Gates)
	})
	for i := range groups {
		sort.Strings(groups[i].Gates)
	}
	return groups
}

func groupAccepts(group []string, candidate string, dependencies map[string][]string) bool {
	for _, id := range group {
		if dependencyReaches(dependencies, candidate, id) || dependencyReaches(dependencies, id, candidate) {
			return false
		}
	}
	return true
}

func dependencyReaches(dependencies map[string][]string, from, target string) bool {
	stack := append([]string(nil), dependencies[from]...)
	seen := map[string]struct{}{}
	for len(stack) > 0 {
		last := len(stack) - 1
		id := stack[last]
		stack = stack[:last]
		if id == target {
			return true
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		stack = append(stack, dependencies[id]...)
	}
	return false
}

func hasOverlapGroup(groups []OverlapGroup, eligible ...string) bool {
	set := map[string]struct{}{}
	for _, id := range eligible {
		set[id] = struct{}{}
	}
	for _, group := range groups {
		if len(group.Gates) < 2 {
			continue
		}
		matched := 0
		for _, id := range group.Gates {
			if _, ok := set[id]; ok {
				matched++
			}
		}
		if matched >= 2 {
			return true
		}
	}
	return false
}
