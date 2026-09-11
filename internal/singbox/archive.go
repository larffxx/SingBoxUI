package singbox

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/larffxx/singboxui/internal/atomicfile"
	"github.com/larffxx/singboxui/internal/domain/apperr"
)

const (
	formatTarGz = "tar.gz"
	formatZip   = "zip"
)

// maxExtractedEntry caps a single archived file. Official binaries are around
// 90 MB unpacked, so this leaves generous headroom while still refusing an
// archive that would exhaust memory during installation.
const maxExtractedEntry = 512 << 20

// windowsDrivePrefix catches a DOS drive-qualified entry name, which is an
// absolute path on Windows even though it is not rooted at "/".
var windowsDrivePrefix = regexp.MustCompile(`^[A-Za-z]:`)

// ExtractBinary extracts the sing-box executable from an official release
// archive and returns its path inside destDir.
//
// The archive is untrusted input, so every entry is validated before anything
// is written: absolute paths, "..", drive-qualified names, backslash separators,
// symlinks, hard links and device nodes abort the extraction, and the archive
// must contain exactly one entry named executableName. Other regular files are
// not written out — the official tarballs also carry LICENSE, and the Windows
// zip a helper DLL — so the installation can never place anything but the
// executable it verified (spec §21).
//
// The executable is written through internal/atomicfile, so a crash mid-extract
// cannot leave a half-written binary behind for the version probe to run.
func ExtractBinary(archivePath, destDir, executableName string) (string, error) {
	const op = "singbox.ExtractBinary"
	switch {
	case strings.TrimSpace(archivePath) == "":
		return "", apperr.New(apperr.CodeInvalidArgument, op, "an archive path is required")
	case strings.TrimSpace(destDir) == "":
		return "", apperr.New(apperr.CodeInvalidArgument, op, "a destination directory is required")
	case executableName == "" || filepath.Base(executableName) != executableName:
		return "", apperr.Newf(apperr.CodeInvalidArgument, op,
			"the executable name %q must be a bare file name", executableName)
	}

	format, err := detectFormat(archivePath)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", apperr.Wrap(apperr.CodeBinaryInstallFailed, op,
			"cannot create the destination directory", err)
	}

	candidates, err := scanArchive(archivePath, format, executableName)
	if err != nil {
		return "", err
	}
	switch len(candidates) {
	case 0:
		return "", apperr.Newf(apperr.CodeBinaryInstallFailed, op,
			"the archive %s does not contain %s", filepath.Base(archivePath), executableName)
	case 1:
	default:
		return "", apperr.Newf(apperr.CodeBinaryInstallFailed, op,
			"the archive %s contains %d entries named %s; exactly one is expected",
			filepath.Base(archivePath), len(candidates), executableName)
	}

	data, err := readArchiveEntry(archivePath, format, candidates[0])
	if err != nil {
		return "", err
	}
	destination, err := filepath.Abs(filepath.Join(destDir, executableName))
	if err != nil {
		return "", apperr.Wrap(apperr.CodeBinaryInstallFailed, op,
			"cannot resolve the destination path", err)
	}
	// The archive's own mode is ignored: an executable we installed is executable.
	if err := atomicfile.Write(destination, data, 0o755); err != nil {
		return "", apperr.Wrap(apperr.CodeBinaryInstallFailed, op,
			"cannot write the extracted executable", err)
	}
	return destination, nil
}

// detectFormat identifies the archive by content first and by name second, so a
// download stored under a temporary name without an extension still installs.
func detectFormat(archivePath string) (string, error) {
	const op = "singbox.ExtractBinary"
	file, err := os.Open(archivePath)
	if err != nil {
		return "", apperr.Wrap(apperr.CodeBinaryInstallFailed, op,
			"cannot open the archive", err)
	}
	defer file.Close()

	var magic [4]byte
	read, readErr := io.ReadFull(file, magic[:])
	if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return "", apperr.Wrap(apperr.CodeBinaryInstallFailed, op,
			"cannot read the archive", readErr)
	}
	switch {
	case read >= 2 && magic[0] == 0x1f && magic[1] == 0x8b:
		return formatTarGz, nil
	case read == 4 && string(magic[:4]) == "PK\x03\x04":
		return formatZip, nil
	}
	lower := strings.ToLower(archivePath)
	switch {
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return formatTarGz, nil
	case strings.HasSuffix(lower, ".zip"):
		return formatZip, nil
	default:
		return "", apperr.Newf(apperr.CodeBinaryInstallFailed, op,
			"%s is neither a .tar.gz nor a .zip archive", filepath.Base(archivePath))
	}
}

