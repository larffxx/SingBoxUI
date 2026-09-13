package desktop

import (
	"context"
	"errors"
	"sync"

	domruntime "github.com/larffxx/singboxui/internal/domain/runtime"
	"github.com/larffxx/singboxui/internal/tray"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// The menu bar is the second surface of the application, and it shows the same
// machine the window shows: the same states, in the same words
// (frontend/src/features/runtime/state.ts). Quitting from it runs the mandatory
// shutdown sequence, so the managed process never outlives the application
// (spec §26).
const (
	trayItemShowWindow = "show-window"
	trayItemStart      = "start"
	trayItemStop       = "stop"
	trayItemQuit       = "quit"
)

// trayStateLabels are the words the interface uses for the lifecycle states.
var trayStateLabels = map[domruntime.State]string{
	domruntime.StateStopped:  "Остановлен",
	domruntime.StateStarting: "Запускается",
	domruntime.StateRunning:  "Запущен",
	domruntime.StateStopping: "Останавливается",
	domruntime.StateFailed:   "Ошибка",
}

// trayPresenter renders the application state into the menu bar and routes a
// selection back into a use case. It owns no state of its own: the runtime
// supervisor is the single source of truth, and every render reads it again.
//
// A render runs on the presenter's own worker while the lifecycle arrives from
// elsewhere — the startup hook, the shutdown sequence, the menu bar's own
// event loop — so the driver is read and replaced under one lock.
type trayPresenter struct {
	app       *App
	newDriver func(tray.Options) (tray.Driver, error)
	// wake carries "the state changed" signals to the render loop, so a state
	// transition never waits for a menu bar update and never renders twice.
	wake chan struct{}
	// status reads the runtime state, and showWindow and quit are the
	// window-bound Wails calls the menu makes. They are set once, at
	// construction, from the process's own seams (spec §63).
	status     func() domruntime.Status
	showWindow func(ctx context.Context)
	quit       func(ctx context.Context)

	mu     sync.RWMutex
	driver tray.Driver
}

// newTrayPresenter builds the presenter. The status item itself is created on
// startup, not here: a presenter that is never started must not reach Cocoa.
func newTrayPresenter(app *App, deps Deps) *trayPresenter {
	newDriver := deps.NewTray
	if newDriver == nil {
		newDriver = tray.New
	}
	status := deps.RuntimeStatus
	if status == nil {
		status = func() domruntime.Status { return app.runtime.Status() }
	}
	showWindow := deps.ShowWindow
	if showWindow == nil {
		showWindow = func(ctx context.Context) {
			wruntime.WindowShow(ctx)
			wruntime.WindowUnminimise(ctx)
		}
	}
	quit := deps.Quit
	if quit == nil {
		quit = func(ctx context.Context) { wruntime.Quit(ctx) }
	}
	return &trayPresenter{
		app:        app,
		newDriver:  newDriver,
		wake:       make(chan struct{}, 1),
		status:     status,
		showWindow: showWindow,
		quit:       quit,
	}
}

// start creates the status item and begins following the application state. A
// platform without a menu bar is an expected answer, not a failure.
func (p *trayPresenter) start(ctx context.Context) {
	if p.attached() {
		return
	}
	driver, err := p.newDriver(tray.Options{
		OnSelect:  p.selectItem,
		OnReady:   p.menuBarReady,
		OnProblem: p.menuBarProblem,
	})
	if err != nil {
		if !errors.Is(err, tray.ErrUnsupported) {
			p.app.logger.Warn("the menu bar icon is unavailable", "error", err)
		}
		return
	}
	p.attach(driver)
	go p.loop(ctx)
	p.refreshed()
}

// menuBarReady records the fact that a menu bar which never appears leaves
// behind nowhere else: the logs said nothing at all when the rewrite shipped
// without one.
func (p *trayPresenter) menuBarReady() {
	p.app.logger.Info("the menu bar icon is in place")
}

// menuBarProblem keeps a menu bar that cannot draw itself out of the silent
// category.
func (p *trayPresenter) menuBarProblem(reason string) {
	p.app.logger.Warn("the menu bar could not draw the icon", "reason", reason)
}

// hide removes the status item.
func (p *trayPresenter) hide() {
	driver := p.detach()
	if driver != nil {
		driver.Hide()
	}
}

// attached reports whether a status item is being drawn.
func (p *trayPresenter) attached() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.driver != nil
}

func (p *trayPresenter) attach(driver tray.Driver) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.driver = driver
}

func (p *trayPresenter) detach() tray.Driver {
	p.mu.Lock()
	defer p.mu.Unlock()
	driver := p.driver
	p.driver = nil
	return driver
}

// refreshed schedules a render. It is called from the event path of the
// services — from inside their locks, on the goroutine that changed the state —
// so it never renders there: it signals the worker.
func (p *trayPresenter) refreshed() {
	if !p.attached() {
		return
	}
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// loop renders until the application shuts down.
func (p *trayPresenter) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.wake:
			p.render()
		}
	}
}

