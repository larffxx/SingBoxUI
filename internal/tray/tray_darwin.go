//go:build darwin && cgo

package tray

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Foundation -framework Cocoa

#include <stdlib.h>

#include "tray_darwin.h"
*/
import "C"

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"unsafe"
)

// The status item is a process-wide singleton, so the callbacks are
// process-wide too. They are written once, when the driver is built, and read
// from the main thread when the menu bar reports something.
var (
	callbackMu sync.RWMutex
	callback   func(string)
	ready      func()
	problem    func(string)
)

// New returns the Cocoa driver behind the macOS menu bar.
func New(opts Options) (Driver, error) {
	if opts.OnSelect == nil {
		return nil, errors.New("tray: New requires an OnSelect callback")
	}
	callbackMu.Lock()
	callback = opts.OnSelect
	ready = opts.OnReady
	problem = opts.OnProblem
	callbackMu.Unlock()
	return darwinDriver{}, nil
}

type darwinDriver struct{}

// Render replaces the icon and the menu of the status item, creating it on the
// first call.
func (darwinDriver) Render(model Model) error {
	payload, err := json.Marshal(model)
	if err != nil {
		return fmt.Errorf("tray: cannot encode the menu: %w", err)
	}
	cpayload := C.CString(string(payload))
	defer C.free(unsafe.Pointer(cpayload))
	C.sbuTrayShow(cpayload)
	return nil
}

// Hide removes the status item.
func (darwinDriver) Hide() { C.sbuTrayHide() }

//export sbuTraySelect
func sbuTraySelect(identifier *C.char) {
	// The string belongs to the caller and only lives for the duration of the
	// call, so it is copied here — the handler itself runs on its own goroutine,
	// after the menu bar has taken its event loop back.
	id := C.GoString(identifier)
	if id == "" {
		return
	}
	callbackMu.RLock()
	handler := callback
	callbackMu.RUnlock()
	if handler == nil {
		return
	}
	go handler(id)
}

//export sbuTrayReady
func sbuTrayReady() {
	callbackMu.RLock()
	handler := ready
	callbackMu.RUnlock()
	if handler != nil {
		handler()
	}
}

//export sbuTrayProblem
func sbuTrayProblem(reason *C.char) {
	// Copied for the same reason as the identifier: the string is the caller's.
	text := C.GoString(reason)
	callbackMu.RLock()
	handler := problem
	callbackMu.RUnlock()
	if handler != nil {
		handler(text)
	}
}
