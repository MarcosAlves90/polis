package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	"github.com/MarcosAlves90/polis/v6/internal/packageapply"
	"github.com/MarcosAlves90/polis/v6/internal/packageverify"
	"golang.org/x/term"
)

type applyCLIOptions struct {
	repo                      string
	baselineMode              packageapply.BaselineMode
	allowMissingBaselineProof bool
	commitMode                packageapply.CommitMode
	format                    string
	signaturePath             string
	trustedKey                string
	artifact                  string
}

func runApply(args []string, out, errOut io.Writer) int {
	opts, ok := parseApplyCLIOptions(args, errOut)
	if !ok {
		return exitUsage
	}
	writeProgress(errOut, opts.format, "verify the detached artifact signature when configured", "apply must authenticate the exact artifact bytes before any consumer validation or repository mutation")
	if err := verifyDetached(opts.artifact, opts.signaturePath, opts.trustedKey); err != nil {
		return writeFailure(errOut, opts.format, applyLabel, exitInvalidArtifact, err)
	}
	writeProgress(errOut, opts.format, "verify the delivery artifact", "apply must reject an invalid package before checking or changing the target repository")
	if _, err := packageverify.Verify(opts.artifact); err != nil {
		return writeFailure(errOut, opts.format, applyLabel, exitInvalidArtifact, err)
	}
	writeProgress(errOut, opts.format, "validate and apply the artifact transactionally", "the target tree may be changed only after baseline, scope, and consumer gate checks succeed")
	result, err := packageapply.ApplyWithOptions(context.Background(), opts.artifact, opts.repo, packageapply.Options{
		BaselineMode:              opts.baselineMode,
		AllowMissingBaselineProof: opts.allowMissingBaselineProof,
		CommitMode:                opts.commitMode,
		ConfirmCommit: func(message, targetTree string) (bool, error) {
			return confirmArtifactCommit(os.Stdin, errOut, message, targetTree, stdinIsTerminal(os.Stdin))
		},
		OnGateStart: gateStartProgress(errOut, opts.format), OnGateComplete: gateCompleteProgress(errOut, opts.format),
	})
	if err != nil {
		return writeFailure(errOut, opts.format, applyLabel, applyExitCode(err), err)
	}
	writeApplySuccess(out, opts.format, result)
	return exitPass
}

func parseApplyCLIOptions(args []string, errOut io.Writer) (applyCLIOptions, bool) {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	fs.SetOutput(errOut)
	repo := fs.String("repo", ".", targetRepoHelp)
	baselineModeValue := fs.String("baseline-mode", string(packageapply.BaselineModeStrict), baselineModeHelp)
	allowMissingBaselineProof := fs.Bool("allow-missing-baseline-proof", false, overrideBaselineHelp)
	commitModeValue := fs.String("commit-mode", string(packageapply.CommitModeNone), commitModeHelp)
	format := fs.String("format", "text", outputFormatHelp)
	signaturePath, trustedKey := signatureFlags(fs)
	if err := fs.Parse(args); err != nil {
		return applyCLIOptions{}, false
	}
	baselineMode, modeErr := packageapply.ParseBaselineMode(*baselineModeValue)
	if modeErr != nil {
		fmt.Fprintln(errOut, modeErr)
	}
	commitMode, commitModeErr := packageapply.ParseCommitMode(*commitModeValue)
	if commitModeErr != nil {
		fmt.Fprintln(errOut, commitModeErr)
	}
	if *allowMissingBaselineProof && baselineMode != packageapply.BaselineModePermissive {
		fmt.Fprintln(errOut, "--allow-missing-baseline-proof requires --baseline-mode permissive")
	}
	valid := fs.NArg() == 1 && validFormat(*format) && validSignaturePair(*signaturePath, *trustedKey) && modeErr == nil && commitModeErr == nil && (!*allowMissingBaselineProof || baselineMode == packageapply.BaselineModePermissive)
	if !valid {
		fmt.Fprintln(errOut, "usage: polis apply [--repo <path>] [--baseline-mode strict|compatible|permissive] [--allow-missing-baseline-proof] [--commit-mode none|prompt|auto] [--format text|json] [--signature <file> --trusted-key <pem>] <artifact.polis>")
		return applyCLIOptions{}, false
	}
	return applyCLIOptions{
		repo:                      *repo,
		baselineMode:              baselineMode,
		allowMissingBaselineProof: *allowMissingBaselineProof,
		commitMode:                commitMode,
		format:                    *format,
		signaturePath:             *signaturePath,
		trustedKey:                *trustedKey,
		artifact:                  fs.Arg(0),
	}, true
}

func applyExitCode(err error) int {
	switch {
	case errors.Is(err, packageapply.ErrCommitBlocked):
		return exitBlocked
	case errors.Is(err, packageapply.ErrBaselineMismatch):
		return exitBaselineMismatch
	case errors.Is(err, packageapply.ErrValidationFailed):
		return exitValidationFailed
	default:
		return exitApplyFailed
	}
}

