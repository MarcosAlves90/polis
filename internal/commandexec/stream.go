package commandexec

import (
	"bytes"
	"context"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

const (
	StreamStdout      = "stdout"
	StreamStderr      = "stderr"
	MaxLiveChunkBytes = 16 << 10

	liveQueueSize = 64
	liveDrainWait = 300 * time.Millisecond
)

// OutputChunk is a presentation-only view of bytes already being captured for
// the immutable Observation. It never replaces digests, byte counts or previews.
type OutputChunk struct {
	Gate          string `json:"id"`
	Stream        string `json:"stream"`
	Data          []byte `json:"data"`
	Sequence      uint64 `json:"sequence"`
	Bytes         int    `json:"bytes"`
	Truncated     bool   `json:"truncated"`
	DroppedChunks uint64 `json:"dropped_chunks"`
	DroppedBytes  int64  `json:"dropped_bytes"`
}

// OutputObserver receives chunks while the child is still running. A returned
// error is accounted but never changes the observed gate outcome.
type OutputObserver func(OutputChunk) error

// StreamStats accounts for the optional live path. It is excluded from all
// persisted evidence and reports.
type StreamStats struct {
	Chunks         uint64 `json:"chunks"`
	DroppedChunks  uint64 `json:"dropped_chunks"`
	DroppedBytes   int64  `json:"dropped_bytes"`
	ObserverErrors uint64 `json:"observer_errors"`
}

// Options extends command execution with an optional context and observer.
// Leaving OnOutput nil preserves the original synchronous capture path.
type Options struct {
	Context  context.Context
	Gate     string
	OnOutput OutputObserver
}

type liveDispatcher struct {
	ch       chan OutputChunk
	observer OutputObserver
	done     chan struct{}
	errors   atomic.Uint64
	mu       sync.Mutex
	closed   bool
}

func newLiveDispatcher(observer OutputObserver) *liveDispatcher {
	if observer == nil {
		return nil
	}
	d := &liveDispatcher{ch: make(chan OutputChunk, liveQueueSize), observer: observer, done: make(chan struct{})}
	go d.drain()
	return d
}

func (d *liveDispatcher) drain() {
	defer close(d.done)
	for chunk := range d.ch {
		if err := d.observer(chunk); err != nil {
			d.errors.Add(1)
		}
	}
}

// offer never blocks the child's copy goroutine. A full queue is accounted as
// dropped chunks instead of applying backpressure to the validated command.
func (d *liveDispatcher) offer(chunk OutputChunk) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return false
	}
	select {
	case d.ch <- chunk:
		return true
	default:
		return false
	}
}

func (d *liveDispatcher) close() {
	if d == nil {
		return
	}
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.closed = true
	close(d.ch)
	d.mu.Unlock()
	select {
	case <-d.done:
	case <-time.After(liveDrainWait):
	}
}

type liveWriter struct {
	digest     *boundedDigestWriter
	dispatcher *liveDispatcher
	gate       string
	stream     string
	sequence   uint64
	dropped    uint64
	droppedB   int64
}

func (w *liveWriter) Write(p []byte) (int, error) {
	n, err := w.digest.Write(p)
	if w.dispatcher == nil || len(p) == 0 {
		return n, err
	}
	for len(p) > 0 {
		size := len(p)
		if size > MaxLiveChunkBytes {
			size = MaxLiveChunkBytes
		}
		part := p[:size]
		w.sequence++
		chunk := OutputChunk{
			Gate:          w.gate,
			Stream:        w.stream,
			Data:          sanitizeLiveBytes(part),
			Sequence:      w.sequence,
			Bytes:         len(part),
			Truncated:     size < len(p),
			DroppedChunks: w.dropped,
			DroppedBytes:  w.droppedB,
		}
		if !w.dispatcher.offer(chunk) {
			w.dropped++
			w.droppedB += int64(size)
		}
		p = p[size:]
	}
	return n, err
}

func (w *liveWriter) stats() StreamStats {
	if w == nil {
		return StreamStats{}
	}
	return StreamStats{Chunks: w.sequence, DroppedChunks: w.dropped, DroppedBytes: w.droppedB}
}

func combineStreamStats(stdout, stderr *liveWriter, dispatcher *liveDispatcher) StreamStats {
	stats := stdout.stats()
	other := stderr.stats()
	stats.Chunks += other.Chunks
	stats.DroppedChunks += other.DroppedChunks
	stats.DroppedBytes += other.DroppedBytes
	if dispatcher != nil {
		stats.ObserverErrors = dispatcher.errors.Load()
	}
	return stats
}

// sanitizeLiveBytes removes terminal control sequences and replaces invalid
// UTF-8/C0 controls so text and JSON consumers cannot be corrupted by a child.
func sanitizeLiveBytes(input []byte) []byte {
	var out bytes.Buffer
	out.Grow(len(input))
	for i := 0; i < len(input); {
		if input[i] == 0x1b {
			i = skipEscape(input, i)
			continue
		}
		r, size := utf8.DecodeRune(input[i:])
		if r == utf8.RuneError && size == 1 {
			out.WriteRune(utf8.RuneError)
			i++
			continue
		}
		if r < 0x20 && r != '\n' && r != '\t' || r == 0x7f {
			out.WriteByte(' ')
			i++
			continue
		}
		out.WriteRune(r)
		i += size
	}
	return out.Bytes()
}

func skipEscape(input []byte, at int) int {
	if at+1 >= len(input) {
		return at + 1
	}
	switch input[at+1] {
	case '[':
		for i := at + 2; i < len(input); i++ {
			if input[i] >= 0x40 && input[i] <= 0x7e {
				return i + 1
			}
		}
	case ']':
		for i := at + 2; i < len(input); i++ {
			if input[i] == 0x07 {
				return i + 1
			}
			if input[i] == 0x1b && i+1 < len(input) && input[i+1] == '\\' {
				return i + 2
			}
		}
	}
	if at+2 <= len(input) {
		return at + 2
	}
	return at + 1
}
