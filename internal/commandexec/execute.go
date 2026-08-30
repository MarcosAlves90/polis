package commandexec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/MarcosAlves90/polis/v5/spec"
)

const MaxRetainedOutputBytes = 1 << 20

type Observation struct {
	Status          spec.Status
	ExitCode        int
	DurationMS      int64
	Stdout          string
	Stderr          string
	StdoutBytes     int64
	StderrBytes     int64
	StdoutSHA256    string
	StderrSHA256    string
	StdoutTruncated bool
	StderrTruncated bool
}

type boundedDigestWriter struct {
	h         hash.Hash
	buf       []byte
	total     int64
	truncated bool
}

func newBoundedDigestWriter() *boundedDigestWriter {
	return &boundedDigestWriter{h: sha256.New(), buf: make([]byte, 0, 4096)}
}

func (w *boundedDigestWriter) Write(p []byte) (int, error) {
	_, _ = w.h.Write(p)
	w.total += int64(len(p))
	remaining := MaxRetainedOutputBytes - len(w.buf)
	if remaining > 0 {
		take := len(p)
		if take > remaining {
			take = remaining
		}
		w.buf = append(w.buf, p[:take]...)
	}
	if w.total > MaxRetainedOutputBytes {
		w.truncated = true
	}
	return len(p), nil
}

func (w *boundedDigestWriter) string() string { return string(w.buf) }
func (w *boundedDigestWriter) digest() string { return hex.EncodeToString(w.h.Sum(nil)) }

func Run(repoRoot string, command spec.CommandSpec) Observation {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(command.TimeoutSeconds)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, command.Argv[0], command.Argv[1:]...)
	cmd.Dir = filepath.Join(repoRoot, filepath.FromSlash(command.Cwd))
	if command.Environment != nil {
		cmd.Env = environmentFor(*command.Environment)
	}
	stdout, stderr := newBoundedDigestWriter(), newBoundedDigestWriter()
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	started := time.Now()
	err := cmd.Run()
	obs := Observation{
		Status: spec.StatusPass, ExitCode: 0, DurationMS: time.Since(started).Milliseconds(),
		Stdout: stdout.string(), Stderr: stderr.string(), StdoutBytes: stdout.total, StderrBytes: stderr.total,
		StdoutSHA256: stdout.digest(), StderrSHA256: stderr.digest(), StdoutTruncated: stdout.truncated, StderrTruncated: stderr.truncated,
	}
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
	message := err.Error()
	if obs.Stderr != "" {
		message = "\n" + message
	}
	_, _ = stderr.Write([]byte(message))
	obs.Stderr = stderr.string()
	obs.StderrBytes = stderr.total
	obs.StderrSHA256 = stderr.digest()
	obs.StderrTruncated = stderr.truncated
	return obs
}

func environmentFor(environment spec.EnvironmentSpec) []string {
	if environment.Mode == spec.EnvironmentModeInherit {
		return os.Environ()
	}
	result := make([]string, 0, len(environment.Pass))
	for _, name := range environment.Pass {
		if value, ok := os.LookupEnv(name); ok {
			result = append(result, name+"="+value)
		}
	}
	return result
}