// scanArchive validates every entry and returns the names of the entries that
// match executableName.
//
// Validation is deliberately done on a first pass over the whole archive: an
// unsafe entry anywhere is a reason to refuse the download, not just a reason to
// skip one file.
func scanArchive(archivePath, format, executableName string) ([]string, error) {
	var candidates []string
	visit := func(entryName string, entry archiveEntry) error {
		clean, err := safeEntryName(entryName)
		if err != nil {
			return err
		}
		if entry.size > maxExtractedEntry {
			return apperr.Newf(apperr.CodeBinaryInstallFailed, "singbox.ExtractBinary",
				"the archive entry %s is larger than the extraction limit", entryName)
		}
		if entry.directory {
			return nil
		}
		if path.Base(clean) == executableName {
			candidates = append(candidates, clean)
		}
		return nil
	}
	if err := walkArchive(archivePath, format, visit); err != nil {
		return nil, err
	}
	return candidates, nil
}

// readArchiveEntry reads one validated entry by name.
func readArchiveEntry(archivePath, format, wanted string) ([]byte, error) {
	var data []byte
	visit := func(entryName string, entry archiveEntry) error {
		clean, err := safeEntryName(entryName)
		if err != nil {
			return err
		}
		if entry.directory || clean != wanted {
			return nil
		}
		// +1 so an entry that exceeds the limit is detected instead of silently
		// truncated into a corrupt binary.
		content, err := io.ReadAll(io.LimitReader(entry.reader, maxExtractedEntry+1))
		if err != nil {
			return apperr.Wrap(apperr.CodeBinaryInstallFailed, "singbox.ExtractBinary",
				"cannot read "+wanted+" from the archive", err)
		}
		if int64(len(content)) > maxExtractedEntry {
			return apperr.Newf(apperr.CodeBinaryInstallFailed, "singbox.ExtractBinary",
				"the archive entry %s is larger than the extraction limit", wanted)
		}
		data = content
		return errStopArchive
	}
	if err := walkArchive(archivePath, format, visit); err != nil && !errors.Is(err, errStopArchive) {
		return nil, err
	}
	if data == nil {
		return nil, apperr.Newf(apperr.CodeBinaryInstallFailed, "singbox.ExtractBinary",
			"the archive changed while it was being read: %s is gone", wanted)
	}
	return data, nil
}

// errStopArchive ends a scan early once the wanted entry has been read.
var errStopArchive = errors.New("singbox: archive scan complete")

// archiveEntry is the platform-independent view of an archive entry the
// validation rules need.
type archiveEntry struct {
	reader    io.Reader
	directory bool
	size      int64
}

