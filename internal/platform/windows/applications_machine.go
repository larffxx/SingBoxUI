//go:build windows

package windows

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// This file is the machine half of the Windows catalog: what the shell and the
// kernel answer when they are asked which programs are running, what a Start
// Menu shortcut points at and what a file calls itself. The rules that turn
// those answers into a listing live in applications.go.

var (
	user32             = windows.NewLazySystemDLL("user32.dll")
	procWindowTitleLen = user32.NewProc("GetWindowTextLengthW")
	procWindowVisible  = user32.NewProc("IsWindowVisible")
	procWindowPID      = user32.NewProc("GetWindowThreadProcessId")
)

// windowsDirectory is the Windows directory, resolved through the known-folder
// API with the environment variable as the documented fallback (internal
// contracts §2).
func windowsDirectory() string {
	path, err := windows.KnownFolderPath(windows.FOLDERID_Windows, windows.KF_FLAG_DEFAULT)
	if err != nil || path == "" {
		path = strings.TrimSpace(os.Getenv("SystemRoot"))
	}
	return filepath.Clean(path)
}

// excludedPrefixes are the paths the listing never offers: the application data
// directory with the managed sing-box releases inside it, and this application's
// own executables.
func excludedPrefixes(dataDir string) []string {
	prefixes := make([]string, 0, 3)
	if dataDir != "" {
		prefixes = append(prefixes, filepath.Clean(dataDir))
	}
	if self, err := os.Executable(); err == nil && self != "" {
		prefixes = append(prefixes, filepath.Clean(self))
		return append(prefixes, filepath.Join(filepath.Dir(self), HelperExecutable))
	}
	return prefixes
}

// windowedProgramPaths returns the executable of every program that shows a
// window on this machine.
//
// The window is the signal, because Windows has none of its own: a process is
// not required to declare that it is an application. A program is therefore
// "running" for this listing when it owns a top-level window with a title, or a
// window that is visible — which is how a program is recognised by a user, and
// which leaves services, updaters and helpers that own no window out of the list.
//
// Processes this user may not inspect are skipped rather than failed on: a
// process of another user, or a system process, cannot be opened for its image
// path, and nothing that the application cannot see belongs in a listing it
// offers. Each window costs one lookup, and the answer is deduplicated because
// one program owns many windows.
func windowedProgramPaths() ([]string, error) {
	pids, err := windowOwnerPIDs()
	if err != nil {
		return nil, err
	}
	var (
		paths []string
		seen  = make(map[string]struct{})
	)
	for _, pid := range pids {
		path, err := processImagePath(pid)
		if err != nil || path == "" {
			continue
		}
		key := strings.ToLower(path)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		paths = append(paths, path)
	}
	return paths, nil
}

// windowOwnerPIDs lists the processes that own a window a user could recognise:
// one with a title, or one that is visible.
func windowOwnerPIDs() ([]uint32, error) {
	var (
		owners []uint32
		seen   = make(map[uint32]struct{})
	)
	callback := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		var pid uint32
		procWindowPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if pid == 0 {
			return 1
		}
		title, _, _ := procWindowTitleLen.Call(hwnd)
		visible, _, _ := procWindowVisible.Call(hwnd)
		if title == 0 && visible == 0 {
			return 1
		}
		if _, dup := seen[pid]; !dup {
			seen[pid] = struct{}{}
			owners = append(owners, pid)
		}
		// A non-zero return keeps the enumeration going.
		return 1
	})
	if err := windows.EnumWindows(callback, nil); err != nil {
		return nil, fmt.Errorf("windows: EnumWindows: %w", err)
	}
	return owners, nil
}

// processImagePath returns the full path of a process image, or an error when
// this user may not ask. The full path is what sing-box reports and matches, so
// a truncated name would produce a rule that never fires.
func processImagePath(pid uint32) (string, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", fmt.Errorf("windows: open process %d: %w", pid, err)
	}
	defer windows.CloseHandle(handle)
	buffer := make([]uint16, windows.MAX_PATH)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size); err != nil {
		return "", fmt.Errorf("windows: image path of %d: %w", pid, err)
	}
	return windows.UTF16ToString(buffer[:size]), nil
}

// startMenuRoots are the directories the Start Menu is built from: the machine's
// programs and the user's own. Both are walked, including their subdirectories,
// because installers group their shortcuts into a folder per product.
func startMenuRoots() []string {
	var roots []string
	if base, err := windows.KnownFolderPath(windows.FOLDERID_CommonPrograms, windows.KF_FLAG_DEFAULT); err == nil && base != "" {
		roots = append(roots, base)
	} else if programData := strings.TrimSpace(os.Getenv("ProgramData")); programData != "" {
		roots = append(roots, filepath.Join(programData, `Microsoft\Windows\Start Menu\Programs`))
	}
	if base, err := windows.KnownFolderPath(windows.FOLDERID_Programs, windows.KF_FLAG_DEFAULT); err == nil && base != "" {
		roots = append(roots, base)
	} else {
		appData := strings.TrimSpace(os.Getenv("APPDATA"))
		if appData == "" {
			return roots
		}
		roots = append(roots, filepath.Join(appData, `Microsoft\Windows\Start Menu\Programs`))
	}
	return roots
}

