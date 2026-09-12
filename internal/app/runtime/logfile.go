package runtime

import (
	"errors"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

// tailReadLimit bounds how much of a launch log is read back when a startup
// failure is reported. sing-box writes its decoder error first, and the file can
// also hold hours of traffic-driven output after it.
const tailReadLimit = 32 * 1024

// tailLineLimit bounds a single line; a malformed configuration can make sing-box
// echo a whole file back on one line.
const tailLineLimit = 500

// tailFileLines returns the last n non-empty lines of path, oldest first.
//
// It never fails: a missing or unreadable log has no tail to contribute, which is
// the same situation as a process that produced no output at all.
func tailFileLines(path string, n int) []string {
	if path == "" || n <= 0 {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil || info.IsDir() || info.Size() == 0 {
		return nil
	}
	offset := int64(0)
	if info.Size() > tailReadLimit {
		offset = info.Size() - tailReadLimit
	}
	buf := make([]byte, info.Size()-offset)
	if _, err := file.ReadAt(buf, offset); err != nil && !errors.Is(err, io.EOF) {
		return nil
	}
	lines := strings.Split(strings.TrimRight(string(buf), "\n"), "\n")
	if offset > 0 && len(lines) > 0 {
		// The first line is a fragment of a line that started before the window.
		lines = lines[1:]
	}

	out := make([]string, 0, n)
	for i := len(lines) - 1; i >= 0 && len(out) < n; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		out = append(out, truncateLine(line))
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func truncateLine(line string) string {
	if len(line) <= tailLineLimit {
		return line
	}
	cut := tailLineLimit
	for cut > 0 && !utf8.RuneStart(line[cut]) {
		cut--
	}
	return line[:cut] + "…"
}
