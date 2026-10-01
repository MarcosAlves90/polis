package gaterun

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/MarcosAlves90/polis/v6/internal/devlock"
	"github.com/MarcosAlves90/polis/v6/internal/fileutil"
	"github.com/MarcosAlves90/polis/v6/internal/gitutil"
	"github.com/MarcosAlves90/polis/v6/internal/pathguard"
	"github.com/MarcosAlves90/polis/v6/internal/policyplan"
	"github.com/MarcosAlves90/polis/v6/spec"
)

type Inputs struct {
	Repository     string             `json:"repository"`
	Head           string             `json:"head"`
	SourceSHA256   string             `json:"source_sha256"`
	ContractSHA256 string             `json:"contract_sha256"`
	Baseline       *spec.BaselineLock `json:"baseline"`
	PolicySHA256   string             `json:"policy_sha256"`
	POLISVersion   string             `json:"polis_version"`
	Runtime        string             `json:"runtime"`
	EnvironmentID  string             `json:"environment_id"`
}

func digest(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func snapshot(ctx context.Context, repo string) (string, string, error) {
	head, err := gitutil.Output(ctx, repo, nil, nil, "rev-parse", "HEAD")
	if err != nil {
		return "", "", err
	}
	stages, err := gitutil.Bytes(ctx, repo, nil, nil, "ls-files", "--stage", "-z")
	if err != nil {
		return "", "", err
	}
	for _, entry := range bytes.Split(stages, []byte{0}) {
		if bytes.HasPrefix(entry, []byte("160000 ")) {
			return "", "", errors.New("source identity does not support submodules")
		}
	}
	// Stage entries alone omit intent-to-add and index visibility flags. Bind
	// the staged delta and semantic flags, without unstable index timestamps.
	stagedDelta, err := gitutil.Bytes(ctx, repo, nil, nil, "diff", "--cached", "--raw", "--no-abbrev", "--no-renames", "--no-ext-diff", "--no-textconv", "--ita-invisible-in-index", "-z", "HEAD", "--")
	if err != nil {
		return "", "", err
	}
	indexFlags, err := gitutil.Bytes(ctx, repo, nil, nil, "ls-files", "-v", "-z")
	if err != nil {
		return "", "", err
	}
	raw, err := gitutil.Bytes(ctx, repo, nil, nil, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err != nil {
		return "", "", err
	}
	paths := map[string]bool{}
	for _, p := range bytes.Split(raw, []byte{0}) {
		if len(p) != 0 {
			paths[string(p)] = true
		}
	}
	if len(paths) > 100000 {
		return "", "", errors.New("source identity exceeds file limit")
	}
	ordered := make([]string, 0, len(paths))
	for p := range paths {
		ordered = append(ordered, p)
	}
	sort.Strings(ordered)
	h := sha256.New()
	// Commands can inspect staging as well as worktree bytes.
	fmt.Fprintf(h, "%q %q %q\n", stages, stagedDelta, indexFlags)
	var total int64
	for _, p := range ordered {
		if err := spec.ValidateRepoRelativePath(p); err != nil {
			return "", "", fmt.Errorf("source path: %w", err)
		}
		filename := filepath.Join(repo, filepath.FromSlash(p))
		info, err := os.Lstat(filename)
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(h, "%q missing\n", p)
			continue
		}
		if err != nil {
			return "", "", err
		}
		contained, err := pathguard.Contains(repo, filename)
		if err != nil || !contained {
			return "", "", fmt.Errorf("source path %q resolves outside worktree", p)
		}
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			link, err = os.Readlink(filename)
			if err != nil {
				return "", "", err
			}
			info, err = os.Stat(filename)
			if err != nil {
				return "", "", err
			}
		}
		if !info.Mode().IsRegular() {
			return "", "", fmt.Errorf("source path %q is not a regular file", p)
		}
		total += info.Size()
		if total > 256<<20 {
			return "", "", errors.New("source identity exceeds 256 MiB")
		}
		f, err := os.Open(filename)
		if err != nil {
			return "", "", err
		}
		fh := sha256.New()
		n, copyErr := io.Copy(fh, io.LimitReader(f, info.Size()+1))
		_ = f.Close()
		if copyErr != nil {
			return "", "", copyErr
		}
		if n != info.Size() {
			return "", "", errors.New("source changed while hashing")
		}
		fmt.Fprintf(h, "%q %t %q %x\n", p, info.Mode()&0111 != 0, link, fh.Sum(nil))
	}
	return head, hex.EncodeToString(h.Sum(nil)), nil
}

