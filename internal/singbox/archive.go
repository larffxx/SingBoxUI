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

// Bundle is what an official release archive yields: the sing-box executable and
// the libraries it loads at run time, all of them written beside each other.
type Bundle struct {
	// Executable is the absolute path of the extracted sing-box executable.
	Executable string
	// Companions are the absolute paths of the companion libraries found in the
	// archive, in the order they were requested. It is empty when the archive
	// carries none.
	Companions []string
}

// CompanionFiles returns the libraries the official archive for a GOOS ships
// beside the executable and that must be installed together with it, because
// sing-box loads them from its own directory at run time.
//
// The Windows archive carries libcronet.dll: the naive outbound loads it when a
// profile uses that protocol, and an installation missing it silently cannot run
// such a profile. The macOS archive carries the executable and LICENSE only.
func CompanionFiles(goos string) []string {
	if goos == "windows" {
		return []string{"libcronet.dll"}
	}
	return nil
}

// ExtractBinary extracts the sing-box executable from an official release
// archive and returns its path inside destDir, without any companion file.
//
// Most callers want ExtractBundle, which installs the libraries the executable
// needs as well.
func ExtractBinary(archivePath, destDir, executableName string) (string, error) {
	bundle, err := ExtractBundle(archivePath, destDir, executableName, nil)
	if err != nil {
		return "", err
	}
	return bundle.Executable, nil
}

// ExtractBundle extracts the sing-box executable and the named companion files
// from an official release archive into destDir, and returns where they landed.
//
// The archive is untrusted input, so every entry is validated before anything
// is written: absolute paths, "..", drive-qualified names, backslash separators,
// symlinks, hard links and device nodes abort the extraction, and the archive
// must contain exactly one entry named executableName. A companion is written
// only when it is named in companions, is present exactly once, and is a regular
// file; everything else — the LICENSE an official tarball carries, a helper DLL
// nobody asked for — is skipped, so the installation can never place anything but
// files this function was told to verify (spec §21). A companion that is missing
// from an archive is not an error: the executable is what was verified, and
// sing-box works without a library only an optional outbound needs.
//
// Every file is written through internal/atomicfile, so a crash mid-extract
// cannot leave a half-written binary behind for the version probe to run.
func ExtractBundle(archivePath, destDir, executableName string, companions []string) (Bundle, error) {
	const op = "singbox.ExtractBinary"
	switch {
	case strings.TrimSpace(archivePath) == "":
		return Bundle{}, apperr.New(apperr.CodeInvalidArgument, op, "an archive path is required")
	case strings.TrimSpace(destDir) == "":
		return Bundle{}, apperr.New(apperr.CodeInvalidArgument, op, "a destination directory is required")
	case executableName == "" || filepath.Base(executableName) != executableName:
		return Bundle{}, apperr.Newf(apperr.CodeInvalidArgument, op,
			"the executable name %q must be a bare file name", executableName)
	}
	wanted := make([]string, 0, len(companions))
	seen := make(map[string]bool, len(companions)+1)
	seen[executableName] = true
	for _, companion := range companions {
		name := strings.TrimSpace(companion)
		switch {
		case name == "":
			return Bundle{}, apperr.New(apperr.CodeInvalidArgument, op, "a companion file name is empty")
		case filepath.Base(name) != name:
			return Bundle{}, apperr.Newf(apperr.CodeInvalidArgument, op,
				"the companion name %q must be a bare file name", name)
		case seen[name]:
			return Bundle{}, apperr.Newf(apperr.CodeInvalidArgument, op,
				"%q was requested twice", name)
		}
		seen[name] = true
		wanted = append(wanted, name)
	}

	format, err := detectFormat(archivePath)
	if err != nil {
		return Bundle{}, err
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return Bundle{}, apperr.Wrap(apperr.CodeBinaryInstallFailed, op,
			"cannot create the destination directory", err)
	}

	found, err := scanArchive(archivePath, format, executableName, wanted)
	if err != nil {
		return Bundle{}, err
	}
	candidates := found[executableName]
	switch len(candidates) {
	case 0:
		return Bundle{}, apperr.Newf(apperr.CodeBinaryInstallFailed, op,
			"the archive %s does not contain %s", filepath.Base(archivePath), executableName)
	case 1:
	default:
		return Bundle{}, apperr.Newf(apperr.CodeBinaryInstallFailed, op,
			"the archive %s contains %d entries named %s; exactly one is expected",
			filepath.Base(archivePath), len(candidates), executableName)
	}
	// A companion has to sit next to the executable: that is the only place
	// sing-box looks for the library, and it keeps a second copy elsewhere in the
	// archive from being installed as if it belonged to this binary.
	exeDir := path.Dir(candidates[0])
	companionEntry := make(map[string]string, len(wanted)-1)
	for _, name := range wanted {
		if name == executableName {
			continue
		}
		matches := entriesInDir(found[name], exeDir)
		if len(matches) > 1 {
			return Bundle{}, apperr.Newf(apperr.CodeBinaryInstallFailed, op,
				"the archive %s contains %d entries named %s next to %s; at most one is expected",
				filepath.Base(archivePath), len(matches), name, executableName)
		}
		if len(matches) == 1 {
			companionEntry[name] = matches[0]
		}
	}

	data, err := readArchiveEntry(archivePath, format, candidates[0])
	if err != nil {
		return Bundle{}, err
	}
	executable, err := writeExtracted(destDir, executableName, data, 0o755)
	if err != nil {
		return Bundle{}, err
	}
	bundle := Bundle{Executable: executable}
	for _, name := range wanted {
		entry, ok := companionEntry[name]
		if !ok {
			continue
		}
		content, err := readArchiveEntry(archivePath, format, entry)
		if err != nil {
			return Bundle{}, err
		}
		// A library is not executable: the mode it is installed with says so.
		path, err := writeExtracted(destDir, name, content, 0o644)
		if err != nil {
			return Bundle{}, err
		}
		bundle.Companions = append(bundle.Companions, path)
	}
	return bundle, nil
}

