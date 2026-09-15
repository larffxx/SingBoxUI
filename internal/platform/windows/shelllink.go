//go:build windows

package windows

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Shortcut resolution (ADR 013).
//
// A Start Menu entry is a `.lnk` file, and only the shell knows what it points
// at: the file stores a target path, an optional item identifier list, an
// environment-variable form of the same path and, for a shortcut an installer
// created, an advertised target instead of a path. Reading that format by hand
// would mean reimplementing the shell's own resolver, so this file asks the
// shell: `IShellLinkW`, loaded through `IPersistFile::Load`.
//
// Nothing here needs a third-party COM library: the object is created with
// `CoCreateInstance`, its two interfaces are called through their vtables, and
// the identifiers, vtable indices and flags are spelled out below with the
// header each comes from.
var (
	ole32                = syscall.NewLazyDLL("ole32.dll")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
	procCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	procCoUninitialize   = ole32.NewProc("CoUninitialize")
)

// Interface and class identifiers (objbase.h, shobjidl_core.h):
//
//	IID_IShellLinkW  {000214F9-0000-0000-C000-000000000046}
//	IID_IPersistFile {0000010B-0000-0000-C000-000000000046}
//	CLSID_ShellLink  {00021401-0000-0000-C000-000000000046}
var (
	iidIShellLinkW = windows.GUID{
		Data1: 0x000214F9, Data2: 0x0000, Data3: 0x0000,
		Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46},
	}
	iidIPersistFile = windows.GUID{
		Data1: 0x0000010B, Data2: 0x0000, Data3: 0x0000,
		Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46},
	}
	clsidShellLink = windows.GUID{
		Data1: 0x00021401, Data2: 0x0000, Data3: 0x0000,
		Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46},
	}
)

// COM constants from the same headers.
const (
	// CLSCTX_INPROC_SERVER loads the shell link object into this process.
	clsctxInprocServer = 0x1
	// COINIT_APARTMENTTHREADED is the model the shell link object expects.
	coinitApartment = 0x2
	// stgmRead is STGM_READ: the shortcut is only read, never modified.
	stgmRead = 0x0
	// slgpUncPriority is SLGP_UNCPRIORITY (shobjidl_core.h). It is the flag
	// that makes GetPath return the path the shortcut was created with, in its
	// long form, instead of a short (8.3) or resolved alternative.
	slgpUncPriority = 0x00000008
	// maxShortcutPath is MAX_PATH: the buffer GetPath fills.
	maxShortcutPath = 260
)

// hrChangedMode is RPC_E_CHANGED_MODE: the thread's apartment is already
// initialized with another concurrency model, which is not a failure for a
// caller that only wants to use an in-process object.
const hrChangedMode = 0x80010106

// ShellLinkTarget returns the program a shortcut starts.
//
// An advertised shortcut — one an MSI installer created — resolves to the
// installer instead of the program (`msiexec.exe`), and a shortcut may point at
// a document, a folder or nothing at all. This function reports what the shell
// resolves and leaves the judgement to its caller.
func ShellLinkTarget(path string) (string, error) {
	// COM is apartment-bound: the object must be created and used on the thread
	// that initialized the apartment, and the go runtime may move a goroutine to
	// another thread at any call. The thread is pinned for the duration, and the
	// apartment is initialized per call, so nothing has to survive between them.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hr, _, _ := procCoInitializeEx.Call(0, coinitApartment)
	code := uint32(hr)
	if int32(code) < 0 && code != hrChangedMode {
		return "", fmt.Errorf("windows: CoInitializeEx: hr=0x%08x", code)
	}
	if code != hrChangedMode {
		// Only an initialization this call performed may be undone.
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
		return "", fmt.Errorf("windows: CoCreateInstance(ShellLink): hr=0x%08x", uint32(hr))
	}
	defer link.release()
	return link.target(path)
}

// comPointer is an interface pointer: a COM object this process does not own,
// without a Go type that could describe it. The address stays an unsafe.Pointer
// inside a struct rather than a bare uintptr, so it keeps pointing at the object
// for as long as the reference is held, and the receiver is a named type that
// the language allows methods on.
type comPointer struct {
	// addr is the interface pointer: the address of a vtable pointer.
	addr unsafe.Pointer
}

// vtableEntry returns the address of the method at index in the interface's
// vtable. Index 0 is the first method after IUnknown, whose three entries are at
// 0 (QueryInterface), 1 (AddRef) and 2 (Release).
func (p comPointer) vtableEntry(index int) uintptr {
	vtable := *(*unsafe.Pointer)(p.addr)
	return *(*uintptr)(unsafe.Add(vtable, index*int(unsafe.Sizeof(uintptr(0)))))
}

// call invokes the vtable entry at index with args and turns a failing HRESULT
// into an error.
func (p comPointer) call(index int, args ...uintptr) (uintptr, error) {
	argv := append([]uintptr{uintptr(p.addr)}, args...)
	result, _, _ := syscall.SyscallN(p.vtableEntry(index), argv...)
	if int32(uint32(result)) < 0 {
		return result, fmt.Errorf("windows: COM call %d failed: hr=0x%08x", index, uint32(result))
	}
	return result, nil
}

// release drops this reference (IUnknown::Release).
func (p comPointer) release() { _, _ = p.call(2) }

// target loads the shortcut at path and asks for its target.
//
// The indices are the ones the two vtables declare: IPersistFile::Load is the
// third method after IUnknown (GetClassID, IsDirty, Load), and
// IShellLinkW::GetPath is the first one of its own interface.
func (p comPointer) target(path string) (string, error) {
	wide, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", fmt.Errorf("windows: shortcut path %q: %w", path, err)
	}
	var persist comPointer
	if _, err := p.call(0, uintptr(unsafe.Pointer(&iidIPersistFile)), uintptr(unsafe.Pointer(&persist))); err != nil {
		return "", fmt.Errorf("windows: %s is not loadable through IPersistFile: %w", path, err)
	}
	defer persist.release()
	if _, err := persist.call(5, uintptr(unsafe.Pointer(wide)), stgmRead); err != nil {
		return "", fmt.Errorf("windows: load shortcut %s: %w", path, err)
	}
	buffer := make([]uint16, maxShortcutPath)
	if _, err := p.call(3, uintptr(unsafe.Pointer(&buffer[0])), maxShortcutPath, 0, slgpUncPriority); err != nil {
		return "", fmt.Errorf("windows: read target of %s: %w", path, err)
	}
	return windows.UTF16ToString(buffer), nil
}
