package desktop

import (
	"github.com/larffxx/singboxui/internal/app/apps"
	"github.com/larffxx/singboxui/internal/domain/apperr"
)

// AppsAPI is the application catalog facade bound to the frontend (ADR 011).
// It lists what the operating system has installed so that the routing screen
// can offer a program by name instead of an address.
type AppsAPI struct{ app *App }

// AppsPayload is the applications surface the routing screen renders.
type AppsPayload struct {
	Apps  apps.List     `json:"apps"`
	Error *apperr.Error `json:"error,omitempty"`
}

// ListApplications returns the applications installed on this machine. A
// machine that cannot list them answers with Supported=false and the reason, so
// the screen can explain itself instead of showing a failure.
func (a *AppsAPI) ListApplications() AppsPayload {
	list, err := a.app.apps.List(a.app.callCtx())
	if err != nil {
		return AppsPayload{Apps: list, Error: fail("AppsAPI.ListApplications", err)}
	}
	return AppsPayload{Apps: list}
}
