package desktop

import (
	"context"
	"errors"

	"github.com/larffxx/singboxui/internal/domain/apperr"
)

// fail converts any error into the stable error object the frontend branches
// on (spec §33). Human-readable messages may change; codes may not.
func fail(operation string, err error) *apperr.Error {
	if err == nil {
		return nil
	}
	var typed *apperr.Error
	if errors.As(err, &typed) {
		return typed
	}
	return apperr.From(operation, apperr.CodeInternal, err)
}

// callCtx returns the application context of a bound call. Every call is
// attached to the application lifecycle, so shutdown cancels in-flight work
// instead of leaving it running (spec §62).
func (a *App) callCtx() context.Context {
	a.mu.RLock()
	ctx := a.rootCtx
	a.mu.RUnlock()
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// guard rejects new operations once shutdown has started (spec §26).
func (a *App) guard(operation string) *apperr.Error {
	if !a.ShuttingDown() {
		return nil
	}
	return apperr.New(apperr.CodeAppShuttingDown, operation, "SingBoxUI is shutting down and no longer accepts operations")
}
