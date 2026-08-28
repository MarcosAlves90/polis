package commandexec

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/polis-dev/polis-v4/spec"
)

type Observation struct {
	Status     spec.Status
	ExitCode   int
	DurationMS int64
	Stdout     string
	Stderr     string
}

func Run(repoRoot string, command spec.CommandSpec) Observation {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(command.TimeoutSeconds)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, command.Argv[0], command.Argv[1:]...)
	cmd.Dir = filepath.Join(repoRoot, filepath.FromSlash(command.Cwd))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	started := time.Now()
	err := cmd.Run()
	obs := Observation{Status: spec.StatusPass, ExitCode: 0, DurationMS: time.Since(started).Milliseconds(), Stdout: stdout.String(), Stderr: stderr.String()}
	if ctx.Err() == context.DeadlineExceeded {
		obs.Status = spec.StatusFail
		obs.ExitCode = -1
		return obs
	}
	if err == nil {
		return obs
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		obs.Status = spec.StatusFail
		obs.ExitCode = exitErr.ExitCode()
		return obs
	}
	obs.Status = spec.StatusBlocked
	obs.ExitCode = -1
	if obs.Stderr != "" {
		obs.Stderr += "\n"
	}
	obs.Stderr += err.Error()
	return obs
}
