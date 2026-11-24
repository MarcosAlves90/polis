package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MarcosAlves90/polis/v6/internal/commandexec"
	"github.com/MarcosAlves90/polis/v6/internal/pathguard"
	"github.com/MarcosAlves90/polis/v6/internal/policyexec"
	"github.com/MarcosAlves90/polis/v6/spec"
)

const (
	liveSchemaVersion = "polis-live-v1"
	liveSinkQueueSize = 256
	liveSinkCloseWait = 200 * time.Millisecond
)

var liveContext struct {
	sync.Mutex
	active bool
	runID  string
	seq    uint64
}

func startLiveContext() {
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		id = []byte(time.Now().Format(time.RFC3339Nano) + "-" + strconv.FormatUint(uint64(os.Getpid()), 10))
	}
	liveContext.Lock()
	liveContext.active = true
	liveContext.runID = hex.EncodeToString(id)
	liveContext.seq = 0
	liveContext.Unlock()
}

func clearLiveContext() {
	liveContext.Lock()
	liveContext.active = false
	liveContext.runID = ""
	liveContext.seq = 0
	liveContext.Unlock()
}

func liveRunID() string {
	liveContext.Lock()
	defer liveContext.Unlock()
	return liveContext.runID
}

// annotateLiveRecord adds the live envelope to presentation-only JSON Lines
// emitted through the existing progress channel when streaming was requested.
func annotateLiveRecord(record map[string]any) {
	liveContext.Lock()
	defer liveContext.Unlock()
	if !liveContext.active {
		return
	}
	liveContext.seq++
	record["schema_version"] = liveSchemaVersion
	record["run_id"] = liveContext.runID
	record["sequence"] = liveContext.seq
	if _, ok := record["timestamp"]; !ok {
		record["timestamp"] = time.Now().UTC().Format(time.RFC3339Nano)
	}
}

func streamingRequested(args []string) bool {
	for _, arg := range args {
		if arg == "--stream-output" || strings.HasPrefix(arg, "--stream-output=") || arg == "--events-file" || strings.HasPrefix(arg, "--events-file=") {
			return true
		}
	}
	return false
}

// liveReporter is presentation-only. It emits correlated JSON Lines on stderr
// and, when requested, to a new external events file. It never writes to the
// command's stdout or to POLIS evidence, manifests or retained artifacts.
type liveDelivery struct {
	line string
	text string
}

// liveReporter is presentation-only. Records are annotated and queued without
// waiting on a sink; a single delivery worker owns all sink writes so a slow or
// blocked stderr/events-file writer cannot delay validation or gate callbacks.
type liveReporter struct {
	enabled    bool
	format     string
	runID      string
	errOut     io.Writer
	events     *os.File
	mu         sync.Mutex
	emitMu     sync.Mutex
	deliveryMu sync.Mutex
	commands   map[string]spec.CommandSpec
	closed     bool
	abandoned  atomic.Bool
	sink       chan liveDelivery
	sinkDone   chan struct{}
}

func newLiveReporter(enabled bool, format, eventsFile, repoRoot string, errOut io.Writer) (*liveReporter, error) {
	reporter := &liveReporter{
		enabled: enabled, format: format, errOut: errOut,
		commands: map[string]spec.CommandSpec{},
		sink:     make(chan liveDelivery, liveSinkQueueSize),
		sinkDone: make(chan struct{}),
	}
	ready := func() (*liveReporter, error) {
		go reporter.deliver()
		return reporter, nil
	}
	if !enabled {
		if eventsFile != "" {
			return nil, errors.New("--events-file requires --stream-output")
		}
		return ready()
	}
	reporter.runID = liveRunID()
	if eventsFile == "" {
		return ready()
	}
	path, err := prepareLiveEventsFile(eventsFile, repoRoot)
	if err != nil {
		return nil, err
	}
	reporter.events = path
	return ready()
}

