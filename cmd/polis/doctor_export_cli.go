package main

import (
	"flag"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"

	"github.com/MarcosAlves90/polis/v6/internal/diagnostic"
	"github.com/MarcosAlves90/polis/v6/internal/offlinekit"
	artifactsig "github.com/MarcosAlves90/polis/v6/internal/signature"
)

func runSign(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("sign", flag.ContinueOnError)
	fs.SetOutput(errOut)
	key := fs.String("key", "", "Ed25519 PKCS#8 private key PEM")
	outPath := fs.String("out", "", "detached signature output")
	format := fs.String("format", "text", outputFormatHelp)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 1 || *key == "" || *outPath == "" || !validFormat(*format) {
		fmt.Fprintln(errOut, "usage: polis sign --key <private.pem> --out <artifact.polis.sig> [--format text|json] <artifact.polis>")
		return exitUsage
	}
	writeProgress(errOut, *format, "create the detached artifact signature", "the signature must cover the exact artifact bytes without modifying the package")
	result, err := artifactsig.SignFile(fs.Arg(0), *key, *outPath)
	if err != nil {
		return writeFailure(errOut, *format, "POLIS SIGN", exitValidationFailed, err)
	}
	if *format == "json" {
		writeJSON(out, map[string]any{"status": "PASS", "signature": result.SignaturePath, "artifact_sha256": result.ArtifactSHA256, "public_key_sha256": result.PublicKeySHA256})
	} else {
		fmt.Fprintf(out, "POLIS SIGN: PASS\nSignature: %s\nArtifact SHA256: %s\nPublic key SHA256: %s\n", result.SignaturePath, result.ArtifactSHA256, result.PublicKeySHA256)
	}
	return exitPass
}

func runExport(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(errOut)
	outPath := fs.String("out", "", "self-contained POLIS V6 offline bundle output")
	format := fs.String("format", "text", outputFormatHelp)
	executable := fs.String("executable", "", "POLIS executable to embed (defaults to current executable)")
	targetRuntime := fs.String("runtime", "", "target runtime as GOOS/GOARCH (defaults to current runtime)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 || *outPath == "" || !validFormat(*format) {
		fmt.Fprintln(errOut, "usage: polis export --out <polis-offline.zip> [--format text|json] [--executable <file>] [--runtime <GOOS/GOARCH>]")
		return exitUsage
	}
	writeProgress(errOut, *format, "assemble and verify the offline runtime bundle", "the exported bundle must contain a runnable POLIS binary and its required embedded resources")
	result, err := offlinekit.Export(offlinekit.Options{Out: *outPath, Executable: *executable, TargetRuntime: *targetRuntime, Version: version})
	if err != nil {
		return writeFailure(errOut, *format, "POLIS EXPORT", exitValidationFailed, err)
	}
	if *format == "json" {
		writeJSON(out, map[string]any{
			"status":           "PASS",
			"bundle":           result.Path,
			"sha256":           result.SHA256,
			"polis_version":    result.Version,
			"runtime":          result.Runtime,
			"executable":       result.Executable,
			"network_required": result.NetworkRequired,
			"entries":          result.Entries,
		})
	} else {
		fmt.Fprintf(out, "POLIS EXPORT: PASS\nBundle: %s\nSHA256: %s\nPOLIS version: %s\nRuntime: %s\nExecutable: %s\nNetwork required: no\nGit required: yes\n", result.Path, result.SHA256, result.Version, result.Runtime, result.Executable)
	}
	return exitPass
}

func runDoctor(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(errOut)
	format := fs.String("format", "text", outputFormatHelp)
	repo := fs.String("repo", "", repoHelp)
	policy := fs.String("policy", "", externalPolicyHelp)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	repoSpecified := false
	policySpecified := false
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "repo":
			repoSpecified = true
		case "policy":
			policySpecified = true
		}
	})
	if fs.NArg() != 0 || !validFormat(*format) || (repoSpecified && *repo == "") || (policySpecified && (!repoSpecified || *policy == "")) {
		fmt.Fprintln(errOut, "usage: polis doctor [--repo <path> [--policy <policy-v3.json>]] [--format text|json]")
		return exitUsage
	}
	writeProgress(errOut, *format, "locate the Git executable", "POLIS repository operations require a working Git installation")
	gitPath, err := exec.LookPath("git")
	if err != nil {
		if *format == "json" {
			if writer, ok := errOut.(interface{ finishBeforeFailure(int) }); ok {
				writer.finishBeforeFailure(exitBlocked)
			}
			writeJSON(errOut, map[string]any{"status": "BLOCKED", "version": version, "os": runtime.GOOS, "arch": runtime.GOARCH, "go_runtime": runtime.Version(), "error": "git not found",
				"diagnostic": diagnostic.Report{Code: "POLIS_GIT_PREREQUISITE_MISSING", Category: "prerequisite", Stage: "doctor", ObservedCause: "missing_executable", AffectedOperations: []string{"doctor"}, NotRun: []string{"Git version check"}, Remediation: "install_git"}})
		} else {
			fmt.Fprintf(out, "POLIS doctor %s\nOS/Arch: %s/%s\nGo runtime: %s\n", version, runtime.GOOS, runtime.GOARCH, runtime.Version())
			fmt.Fprintln(errOut, "Git: BLOCKED (not found)")
		}
		return exitBlocked
	}
	writeProgress(errOut, *format, "query the Git runtime version", "the doctor result should identify the concrete Git runtime available to POLIS")
	b, err := exec.Command(gitPath, "--version").CombinedOutput()
	if err != nil {
		return writeFailure(errOut, *format, "POLIS DOCTOR", exitBlocked, err)
	}
	gitVersion := strings.TrimSpace(string(b))
	if repoSpecified {
		writeProgress(errOut, *format, "inspect the effective repository policy and static gate prerequisites", "validate the plan and check paths and executables without running project gates")
		report, code := inspectDoctorRepository(*repo, *policy)
		if *format == "json" {
			writeJSON(out, map[string]any{"status": report.Status, "version": version, "os": runtime.GOOS, "arch": runtime.GOARCH, "go_runtime": runtime.Version(), "git": gitVersion, "repository": report})
		} else {
			fmt.Fprintf(out, "POLIS doctor %s\nOS/Arch: %s/%s\nGo runtime: %s\nGit: %s\n", version, runtime.GOOS, runtime.GOARCH, runtime.Version(), gitVersion)
			writeDoctorRepositoryText(out, report)
		}
		return code
	}
	if *format == "json" {
		writeJSON(out, map[string]any{"status": "PASS", "version": version, "os": runtime.GOOS, "arch": runtime.GOARCH, "go_runtime": runtime.Version(), "git": gitVersion})
	} else {
		fmt.Fprintf(out, "POLIS doctor %s\n", version)
		fmt.Fprintf(out, "OS/Arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
		fmt.Fprintf(out, "Go runtime: %s\n", runtime.Version())
		fmt.Fprintf(out, "Git: %s\n", gitVersion)
	}
	return exitPass
}

func signatureFlags(fs *flag.FlagSet) (*string, *string) {
	return fs.String("signature", "", "detached POLIS signature"), fs.String("trusted-key", "", "trusted Ed25519 public key PEM")
}

func validSignaturePair(signaturePath, trustedKey string) bool {
	return (signaturePath == "" && trustedKey == "") || (signaturePath != "" && trustedKey != "")
}

func verifyDetached(artifact, signaturePath, trustedKey string) error {
	if signaturePath == "" {
		return nil
	}
	return artifactsig.VerifyFile(artifact, signaturePath, trustedKey)
}
