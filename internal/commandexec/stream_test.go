package commandexec

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MarcosAlves90/polis/v6/spec"
)

func TestIssue44StreamHelper(t *testing.T) {
	mode := os.Getenv("ISSUE44_STREAM_HELPER")
	if mode == "" {
		return
	}
	switch mode {
	case "incremental":
		_, _ = os.Stdout.WriteString("ISSUE44_STDOUT_FIRST\n")
		time.Sleep(350 * time.Millisecond)
		_, _ = os.Stderr.WriteString("ISSUE44_STDERR_SECOND\n")
		time.Sleep(350 * time.Millisecond)
	case "bulk":
		block := bytes.Repeat([]byte("A"), 64<<10)
		for i := 0; i < 17; i++ {
			_, _ = os.Stdout.Write(block)
		}
		_, _ = os.Stderr.Write([]byte{0x1b, '[', '3', '1', 'm', 0xff, 0xfe, 'x', '\n'})
	case "chatty":
		block := bytes.Repeat([]byte("L"), 4<<10)
		for i := 0; i < 400; i++ {
			_, _ = os.Stdout.Write(block)
			time.Sleep(time.Millisecond)
		}
	case "sleep":
		time.Sleep(6 * time.Second)
	default:
		os.Exit(9)
	}
	if marker := os.Getenv("ISSUE44_STREAM_MARKER"); marker != "" {
		_ = os.WriteFile(marker, []byte(fmt.Sprintf("%d", time.Now().UnixNano())), 0o600)
	}
	os.Exit(0)
}

func issue44Command(t *testing.T, mode string) spec.CommandSpec {
	t.Helper()
	return spec.CommandSpec{
		Argv:           []string{os.Args[0], "-test.run=^TestIssue44StreamHelper$"},
		Cwd:            ".",
		TimeoutSeconds: 20,
	}
}

func TestIssue44StreamObserverSeesOutputBeforeExit(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "exited.marker")
	t.Setenv("ISSUE44_STREAM_HELPER", "incremental")
	t.Setenv("ISSUE44_STREAM_MARKER", marker)

	var mu sync.Mutex
	var chunks []OutputChunk
	observedAt := map[string]time.Time{}
	markerAtObservation := map[string]bool{}
	obs := RunWithOptions(root, issue44Command(t, "incremental"), Options{
		Gate: "test.complete",
		OnOutput: func(chunk OutputChunk) error {
			mu.Lock()
			defer mu.Unlock()
			chunks = append(chunks, chunk)
			if strings.Contains(string(chunk.Data), "ISSUE44_STDOUT_FIRST") || strings.Contains(string(chunk.Data), "ISSUE44_STDERR_SECOND") {
				observedAt[chunk.Stream] = time.Now()
				_, err := os.Stat(marker)
				markerAtObservation[chunk.Stream] = err == nil
			}
			return nil
		},
	})
	if obs.Status != spec.StatusPass {
		t.Fatalf("status=%s prerequisite=%q stderr=%q", obs.Status, obs.Prerequisite, obs.Stderr)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, stream := range []string{StreamStdout, StreamStderr} {
		if observedAt[stream].IsZero() {
			t.Fatalf("no observed chunk for %s: %+v", stream, chunks)
		}
		if markerAtObservation[stream] {
			t.Fatalf("%s was observed only after child exit", stream)
		}
	}
	raw, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("helper did not record exit: %v", err)
	}
	var exited int64
	if _, err := fmt.Sscan(string(raw), &exited); err != nil {
		t.Fatalf("invalid exit marker %q: %v", raw, err)
	}
	for stream, at := range observedAt {
		if at.UnixNano() >= exited {
			t.Fatalf("%s observed at %d not before exit %d", stream, at.UnixNano(), exited)
		}
	}
	for _, chunk := range chunks {
		if chunk.Gate != "test.complete" || (chunk.Stream != StreamStdout && chunk.Stream != StreamStderr) || chunk.Sequence == 0 {
			t.Fatalf("unattributed chunk: %+v", chunk)
		}
		if _, err := json.Marshal(chunk.Data); err != nil {
			t.Fatalf("chunk data is not JSON-safe: %v", err)
		}
	}
	seq := map[string]uint64{}
	for _, chunk := range chunks {
		seq[chunk.Stream]++
		if chunk.Sequence != seq[chunk.Stream] {
			t.Fatalf("stream %s sequence=%d want=%d", chunk.Stream, chunk.Sequence, seq[chunk.Stream])
		}
	}
}

