package runtime

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/larffxx/singboxui/internal/events"
	"github.com/larffxx/singboxui/internal/logging"
)

// maxLineBytes bounds a single log line; longer output is truncated instead of
// growing the buffer without limit (spec §83: no unbounded log arrays).
const maxLineBytes = 8 * 1024

// maxBatch is the number of records that forces an immediate flush, so a chatty
// sing-box cannot delay log delivery until the next tick.
const maxBatch = 64

// LogRecord is one line of managed sing-box output.
type LogRecord struct {
	Seq     int64     `json:"seq"`
	Time    time.Time `json:"time"`
	Source  string    `json:"source"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
}

// LogQuery filters the bounded ring buffer (spec §47: search and filter are
// served from the buffer the backend already owns).
type LogQuery struct {
	// Limit is the maximum number of records to return (newest last).
	Limit int `json:"limit"`
	// AfterSeq returns only records newer than this sequence number.
	AfterSeq int64 `json:"afterSeq"`
	// Filter is a case-insensitive substring match on the message.
	Filter string `json:"filter"`
	// Level filters by level when set, e.g. "error".
	Level string `json:"level"`
}

// logBatch is the event payload: records are delivered in batches, never one
// event per line (spec §46, §47).
type logBatch struct {
	RevisionID string      `json:"revisionId"`
	Records    []LogRecord `json:"records"`
	// Dropped counts records evicted from the ring buffer since the last batch.
	Dropped int `json:"dropped"`
}

var levelRe = regexp.MustCompile(`\b(TRACE|DEBUG|INFO|WARN|WARNING|ERROR|FATAL|PANIC)\b`)

// logBuffer is the bounded, self-flushing ring buffer behind the log stream.
type logBuffer struct {
	capacity int
	period   time.Duration
	emitter  events.Emitter
	logger   *slog.Logger
	now      func() time.Time

	mu        sync.Mutex
	records   []LogRecord
	nextSeq   int64
	pending   []LogRecord
	dropped   int
	revision  string
	attachMu  sync.Mutex
	stream    *logStream
	closeOnce sync.Once
	closed    bool
}

// logStream is one attached reader (one managed process run).
type logStream struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func newLogBuffer(capacity int, period time.Duration, emitter events.Emitter, logger *slog.Logger, now func() time.Time) *logBuffer {
	return &logBuffer{
		capacity: capacity,
		period:   period,
		emitter:  emitter,
		logger:   logger,
		now:      now,
		records:  make([]LogRecord, 0, 256),
	}
}

// reset starts a new log generation for a fresh process run.
func (b *logBuffer) reset(revisionID string) {
	b.mu.Lock()
	b.records = b.records[:0]
	b.nextSeq = 0
	b.pending = nil
	b.dropped = 0
	b.revision = revisionID
	b.mu.Unlock()
}

// attach consumes the merged stdout+stderr of a process and streams it to the
// frontend until the stream ends or the context is cancelled.
func (b *logBuffer) attach(reader io.ReadCloser, parent context.Context) {
	if reader == nil {
		return
	}
	b.attachMu.Lock()
	if b.closed {
		b.attachMu.Unlock()
		_ = reader.Close()
		return
	}
	detached := make(chan struct{})
	streamCtx, cancel := context.WithCancel(parent)
	stream := &logStream{cancel: cancel, done: detached}
	b.stream = stream
	b.attachMu.Unlock()

	go b.readLoop(streamCtx, reader, detached)
	go b.flushLoop(streamCtx, detached)
}

// detach stops streaming and flushes whatever is left. Safe to call twice.
func (b *logBuffer) detach() {
	b.attachMu.Lock()
	stream := b.stream
	b.stream = nil
	b.attachMu.Unlock()
	if stream == nil {
		return
	}
	stream.cancel()
	select {
	case <-stream.done:
	case <-time.After(2 * time.Second):
		b.logger.Warn("log reader did not stop in time")
	}
	b.flush()
}

// close releases the buffer for good.
func (b *logBuffer) close() {
	b.closeOnce.Do(func() {
		b.detach()
		b.attachMu.Lock()
		b.closed = true
		b.attachMu.Unlock()
		b.flush()
	})
}

func (b *logBuffer) readLoop(ctx context.Context, reader io.ReadCloser, done chan<- struct{}) {
	defer close(done)
	defer func() {
		if err := reader.Close(); err != nil && !errors.Is(err, io.EOF) {
			b.logger.Debug("closing the log stream failed", "error", err)
		}
	}()
	buf := bufio.NewReaderSize(reader, 64*1024)
	var carry []byte
	for {
		if ctx.Err() != nil {
			return
		}
		line, err := buf.ReadBytes('\n')
		if len(line) > 0 {
			piece := append(carry, line...)
			if len(piece) > maxLineBytes {
				piece = append(piece[:maxLineBytes], []byte(" …[truncated]")...)
				carry = carry[:0]
			} else if piece[len(piece)-1] != '\n' {
				// Partial line: keep it until the writer finishes it.
				carry = append(carry[:0], piece...)
				if err == nil {
					continue
				}
			} else {
				carry = carry[:0]
			}
			b.append("sing-box", string(piece))
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && ctx.Err() == nil {
				b.logger.Debug("reading sing-box output stopped", "error", err)
			}
			return
		}
	}
}

func (b *logBuffer) flushLoop(ctx context.Context, done chan<- struct{}) {
	ticker := time.NewTicker(b.period)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.flush()
		}
	}
}

// append adds one line, redacted, evicting the oldest record when full.
func (b *logBuffer) append(source, raw string) {
	message := strings.TrimRight(raw, "\r\n")
	if message == "" {
		return
	}
	message = logging.RedactString(message)
	record := LogRecord{
		Time:    b.now(),
		Source:  source,
		Level:   detectLevel(message),
		Message: message,
	}

	b.mu.Lock()
	b.nextSeq++
	record.Seq = b.nextSeq
	if len(b.records) >= b.capacity {
		drop := len(b.records) - b.capacity + 1
		b.records = append(b.records[:0], b.records[drop:]...)
		b.dropped += drop
	}
	b.records = append(b.records, record)
	b.pending = append(b.pending, record)
	ready := len(b.pending) >= maxBatch
	b.mu.Unlock()

	if ready {
		b.flush()
	}
}

// flush publishes the pending records as one event.
func (b *logBuffer) flush() {
	b.mu.Lock()
	if len(b.pending) == 0 {
		// Release before returning: a flush with nothing to send is the common
		// case on a quiet process, and leaving the mutex held here deadlocks
		// every later append, flush and detach.
		b.mu.Unlock()
		return
	}
	batch := logBatch{
		RevisionID: b.revision,
		Records:    append([]LogRecord(nil), b.pending...),
		Dropped:    b.dropped,
	}
	b.pending = b.pending[:0]
	b.dropped = 0
	b.mu.Unlock()
	b.emitter.Emit(events.RuntimeLog, batch)
}

// tail returns the last n messages for error details.
func (b *logBuffer) tail(n int) []string {
	if n <= 0 {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	start := len(b.records) - n
	if start < 0 {
		start = 0
	}
	out := make([]string, 0, len(b.records)-start)
	for _, record := range b.records[start:] {
		out = append(out, record.Message)
	}
	return out
}

// query serves the initial tail, search and level filtering.
func (b *logBuffer) query(q LogQuery) []LogRecord {
	if q.Limit <= 0 || q.Limit > b.capacity {
		q.Limit = 500
	}
	filter := strings.ToLower(strings.TrimSpace(q.Filter))
	level := strings.ToLower(strings.TrimSpace(q.Level))

	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]LogRecord, 0, q.Limit)
	for _, record := range b.records {
		if q.AfterSeq > 0 && record.Seq <= q.AfterSeq {
			continue
		}
		if level != "" && record.Level != level {
			continue
		}
		if filter != "" && !strings.Contains(strings.ToLower(record.Message), filter) {
			continue
		}
		out = append(out, record)
	}
	if len(out) > q.Limit {
		out = out[len(out)-q.Limit:]
	}
	return out
}

// clear empties the backend buffer (spec §47: clear UI buffer).
func (b *logBuffer) clear() {
	b.mu.Lock()
	b.records = b.records[:0]
	b.pending = nil
	b.nextSeq = 0
	b.dropped = 0
	b.mu.Unlock()
}

func detectLevel(line string) string {
	match := levelRe.FindString(strings.ToUpper(line))
	switch match {
	case "TRACE", "DEBUG":
		return "debug"
	case "INFO":
		return "info"
	case "WARN", "WARNING":
		return "warn"
	case "ERROR", "FATAL", "PANIC":
		return "error"
	case "":
		return "info"
	default:
		return strings.ToLower(match)
	}
}
