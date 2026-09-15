package desktop

import (
	"context"
	"errors"

	"github.com/larffxx/singboxui/internal/domain/apperr"
)

// fail converts any error into the stable error object the frontend branches
// on (spec §33). Human-readable messages may change; codes may not.
//
// The wrapped reasons travel with it: a typed error is encoded as JSON, where its
// unexported cause is lost, so an error that reaches the interface without them
// says "could not start sing-box" and nothing about why.
func fail(operation string, err error) *apperr.Error {
	if err == nil {
		return nil
	}
	var typed *apperr.Error
	if errors.As(err, &typed) {
		return withCauses(typed)
	}
	return withCauses(apperr.From(operation, apperr.CodeInternal, err))
}

// withCauses returns a copy of the typed error whose details end with the reasons
// it was wrapped with. Details the caller already set are kept, and one the chain
// repeats is not added twice.
func withCauses(typed *apperr.Error) *apperr.Error {
	if typed == nil {
		return nil
	}
	causes := apperr.CauseMessages(typed)
	if len(causes) == 0 {
		return typed
	}
	details := append([]string{}, typed.Details...)
	seen := make(map[string]bool, len(details))
	for _, detail := range details {
		seen[detail] = true
	}
	for _, cause := range causes {
		if seen[cause] {
			continue
		}
		seen[cause] = true
		details = append(details, cause)
	}
	out := *typed
	out.Details = details
	return &out
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
