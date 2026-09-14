//go:build windows

package windows

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/larffxx/singboxui/internal/domain/applications"
)

// The machine itself is covered here: the real Start Menu, the real processes
// with windows, the real shell's shortcut reader and the real version resource.
// The rules that turn those answers into a listing are covered without a machine
// in applications_test.go.

// saveShortcut writes a real `.lnk` through the shell object the production code
// reads, so the reader is exercised against the format the shell actually
// produces.
//
// A session without the shell's COM objects — a CI service session — cannot
// create that object at all, and it is the fixture *and* the production reader:
// the test skips instead of failing, because the machine, not the reader, is what
// is missing.
func saveShortcut(t *testing.T, path, target string) {
	t.Helper()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hr, _, _ := procCoInitializeEx.Call(0, coinitApartment)
	code := uint32(hr)
	if int32(code) < 0 && code != hrChangedMode {
		t.Skipf("this session cannot initialize COM (hr=0x%08x)", code)
	}
	if code != hrChangedMode {
		defer procCoUninitialize.Call()
	}

	var link comPointer
	hr, _, _ = procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidShellLink)),
		0,
		clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIShellLinkW)),
		uintptr(unsafe.Pointer(&link)),
	)
	if int32(uint32(hr)) < 0 || link.addr == nil {
		t.Skipf("the shell's shortcut object is unavailable in this session (hr=0x%08x)", uint32(hr))
	}
	defer link.release()

	wideTarget, err := windows.UTF16PtrFromString(target)
	if err != nil {
		t.Fatalf("target %q: %v", target, err)
	}
	// IShellLinkW::SetPath is the eighteenth method of its own interface (after
	// IUnknown's three): GetPath, GetIDList, SetIDList, GetDescription,
	// SetDescription, GetWorkingDirectory, SetWorkingDirectory, GetArguments,
	// SetArguments, GetHotkey, SetHotkey, GetShowCmd, SetShowCmd, GetIconLocation,
	// SetIconLocation, SetRelativePath, Resolve, SetPath.
	if _, err := link.call(20, uintptr(unsafe.Pointer(wideTarget))); err != nil {
		t.Fatalf("SetPath: %v", err)
	}
	var persist comPointer
	if _, err := link.call(0, uintptr(unsafe.Pointer(&iidIPersistFile)), uintptr(unsafe.Pointer(&persist))); err != nil {
		t.Fatalf("IPersistFile: %v", err)
	}
	defer persist.release()
	widePath, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatalf("path %q: %v", path, err)
	}
	// IPersistFile::Save is the fourth method after IUnknown.
	if _, err := persist.call(6, uintptr(unsafe.Pointer(widePath)), 0); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

func TestShellLinkTargetReadsAShortcutTheShellWrote(t *testing.T) {
	dir := t.TempDir()
	target := program(t, filepath.Join(dir, "Programs"), "tool.exe")
	shortcut := filepath.Join(dir, "Tool.lnk")
	saveShortcut(t, shortcut, target)

	got, err := ShellLinkTarget(shortcut)
	if err != nil {
		t.Fatalf("ShellLinkTarget: %v", err)
	}
	// The same *file* is what matters: the shell answers in the form the shortcut
	// carries, which can be the long or the short (8.3) form of the path.
	if !strings.EqualFold(got, target) && !sameFile(t, got, target) {
		t.Errorf("ShellLinkTarget = %q, want %q", got, target)
	}
}

// sameFile reports whether two paths name the same file, whatever form each of
// them is written in.
func sameFile(t *testing.T, left, right string) bool {
	t.Helper()
	first, err := os.Stat(left)
	if err != nil {
		return false
	}
	second, err := os.Stat(right)
	if err != nil {
		return false
	}
	return os.SameFile(first, second)
}

func TestShellLinkTargetRefusesAFileThatIsNotAShortcut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("not a shortcut"), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if _, err := ShellLinkTarget(path); err == nil {
		t.Error("a plain file was read as a shortcut")
	}
}

