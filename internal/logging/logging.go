// Package logging: slog setup with a size-bounded rotating file writer.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// Options configures Setup.
type Options struct {
	Level string // debug | info | warn | error
	Path  string // empty = stderr only
}

// Setup builds the application logger. The returned io.Closer flushes and closes
// the log file; callers must close it during shutdown.
func Setup(opts Options) (*slog.Logger, io.Closer, error) {
	level := parseLevel(opts.Level)
	var writers []io.Writer
	closers := []io.Closer{}
	if opts.Path != "" {
		if err := os.MkdirAll(filepath.Dir(opts.Path), 0o755); err != nil {
			return nil, nil, fmt.Errorf("create log dir: %w", err)
		}
		rotating, err := newRotatingWriter(opts.Path, 5<<20, 3)
		if err != nil {
			return nil, nil, err
		}
		writers = append(writers, rotating)
		closers = append(closers, rotating)
	}
	if len(writers) == 0 {
		writers = append(writers, os.Stderr)
	}
	handler := slog.NewJSONHandler(io.MultiWriter(writers...), &slog.HandlerOptions{Level: level})
	return slog.New(handler), multiCloser(closers), nil
}

func parseLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

type multiCloser []io.Closer

func (m multiCloser) Close() error {
	var err error
	for _, c := range m {
		if e := c.Close(); e != nil && err == nil {
			err = e
		}
	}
	return err
}

// SetContext stores a logger on a context so lower layers can log without a
// package-level logger.
type ctxKey struct{}

// WithLogger returns a context carrying the logger.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, logger)
}

// FromContext returns the logger stored on the context, or the default logger.
func FromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok && logger != nil {
		return logger
	}
	return slog.Default()
}

// rotatingWriter is a minimal size-bounded rotating file writer.
type rotatingWriter struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	keep     int
	file     *os.File
	size     int64
}

func newRotatingWriter(path string, maxBytes int64, keep int) (*rotatingWriter, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("stat log file: %w", err)
	}
	return &rotatingWriter{path: path, maxBytes: maxBytes, keep: keep, file: file, size: info.Size()}, nil
}

func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.size+int64(len(p)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rotatingWriter) rotate() error {
	if err := w.file.Close(); err != nil {
		return err
	}
	for i := w.keep - 1; i >= 1; i-- {
		from := fmt.Sprintf("%s.%d", w.path, i)
		to := fmt.Sprintf("%s.%d", w.path, i+1)
		if _, err := os.Stat(from); err == nil {
			_ = os.Rename(from, to)
		}
	}
	if err := os.Rename(w.path, w.path+".1"); err != nil && !os.IsNotExist(err) {
		return err
	}
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	w.file = file
	w.size = 0
	return nil
}

func (w *rotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Close()
}