func TestIssue44StreamPreservesDigestsPreviewAndSanitizesData(t *testing.T) {
	t.Setenv("ISSUE44_STREAM_HELPER", "bulk")
	root := t.TempDir()
	var mu sync.Mutex
	var chunks []OutputChunk
	obs := RunWithOptions(root, issue44Command(t, "bulk"), Options{
		Gate: "bulk",
		OnOutput: func(chunk OutputChunk) error {
			mu.Lock()
			chunks = append(chunks, chunk)
			mu.Unlock()
			return nil
		},
	})
	want := bytes.Repeat([]byte("A"), 17*(64<<10))
	digest := sha256.Sum256(want)
	if obs.Status != spec.StatusPass || obs.StdoutBytes != int64(len(want)) || obs.StdoutSHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("digests changed: status=%s bytes=%d digest=%s", obs.Status, obs.StdoutBytes, obs.StdoutSHA256)
	}
	if !obs.StdoutTruncated || len(obs.Stdout) != MaxRetainedOutputBytes {
		t.Fatalf("preview bound changed: truncated=%t len=%d", obs.StdoutTruncated, len(obs.Stdout))
	}
	if len(chunks) < 2 || obs.Live.Chunks == 0 {
		t.Fatalf("no live chunks observed: %d", len(chunks))
	}
	for _, chunk := range chunks {
		if !utf8Valid(chunk.Data) || bytes.ContainsRune(chunk.Data, 0x1b) {
			t.Fatalf("unsafe chunk data: %q", chunk.Data)
		}
		if _, err := json.Marshal(chunk.Data); err != nil {
			t.Fatalf("invalid JSON data: %v", err)
		}
	}
	if !strings.Contains(obs.Stderr, "x") || obs.StderrBytes != 9 {
		t.Fatalf("stderr capture changed: bytes=%d %q", obs.StderrBytes, obs.Stderr)
	}
}

func utf8Valid(data []byte) bool {
	for i := 0; i < len(data); {
		if data[i] < 0x80 {
			i++
			continue
		}
		size := 1
		switch {
		case data[i]&0xe0 == 0xc0:
			size = 2
		case data[i]&0xf0 == 0xe0:
			size = 3
		case data[i]&0xf8 == 0xf0:
			size = 4
		default:
			return false
		}
		if i+size > len(data) {
			return false
		}
		for j := 1; j < size; j++ {
			if data[i+j]&0xc0 != 0x80 {
				return false
			}
		}
		i += size
	}
	return true
}

func TestIssue44StreamBackpressureDoesNotBlockValidation(t *testing.T) {
	t.Setenv("ISSUE44_STREAM_HELPER", "chatty")
	release := make(chan struct{})
	var calls atomic.Int64
	started := make(chan struct{}, 1)
	obs := RunWithOptions(t.TempDir(), issue44Command(t, "chatty"), Options{
		Gate: "chatty",
		OnOutput: func(chunk OutputChunk) error {
			if calls.Add(1) == 1 {
				select {
				case started <- struct{}{}:
				default:
				}
				<-release
			}
			return nil
		},
	})
	close(release)
	if obs.Status != spec.StatusPass || obs.StdoutBytes != 400*(4<<10) {
		t.Fatalf("observer changed gate result: status=%s bytes=%d", obs.Status, obs.StdoutBytes)
	}
	select {
	case <-started:
	default:
		t.Fatal("observer was never called")
	}
	if obs.Live.DroppedChunks == 0 || obs.Live.DroppedBytes <= 0 {
		t.Fatalf("no backpressure accounting: %+v", obs.Live)
	}
	if obs.Live.ObserverErrors != 0 {
		t.Fatalf("valid observer counted as broken: %+v", obs.Live)
	}
}

func TestIssue44StreamObserverErrorDoesNotFailGate(t *testing.T) {
	t.Setenv("ISSUE44_STREAM_HELPER", "incremental")
	obs := RunWithOptions(t.TempDir(), issue44Command(t, "incremental"), Options{
		Gate: "broken-observer",
		OnOutput: func(chunk OutputChunk) error {
			return fmt.Errorf("observer sink unavailable")
		},
	})
	if obs.Status != spec.StatusPass || obs.Live.ObserverErrors == 0 {
		t.Fatalf("observer error changed gate: status=%s stats=%+v", obs.Status, obs.Live)
	}
	if obs.StdoutBytes == 0 || obs.StdoutSHA256 == "" {
		t.Fatalf("observation lost capture after observer error: %+v", obs)
	}
}

func TestIssue44StreamCancellationStopsChild(t *testing.T) {
	t.Setenv("ISSUE44_STREAM_HELPER", "sleep")
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(120*time.Millisecond, cancel)
	started := time.Now()
	obs := RunWithOptions(t.TempDir(), issue44Command(t, "sleep"), Options{Context: ctx, Gate: "cancel-me"})
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("cancellation took too long: %s", elapsed)
	}
	if obs.Status == spec.StatusPass {
		t.Fatal("cancelled command reported PASS")
	}
}
