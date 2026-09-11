package binary

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/larffxx/singboxui/internal/domain/apperr"
)

// ensureDir creates a directory if it is missing.
func ensureDir(path string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return apperr.Wrap(apperr.CodeBinaryInstallFailed, "binary.ensureDir",
			"the directory "+path+" could not be created", err)
	}
	return nil
}

// copyFile copies a regular file preserving its permission bits. The destination
// is written through a temporary file and renamed, so a failure never leaves a
// half-written binary behind (spec §15).
func copyFile(src, dst string) error {
	const op = "binary.copyFile"
	in, err := os.Open(src)
	if err != nil {
		return apperr.Wrap(apperr.CodeBinaryInstallFailed, op, "cannot open "+src, err)
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return apperr.Wrap(apperr.CodeBinaryInstallFailed, op, "cannot stat "+src, err)
	}
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return apperr.Wrap(apperr.CodeBinaryInstallFailed, op, "cannot write "+tmp, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return apperr.Wrap(apperr.CodeBinaryInstallFailed, op, "cannot copy "+src, err)
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(tmp)
		return apperr.Wrap(apperr.CodeBinaryInstallFailed, op, "cannot flush "+tmp, err)
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return apperr.Wrap(apperr.CodeBinaryInstallFailed, op, "cannot close "+tmp, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return apperr.Wrap(apperr.CodeBinaryInstallFailed, op, "cannot replace "+dst, err)
	}
	return nil
}

// moveFile renames a file, falling back to a copy for cross-device moves.
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyFile(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

// fileSHA256 hashes a file, used when the release client did not hash the
// download while streaming.
func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", apperr.Wrap(apperr.CodeBinaryDownloadFailed, "binary.fileSHA256", "cannot read the download", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", apperr.Wrap(apperr.CodeBinaryDownloadFailed, "binary.fileSHA256", "cannot hash the download", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// percentOf converts a byte progress pair into a bounded percentage.
func percentOf(done, total int64) int {
	if total <= 0 || done <= 0 {
		return 0
	}
	percent := int(done * 100 / total)
	if percent < 0 {
		return 0
	}
	if percent > 100 {
		return 100
	}
	return percent
}

func ptr[T any](value T) *T { return &value }

// hostOf extracts the host of a URL for reporting; it never logs the full URL,
// which may carry query parameters.
func hostOf(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Host
}

var _ = filepath.Base
var _ = time.Now
