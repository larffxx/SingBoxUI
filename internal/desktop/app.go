package desktop

import (
	"context"
	"log/slog"
	"net/http"
	"runtime"
	"sync"
	"time"

	appbinary "github.com/larffxx/singboxui/internal/app/binary"
	configsvc "github.com/larffxx/singboxui/internal/app/config"
	"github.com/larffxx/singboxui/internal/app/profiles"
	appruntime "github.com/larffxx/singboxui/internal/app/runtime"
	appsettings "github.com/larffxx/singboxui/internal/app/settings"
	"github.com/larffxx/singboxui/internal/app/traffic"
	"github.com/larffxx/singboxui/internal/domain/apperr"
	domruntime "github.com/larffxx/singboxui/internal/domain/runtime"
	"github.com/larffxx/singboxui/internal/events"
	"github.com/larffxx/singboxui/internal/platform"
	"github.com/larffxx/singboxui/internal/privilege"
	"github.com/larffxx/singboxui/internal/singbox"
	"github.com/larffxx/singboxui/internal/storage/sqlite"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Deps are the process-level collaborators the composition root (cmd/singboxui)
// has already built. Nothing here is a package global (spec §63).
type Deps struct {
	Logger   *slog.Logger
	Store    *sqlite.Store
	Platform platform.Platform
	// Privilege is the narrow privileged-launch adapter of the platform
	// (spec §27). Only the runtime supervisor is given it.
	Privilege privilege.Runner
	// Releases is the GitHub release client. A nil client disables update
	// checks while every local capability keeps working (spec §77).
	Releases   *singbox.Client
	HTTPClient *http.Client
	// Emitter delivers events to the frontend. A nil emitter is built with the
	// Wails runtime, which is what production does; a shell or a test can inject
	// one built with an explicit sink.
	Emitter *Emitter
	// ExecPath is this application's own executable, used for autostart.
	ExecPath string
	// Version is the application version reported to the UI.
	Version string
	GOOS    string
	GOARCH  string
	Now     func() time.Time
	// OpenFileDialog is the native file picker used by the configuration
	// import. A nil value uses the Wails dialog, which needs a live window;
	// a test injects a stub so the import path stays testable (spec §63).
	OpenFileDialog func(ctx context.Context, options wruntime.OpenDialogOptions) (string, error)
}

// App is the root object bound to the frontend. It owns the application
// services built in one place and the Wails lifecycle hooks.
type App struct {
	deps    Deps
	logger  *slog.Logger
	emitter *Emitter

	store    *sqlite.Store
	plat     platform.Platform
	traffic  *traffic.Service
	binaries *appbinary.Service
	settings *appsettings.Service
	config   *configsvc.Service
	profiles *profiles.Service
	runtime  *appruntime.Supervisor

	mu          sync.RWMutex
	rootCtx     context.Context
	cancelRoot  context.CancelFunc
	shutting    bool
	shutdownErr error

	ProfileAPI  *ProfileAPI
	ConfigAPI   *ConfigAPI
	RuntimeAPI  *RuntimeAPI
	BinaryAPI   *BinaryAPI
	SettingsAPI *SettingsAPI
	ShareAPI    *ShareAPI
	TrafficAPI  *TrafficAPI
}

// runtimeLink breaks the construction cycle between the configuration service
// and the runtime supervisor: each needs the other, so both are built with this
// indirection and wired immediately afterwards.
type runtimeLink struct {
	config *configsvc.Service
	runtim *appruntime.Supervisor
}

// -- config.RuntimeControl -------------------------------------------------

func (l *runtimeLink) Status() domruntime.Status { return l.runtim.Status() }

func (l *runtimeLink) Running() bool { return l.runtim.Running() }

func (l *runtimeLink) StartProfile(ctx context.Context, profileID string) error {
	if l.runtim == nil {
		return apperr.New(apperr.CodeInternal, "desktop.runtimeLink", "runtime supervisor is not wired")
	}
	return l.runtim.StartProfile(ctx, profileID)
}

func (l *runtimeLink) Stop(ctx context.Context) error {
	if l.runtim == nil {
		return apperr.New(apperr.CodeInternal, "desktop.runtimeLink", "runtime supervisor is not wired")
	}
	return l.runtim.Stop(ctx)
}

func (l *runtimeLink) Restart(ctx context.Context) error {
	if l.runtim == nil {
		return apperr.New(apperr.CodeInternal, "desktop.runtimeLink", "runtime supervisor is not wired")
	}
	return l.runtim.Restart(ctx)
}

// -- runtime.ConfigPort ----------------------------------------------------

func (l *runtimeLink) MaterializeActive(ctx context.Context, profileID, revisionID string) (appruntime.Materialized, error) {
	if l.config == nil {
		return appruntime.Materialized{}, apperr.New(apperr.CodeInternal, "desktop.runtimeLink", "config service is not wired")
	}
	return l.config.MaterializeActive(ctx, profileID, revisionID)
}

func (l *runtimeLink) wire(config *configsvc.Service, supervisor *appruntime.Supervisor) {
	l.config = config
	l.runtim = supervisor
}

// runtimeObserver starts and stops traffic collection with the runtime
// (spec §45): the collector is a property of RUNNING, not of an open screen.
type runtimeObserver struct {
	app *App
}

func (o *runtimeObserver) RuntimeRunning(ctx context.Context, info appruntime.RunInfo) {
	if info.ClashAPIEnabled {
		o.app.traffic.Start(ctx, traffic.Target{
			BaseURL:    info.ClashAPIBaseURL,
			Secret:     info.ClashAPISecret,
			ProfileID:  info.ProfileID,
			RevisionID: info.RevisionID,
		})
		return
	}
	o.app.traffic.Stop()
}

func (o *runtimeObserver) RuntimeStopped() {
	o.app.traffic.Stop()
}

// New builds the whole application graph explicitly (spec §63).
func New(deps Deps) *App {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	if deps.GOOS == "" {
		deps.GOOS = runtime.GOOS
	}
	if deps.GOARCH == "" {
		deps.GOARCH = runtime.GOARCH
	}
	if deps.HTTPClient == nil {
		deps.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}
	if deps.OpenFileDialog == nil {
		deps.OpenFileDialog = wruntime.OpenFileDialog
	}

	root, cancel := context.WithCancel(context.Background())
	emitter := deps.Emitter
	if emitter == nil {
		emitter = NewEmitter(logger)
	}
	a := &App{
		deps:       deps,
		logger:     logger,
		emitter:    emitter,
		store:      deps.Store,
		plat:       deps.Platform,
		rootCtx:    root,
		cancelRoot: cancel,
	}
	paths := deps.Platform.Paths()

	a.binaries = appbinary.New(appbinary.Deps{
		Store:   deps.Store,
		Paths:   paths,
		Emitter: a.emitter,
		Logger:  logger,
		Client:  deps.Releases,
		Now:     deps.Now,
		GOOS:    deps.GOOS,
		GOARCH:  deps.GOARCH,
	})

	a.traffic = traffic.New(traffic.Deps{
		Emitter:    a.emitter,
		Logger:     logger,
		HTTPClient: deps.HTTPClient,
	})

	a.settings = appsettings.New(appsettings.Deps{
		Store:     deps.Store,
		Autostart: deps.Platform.Autostart(),
		Emitter:   a.emitter,
		Logger:    logger,
		Paths:     paths,
		ExecPath:  deps.ExecPath,
		BinaryPath: func(ctx context.Context) (string, error) {
			return a.binaries.Path(ctx)
		},
		ProfileExists: func(ctx context.Context, id string) (bool, error) {
			if _, err := deps.Store.GetProfile(ctx, id); err != nil {
				if apperr.IsCode(err, apperr.CodeProfileNotFound) {
					return false, nil
				}
				return false, err
			}
			return true, nil
		},
	})

	link := &runtimeLink{}
	a.config = configsvc.New(configsvc.Deps{
		Store:    deps.Store,
		Binaries: a.binaries,
		Runtime:  link,
		Paths:    paths,
		Emitter:  a.emitter,
		Logger:   logger,
	})

	a.runtime = appruntime.New(root, appruntime.Deps{
		Store:     deps.Store,
		Config:    link,
		Binaries:  a.binaries,
		Privilege: deps.Privilege,
		Emitter:   a.emitter,
		Logger:    logger,
		Paths:     paths,
		Observer:  &runtimeObserver{app: a},
	})
	link.wire(a.config, a.runtime)

	a.profiles = profiles.New(deps.Store, a.runtime, a.emitter, logger, profiles.Options{
		NewID: nil,
		Now:   deps.Now,
	})

	a.ProfileAPI = &ProfileAPI{app: a}
	a.ConfigAPI = &ConfigAPI{app: a}
	a.RuntimeAPI = &RuntimeAPI{app: a}
	a.BinaryAPI = &BinaryAPI{app: a}
	a.SettingsAPI = &SettingsAPI{app: a}
	a.ShareAPI = &ShareAPI{app: a}
	a.TrafficAPI = &TrafficAPI{app: a}
	return a
}

// Services exposes the application services for tests and for the shell's own
// needs. The frontend never sees these objects (spec §31).
func (a *App) Services() (store *sqlite.Store, runtime *appruntime.Supervisor, config *configsvc.Service, binaries *appbinary.Service) {
	return a.store, a.runtime, a.config, a.binaries
}

// OnStartup implements the Wails startup hook: it attaches event delivery and
// starts the cancellable background jobs (spec §51, §62).
func (a *App) OnStartup(ctx context.Context) {
	a.mu.Lock()
	a.shutting = false
	a.mu.Unlock()

	a.emitter.Attach(ctx)
	info := a.plat.Info()
	a.logger.Info("singboxui started",
		"version", a.deps.Version,
		"os", info.OS,
		"arch", info.Arch,
		"supported", info.Supported,
	)
	if !info.Supported {
		a.emitter.Emit(events.AppNotice, events.Notice{
			Level:      "warning",
			Title:      "Unsupported operating system",
			Message:    "SingBoxUI targets Windows and macOS. Some operations are disabled.",
			Details:    []string{info.UnsupportedReason},
			Persistent: true,
			OccurredAt: a.now().Format(time.RFC3339),
		})
	}
	go a.autoConnect()
}

// autoConnect starts the last active profile once, if the user enabled it
// (spec §51). Failures are surfaced as notices instead of staying in the logs.
func (a *App) autoConnect() {
	ctx := a.callCtx()
	if err := a.settings.AutoConnectOnStartup(ctx, a.runtime); err != nil {
		a.logger.Warn("auto-connect failed", "error", err)
		code := apperr.CodeOf(err)
		a.emitter.Emit(events.AppNotice, events.Notice{
			Level:      "error",
			Title:      "Auto-connect failed",
			Message:    apperr.MessageOf(err),
			Code:       string(code),
			Operation:  err.Error(),
			Details:    apperr.DetailsOf(err),
			Persistent: true,
			OccurredAt: a.now().Format(time.RFC3339),
		})
	}
}

// OnShutdown implements the mandatory shutdown sequence (spec §26): stop
// accepting work, stop the collector, stop sing-box, close persistence and let
// Wails exit. The VPN never outlives the application.
func (a *App) OnShutdown(context.Context) {
	a.mu.Lock()
	if a.shutting {
		a.mu.Unlock()
		return
	}
	a.shutting = true
	a.mu.Unlock()

	a.logger.Info("singboxui shutting down")
	a.cancelRoot()

	if a.traffic != nil {
		a.traffic.Stop()
	}

	if a.runtime != nil {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		if err := a.runtime.Shutdown(ctx); err != nil {
			a.logger.Error("stopping sing-box failed during shutdown", "error", err)
			a.mu.Lock()
			a.shutdownErr = err
			a.mu.Unlock()
		}
		cancel()
	}

	a.emitter.Detach()

	if a.store != nil {
		if err := a.store.Close(); err != nil {
			a.logger.Error("closing the database failed", "error", err)
		}
	}
	a.logger.Info("singboxui stopped")
}

// ShuttingDown reports whether the shutdown sequence has started.
func (a *App) ShuttingDown() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.shutting
}

// ShutdownError returns the last error raised while stopping the runtime.
func (a *App) ShutdownError() error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.shutdownErr
}

func (a *App) now() time.Time { return a.deps.Now() }

const shutdownTimeout = 25 * time.Second