// render draws the current state. A failure is logged, never fatal: an icon that
// cannot be updated must not take the application with it.
func (p *trayPresenter) render() {
	p.mu.RLock()
	driver := p.driver
	p.mu.RUnlock()
	if driver == nil {
		return
	}
	if err := driver.Render(p.model(p.app.callCtx())); err != nil {
		p.app.logger.Warn("could not update the menu bar", "error", err)
	}
}

// model is the menu as the user sees it.
func (p *trayPresenter) model(ctx context.Context) tray.Model {
	status := p.status()
	profileID, profileName := p.activeProfile(ctx, status)
	state := trayStateLabels[status.State]
	if state == "" {
		state = string(status.State)
	}
	if profileName != "" && status.State == domruntime.StateRunning {
		state = state + " — " + profileName
	}
	startTitle := "Запустить"
	if profileName != "" {
		startTitle = "Запустить «" + profileName + "»"
	}
	return tray.Model{
		Icon:    trayIcon(status.State),
		Tooltip: "SingBoxUI — " + state,
		Items: []tray.Item{
			{Title: state},
			{Separator: true},
			{ID: trayItemShowWindow, Title: "Показать окно", Enabled: true},
			// The window enables Start under the same conditions: not running, no
			// transition in flight, and a profile to start.
			{ID: trayItemStart, Title: startTitle, Enabled: canStart(status.State) && profileID != ""},
			{ID: trayItemStop, Title: "Остановить", Enabled: status.State == domruntime.StateRunning},
			{Separator: true},
			{ID: trayItemQuit, Title: "Завершить SingBoxUI", Enabled: true},
		},
	}
}

// activeProfile resolves the profile the menu starts and names: the one the
// runtime last used, or the one remembered for auto-connect (spec §51). An empty
// id means there is nothing to start, and Start stays disabled.
func (p *trayPresenter) activeProfile(ctx context.Context, status domruntime.Status) (string, string) {
	id := status.ActiveProfileID
	if id == "" {
		values, err := p.app.settings.Get(ctx)
		if err != nil {
			p.app.logger.Debug("could not read the remembered profile", "error", err)
			return "", ""
		}
		id = values.Values.LastProfileID
	}
	if id == "" {
		return "", ""
	}
	profile, err := p.app.profiles.Get(ctx, id)
	if err != nil {
		// The profile is gone; the id is not a candidate to start any more.
		return "", ""
	}
	return id, profile.Name
}

// selectItem runs the use case behind the menu item the user chose. It runs on
// its own goroutine (the menu bar keeps its event loop), and a start can take
// seconds — the supervisor does the work, this only asks for it.
func (p *trayPresenter) selectItem(id string) {
	switch id {
	case trayItemShowWindow:
		p.showWindowRequest()
	case trayItemStart:
		p.startRequest()
	case trayItemStop:
		p.stopRequest()
	case trayItemQuit:
		p.quitRequest()
	}
}

// showWindowRequest brings the window back, unminimised. Without a live Wails
// context there is no window to show, and the call would panic.
func (p *trayPresenter) showWindowRequest() {
	ctx, ok := p.app.liveCtx()
	if !ok {
		return
	}
	p.showWindow(ctx)
}

// startRequest starts the profile the menu names; the supervisor rejects a
// duplicate start, so no guard is needed here (spec §23).
func (p *trayPresenter) startRequest() {
	ctx := p.app.callCtx()
	profileID, _ := p.activeProfile(ctx, p.status())
	if profileID == "" {
		return
	}
	if err := p.app.runtime.StartProfile(ctx, profileID); err != nil {
		p.app.logger.Warn("starting from the menu bar failed", "error", err, "profileId", profileID)
	}
}

// stopRequest stops the managed process gracefully.
func (p *trayPresenter) stopRequest() {
	if err := p.app.runtime.Stop(p.app.callCtx()); err != nil {
		p.app.logger.Warn("stopping from the menu bar failed", "error", err)
	}
}

// quitRequest quits the application the way the window's close button does: the
// shutdown hook runs first, so sing-box is stopped before the process exits.
func (p *trayPresenter) quitRequest() {
	ctx, ok := p.app.liveCtx()
	if !ok {
		return
	}
	p.quit(ctx)
}

// canStart mirrors the Start button of the window: a profile can be started
// while nothing runs and no transition is in flight.
func canStart(state domruntime.State) bool {
	return !state.Busy() && state != domruntime.StateRunning
}

// trayIcon names the state for the driver to draw.
func trayIcon(state domruntime.State) tray.Icon {
	switch state {
	case domruntime.StateRunning:
		return tray.IconActive
	case domruntime.StateStarting, domruntime.StateStopping:
		return tray.IconBusy
	case domruntime.StateFailed:
		return tray.IconFailed
	default:
		return tray.IconIdle
	}
}
