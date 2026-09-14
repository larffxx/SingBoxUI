package singbox

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/domain/settings"
	"github.com/larffxx/singboxui/internal/logging"
	"github.com/larffxx/singboxui/internal/platform"
	"github.com/larffxx/singboxui/internal/platform/console"
)

// probeTimeout bounds a version probe. It is a package constant rather than a
// parameter because every caller wants the same answer: a binary that cannot
// report its version within this long is unusable, and the runtime supervisor
// must not be able to block start-up on it.
const probeTimeout = 15 * time.Second

// ManagedBinaryRef is the part of the managed-binary record this adapter needs
// to resolve an executable.
//
// It is deliberately a two-field view instead of the storage type: the adapter
// never needs the recorded hash, asset name or install time, and depending on
// the storage package here would point a port package at persistence.
type ManagedBinaryRef struct {
	// Version is the installed release version, e.g. "1.14.0".
	Version string
	// Path is the recorded install path of the executable, if known.
	Path string
}

// Probe runs `<path> version` and parses the reported version.
//
// The file name is never trusted and the command is always built as an argument
// array, so a path containing spaces or shell metacharacters cannot change what
// is executed (spec §24). It reports BINARY_NOT_FOUND when there is nothing
// runnable at the path and BINARY_SOURCE_INVALID when something ran but did not
// look like sing-box, which is what lets the settings screen tell the user which
// of the two went wrong.
func Probe(ctx context.Context, path string) (Version, error) {
	const op = "singbox.Probe"
	if strings.TrimSpace(path) == "" {
		return Version{}, apperr.New(apperr.CodeBinaryNotFound, op, "no sing-box executable path was given")
	}
	if !IsExecutable(path) {
		return Version{}, apperr.Newf(apperr.CodeBinaryNotFound, op,
			"no executable file at %s", path)
	}

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, path, "version")
	// The probe must not flash a console window on Windows; it runs on every start and on
	// every binary change.
	console.Windowless(cmd)
	output := &boundedBuffer{}
	cmd.Stdout, cmd.Stderr = output, output
	cmd.Stdin = nil

	err := cmd.Run()
	text := redactOutput(output.String())
	switch {
	case ctx.Err() != nil:
		return Version{}, apperr.Wrap(apperr.CodeBinarySourceInvalid, op,
			"version probe did not finish in time", ctx.Err())
	case err == nil:
		// Expected path: the binary exited 0 and printed its banner.
	case isExitError(err):
		return Version{}, apperr.WithDetails(apperr.Wrap(apperr.CodeBinarySourceInvalid, op,
			"the executable did not accept `version`", err), text)
	default:
		return Version{}, apperr.Wrap(apperr.CodeBinaryNotFound, op,
			"cannot run "+path, err)
	}

	version, parseErr := ParseVersion(text)
	if parseErr != nil {
		return Version{}, apperr.Wrap(apperr.CodeBinarySourceInvalid, op,
			"the executable did not report a sing-box version", parseErr)
	}
	return version, nil
}

// IsExecutable reports whether path is a regular file this process may execute.
//
// It exists so the "is the configured binary usable" question is answered the
// same way everywhere; on Windows there is no execute bit, so the regular-file
// check plus Probe is as far as a path-only test can honestly go.
func IsExecutable(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode().Perm()&0o111 != 0
}

// Locate resolves the executable the settings select.
//
// It never falls back to PATH (spec §20): a bare "sing-box" would otherwise
// resolve to whatever the user's shell happens to find, which is exactly the
// ambiguity the managed/custom split exists to remove. A custom path therefore
// has to be absolute, and a managed binary has to be recorded or present under
// the per-version bin directory.
func Locate(managed ManagedBinaryRef, s settings.Settings, p platform.Paths) (string, error) {
	const op = "singbox.Locate"
	switch s.BinarySource {
	case settings.BinaryManaged:
		candidates := make([]string, 0, 2)
		if path := strings.TrimSpace(managed.Path); path != "" {
			candidates = append(candidates, path)
		}
		if version := strings.TrimSpace(managed.Version); version != "" {
			// The recorded path may have been removed; the layout under BinDir is
			// the fallback source of truth.
			derived := p.ManagedBinaryPath(version, ExecutableName(runtime.GOOS))
			if len(candidates) == 0 || candidates[0] != derived {
				candidates = append(candidates, derived)
			}
		}
		for _, candidate := range candidates {
			if IsExecutable(candidate) {
				return candidate, nil
			}
		}
		if len(candidates) == 0 {
			return "", apperr.New(apperr.CodeBinaryNotFound, op,
				"no managed sing-box binary is installed yet")
		}
		return "", apperr.Newf(apperr.CodeBinaryNotFound, op,
			"the managed sing-box binary is missing: %s", strings.Join(candidates, ", "))

	case settings.BinaryCustom:
		path := strings.TrimSpace(s.CustomBinaryPath)
		if path == "" {
			return "", apperr.New(apperr.CodeBinarySourceInvalid, op,
				"no custom sing-box executable is configured")
		}
		if !filepath.IsAbs(path) {
			return "", apperr.Newf(apperr.CodeBinarySourceInvalid, op,
				"the custom sing-box path must be absolute, got %q", path)
		}
		path = filepath.Clean(path)
		if !IsExecutable(path) {
			return "", apperr.Newf(apperr.CodeBinaryNotFound, op,
				"the custom sing-box executable is missing or not executable: %s", path)
		}
		return path, nil

	default:
		return "", apperr.Newf(apperr.CodeBinarySourceInvalid, op,
			"unknown binary source %q", string(s.BinarySource))
	}
}

// isExitError reports whether the process ran and exited non-zero, as opposed
// to never having started; the two map to different error codes.
func isExitError(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr)
}

// maxProcessOutput bounds how much of a child's output is kept. A sing-box
// `check` on a large configuration can print a lot, and the UI only ever shows
// a summary, so the adapter keeps the head and marks the rest as truncated
// rather than growing an unbounded buffer.
const maxProcessOutput = 64 << 10

// boundedBuffer collects process output up to a fixed limit.
//
// One instance is shared by stdout and stderr: os/exec deduplicates identical
// writers into a single copy goroutine, and the mutex keeps that safe even if
// that ever changes.
type boundedBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	truncated bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	remaining := maxProcessOutput - b.buf.Len()
	switch {
	case remaining <= 0:
		b.truncated = true
	case len(p) > remaining:
		b.buf.Write(p[:remaining])
		b.truncated = true
	default:
		b.buf.Write(p)
	}
	// Report a full write: the child must not see a short write because the
	// adapter stopped storing.
	return len(p), nil
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.buf.String()
	if b.truncated {
		out += "\n[output truncated]"
	}
	return out
}

// redactOutput removes credentials and share links from process output before
// it can reach a log line, an event or the UI (spec §33).
//
// Redaction happens line by line so the structure of the output is preserved
// for error extraction, and only the stored head is redacted because the
// remainder was never kept.
func redactOutput(output string) string {
	if output == "" {
		return ""
	}
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		lines[i] = logging.RedactString(strings.TrimRight(line, "\r"))
	}
	return strings.Join(lines, "\n")
}
