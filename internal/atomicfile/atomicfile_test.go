package atomicfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tempLeftovers reports the temporary files Write may have left behind in dir.
// The implementation names them "."+base+".tmp-*", so a glob on that shape is
// the observable contract: a completed write must never leave one (spec §15).
func tempLeftovers(t *testing.T, dir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".*tmp*"))
	if err != nil {
		t.Fatalf("glob temp files: %v", err)
	}
	return matches
}

func TestWriteCreatesNewFileWithContentAndMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		perm os.FileMode
		data string
	}{
		{name: "owner only", perm: 0o600, data: `{"inbounds":[]}`},
		{name: "group readable", perm: 0o640, data: "log:\n  level: info\n"},
		{name: "world readable", perm: 0o644, data: "{}"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			// The parent directory does not exist yet: Write has to create the
			// whole chain, because a fresh install writes config/active.json
			// before the directory exists (spec §15).
			path := filepath.Join(dir, "nested", "deeper", "active.json")

			if err := Write(path, []byte(tc.data), tc.perm); err != nil {
				t.Fatalf("Write() = %v, want nil", err)
			}

			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read written file: %v", err)
			}
			if string(got) != tc.data {
				t.Errorf("content = %q, want %q", got, tc.data)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat written file: %v", err)
			}
			if perm := info.Mode().Perm(); perm != tc.perm {
				t.Errorf("mode = %o, want %o", perm, tc.perm)
			}
			if leftovers := tempLeftovers(t, filepath.Dir(path)); len(leftovers) != 0 {
				t.Errorf("temporary files left behind: %v", leftovers)
			}
		})
	}
}

func TestWriteReplacesExistingFileAndAppliesNewMode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "active.json")

	if err := Write(path, []byte("first"), 0o600); err != nil {
		t.Fatalf("first Write() = %v, want nil", err)
	}
	if err := Write(path, []byte("second"), 0o644); err != nil {
		t.Fatalf("second Write() = %v, want nil", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read replaced file: %v", err)
	}
	if string(got) != "second" {
		t.Errorf("content = %q, want %q", got, "second")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat replaced file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Errorf("mode = %o, want 644", perm)
	}
	if leftovers := tempLeftovers(t, dir); len(leftovers) != 0 {
		t.Errorf("temporary files left behind: %v", leftovers)
	}
}

// TestWriteLeavesOriginalIntactWhenDirectoryIsNotWritable drives the very first
// failure point of Write (os.CreateTemp) and asserts the pre-existing file and
// its permissions survive an interrupted write untouched.
func TestWriteLeavesOriginalIntactWhenDirectoryIsNotWritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: directory permissions do not deny writes")
	}
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "active.json")
	if err := os.WriteFile(path, []byte("original"), 0o640); err != nil {
		t.Fatalf("seed original file: %v", err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod directory read-only: %v", err)
	}
	// Restore write permission before t.TempDir's own cleanup runs.
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	err := Write(path, []byte("replacement"), 0o600)
	if err == nil {
		t.Fatal("Write() = nil, want an error for an unwritable directory")
	}

	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("original file must survive a failed write: %v", readErr)
	}
	if string(got) != "original" {
		t.Errorf("content = %q, want the untouched %q", got, "original")
	}
	info, statErr := os.Stat(path)
	if statErr != nil {
		t.Fatalf("stat original file: %v", statErr)
	}
	if perm := info.Mode().Perm(); perm != 0o640 {
		t.Errorf("mode = %o, want the preserved 640", perm)
	}
	if leftovers := tempLeftovers(t, dir); len(leftovers) != 0 {
		t.Errorf("temporary files left behind: %v", leftovers)
	}
}

