package singbox

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/larffxx/singboxui/internal/domain/apperr"
)

// archiveItem describes one member of a test archive.
type archiveItem struct {
	name     string
	content  string
	mode     int64
	typeflag byte
	linkname string
	// size overrides the length written to the header, which is how an entry
	// that exceeds the extraction limit is built without writing 512 MiB.
	size int64
}

// writeTarGz builds a gzip-compressed tar, the shape upstream publishes for
// macOS.
func writeTarGz(t *testing.T, path string, items []archiveItem) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("cannot create %s: %v", path, err)
	}
	gz := gzip.NewWriter(file)
	writer := tar.NewWriter(gz)
	for _, item := range items {
		mode := item.mode
		if mode == 0 {
			mode = 0o644
		}
		header := &tar.Header{
			Name:     item.name,
			Mode:     mode,
			Typeflag: item.typeflag,
			Linkname: item.linkname,
			Size:     int64(len(item.content)),
		}
		if header.Typeflag == 0 {
			header.Typeflag = tar.TypeReg
		}
		if item.size != 0 {
			header.Size = item.size
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatalf("cannot write the tar header for %s: %v", item.name, err)
		}
		if item.size == 0 {
			if _, err := writer.Write([]byte(item.content)); err != nil {
				t.Fatalf("cannot write %s: %v", item.name, err)
			}
		}
	}
	// An oversized entry declares more bytes than it carries, so the writer
	// reports the missing data; the header block is already flushed by then.
	_ = writer.Close()
	if err := gz.Close(); err != nil {
		t.Fatalf("cannot finish %s: %v", path, err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("cannot close %s: %v", path, err)
	}
}

