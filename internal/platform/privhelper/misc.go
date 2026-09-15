package privhelper

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// dirMode keeps the application's runtime directory private to its owner. The
// helper only ever creates directories the application itself creates.
const dirMode = 0o700

// filepathDir is filepath.Dir behind a name that reads well in slice literals.
func filepathDir(path string) string { return filepath.Dir(path) }

// statExisting reports the file information of an existing path.
func statExisting(path string) (fs.FileInfo, error) { return os.Stat(path) }

// removeIfPresent removes a file, treating a missing file as success.
func removeIfPresent(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("privhelper: remove %s: %w", path, err)
	}
	return nil
}