// TestWriteLeavesTargetIntactWhenRenameFails drives the last failure point: the
// temporary file was written and fsynced, and only the atomic rename fails
// because the destination is a directory. The destination must be untouched and
// the temporary file must be removed.
func TestWriteLeavesTargetIntactWhenRenameFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "active.json")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("create directory at the destination: %v", err)
	}
	child := filepath.Join(target, "keepme")
	if err := os.WriteFile(child, []byte("keep"), 0o644); err != nil {
		t.Fatalf("seed directory content: %v", err)
	}

	if err := Write(target, []byte("replacement"), 0o644); err == nil {
		t.Fatal("Write() = nil, want an error when the destination cannot be replaced")
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("destination must survive a failed write: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("destination is no longer a directory: mode = %v", info.Mode())
	}
	got, err := os.ReadFile(child)
	if err != nil {
		t.Fatalf("directory content must survive a failed write: %v", err)
	}
	if string(got) != "keep" {
		t.Errorf("directory content = %q, want %q", got, "keep")
	}
	if leftovers := tempLeftovers(t, dir); len(leftovers) != 0 {
		t.Errorf("temporary files left behind: %v", leftovers)
	}
}

func TestWriteFileBehavesLikeWrite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "scratch", "status.json")

	if err := WriteFile(path, []byte(`{"running":true}`), 0o600); err != nil {
		t.Fatalf("WriteFile() = %v, want nil", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if string(got) != `{"running":true}` {
		t.Errorf("content = %q", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat written file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}
	if leftovers := tempLeftovers(t, filepath.Dir(path)); len(leftovers) != 0 {
		t.Errorf("temporary files left behind: %v", leftovers)
	}
}

func TestCopyFileCopiesContentAndMode(t *testing.T) {
	t.Parallel()

	t.Run("copies bytes and applies the requested mode", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		src := filepath.Join(dir, "src")
		dst := filepath.Join(dir, "out", "dst")
		if err := os.WriteFile(src, []byte("payload"), 0o600); err != nil {
			t.Fatalf("seed source: %v", err)
		}

		if err := CopyFile(src, dst, 0o640); err != nil {
			t.Fatalf("CopyFile() = %v, want nil", err)
		}
		got, err := os.ReadFile(dst)
		if err != nil {
			t.Fatalf("read destination: %v", err)
		}
		if string(got) != "payload" {
			t.Errorf("content = %q, want %q", got, "payload")
		}
		info, err := os.Stat(dst)
		if err != nil {
			t.Fatalf("stat destination: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o640 {
			t.Errorf("mode = %o, want 640", perm)
		}
		if leftovers := tempLeftovers(t, filepath.Dir(dst)); len(leftovers) != 0 {
			t.Errorf("temporary files left behind: %v", leftovers)
		}
	})

	t.Run("a missing source leaves the destination untouched", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		src := filepath.Join(dir, "missing")
		dst := filepath.Join(dir, "dst")
		if err := os.WriteFile(dst, []byte("original"), 0o644); err != nil {
			t.Fatalf("seed destination: %v", err)
		}

		err := CopyFile(src, dst, 0o600)
		if err == nil {
			t.Fatal("CopyFile() = nil, want an error for a missing source")
		}
		if !strings.Contains(err.Error(), "missing") {
			t.Errorf("error %q should name the missing source path", err)
		}
		got, readErr := os.ReadFile(dst)
		if readErr != nil {
			t.Fatalf("read destination: %v", readErr)
		}
		if string(got) != "original" {
			t.Errorf("content = %q, want the untouched %q", got, "original")
		}
		if leftovers := tempLeftovers(t, dir); len(leftovers) != 0 {
			t.Errorf("temporary files left behind: %v", leftovers)
		}
	})
}

// TestWriteReplacesSymlinkDestination asserts the directory entry is replaced
// rather than written through, which is what makes the swap atomic.
func TestWriteReplacesSymlinkDestination(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	real := filepath.Join(dir, "real.json")
	link := filepath.Join(dir, "link.json")
	if err := os.WriteFile(real, []byte("real"), 0o644); err != nil {
		t.Fatalf("seed real file: %v", err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := Write(link, []byte("via-link"), 0o644); err != nil {
		t.Fatalf("Write() = %v, want nil", err)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat link: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Errorf("destination is still a symlink; the rename did not replace it")
	}
	got, err := os.ReadFile(link)
	if err != nil {
		t.Fatalf("read link: %v", err)
	}
	if string(got) != "via-link" {
		t.Errorf("content = %q, want %q", got, "via-link")
	}
	original, err := os.ReadFile(real)
	if err != nil {
		t.Fatalf("read real file: %v", err)
	}
	if string(original) != "real" {
		t.Errorf("the symlink target was written through: %q", original)
	}
}