// writeZip builds a zip, the shape upstream publishes for Windows.
func writeZip(t *testing.T, path string, items []archiveItem) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("cannot create %s: %v", path, err)
	}
	writer := zip.NewWriter(file)
	for _, item := range items {
		header := &zip.FileHeader{Name: item.name, Method: zip.Deflate}
		if item.mode != 0 {
			header.SetMode(fs.FileMode(item.mode))
		}
		handle, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatalf("cannot create the zip member %s: %v", item.name, err)
		}
		if _, err := handle.Write([]byte(item.content)); err != nil {
			t.Fatalf("cannot write %s: %v", item.name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("cannot finish %s: %v", path, err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("cannot close %s: %v", path, err)
	}
}

func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("cannot read %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// darwinArchive is the real macOS tarball layout: a versioned directory with
// LICENSE, README and the executable.
func darwinArchive(executable string, mode int64) []archiveItem {
	return []archiveItem{
		{name: "sing-box-1.14.0-darwin-arm64/", typeflag: tar.TypeDir},
		{name: "sing-box-1.14.0-darwin-arm64/LICENSE", content: "license text\n"},
		{name: "sing-box-1.14.0-darwin-arm64/README.md", content: "readme\n"},
		{name: "sing-box-1.14.0-darwin-arm64/" + executable, content: "#!/bin/sh\n# sing-box\n", mode: mode},
	}
}

func TestExtractBinary(t *testing.T) {
	tests := []struct {
		name          string
		archiveName   string
		build         func(t *testing.T, path string)
		executable    string
		wantCode      apperr.Code
		wantErrIn     string
		wantExtracted bool
		wantContents  string
	}{
		{
			name:        "macOS tarball",
			archiveName: "sing-box-1.14.0-darwin-arm64.tar.gz",
			build: func(t *testing.T, path string) {
				writeTarGz(t, path, darwinArchive("sing-box", 0o755))
			},
			executable:    "sing-box",
			wantExtracted: true,
			wantContents:  "#!/bin/sh\n# sing-box\n",
		},
		{
			// The archive's own mode is not trusted: an installed executable has
			// to be executable even if the tarball says otherwise.
			name:        "macOS tarball without the executable bit",
			archiveName: "sing-box-1.14.0-darwin-arm64.tar.gz",
			build: func(t *testing.T, path string) {
				writeTarGz(t, path, darwinArchive("sing-box", 0o644))
			},
			executable:    "sing-box",
			wantExtracted: true,
			wantContents:  "#!/bin/sh\n# sing-box\n",
		},
		{
			name:        "flat tarball without a directory prefix",
			archiveName: "sing-box-release.tar.gz",
			build: func(t *testing.T, path string) {
				writeTarGz(t, path, []archiveItem{{name: "sing-box", content: "binary", mode: 0o755}})
			},
			executable:    "sing-box",
			wantExtracted: true,
			wantContents:  "binary",
		},
		{
			// A download stored under a temporary name has no extension, so the
			// format must be detected from the content.
			name:        "archive without a useful extension",
			archiveName: "download.part",
			build: func(t *testing.T, path string) {
				writeTarGz(t, path, []archiveItem{{name: "sing-box", content: "binary", mode: 0o755}})
			},
			executable:    "sing-box",
			wantExtracted: true,
			wantContents:  "binary",
		},
		{
			name:        "windows zip",
			archiveName: "sing-box-1.14.0-windows-amd64.zip",
			build: func(t *testing.T, path string) {
				writeZip(t, path, []archiveItem{
					{name: "sing-box-1.14.0-windows-amd64/", mode: 0o755},
					{name: "sing-box-1.14.0-windows-amd64/LICENSE.txt", content: "license\n"},
					{name: "sing-box-1.14.0-windows-amd64/libcronet.dll", content: "dll bytes"},
					{name: "sing-box-1.14.0-windows-amd64/sing-box.exe", content: "MZ fake", mode: 0o755},
				})
			},
			executable:    "sing-box.exe",
			wantExtracted: true,
			wantContents:  "MZ fake",
		},
		{
			// The Windows toolchain writes zips without Unix mode bits.
			name:        "windows zip without mode bits",
			archiveName: "sing-box-1.14.0-windows-amd64.zip",
			build: func(t *testing.T, path string) {
				writeZip(t, path, []archiveItem{
					{name: "sing-box-1.14.0-windows-amd64/LICENSE.txt", content: "license\n"},
					{name: "sing-box-1.14.0-windows-amd64/sing-box.exe", content: "MZ fake"},
				})
			},
			executable:    "sing-box.exe",
			wantExtracted: true,
			wantContents:  "MZ fake",
		},
		{
			name:        "archive without the executable",
			archiveName: "sing-box-1.14.0-darwin-arm64.tar.gz",
			build: func(t *testing.T, path string) {
				writeTarGz(t, path, []archiveItem{
					{name: "sing-box-1.14.0-darwin-arm64/LICENSE", content: "license\n"},
				})
			},
			executable: "sing-box",
			wantCode:   apperr.CodeBinaryInstallFailed,
			wantErrIn:  "does not contain",
		},
		{
			name:        "two executables",
			archiveName: "sing-box-1.14.0-darwin-arm64.tar.gz",
			build: func(t *testing.T, path string) {
				writeTarGz(t, path, []archiveItem{
					{name: "sing-box", content: "first", mode: 0o755},
					{name: "nested/sing-box", content: "second", mode: 0o755},
				})
			},
			executable: "sing-box",
			wantCode:   apperr.CodeBinaryInstallFailed,
			wantErrIn:  "exactly one",
		},
		{
			name:        "path traversal",
			archiveName: "sing-box-1.14.0-darwin-arm64.tar.gz",
			build: func(t *testing.T, path string) {
				writeTarGz(t, path, []archiveItem{{name: "../sing-box", content: "evil", mode: 0o755}})
			},
			executable: "sing-box",
			wantCode:   apperr.CodeBinaryInstallFailed,
			wantErrIn:  "escapes",
		},
		{
			name:        "absolute path",
			archiveName: "sing-box-1.14.0-darwin-arm64.tar.gz",
			build: func(t *testing.T, path string) {
				writeTarGz(t, path, []archiveItem{{name: "/tmp/sing-box", content: "evil", mode: 0o755}})
			},
			executable: "sing-box",
			wantCode:   apperr.CodeBinaryInstallFailed,
			wantErrIn:  "absolute path",
		},
		{
			name:        "windows drive-qualified name",
			archiveName: "sing-box-1.14.0-windows-amd64.zip",
			build: func(t *testing.T, path string) {
				writeZip(t, path, []archiveItem{{name: "C:/sing-box.exe", content: "evil"}})
			},
			executable: "sing-box.exe",
			wantCode:   apperr.CodeBinaryInstallFailed,
			wantErrIn:  "absolute path",
		},
		{
			name:        "backslash separator",
			archiveName: "sing-box-1.14.0-windows-amd64.zip",
			build: func(t *testing.T, path string) {
				writeZip(t, path, []archiveItem{{name: `..\sing-box.exe`, content: "evil"}})
			},
			executable: "sing-box.exe",
			wantCode:   apperr.CodeBinaryInstallFailed,
			wantErrIn:  "path separator",
		},
		{
			name:        "traversal hidden behind a directory prefix",
			archiveName: "sing-box-1.14.0-darwin-arm64.tar.gz",
			build: func(t *testing.T, path string) {
				writeTarGz(t, path, []archiveItem{{name: "dir/../../sing-box", content: "evil", mode: 0o755}})
			},
			executable: "sing-box",
			wantCode:   apperr.CodeBinaryInstallFailed,
			wantErrIn:  "escapes",
		},
		{
			name:        "not normalised",
			archiveName: "sing-box-1.14.0-darwin-arm64.tar.gz",
			build: func(t *testing.T, path string) {
				writeTarGz(t, path, []archiveItem{{name: "dir/./sing-box", content: "evil", mode: 0o755}})
			},
			executable: "sing-box",
			wantCode:   apperr.CodeBinaryInstallFailed,
			wantErrIn:  "normalised",
		},
		{
			name:        "tar symlink",
			archiveName: "sing-box-1.14.0-darwin-arm64.tar.gz",
			build: func(t *testing.T, path string) {
				writeTarGz(t, path, []archiveItem{
					{name: "sing-box", typeflag: tar.TypeSymlink, linkname: "/etc/passwd", mode: 0o777},
				})
			},
			executable: "sing-box",
			wantCode:   apperr.CodeBinaryInstallFailed,
			wantErrIn:  "not a regular file",
		},
		{
			name:        "zip symlink",
			archiveName: "sing-box-1.14.0-darwin-arm64.zip",
			build: func(t *testing.T, path string) {
				writeZip(t, path, []archiveItem{
					{name: "sing-box", content: "/etc/passwd", mode: int64(os.ModeSymlink | 0o777)},
				})
			},
			executable: "sing-box",
			wantCode:   apperr.CodeBinaryInstallFailed,
			wantErrIn:  "symbolic link",
		},
		{
			name:        "tar fifo",
			archiveName: "sing-box-1.14.0-darwin-arm64.tar.gz",
			build: func(t *testing.T, path string) {
				writeTarGz(t, path, []archiveItem{
					{name: "sing-box", typeflag: tar.TypeFifo, mode: 0o644},
				})
			},
			executable: "sing-box",
			wantCode:   apperr.CodeBinaryInstallFailed,
			wantErrIn:  "not a regular file",
		},
		{
			name:        "entry above the extraction limit",
			archiveName: "sing-box-1.14.0-darwin-arm64.tar.gz",
			build: func(t *testing.T, path string) {
				writeTarGz(t, path, []archiveItem{
					{name: "sing-box", mode: 0o755, size: maxExtractedEntry + 1},
				})
			},
			executable: "sing-box",
			wantCode:   apperr.CodeBinaryInstallFailed,
			wantErrIn:  "extraction limit",
		},
		{
			name:        "invalid gzip header",
			archiveName: "sing-box-1.14.0-darwin-arm64.tar.gz",
			build: func(t *testing.T, path string) {
				writeFile(t, path, "\x1f\x8b\xff\xff not really gzip")
			},
			executable: "sing-box",
			wantCode:   apperr.CodeBinaryInstallFailed,
			wantErrIn:  "decompress",
		},
		{
			name:        "corrupt gzip stream",
			archiveName: "sing-box-1.14.0-darwin-arm64.tar.gz",
			build: func(t *testing.T, path string) {
				writeFile(t, path, "\x1f\x8b\x08\x00 not really gzip")
			},
			executable: "sing-box",
			wantCode:   apperr.CodeBinaryInstallFailed,
			wantErrIn:  "corrupt",
		},
		{
			name:        "neither tar.gz nor zip",
			archiveName: "notes.txt",
			build: func(t *testing.T, path string) {
				writeFile(t, path, "just some text")
			},
			executable: "sing-box",
			wantCode:   apperr.CodeBinaryInstallFailed,
			wantErrIn:  "neither",
		},
		{
			name:        "empty archive",
			archiveName: "sing-box.tar.gz",
			build: func(t *testing.T, path string) {
				writeTarGz(t, path, nil)
			},
			executable: "sing-box",
			wantCode:   apperr.CodeBinaryInstallFailed,
			wantErrIn:  "does not contain",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			archivePath := filepath.Join(root, test.archiveName)
			test.build(t, archivePath)
			destDir := filepath.Join(root, "bin", "1.14.0")

			got, err := ExtractBinary(archivePath, destDir, test.executable)
			if test.wantCode != "" {
				if err == nil {
					t.Fatalf("ExtractBinary() = %q, want an error", got)
				}
				if code := apperr.CodeOf(err); code != test.wantCode {
					t.Errorf("error code = %s, want %s (%v)", code, test.wantCode, err)
				}
				if test.wantErrIn != "" && !strings.Contains(err.Error(), test.wantErrIn) {
					t.Errorf("error %q does not mention %q", err, test.wantErrIn)
				}
				// Nothing may be written out of a refused archive.
				if entries := dirEntries(t, destDir); len(entries) != 0 {
					t.Errorf("destDir contains %v, want nothing", entries)
				}
				if entries := dirEntries(t, filepath.Join(root, "bin")); len(entries) > 1 {
					t.Errorf("the installation directory contains %v, want at most the version directory", entries)
				}
				return
			}

			if err != nil {
				t.Fatalf("ExtractBinary() failed: %v", err)
			}
			want := filepath.Join(destDir, test.executable)
			if got != want {
				t.Errorf("ExtractBinary() = %q, want %q", got, want)
			}
			info, err := os.Stat(got)
			if err != nil {
				t.Fatalf("the extracted executable is missing: %v", err)
			}
			// Windows records no permission bits: a file there is executable by
			// its extension, and the installed copy keeps the name it was
			// extracted under, which TestExtractBundleInstallsCompanions asserts.
			if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
				t.Errorf("mode = %s, want the executable bit set", info.Mode())
			}
			raw, err := os.ReadFile(got)
			if err != nil {
				t.Fatalf("cannot read the extracted executable: %v", err)
			}
			if string(raw) != test.wantContents {
				t.Errorf("contents = %q, want %q", raw, test.wantContents)
			}
			// Only the executable is placed: the tarball's LICENSE and the zip's
			// helper DLL must not reach the installation directory.
			if entries := dirEntries(t, destDir); len(entries) != 1 {
				t.Errorf("destDir contains %v, want exactly the executable", entries)
			}
		})
	}
}

