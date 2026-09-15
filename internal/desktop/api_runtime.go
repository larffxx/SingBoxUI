package desktop

import (
	"github.com/larffxx/singboxui/internal/app/runtime"
	"github.com/larffxx/singboxui/internal/domain/apperr"
	domruntime "github.com/larffxx/singboxui/internal/domain/runtime"
)

// RuntimeAPI is the runtime facade bound to the frontend (spec §31): one
// authoritative supervisor exposed as a typed snapshot, plus the log buffer
// that is streamed through Wails events.
type RuntimeAPI struct{ app *App }

// RuntimePayload is the complete runtime view of the UI: the typed state
// machine snapshot plus the context the dashboard needs (spec §22, §53).
type RuntimePayload struct {
	Status            domruntime.Status `json:"status"`
	ActiveProfileName string            `json:"activeProfileName"`
	BinaryVersion     string            `json:"binaryVersion"`
	TrafficAvailable  bool              `json:"trafficAvailable"`
	ShuttingDown      bool              `json:"shuttingDown"`
	// ForeignProcesses are sing-box processes that are running a configuration
	// and that this application did not start (ADR 012). A profile cannot start
	// while one of them holds the TUN device and the ports, so the runtime screen
	// shows them and offers to stop the ones that left a record.
	ForeignProcesses []runtime.ForeignProcess `json:"foreignProcesses"`
	Error            *apperr.Error            `json:"error,omitempty"`
}

// LogsPayload carries buffered log records (spec §47).
type LogsPayload struct {
	Records []runtime.LogRecord `json:"records"`
	Error   *apperr.Error       `json:"error,omitempty"`
}

// GetRuntimeStatus returns the current runtime snapshot.
func (a *RuntimeAPI) GetRuntimeStatus() RuntimePayload {
	return a.status(nil)
}

// StartRuntime starts the active revision of a profile. Duplicate starts are
// rejected by the supervisor, never by this facade (spec §23).
func (a *RuntimeAPI) StartRuntime(profileID string) RuntimePayload {
	if err := a.app.guard("RuntimeAPI.StartRuntime"); err != nil {
		return a.status(err)
	}
	return a.status(fail("RuntimeAPI.StartRuntime", a.app.runtime.StartProfile(a.app.callCtx(), profileID)))
}

// StopRuntime stops the managed process gracefully.
func (a *RuntimeAPI) StopRuntime() RuntimePayload {
	if err := a.app.guard("RuntimeAPI.StopRuntime"); err != nil {
		return a.status(err)
	}
	return a.status(fail("RuntimeAPI.StopRuntime", a.app.runtime.Stop(a.app.callCtx())))
}

// StopForeignProcesses stops the sing-box processes this application did not
// start (ADR 012). A process that was launched by hand has no record the
// privileged helper would accept, so it is reported rather than stopped.
func (a *RuntimeAPI) StopForeignProcesses() RuntimePayload {
	const op = "RuntimeAPI.StopForeignProcesses"
	if err := a.app.guard(op); err != nil {
		return a.status(err)
	}
	stopped, err := a.app.runtime.StopForeign(a.app.callCtx())
	if err != nil {
		return a.status(fail(op, err))
	}
	a.app.logger.Info("stopped sing-box processes the application did not own", "count", stopped)
	return a.status(nil)
}

// RestartRuntime restarts the managed process; a profile id is only needed when
// nothing is running yet.
func (a *RuntimeAPI) RestartRuntime(profileID string) RuntimePayload {
	if err := a.app.guard("RuntimeAPI.RestartRuntime"); err != nil {
		return a.status(err)
	}
	ctx := a.app.callCtx()
	if profileID != "" && a.app.runtime.ActiveProfileID() == "" {
		if err := a.app.runtime.StartProfile(ctx, profileID); err != nil {
			return a.status(fail("RuntimeAPI.RestartRuntime", err))
		}
		return a.status(nil)
	}
	return a.status(fail("RuntimeAPI.RestartRuntime", a.app.runtime.Restart(ctx)))
}

// GetLogs returns the buffered log records matching the query.
func (a *RuntimeAPI) GetLogs(query runtime.LogQuery) LogsPayload {
	return LogsPayload{Records: a.app.runtime.Logs(query)}
}

// ClearLogs empties the in-memory log buffer. The process keeps logging.
func (a *RuntimeAPI) ClearLogs() LogsPayload {
	a.app.runtime.ClearLogs()
	return LogsPayload{Records: nil}
}

// TailLogs returns the most recent log lines as plain text, for copy/export.
func (a *RuntimeAPI) TailLogs(lines int) LogsPayload {
	return LogsPayload{Records: a.app.runtime.Logs(runtime.LogQuery{Limit: lines})}
}

// status assembles the runtime payload. The profile name and binary version are
// best-effort context: a missing value never turns a successful operation into
// a failure.
func (a *RuntimeAPI) status(callErr *apperr.Error) RuntimePayload {
	out := RuntimePayload{
		Status:           a.app.runtime.Status(),
		ShuttingDown:     a.app.ShuttingDown(),
		ForeignProcesses: a.app.runtime.ForeignProcesses(),
		Error:            callErr,
	}
	out.TrafficAvailable = a.app.traffic.Running()
	ctx := a.app.callCtx()
	if out.Status.ActiveProfileID != "" {
		if p, err := a.app.profiles.Get(ctx, out.Status.ActiveProfileID); err == nil {
			out.ActiveProfileName = p.Name
		}
	}
	if v, err := a.app.binaries.Version(ctx); err == nil {
		out.BinaryVersion = v.String()
	}
	return out
}