// walkArchive streams an archive of the given format.
func walkArchive(archivePath, format string, visit func(name string, entry archiveEntry) error) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return apperr.Wrap(apperr.CodeBinaryInstallFailed, "singbox.ExtractBinary",
			"cannot open the archive", err)
	}
	defer file.Close()

	switch format {
	case formatTarGz:
		gz, err := gzip.NewReader(file)
		if err != nil {
			return apperr.Wrap(apperr.CodeBinaryInstallFailed, "singbox.ExtractBinary",
				"cannot decompress the archive", err)
		}
		defer gz.Close()
		reader := tar.NewReader(gz)
		for {
			header, err := reader.Next()
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return apperr.Wrap(apperr.CodeBinaryInstallFailed, "singbox.ExtractBinary",
					"the archive is corrupt", err)
			}
			switch header.Typeflag {
			case tar.TypeReg, tar.TypeDir, '\x00':
				// '\x00' is the pre-Go-1.11 spelling of tar.TypeRegA: old v7
				// archives mark regular files with a NUL type flag.
			default:
				return apperr.Newf(apperr.CodeBinaryInstallFailed, "singbox.ExtractBinary",
					"the archive entry %s is not a regular file", header.Name)
			}
			directory := header.Typeflag == tar.TypeDir || strings.HasSuffix(header.Name, "/")
			if err := visit(header.Name, archiveEntry{
				reader:    reader,
				directory: directory,
				size:      header.Size,
			}); err != nil {
				return err
			}
		}

	case formatZip:
		reader, err := zip.OpenReader(archivePath)
		if err != nil {
			return apperr.Wrap(apperr.CodeBinaryInstallFailed, "singbox.ExtractBinary",
				"cannot read the archive", err)
		}
		defer reader.Close()
		for _, item := range reader.File {
			entry, err := zipEntry(item)
			if err != nil {
				return err
			}
			if entry.directory {
				if err := visit(item.Name, entry); err != nil {
					return err
				}
				continue
			}
			handle, err := item.Open()
			if err != nil {
				return apperr.Wrap(apperr.CodeBinaryInstallFailed, "singbox.ExtractBinary",
					"cannot read "+item.Name+" from the archive", err)
			}
			entry.reader = handle
			visitErr := visit(item.Name, entry)
			handle.Close()
			if visitErr != nil {
				return visitErr
			}
		}
		return nil

	default:
		return apperr.Newf(apperr.CodeBinaryInstallFailed, "singbox.ExtractBinary",
			"unsupported archive format %q", format)
	}
}

// zipEntry validates one zip member's type and size.
//
// A zip may be written without Unix mode bits at all (the Windows toolchain
// does this), so an untyped entry is accepted as a regular file while an
// explicit symlink or device is not.
func zipEntry(item *zip.File) (archiveEntry, error) {
	mode := item.Mode()
	switch {
	case mode&os.ModeSymlink != 0:
		return archiveEntry{}, apperr.Newf(apperr.CodeBinaryInstallFailed, "singbox.ExtractBinary",
			"the archive entry %s is a symbolic link", item.Name)
	case item.FileInfo().IsDir():
		return archiveEntry{directory: true}, nil
	case mode.Type() == 0 || mode.IsRegular():
	default:
		return archiveEntry{}, apperr.Newf(apperr.CodeBinaryInstallFailed, "singbox.ExtractBinary",
			"the archive entry %s is not a regular file", item.Name)
	}
	if item.UncompressedSize64 > maxExtractedEntry {
		return archiveEntry{}, apperr.Newf(apperr.CodeBinaryInstallFailed, "singbox.ExtractBinary",
			"the archive entry %s is larger than the extraction limit", item.Name)
	}
	return archiveEntry{size: int64(item.UncompressedSize64)}, nil
}

// safeEntryName rejects every entry name that could escape destDir or that this
// application is not prepared to interpret.
func safeEntryName(name string) (string, error) {
	const op = "singbox.ExtractBinary"
	original := name
	if strings.Contains(name, "\x00") || strings.Contains(name, "\\") {
		return "", apperr.Newf(apperr.CodeBinaryInstallFailed, op,
			"the archive entry %q uses an unsupported path separator", original)
	}
	trimmed := strings.TrimSuffix(name, "/")
	if trimmed == "" {
		return "", apperr.Newf(apperr.CodeBinaryInstallFailed, op,
			"the archive contains an entry with an empty name")
	}
	if path.IsAbs(trimmed) || strings.HasPrefix(trimmed, "/") || windowsDrivePrefix.MatchString(trimmed) {
		return "", apperr.Newf(apperr.CodeBinaryInstallFailed, op,
			"the archive entry %q is an absolute path", original)
	}
	clean := path.Clean(trimmed)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", apperr.Newf(apperr.CodeBinaryInstallFailed, op,
			"the archive entry %q escapes the destination directory", original)
	}
	if clean != trimmed {
		return "", apperr.Newf(apperr.CodeBinaryInstallFailed, op,
			"the archive entry %q is not a normalised path", original)
	}
	return clean, nil
}