func TestExtractBinaryArgumentErrors(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "sing-box-1.14.0-darwin-arm64.tar.gz")
	writeTarGz(t, archivePath, darwinArchive("sing-box", 0o755))

	tests := []struct {
		name       string
		archive    string
		dest       string
		executable string
		wantCode   apperr.Code
	}{
		{name: "no archive", archive: "", dest: filepath.Join(root, "dest"), executable: "sing-box", wantCode: apperr.CodeInvalidArgument},
		{name: "no destination", archive: archivePath, dest: "  ", executable: "sing-box", wantCode: apperr.CodeInvalidArgument},
		{name: "no executable name", archive: archivePath, dest: filepath.Join(root, "dest"), executable: "", wantCode: apperr.CodeInvalidArgument},
		{name: "executable name with a path", archive: archivePath, dest: filepath.Join(root, "dest"), executable: "../sing-box", wantCode: apperr.CodeInvalidArgument},
		{name: "missing archive file", archive: filepath.Join(root, "absent.tar.gz"), dest: filepath.Join(root, "dest"), executable: "sing-box", wantCode: apperr.CodeBinaryInstallFailed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ExtractBinary(test.archive, test.dest, test.executable)
			if err == nil {
				t.Fatal("ExtractBinary() succeeded, want an error")
			}
			if code := apperr.CodeOf(err); code != test.wantCode {
				t.Errorf("error code = %s, want %s (%v)", code, test.wantCode, err)
			}
		})
	}
}

