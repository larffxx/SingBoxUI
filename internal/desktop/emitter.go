// Package desktop is the Wails transport layer (spec §5, §31).
//
// It owns the application lifecycle, builds the composition root and exposes
// facades that translate typed frontend calls into application use cases. It
// contains no business logic: every method accepts typed arguments, calls one
// use case and translates the result into a frontend payload.
package desktop

import (
	"context"
	"log/slog"
	"sync"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Emitter publishes application events to the React frontend through Wails
// events (spec §46, §47).
//
// The Wails context only exists between startup and shutdown, so Emit is safe
// at any time: before the frontend is attached and after it detaches, the
// payload is dropped instead of panicking. This is what lets the application
// layer depend only on internal/events.
type Emitter struct {
	logger *slog.Logger

	// sink delivers an event to the frontend. Production always uses the Wails
	// runtime; tests substitute a recording function so the event wire can be
	// asserted without a webview. It is set at construction and never mutated,
	// so Emit reads it without the lock.
	sink func(ctx context.Context, name string, payload any)

	mu    sync.RWMutex
	ctx   context.Context
	ready bool
}

// NewEmitter builds an emitter that is not yet attached to a Wails context.
func NewEmitter(logger *slog.Logger) *Emitter {
	return NewEmitterWithSink(logger, nil)
}

// NewEmitterWithSink builds an emitter that delivers events through sink
// instead of the Wails runtime. A nil sink selects the Wails runtime, so
// production behaviour is unchanged.
func NewEmitterWithSink(logger *slog.Logger, sink func(ctx context.Context, name string, payload any)) *Emitter {
	if logger == nil {
		logger = slog.Default()
	}
	if sink == nil {
		sink = func(ctx context.Context, name string, payload any) {
			wruntime.EventsEmit(ctx, name, payload)
		}
	}
	return &Emitter{logger: logger, sink: sink}
}

// Attach binds the emitter to the live Wails context.
func (e *Emitter) Attach(ctx context.Context) {
	if ctx == nil {
		return
	}
	e.mu.Lock()
	e.ctx = ctx
	e.ready = true
	e.mu.Unlock()
}

// Detach stops delivering events. It is called on shutdown before the Wails
// context becomes invalid.
func (e *Emitter) Detach() {
	e.mu.Lock()
	e.ctx = nil
	e.ready = false
	e.mu.Unlock()
}

// Emit implements events.Emitter.
func (e *Emitter) Emit(name string, payload any) {
	e.mu.RLock()
	ctx, ready := e.ctx, e.ready
	e.mu.RUnlock()
	if !ready || ctx == nil {
		return
	}
	// A panic caused by a torn-down frontend must never take down the runtime.
	defer func() {
		if r := recover(); r != nil {
			e.logger.Warn("dropping event after frontend teardown", "event", name, "error", r)
		}
	}()
	e.sink(ctx, name, payload)
}