func loadInputs(ctx context.Context, repo string, plan policyplan.Plan, opts Options) (Inputs, string, error) {
	in := Inputs{Repository: repo, PolicySHA256: plan.PolicySHA256, POLISVersion: opts.Version,
		Runtime: runtime.GOOS + "/" + runtime.GOARCH + "/" + runtime.Version(), EnvironmentID: opts.EnvironmentID}
	var err error
	in.Head, in.SourceSHA256, err = snapshot(ctx, repo)
	if err != nil {
		return in, "", err
	}
	base := "HEAD"
	if opts.Contract != "" {
		raw, err := fileutil.ReadOutside(repo, opts.Contract, fileutil.OutsideReadOptions{Max: 1 << 20, OversizeMessage: "contract exceeds maximum size"})
		if err != nil {
			return in, "", fmt.Errorf("contract: %w", err)
		}
		contract, err := spec.DecodeChangeContract(raw)
		if err != nil {
			return in, "", err
		}
		if !contract.IsLockedStrictDevelopment() || contract.BaselineLock == nil {
			return in, "", errors.New("gate runs require a locked contract when --contract is supplied")
		}
		if err := devlock.ValidateBuildRepository(ctx, repo, contract); err != nil {
			return in, "", err
		}
		if contract.BaselineLock.PolicySHA256 != plan.PolicySHA256 {
			return in, "", errors.New("contract baseline policy mismatch")
		}
		sum := sha256.Sum256(raw)
		in.ContractSHA256 = hex.EncodeToString(sum[:])
		in.Baseline = contract.BaselineLock
		base = contract.BaselineLock.BaseCommit
	}
	return in, base, nil
}

func changedPaths(ctx context.Context, repo, base string) ([]string, error) {
	staged, err := gitutil.Bytes(ctx, repo, nil, nil, "diff", "--cached", "--no-renames", "--name-only", "-z", base, "--")
	if err != nil {
		return nil, err
	}
	unstaged, err := gitutil.Bytes(ctx, repo, nil, nil, "diff", "--no-renames", "--name-only", "-z", "--")
	if err != nil {
		return nil, err
	}
	untracked, err := gitutil.Bytes(ctx, repo, nil, nil, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	// A net base-to-worktree diff can hide a staged edit undone only in the
	// worktree. Preserve the union of both deltas and nonignored new files.
	for _, raw := range [][]byte{staged, unstaged, untracked} {
		for _, p := range bytes.Split(raw, []byte{0}) {
			if len(p) != 0 {
				set[string(p)] = true
			}
		}
	}
	paths := make([]string, 0, len(set))
	for p := range set {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths, nil
}

func selectGates(plan policyplan.Plan, requested []string, affected bool, paths []string) (map[string]bool, string, error) {
	selected := map[string]bool{}
	gates := plan.GatePolicies()
	byID := map[string]spec.GatePolicy{}
	for _, g := range gates {
		byID[g.ID] = g
	}
	for _, id := range requested {
		g, exists := byID[id]
		if !exists || g.Mode == spec.GateModeNotApplicable {
			return nil, "", fmt.Errorf("gate %q is unknown or not applicable", id)
		}
		if selected[id] {
			return nil, "", fmt.Errorf("duplicate selected gate %q", id)
		}
		selected[id] = true
	}
	reason := "explicit selection"
	if len(requested) == 0 && !affected {
		reason = "full configured validation"
		for _, g := range gates {
			if g.Mode != spec.GateModeNotApplicable {
				selected[g.ID] = true
			}
		}
	}
	if affected {
		reason = "affected paths with conservative fallback for unmapped gates or paths"
		for _, g := range gates {
			if g.Mode == spec.GateModeNotApplicable {
				continue
			}
			if len(g.InputPaths) == 0 {
				selected[g.ID] = true
				continue
			}
			for _, p := range paths {
				for _, input := range g.InputPaths {
					if pathMatches(input, p) {
						selected[g.ID] = true
					}
				}
			}
		}
		for _, p := range paths {
			mapped := false
			for _, g := range gates {
				for _, input := range g.InputPaths {
					mapped = mapped || pathMatches(input, p)
				}
			}
			if !mapped {
				for _, g := range gates {
					if g.Mode != spec.GateModeNotApplicable {
						selected[g.ID] = true
					}
				}
			}
		}
	}
	var include func(string)
	include = func(id string) {
		for _, dep := range byID[id].DependsOn {
			if !selected[dep] {
				selected[dep] = true
				include(dep)
			}
		}
	}
	for id := range selected {
		include(id)
	}
	return selected, reason, nil
}

func pathMatches(input, p string) bool {
	return input == "." || input == p || strings.HasPrefix(p, input+"/")
}