func TestSafeEntryName(t *testing.T) {
	tests := []struct {
		name    string
		entry   string
		want    string
		wantErr bool
	}{
		{name: "plain name", entry: "sing-box", want: "sing-box"},
		{name: "nested name", entry: "sing-box-1.14.0-darwin-arm64/sing-box", want: "sing-box-1.14.0-darwin-arm64/sing-box"},
		{name: "directory entry", entry: "dir/", want: "dir"},
		{name: "traversal", entry: "../sing-box", wantErr: true},
		{name: "traversal above a nested path", entry: "a/../../sing-box", wantErr: true},
		{name: "absolute", entry: "/sing-box", wantErr: true},
		{name: "drive qualified", entry: "C:/sing-box.exe", wantErr: true},
		{name: "backslash", entry: `a\sing-box`, wantErr: true},
		{name: "nul byte", entry: "sing-box\x00.txt", wantErr: true},
		{name: "empty", entry: "", wantErr: true},
		{name: "only a slash", entry: "/", wantErr: true},
		{name: "not normalised", entry: "a/./b", wantErr: true},
		{name: "double separator", entry: "a//b", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := safeEntryName(test.entry)
			if test.wantErr {
				if err == nil {
					t.Fatalf("safeEntryName(%q) = %q, want an error", test.entry, got)
				}
				if code := apperr.CodeOf(err); code != apperr.CodeBinaryInstallFailed {
					t.Errorf("error code = %s, want %s", code, apperr.CodeBinaryInstallFailed)
				}
				return
			}
			if err != nil {
				t.Fatalf("safeEntryName(%q) failed: %v", test.entry, err)
			}
			if got != test.want {
				t.Errorf("safeEntryName(%q) = %q, want %q", test.entry, got, test.want)
			}
		})
	}
}