func TestStartMenuShortcutTargetsResolveThisMachine(t *testing.T) {
	roots := startMenuRoots()
	if len(roots) == 0 {
		t.Skip("this machine has no Start Menu to read")
	}
	targets, _ := startMenuShortcutTargets()
	if len(targets) == 0 {
		t.Skipf("no shortcut under %v could be resolved", roots)
	}
	// What a target *is* is not asserted here: the shell answers with the form the
	// shortcut was created in, which may carry %VAR% and is expanded (or dropped)
	// when the listing is assembled. That a target comes back at all is the half
	// of the reader that only a machine can prove.
	for _, target := range targets {
		if strings.TrimSpace(target) == "" {
			t.Error("a resolved target is empty")
		}
	}
}

// TestApplicationsListsThisMachine keeps the whole path honest on a Windows
// machine: the real processes, the real Start Menu, the real shell and the real
// version resources.
func TestApplicationsListsThisMachine(t *testing.T) {
	catalog := NewApplications(t.TempDir())
	listed, err := catalog.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) == 0 {
		// A machine whose programs all live in the Windows directory — a fresh
		// runner, or a session with no interactive desktop — answers with a valid
		// empty listing: nothing outside the operating system is installed or
		// running there. How a listing is assembled is covered without a machine
		// in applications_test.go.
		t.Skip("this machine offers no program outside the Windows directory")
	}
	seen := make(map[string]struct{}, len(listed))
	for _, item := range listed {
		if item.Name == "" || item.Path == "" || item.Executable == "" ||
			item.MatchKey != applications.MatchProcessName || item.MatchValue == "" {
			t.Fatalf("incomplete entry: %+v", item)
		}
		if !strings.EqualFold(item.MatchValue, filepath.Base(item.Path)) {
			t.Errorf("%s: MatchValue = %q, want the file name of %q", item.Name, item.MatchValue, item.Path)
		}
		if !strings.EqualFold(filepath.Ext(item.Path), executableSuffix) {
			t.Errorf("%s: %q is not an image", item.Name, item.Path)
		}
		if withinDirectory(item.Path, catalog.windowsDir) {
			t.Errorf("%s: %q belongs to the operating system", item.Name, item.Path)
		}
		if _, err := os.Stat(item.Path); err != nil {
			t.Errorf("%s: %q cannot be stat'ed: %v", item.Name, item.Path, err)
		}
		key := strings.ToLower(item.MatchValue)
		if _, dup := seen[key]; dup {
			t.Errorf("%s: two rows carry the same condition %q", item.Name, item.MatchValue)
		}
		seen[key] = struct{}{}
	}
	for i := 1; i < len(listed); i++ {
		previous, current := strings.ToLower(listed[i-1].Name), strings.ToLower(listed[i].Name)
		if previous > current {
			t.Fatalf("the listing is not sorted by name: %q before %q", listed[i-1].Name, listed[i].Name)
		}
	}
}

func TestFileDescriptionReadsAVersionResource(t *testing.T) {
	// The command processor carries a version resource on every Windows, in the
	// language of the installation: only that it is readable is asserted here.
	if got := fileDescription(`C:\Windows\System32\cmd.exe`); got == "" {
		t.Error("the version resource of cmd.exe could not be read")
	}
	// A file without a version resource answers with nothing, which is what makes
	// the file name the fallback of the listing.
	plain := filepath.Join(t.TempDir(), "plain.exe")
	if err := os.WriteFile(plain, []byte("MZ"), 0o644); err != nil {
		t.Fatalf("write %s: %v", plain, err)
	}
	if got := fileDescription(plain); got != "" {
		t.Errorf("fileDescription(%s) = %q, want an empty name", plain, got)
	}
}

func TestProcessImagePathDescribesARealProcess(t *testing.T) {
	// The path sing-box matches is this one, so it must be the full image path of
	// a real process.
	got, err := processImagePath(uint32(syscall.Getpid()))
	if err != nil {
		t.Fatalf("processImagePath: %v", err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	if !strings.EqualFold(got, executable) {
		t.Errorf("processImagePath = %q, want %q", got, executable)
	}
}
