package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/MarcosAlves90/polis/internal/packageapply"
	"github.com/MarcosAlves90/polis/internal/packagebuild"
	"github.com/MarcosAlves90/polis/internal/packageverify"
	"github.com/MarcosAlves90/polis/internal/policyinit"
	"github.com/MarcosAlves90/polis/internal/redcapture"
)

const version = "4.0.0"

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, out, errOut io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(errOut, "usage: polis <doctor|init|capture-red|verify|build|apply>")
		return 2
	}
	switch args[0] {
	case "doctor":
		return runDoctor(out, errOut)
	case "init":
		return runInit(args[1:], out, errOut)
	case "capture-red":
		return runCaptureRed(args[1:], out, errOut)
	case "verify":
		if len(args) != 2 {
			fmt.Fprintln(errOut, "usage: polis verify <artifact.polis>")
			return 2
		}
		r, err := packageverify.Verify(args[1])
		if err != nil {
			fmt.Fprintf(errOut, "POLIS VERIFY: FAIL: %v\n", err)
			return 2
		}
		fmt.Fprintf(out, "POLIS VERIFY: PASS\nProject: %s\nBase: %s\nTarget: %s\n", r.Project, r.BaseCommit, r.TargetTree)
		return 0
	case "build":
		return runBuild(args[1:], out, errOut)
	case "apply":
		return runApply(args[1:], out, errOut)
	default:
		fmt.Fprintf(errOut, "unknown command %q\n", args[0])
		return 2
	}
}

func runInit(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(errOut)
	repo := fs.String("repo", ".", "target Git worktree path")
	profile := fs.String("profile", "auto", "policy profile: auto or go")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(errOut, "usage: polis init [--repo <path>] [--profile auto|go]")
		return 2
	}
	result, err := policyinit.Init(context.Background(), policyinit.Options{Repo: *repo, Profile: *profile})
	if err != nil {
		fmt.Fprintf(errOut, "POLIS INIT: FAIL: %v\n", err)
		return 2
	}
	fmt.Fprintf(out, "POLIS INIT: PASS\nProfile: %s\nPolicy: %s\nNext: review and commit .polis/policy.json before polis build\n", result.Profile, result.PolicyPath)
	return 0
}

func runCaptureRed(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("capture-red", flag.ContinueOnError)
	fs.SetOutput(errOut)
	repo := fs.String("repo", "", "Git worktree path")
	contract := fs.String("contract", "", "defect Change Contract JSON outside the worktree")
	outPath := fs.String("out", "", "output regression patch outside the worktree")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 || *repo == "" || *contract == "" || *outPath == "" {
		fmt.Fprintln(errOut, "usage: polis capture-red --repo <path> --contract <change.json> --out <regression.patch>")
		return 2
	}
	result, err := redcapture.Capture(context.Background(), redcapture.Options{Repo: *repo, Contract: *contract, Out: *outPath})
	if err != nil {
		fmt.Fprintf(errOut, "POLIS CAPTURE-RED: FAIL: %v\n", err)
		return 2
	}
	fmt.Fprintf(out, "POLIS CAPTURE-RED: PASS\nPatch: %s\nSHA256: %s\n", result.Path, result.SHA256)
	return 0
}

func runBuild(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(errOut)
	repo := fs.String("repo", "", "Git worktree path")
	project := fs.String("project", "", "canonical project slug")
	change := fs.String("change", "", "canonical change slug")
	outDir := fs.String("out", "", "output directory")
	contract := fs.String("contract", "", "delivery Change Contract JSON outside the worktree")
	regressionPatch := fs.String("regression-patch", "", "validated Red-state patch for defect contracts")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 || *repo == "" || *project == "" || *change == "" || *outDir == "" || *contract == "" {
		fmt.Fprintln(errOut, "usage: polis build --repo <path> --project <slug> --change <slug> --contract <change.json> [--regression-patch <red.patch>] --out <directory>")
		return 2
	}
	result, err := packagebuild.Build(context.Background(), packagebuild.Options{
		Repo: *repo, Project: *project, Change: *change, Out: *outDir, Contract: *contract, RegressionPatch: *regressionPatch,
	})
	if err != nil {
		fmt.Fprintf(errOut, "POLIS BUILD: FAIL: %v\n", err)
		return 2
	}
	fmt.Fprintf(out, "POLIS BUILD: PASS\nArtifact: %s\nSHA256: %s\nBase: %s\nTarget: %s\n", result.Path, result.SHA256, result.BaseCommit, result.TargetTree)
	return 0
}

func runApply(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	fs.SetOutput(errOut)
	repo := fs.String("repo", ".", "target Git worktree path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(errOut, "usage: polis apply [--repo <path>] <artifact.polis>")
		return 2
	}
	result, err := packageapply.Apply(context.Background(), fs.Arg(0), *repo)
	if err != nil {
		fmt.Fprintf(errOut, "POLIS APPLY: FAIL: %v\n", err)
		return 2
	}
	fmt.Fprintf(out, "POLIS APPLY: PASS\nProject: %s\nChange: %s\nTarget: %s\nEvidence: %s\n", result.Project, result.Change, result.TargetTree, result.EvidencePath)
	return 0
}

func runDoctor(out, errOut io.Writer) int {
	fmt.Fprintf(out, "POLIS doctor %s\n", version)
	fmt.Fprintf(out, "OS/Arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(out, "Go runtime: %s\n", runtime.Version())
	gitPath, err := exec.LookPath("git")
	if err != nil {
		fmt.Fprintln(errOut, "Git: BLOCKED (not found)")
		return 4
	}
	b, err := exec.Command(gitPath, "--version").CombinedOutput()
	if err != nil {
		fmt.Fprintf(errOut, "Git: BLOCKED (%v)\n", err)
		return 4
	}
	fmt.Fprintf(out, "Git: %s\n", strings.TrimSpace(string(b)))
	return 0
}