func writeApplySuccess(out io.Writer, format string, result packageapply.Result) {
	if format == "json" {
		var commitSHA any
		if result.Committed {
			commitSHA = result.CommitSHA
		}
		payload := map[string]any{"status": "PASS", "project": result.Project, "change": result.Change, "target_tree": result.TargetTree, "committed": result.Committed, "commit_sha": commitSHA, "commit_message": result.CommitMessage, "validation_level": result.ValidationLevel, "enabled_gates": result.EnabledGates, "disabled_gates": result.DisabledGates, "producer_deferred_gates": result.ProducerDeferredGates, "outstanding_deferred_gates": result.OutstandingDeferredGates, "consumer_validation_required": result.ConsumerValidationRequired, "consumer_validation_status": result.ConsumerValidationStatus, "consumer_gate_statuses": result.ConsumerGateStatuses}
		addBaselineResultFields(payload, result)
		if result.EvidencePath != "" {
			payload["evidence"] = result.EvidencePath
		}
		writeJSON(out, payload)
		return
	}
	fmt.Fprintf(out, applyLabel+": PASS\nProject: %s\nChange: %s\nTarget: %s\nValidation level: %s\nEnabled gates: %s\nDisabled gates: %s\nProducer deferred gates: %s\nOutstanding deferred gates: %s\nConsumer validation required: %t\nConsumer validation status: %s\nConsumer gate statuses: %v\n", result.Project, result.Change, result.TargetTree, result.ValidationLevel, strings.Join(result.EnabledGates, ", "), strings.Join(result.DisabledGates, ", "), displayGateList(result.ProducerDeferredGates), displayGateList(result.OutstandingDeferredGates), result.ConsumerValidationRequired, result.ConsumerValidationStatus, result.ConsumerGateStatuses)
	writeBaselineResultText(out, result)
	if result.Committed {
		fmt.Fprintln(out, "Committed: yes")
		fmt.Fprintf(out, "Commit SHA: %s\n", result.CommitSHA)
		if result.CommitMessage != nil {
			writeCommitMessage(out, "Commit message", *result.CommitMessage)
		}
	} else {
		fmt.Fprintln(out, "Committed: no")
		if result.CommitMessage != nil {
			writeCommitMessage(out, "Suggested commit message", *result.CommitMessage)
		}
	}
	if result.EvidencePath != "" {
		fmt.Fprintf(out, "Evidence: %s\n", result.EvidencePath)
	}
}

func confirmArtifactCommit(reader io.Reader, writer io.Writer, message, targetTree string, terminal bool) (bool, error) {
	if !terminal {
		return false, errors.New("--commit-mode prompt requires a terminal")
	}
	fmt.Fprintf(writer, "Target tree: %s\nArtifact commit message:\n%s", targetTree, escapeCommitMessageForDisplay(message))
	fmt.Fprintln(writer)
	fmt.Fprint(writer, "Create this local commit? [y/N]: ")
	line, err := bufio.NewReader(reader).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer := strings.TrimSpace(line)
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"), nil
}

func writeCommitMessage(out io.Writer, label, message string) {
	fmt.Fprintf(out, "%s:\n%s", label, escapeCommitMessageForDisplay(message))
	fmt.Fprintln(out)
}

func escapeCommitMessageForDisplay(message string) string {
	var escaped strings.Builder
	for _, r := range message {
		switch r {
		case '\n':
			escaped.WriteString(`\n`)
		case '\r':
			escaped.WriteString(`\r`)
		case '\t':
			escaped.WriteString(`\t`)
		case '\b':
			escaped.WriteString(`\b`)
		case '\f':
			escaped.WriteString(`\f`)
		default:
			if unicode.IsControl(r) && r <= 0xFF {
				fmt.Fprintf(&escaped, `\x%02x`, r)
			} else if unicode.IsControl(r) {
				fmt.Fprintf(&escaped, `\u%04x`, r)
			} else {
				escaped.WriteRune(r)
			}
		}
	}
	return escaped.String()
}

func stdinIsTerminal(reader io.Reader) bool {
	file, ok := reader.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

func addBaselineResultFields(payload map[string]any, result packageapply.Result) {
	payload["baseline_mode"] = result.BaselineMode
	payload["baseline_source"] = result.BaselineSource
	payload["consumer_base_commit"] = result.ConsumerBaseCommit
	payload["baseline_ancestry"] = result.BaselineAncestry
	payload["baseline_compatibility"] = result.BaselineCompatibility
	payload["override_active"] = result.OverrideActive
	payload["bypassed_guarantees"] = result.BypassedGuarantees
	if len(result.Warnings) != 0 {
		payload["warnings"] = result.Warnings
	}
}

func writeBaselineResultText(out io.Writer, result packageapply.Result) {
	fmt.Fprintf(out, "Baseline mode: %s\nBaseline source: %s\nConsumer base: %s\nBaseline ancestry: %s\nBaseline compatibility: %s\n", result.BaselineMode, result.BaselineSource, result.ConsumerBaseCommit, result.BaselineAncestry, result.BaselineCompatibility)
	if result.OverrideActive {
		fmt.Fprintln(out, "WARNING: USER SAFETY OVERRIDE ACTIVE")
		for _, guarantee := range result.BypassedGuarantees {
			fmt.Fprintf(out, "Bypassed guarantee: %s\n", guarantee)
		}
	}
	for _, warning := range result.Warnings {
		fmt.Fprintf(out, "Warning: %s\n", warning)
	}
}
