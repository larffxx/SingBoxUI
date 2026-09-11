package childrun

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"sync"
	"time"
)

// defaultPoll is how often a tail reader re-checks a file for appended data.
const defaultPoll = 25 * time.Millisecond

// NewTailReader returns a reader over a file that another process keeps
// appending to: merged stdout+stderr of a managed sing-box process, or the log
// file written by the privileged helper (internal-contracts.md §3).
//
// Read blocks until data is available. It reports io.EOF once the file has been
// fully read and exited reports that the writer has terminated. The reader is
// valid until the process is waited for and must be closed by the caller (they
// own the goroutine; Close cancels it, spec §62).
func NewTailReader(ctx context.Context, path string, exited func() bool) *TailReader {
	readerCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	return &TailReader{
		ctx:    readerCtx,
		cancel: cancel,
		path:   path,
		exited: exited,
		poll:   defaultPoll,
	}
}

// TailReader follows a growing file.
type TailReader struct {
	ctx    context.Context
	cancel context.CancelFunc
	path   string
	exited func() bool
	poll   time.Duration

	mu     sync.Mutex
	file   *os.File
	closed bool
}

// Read returns data as soon as it has been appended, and io.EOF once the writer
// has terminated and everything has been read.
func (t *TailReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return 0, io.EOF
	}
	for {
		if t.ctx.Err() != nil {
			// Close ran while this call was waiting; nothing may block on a
			// reader the caller has given up on.
			return 0, io.EOF
		}
		if t.file == nil {
			file, err := os.Open(t.path)
			if err != nil {
				if !errors.Is(err, fs.ErrNotExist) {
					return 0, err
				}
				// The writer has not created the file yet; wait unless it is gone.
				if t.done() {
					return 0, io.EOF
				}
				if err := t.sleep(); err != nil {
					return 0, io.EOF
				}
				continue
			}
			t.file = file
		}
		n, err := t.file.Read(p)
		if n > 0 {
			return n, nil
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		}
		if t.done() {
			return 0, io.EOF
		}
		if err := t.sleep(); err != nil {
			return 0, io.EOF
		}
	}
}

// Close releases the reader and cancels its polling. Read returns io.EOF, also
// when it is blocked in another goroutine: the cancellation happens before the
// lock is taken, so a reader that holds the lock can never keep Close waiting.
func (t *TailReader) Close() error {
	t.cancel()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	if t.file != nil {
		err := t.file.Close()
		t.file = nil
		return err
	}
	return nil
}

// done reports whether the writer has terminated.
func (t *TailReader) done() bool { return t.exited != nil && t.exited() }

// sleep waits for the poll interval; it returns an error when the caller closed
// the reader.
func (t *TailReader) sleep() error {
	timer := time.NewTimer(t.poll)
	defer timer.Stop()
	select {
	case <-t.ctx.Done():
		return t.ctx.Err()
	case <-timer.C:
		return nil
	}
}
