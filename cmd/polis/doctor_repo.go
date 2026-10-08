package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/MarcosAlves90/polis/v6/internal/gitutil"
	"github.com/MarcosAlves90/polis/v6/internal/pathguard"
	"github.com/MarcosAlves90/polis/v6/internal/policyplan"
)

// Doctor checks describe static observations only. Even a PASS is not gate proof.
type doctorCheck struct {
	Code   string `json:"code"`
	Status string `json:"status"`
	Gate   string `json:"gate,omitempty"`
	Detail string `json:"detail"`
}

type doctorRepositoryReport struct {
	Status          string        `json:"status"`
	Root            string        `json:"root,omitempty"`
	PolicySource    string        `json:"policy_source,omitempty"`
	PolicySHA256    string        `json:"policy_sha256,omitempty"`
	ValidationLevel string        `json:"validation_level,omitempty"`
	ExecutionOrder  []string      `json:"execution_order,omitempty"`
	Checks          []doctorCheck `json:"checks"`
	GatesExecuted   bool          `json:"gates_executed"`
}

func inspectDoctorRepository(repo, policy string) (doctorRepositoryReport, int) {
	report := doctorRepositoryReport{Status: "PASS", Checks: []doctorCheck{}}
	block := func(code, gate, detail string) {
		report.Status = "BLOCKED"
		report.Checks = append(report.Checks, doctorCheck{Code: code, Status: "BLOCKED", Gate: gate, Detail: detail})
	}
	ctx := context.Background()
	root, err := gitutil.ResolveRoot(ctx, repo, gitutil.ResolveRootOptions{GitError: "not a Git worktree"})
	if err != nil {
		block("repository", "", err.Error())
		return report, exitBlocked
	}
	report.Root = root
	report.Checks = append(report.Checks, doctorCheck{Code: "repository", Status: "PASS", Detail: "Git worktree resolved"})
	plan, err := policyplan.Load(ctx, policyplan.Options{Repo: root, Policy: policy})
	if err != nil {
		report.Status = "FAIL"
		report.Checks = append(report.Checks, doctorCheck{Code: "policy_plan", Status: "FAIL", Detail: err.Error()})
		return report, exitValidationFailed
	}
	report.PolicySource = plan.PolicySource
	report.PolicySHA256 = plan.PolicySHA256
	report.ValidationLevel = plan.ValidationLevel
	report.ExecutionOrder = plan.ExecutionOrder
	report.Checks = append(report.Checks, doctorCheck{Code: "policy_plan", Status: "PASS", Detail: "policy schema, platform portability and dependency plan validated"})

	for _, gate := range plan.GatePolicies() {
		if gate.Command == nil {
			continue
		}
		// Coverage reports may be produced by the gate, so only check their
		// path containment here. Requiring the report to exist would reject
		// otherwise valid first runs.
		if gate.Report != "" {
			reportPath := filepath.Join(root, filepath.FromSlash(gate.Report))
			inside, err := pathguard.Contains(root, reportPath)
			switch {
			case err != nil:
				block("report_path", gate.ID, "cannot inspect report path: "+err.Error())
			case !inside:
				block("report_path", gate.ID, "report path resolves outside the repository: "+gate.Report)
			default:
				report.Checks = append(report.Checks, doctorCheck{Code: "report_path", Status: "PASS", Gate: gate.ID, Detail: "report path stays within the repository (report existence not checked): " + gate.Report})
			}
		}
		cwd := filepath.Join(root, filepath.FromSlash(gate.Command.Cwd))
		inside, err := pathguard.Contains(root, cwd)
		switch {
		case err != nil:
			block("working_directory", gate.ID, "cannot inspect working directory: "+err.Error())
			continue
		case !inside:
			block("working_directory", gate.ID, "working directory resolves outside the repository: "+gate.Command.Cwd)
			continue
		}
		info, err := os.Stat(cwd)
		switch {
		case err != nil:
			block("working_directory", gate.ID, fmt.Sprintf("working directory %q: %v", gate.Command.Cwd, err))
			continue
		case !info.IsDir():
			block("working_directory", gate.ID, fmt.Sprintf("working directory %q is not a directory", gate.Command.Cwd))
			continue
		}
		report.Checks = append(report.Checks, doctorCheck{Code: "working_directory", Status: "PASS", Gate: gate.ID, Detail: gate.Command.Cwd})

		program := gate.Command.Argv[0]
		lookup := program
		if !filepath.IsAbs(program) && strings.ContainsAny(program, `/\`) {
			lookup = filepath.Join(cwd, filepath.FromSlash(program))
		}
		path, err := exec.LookPath(lookup)
		if err != nil {
			block("executable", gate.ID, fmt.Sprintf("executable %q is unavailable: %v", program, err))
			continue
		}
		report.Checks = append(report.Checks, doctorCheck{Code: "executable", Status: "PASS", Gate: gate.ID, Detail: fmt.Sprintf("%q resolved to %q using the doctor's environment", program, path)})
	}
	if report.Status == "BLOCKED" {
		return report, exitBlocked
	}
	return report, exitPass
}

func writeDoctorRepositoryText(out io.Writer, report doctorRepositoryReport) {
	fmt.Fprintf(out, "Repository diagnostics: %s\n", report.Status)
	if report.Root != "" {
		fmt.Fprintf(out, "Repository: %s\n", report.Root)
	}
	for _, check := range report.Checks {
		label := check.Code
		if check.Gate != "" {
			label += " [" + check.Gate + "]"
		}
		fmt.Fprintf(out, "  %s: %s — %s\n", label, check.Status, check.Detail)
	}
	fmt.Fprintln(out, "Project gates executed: no (static prerequisites only; run gates/workspace validation for actual proof)")
}