func prepareLiveEventsFile(filename, repoRoot string) (*os.File, error) {
	if filename == "" || repoRoot == "" {
		return nil, errors.New("events file and repository root are required")
	}
	abs, err := filepath.Abs(filename)
	if err != nil {
		return nil, fmt.Errorf("resolve events file: %w", err)
	}
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}
	contained, err := pathguard.Contains(root, abs)
	if err != nil {
		return nil, fmt.Errorf("check events file boundary: %w", err)
	}
	if contained {
		return nil, errors.New("events file must be outside the target worktree")
	}
	parent := filepath.Dir(abs)
	_, err = os.Lstat(abs)
	if err == nil {
		return nil, errors.New("events file already exists")
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect events file: %w", err)
	}
	parentInfo, err := os.Stat(parent)
	if err != nil || !parentInfo.IsDir() {
		return nil, errors.New("events file parent directory is unavailable")
	}
	realParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return nil, fmt.Errorf("resolve events file parent: %w", err)
	}
	realPath := filepath.Join(realParent, filepath.Base(abs))
	contained, err = pathguard.Contains(root, realPath)
	if err != nil {
		return nil, fmt.Errorf("check resolved events file boundary: %w", err)
	}
	if contained {
		return nil, errors.New("events file must be outside the target worktree")
	}
	if _, err := os.Lstat(realPath); err == nil {
		return nil, errors.New("events file already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect resolved events file: %w", err)
	}
	file, err := os.OpenFile(abs, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create events file: %w", err)
	}
	return file, nil
}

func (r *liveReporter) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	close(r.sink)
	r.mu.Unlock()
	select {
	case <-r.sinkDone:
		return
	case <-time.After(liveSinkCloseWait):
	}
	r.abandoned.Store(true)
}

func (r *liveReporter) offer(delivery liveDelivery) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return false
	}
	select {
	case r.sink <- delivery:
		return true
	default:
		return false
	}
}

func (r *liveReporter) sinkAbandoned() bool {
	return r.abandoned.Load()
}

func (r *liveReporter) deliver() {
	defer close(r.sinkDone)
	for delivery := range r.sink {
		r.deliveryMu.Lock()
		if r.sinkAbandoned() {
			r.deliveryMu.Unlock()
			continue
		}
		if r.format == "json" {
			_, _ = io.WriteString(r.errOut, delivery.line)
		} else if delivery.text != "" {
			_, _ = io.WriteString(r.errOut, delivery.text)
		}
		if r.events != nil && !r.sinkAbandoned() {
			_, _ = io.WriteString(r.events, delivery.line)
		}
		r.deliveryMu.Unlock()
	}
	if r.events != nil {
		_ = r.events.Close()
	}
}

func (r *liveReporter) SetCommand(id string, command *spec.CommandSpec) {
	if r == nil || !r.enabled || command == nil {
		return
	}
	clone := *command
	clone.Argv = append([]string(nil), command.Argv...)
	clone.Environment = cloneEnvironment(command.Environment)
	r.mu.Lock()
	r.commands[id] = clone
	r.mu.Unlock()
}

func (r *liveReporter) command(id string) (spec.CommandSpec, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	command, ok := r.commands[id]
	return command, ok
}

func cloneEnvironment(environment *spec.EnvironmentSpec) *spec.EnvironmentSpec {
	if environment == nil {
		return nil
	}
	clone := *environment
	clone.Pass = append([]string(nil), environment.Pass...)
	return &clone
}

func (r *liveReporter) Stage(id, action, why string) func(status string) {
	if r == nil || !r.enabled {
		return func(string) {}
	}
	r.emit(map[string]any{
		"type": "progress", "event": "started", "scope": "stage", "id": id,
		"action": action, "why": why,
	}, fmt.Sprintf("POLIS STAGE: id=%q event=%q action=%q why=%q", id, "started", action, why))
	var once sync.Once
	return func(status string) {
		once.Do(func() {
			r.emit(map[string]any{
				"type": "progress", "event": "completed", "scope": "stage", "id": id,
				"action": action, "why": why, "status": status,
			}, fmt.Sprintf("POLIS STAGE: id=%q event=%q status=%q action=%q why=%q", id, "completed", status, action, why))
		})
	}
}

func (r *liveReporter) GateResult(gate spec.GatePolicy, outcome policyexec.Outcome, durationMS int64) {
	if r == nil || !r.enabled {
		return
	}
	command := gate.Command
	r.result(gate.ID, command, outcome.Status, outcome.Action, outcome.Reason, durationMS)
}

func (r *liveReporter) ChangeResult(id string, command spec.CommandSpec, status spec.Status, durationMS int64) {
	if r == nil || !r.enabled {
		return
	}
	r.result(id, &command, status, "executed", "", durationMS)
}