func TestDetectFormat(t *testing.T) {
	root := t.TempDir()
	tarGz := filepath.Join(root, "x.tar.gz")
	writeTarGz(t, tarGz, []archiveItem{{name: "sing-box", content: "x", mode: 0o755}})
	// A zip under a name that says nothing about the format, and a tarball that
	// lies about being a zip: content decides, the name is only the fallback.
	zipped := filepath.Join(root, "download.dat")
	writeZip(t, zipped, []archiveItem{{name: "sing-box.exe", content: "x", mode: 0o755}})
	lyingName := filepath.Join(root, "x.zip")
	writeTarGz(t, lyingName, []archiveItem{{name: "sing-box", content: "x", mode: 0o755}})
	text := filepath.Join(root, "x.txt")
	writeFile(t, text, "plain text")

	tests := []struct {
		name     string
		path     string
		want     string
		wantCode apperr.Code
	}{
		{name: "zip content under an unrelated name", path: zipped, want: formatZip},
		{name: "tar content under a .zip name", path: lyingName, want: formatTarGz},
		{name: "tar.gz by content", path: tarGz, want: formatTarGz},
		{name: "plain text", path: text, wantCode: apperr.CodeBinaryInstallFailed},
		{name: "missing file", path: filepath.Join(root, "absent"), wantCode: apperr.CodeBinaryInstallFailed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := detectFormat(test.path)
			if test.wantCode != "" {
				if err == nil {
					t.Fatalf("detectFormat() = %q, want an error", got)
				}
				if code := apperr.CodeOf(err); code != test.wantCode {
					t.Errorf("error code = %s, want %s", code, test.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("detectFormat() failed: %v", err)
			}
			if got != test.want {
				t.Errorf("detectFormat() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestExtractBinaryReplacesAnExistingBinary(t *testing.T) {
	root := t.TempDir()
	destDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatalf("cannot create %s: %v", destDir, err)
	}
	stale := filepath.Join(destDir, "sing-box")
	writeFile(t, stale, "old binary")

	archivePath := filepath.Join(root, "sing-box-1.14.0-darwin-arm64.tar.gz")
	writeTarGz(t, archivePath, darwinArchive("sing-box", 0o755))

	got, err := ExtractBinary(archivePath, destDir, "sing-box")
	if err != nil {
		t.Fatalf("ExtractBinary() failed: %v", err)
	}
	raw, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("cannot read the extracted executable: %v", err)
	}
	if string(raw) != "#!/bin/sh\n# sing-box\n" {
		t.Errorf("contents = %q, want the newly extracted binary", raw)
	}
}

func TestExtractBinaryRefusalLeavesNoTraces(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "evil.tar.gz")
	writeTarGz(t, archivePath, []archiveItem{{name: "sing-box", content: "evil", mode: 0o755}})
	// Validation happens while streaming, so a refused archive must abort before
	// the executable is written: the destination stays empty.
	destDir := filepath.Join(root, "bin")
	if _, err := ExtractBinary(filepath.Join(root, "absent.tar.gz"), destDir, "sing-box"); err == nil {
		t.Fatal("ExtractBinary() accepted a missing archive")
	}
	if entries := dirEntries(t, destDir); len(entries) != 0 {
		t.Errorf("destDir contains %v, want nothing", entries)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("cannot write %s: %v", path, err)
	}
}

// errStopArchive must stay distinct from io.EOF, which the archive walkers read
// as a clean end of input.
func TestErrStopArchiveIsDistinct(t *testing.T) {
	if errStopArchive == nil || errStopArchive.Error() == "" {
		t.Fatal("errStopArchive must be a non-nil error with a message")
	}
	if errors.Is(errStopArchive, io.EOF) {
		t.Error("errStopArchive must not be io.EOF")
	}
}

// ---------------------------------------------------------------------------
// companion libraries (the Windows archive ships libcronet.dll beside the exe)

// windowsArchive is the real Windows zip layout: the executable, the library the
// naive outbound loads from the directory of sing-box.exe, and the license.
func windowsArchive() []archiveItem {
	return []archiveItem{
		{name: "sing-box-1.14.0-windows-amd64/", mode: 0o755},
		{name: "sing-box-1.14.0-windows-amd64/LICENSE", content: "license\n"},
		{name: "sing-box-1.14.0-windows-amd64/libcronet.dll", content: "cronet bytes"},
		{name: "sing-box-1.14.0-windows-amd64/sing-box.exe", content: "MZ", mode: 0o755},
	}
}

// TestExtractBundleInstallsCompanions asserts the library sing-box loads at run
// time is installed next to the executable: without it the naive outbound cannot
// work at all, and the file used to be dropped silently (spec §21).
func TestExtractBundleInstallsCompanions(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "sing-box-1.14.0-windows-amd64.zip")
	writeZip(t, archivePath, windowsArchive())
	destDir := filepath.Join(root, "bin", "1.14.0")

	bundle, err := ExtractBundle(archivePath, destDir, "sing-box.exe", CompanionFiles("windows"))
	if err != nil {
		t.Fatalf("ExtractBundle() failed: %v", err)
	}
	if want := filepath.Join(destDir, "sing-box.exe"); bundle.Executable != want {
		t.Errorf("Executable = %q, want %q", bundle.Executable, want)
	}
	if len(bundle.Companions) != 1 {
		t.Fatalf("Companions = %v, want the single library of the archive", bundle.Companions)
	}
	if want := filepath.Join(destDir, "libcronet.dll"); bundle.Companions[0] != want {
		t.Errorf("Companions[0] = %q, want %q", bundle.Companions[0], want)
	}
	raw, err := os.ReadFile(bundle.Companions[0])
	if err != nil {
		t.Fatalf("cannot read the installed library: %v", err)
	}
	if string(raw) != "cronet bytes" {
		t.Errorf("the installed library holds %q, want the archive content", raw)
	}
	// A library is not executable, and the license is never written: an
	// installation only places files it was told to verify (spec §21).
	info, err := os.Stat(bundle.Companions[0])
	if err != nil {
		t.Fatalf("cannot stat the installed library: %v", err)
	}
	if info.Mode().Perm()&0o111 != 0 {
		t.Errorf("mode = %s, want a library without the executable bit", info.Mode())
	}
	if entries := dirEntries(t, destDir); len(entries) != 2 {
		t.Errorf("destDir contains %v, want the executable and the library only", entries)
	}
}

// TestExtractBundleWithoutCompanionsWritesTheExecutableOnly pins the behaviour of
// ExtractBinary: the same Windows archive it always handled installs one file.
func TestExtractBundleWithoutCompanionsWritesTheExecutableOnly(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "sing-box-1.14.0-windows-amd64.zip")
	writeZip(t, archivePath, windowsArchive())
	destDir := filepath.Join(root, "bin", "1.14.0")

	bundle, err := ExtractBundle(archivePath, destDir, "sing-box.exe", nil)
	if err != nil {
		t.Fatalf("ExtractBundle() failed: %v", err)
	}
	if len(bundle.Companions) != 0 {
		t.Errorf("Companions = %v, want none without a requested list", bundle.Companions)
	}
	if entries := dirEntries(t, destDir); len(entries) != 1 {
		t.Errorf("destDir contains %v, want the executable only", entries)
	}
}

// TestExtractBundleRefusesBadCompanionRequests covers the ways a companion must
// not be picked: a path instead of a name, a name requested twice, a library that
// is not next to the executable, and two candidates for one name.
func TestExtractBundleRefusesBadCompanionRequests(t *testing.T) {
	tests := []struct {
		name       string
		items      []archiveItem
		companions []string
		wantCode   apperr.Code
		wantErrIn  string
		wantFiles  int
	}{
		{
			name:       "companion given as a path",
			items:      windowsArchive(),
			companions: []string{"sub/libcronet.dll"},
			wantCode:   apperr.CodeInvalidArgument,
			wantErrIn:  "bare file name",
		},
		{
			name:       "companion requested twice",
			items:      windowsArchive(),
			companions: []string{"libcronet.dll", "libcronet.dll"},
			wantCode:   apperr.CodeInvalidArgument,
			wantErrIn:  "twice",
		},
		{
			name: "library in another directory",
			items: []archiveItem{
				{name: "release/sing-box.exe", content: "MZ", mode: 0o755},
				{name: "elsewhere/libcronet.dll", content: "cronet bytes"},
			},
			companions: []string{"libcronet.dll"},
			wantFiles:  1,
		},
		{
			name: "two candidates next to the executable",
			items: []archiveItem{
				{name: "release/sing-box.exe", content: "MZ", mode: 0o755},
				{name: "release/libcronet.dll", content: "first"},
				{name: "release/libcronet.dll", content: "second"},
			},
			companions: []string{"libcronet.dll"},
			wantCode:   apperr.CodeBinaryInstallFailed,
			wantErrIn:  "at most one",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			archivePath := filepath.Join(root, "sing-box-1.14.0-windows-amd64.zip")
			writeZip(t, archivePath, test.items)
			destDir := filepath.Join(root, "bin", "1.14.0")

			bundle, err := ExtractBundle(archivePath, destDir, "sing-box.exe", test.companions)
			if test.wantCode != "" {
				if err == nil {
					t.Fatalf("ExtractBundle() = %+v, want an error", bundle)
				}
				if code := apperr.CodeOf(err); code != test.wantCode {
					t.Errorf("error code = %s, want %s (%v)", code, test.wantCode, err)
				}
				if test.wantErrIn != "" && !strings.Contains(err.Error(), test.wantErrIn) {
					t.Errorf("error %q does not mention %q", err, test.wantErrIn)
				}
				if entries := dirEntries(t, destDir); len(entries) != 0 {
					t.Errorf("destDir contains %v, want nothing", entries)
				}
				return
			}
			if err != nil {
				t.Fatalf("ExtractBundle() failed: %v", err)
			}
			if len(bundle.Companions) != 0 {
				t.Errorf("Companions = %v, want none: the library does not belong to this binary", bundle.Companions)
			}
			if entries := dirEntries(t, destDir); len(entries) != test.wantFiles {
				t.Errorf("destDir contains %v, want %d entries", entries, test.wantFiles)
			}
		})
	}
}