// startMenuShortcutTargets returns what every Start Menu shortcut starts.
//
// A shortcut that cannot be resolved, that points at something other than an
// environment, or that an installer left pointing at itself is skipped: the
// listing reports the programs the user can choose, not the entries that failed
// to describe one. A root that does not exist is not an error.
func startMenuShortcutTargets() ([]string, error) {
	var (
		targets  []string
		firstErr error
	)
	for _, root := range startMenuRoots() {
		if _, err := os.Stat(root); err != nil {
			continue
		}
		walkErr := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				// An unreadable subdirectory must not end the walk: the rest of
				// the Start Menu still describes programs.
				if firstErr == nil {
					firstErr = err
				}
				return nil
			}
			if entry.IsDir() || !strings.EqualFold(filepath.Ext(path), ".lnk") {
				return nil
			}
			target, err := ShellLinkTarget(path)
			if err != nil || strings.TrimSpace(target) == "" {
				return nil
			}
			targets = append(targets, target)
			return nil
		})
		if walkErr != nil && firstErr == nil {
			firstErr = walkErr
		}
	}
	// A root that could not be walked at all still returns what the others
	// listed: the caller reports the error only when nothing was found.
	return targets, firstErr
}

// expandEnvironment resolves the %VAR% form of a path, which a shortcut may
// carry instead of an expanded target. Windows expands its own syntax here;
// os.ExpandEnv would look for `$VAR` and leave the path as it found it.
func expandEnvironment(path string) string {
	source, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return path
	}
	buffer := make([]uint16, maxShortcutPath)
	size, err := windows.ExpandEnvironmentStrings(source, &buffer[0], uint32(len(buffer)))
	if err != nil || size == 0 || size > uint32(len(buffer)) {
		// A path that does not fit is returned as it came: the caller decides
		// whether it describes a program, and a truncated path never would.
		return path
	}
	// The returned length includes the terminating null.
	return windows.UTF16ToString(buffer[:size-1])
}

// fileDescription returns the name a program calls itself in its version
// resource — "Google Chrome" for chrome.exe — or an empty string when the file
// has no version resource to read.
//
// The resource is language-tagged, so the caller's language is looked up in the
// file's own translation table first: reading a fixed language would give an
// empty name for a program installed in another one.
func fileDescription(path string) string {
	var ignored windows.Handle
	size, err := windows.GetFileVersionInfoSize(path, &ignored)
	if err != nil || size == 0 {
		return ""
	}
	block := make([]byte, size)
	if err := windows.GetFileVersionInfo(path, 0, size, unsafe.Pointer(&block[0])); err != nil {
		return ""
	}
	for _, translation := range versionTranslations(block) {
		for _, field := range []string{"FileDescription", "ProductName"} {
			if text := versionString(block, translation, field); text != "" {
				return text
			}
		}
	}
	return ""
}

// versionTranslation identifies one language and code page of a version resource.
type versionTranslation struct {
	language uint16
	codePage uint16
}

// versionTranslations reads the translation table of a version resource: the
// languages the file was built in.
func versionTranslations(block []byte) []versionTranslation {
	var (
		value  unsafe.Pointer
		length uint32
	)
	if err := windows.VerQueryValue(unsafe.Pointer(&block[0]), `\VarFileInfo\Translation`, unsafe.Pointer(&value), &length); err != nil {
		return nil
	}
	if length < 4 {
		return nil
	}
	out := make([]versionTranslation, 0, length/4)
	for offset := uintptr(0); offset+4 <= uintptr(length); offset += 4 {
		out = append(out, versionTranslation{
			language: *(*uint16)(unsafe.Add(value, offset)),
			codePage: *(*uint16)(unsafe.Add(value, offset+2)),
		})
	}
	return out
}

// versionString reads one string of a version resource, or an empty string when
// the resource does not carry it.
func versionString(block []byte, translation versionTranslation, field string) string {
	query := fmt.Sprintf(`\StringFileInfo\%04x%04x\%s`, translation.language, translation.codePage, field)
	var (
		value  unsafe.Pointer
		length uint32
	)
	if err := windows.VerQueryValue(unsafe.Pointer(&block[0]), query, unsafe.Pointer(&value), &length); err != nil || value == nil || length == 0 {
		return ""
	}
	// The length of a string value is counted in characters, and the text is
	// null-terminated, so it is read as UTF-16 up to that length.
	if length > maxVersionStringLength {
		length = maxVersionStringLength
	}
	text := windows.UTF16ToString(unsafe.Slice((*uint16)(value), length))
	return strings.TrimSpace(text)
}

// maxVersionStringLength bounds a version-resource string. The format has no
// such limit, and this is a value from a file, so it is read defensively.
const maxVersionStringLength = 4096