func (r *liveReporter) result(id string, command *spec.CommandSpec, status spec.Status, action, reason string, durationMS int64) {
	record := map[string]any{
		"type": "result", "event": "completed", "scope": "gate", "id": id,
		"status": string(status), "action": action, "reason": reason, "duration_ms": durationMS,
	}
	if command != nil {
		stored := *command
		stored.Argv = append([]string(nil), command.Argv...)
		stored.Environment = cloneEnvironment(command.Environment)
		record["command"] = liveCommand(stored)
		r.SetCommand(id, command)
	}
	text := fmt.Sprintf("POLIS RESULT: id=%q status=%q action=%q reason=%q duration_ms=%d", id, string(status), action, reason, durationMS)
	r.emit(record, text)
}

func liveCommand(command spec.CommandSpec) map[string]any {
	record := map[string]any{"argv": append([]string(nil), command.Argv...), "cwd": command.Cwd}
	if command.Environment != nil {
		record["environment_mode"] = command.Environment.Mode
		record["environment_pass"] = append([]string(nil), command.Environment.Pass...)
	}
	return record
}

// Output matches commandexec.OutputObserver. Errors are never returned so a
// presentation sink cannot change a gate outcome; accounting stays internal.
func (r *liveReporter) Output(chunk commandexec.OutputChunk) error {
	if r == nil || !r.enabled {
		return nil
	}
	// commandexec has already removed terminal control sequences and replaced
	// invalid UTF-8. JSON marshaling escapes the remaining characters, so the
	// authenticated text can be carried directly for normal consumers.
	record := map[string]any{
		"type": "output", "event": "chunk", "scope": "gate", "id": chunk.Gate,
		"stream": chunk.Stream, "data": string(chunk.Data), "encoding": "utf-8",
		"bytes": chunk.Bytes, "chunk_sequence": chunk.Sequence,
		"truncated": chunk.Truncated, "dropped_chunks": chunk.DroppedChunks,
		"dropped_bytes": chunk.DroppedBytes,
	}
	if command, ok := r.command(chunk.Gate); ok {
		record["command"] = liveCommand(command)
	}
	text := fmt.Sprintf("POLIS LIVE: gate=%q stream=%q sequence=%d bytes=%d data=%q",
		chunk.Gate, chunk.Stream, chunk.Sequence, chunk.Bytes, string(chunk.Data))
	r.emit(record, text)
	return nil
}

func sanitizeLiveText(value string) string {
	return strings.Map(func(r rune) rune {
		if r == 0x1b || r == 0x7f || (r < 0x20 && r != '\n' && r != '\t') {
			return -1
		}
		return r
	}, value)
}

func (r *liveReporter) emit(record map[string]any, text string) {
	if r == nil || !r.enabled {
		return
	}
	r.emitMu.Lock()
	defer r.emitMu.Unlock()
	annotateLiveRecord(record)
	var line strings.Builder
	writeJSON(&line, record)
	delivery := liveDelivery{line: line.String()}
	if r.format != "json" && text != "" {
		delivery.text = sanitizeLiveText(text) + "\n"
	}
	_ = r.offer(delivery)
}

// liveGateStart registers the configured command and retains the ordinary gate
// start progress event when one is supplied.
func liveGateStart(reporter *liveReporter, next func(spec.GatePolicy), format string, errOut io.Writer) func(spec.GatePolicy) {
	return func(gate spec.GatePolicy) {
		reporter.SetCommand(gate.ID, gate.Command)
		if next != nil {
			next(gate)
		} else {
			gateStartProgress(errOut, format)(gate)
		}
	}
}

func liveChangeStart(reporter *liveReporter) func(string, spec.CommandSpec) {
	return func(id string, command spec.CommandSpec) {
		reporter.SetCommand(id, &command)
	}
}

func liveOutput(reporter *liveReporter) commandexec.OutputObserver {
	return func(chunk commandexec.OutputChunk) error {
		return reporter.Output(chunk)
	}
}

// liveStageStatus converts a boolean outcome into the presentation status used
// by stage events without affecting the command result itself.
func liveStageStatus(passed bool) string {
	if passed {
		return "PASS"
	}
	return "FAIL"
}