// TestExtractBundleToleratesAnArchiveWithoutTheLibrary: a release that stops
// shipping the DLL must still install. The executable is what was verified, and a
// profile that does not use the library does not need it.
func TestExtractBundleToleratesAnArchiveWithoutTheLibrary(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "sing-box-1.15.0-windows-amd64.zip")
	writeZip(t, archivePath, []archiveItem{
		{name: "sing-box-1.15.0-windows-amd64/sing-box.exe", content: "MZ", mode: 0o755},
		{name: "sing-box-1.15.0-windows-amd64/LICENSE", content: "license\n"},
	})
	destDir := filepath.Join(root, "bin", "1.15.0")

	bundle, err := ExtractBundle(archivePath, destDir, "sing-box.exe", CompanionFiles("windows"))
	if err != nil {
		t.Fatalf("ExtractBundle() failed: %v", err)
	}
	if len(bundle.Companions) != 0 {
		t.Errorf("Companions = %v, want none", bundle.Companions)
	}
	if entries := dirEntries(t, destDir); len(entries) != 1 {
		t.Errorf("destDir contains %v, want the executable only", entries)
	}
}

// TestCompanionFilesPerPlatform keeps the allow-list honest: only Windows ships a
// library that sing-box loads from its own directory.
func TestCompanionFilesPerPlatform(t *testing.T) {
	if got := CompanionFiles("windows"); len(got) != 1 || got[0] != "libcronet.dll" {
		t.Errorf("CompanionFiles(windows) = %v, want libcronet.dll", got)
	}
	for _, goos := range []string{"darwin", "linux", ""} {
		if got := CompanionFiles(goos); len(got) != 0 {
			t.Errorf("CompanionFiles(%q) = %v, want none", goos, got)
		}
	}
}