// writeExtracted writes one validated archive entry into destDir.
func writeExtracted(destDir, name string, data []byte, mode os.FileMode) (string, error) {
	destination, err := filepath.Abs(filepath.Join(destDir, name))
	if err != nil {
		return "", apperr.Wrap(apperr.CodeBinaryInstallFailed, "singbox.ExtractBinary",
			"cannot resolve the destination path", err)
	}
	// The archive's own mode is ignored: an executable we installed is executable,
	// and a library we installed is not.
	if err := atomicfile.Write(destination, data, mode); err != nil {
		return "", apperr.Wrap(apperr.CodeBinaryInstallFailed, "singbox.ExtractBinary",
			"cannot write the extracted file", err)
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

// scanArchive validates every entry and returns, per wanted name, the entries
// that match it.
//
// Validation is deliberately done on a first pass over the whole archive: an
// unsafe entry anywhere is a reason to refuse the download, not just a reason to
// skip one file.
func scanArchive(archivePath, format, executableName string, companions []string) (map[string][]string, error) {
	wanted := make(map[string]bool, len(companions)+1)
	wanted[executableName] = true
	for _, name := range companions {
		wanted[name] = true
	}
	found := make(map[string][]string, len(wanted))
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
		if base := path.Base(clean); wanted[base] {
			found[base] = append(found[base], clean)
		}
		return nil
	}
	if err := walkArchive(archivePath, format, visit); err != nil {
		return nil, err
	}
	return found, nil
}

// entriesInDir returns the entries of one name that live in the directory the
// executable sits in. An official archive keeps its files in one directory, so a
// companion found anywhere else is not the library that belongs to this binary.
func entriesInDir(entries []string, dir string) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if path.Dir(entry) == dir {
			out = append(out, entry)
		}
	}
	return out
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
