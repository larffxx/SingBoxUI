package desktop

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/larffxx/singboxui/internal/app/settings"
	domruntime "github.com/larffxx/singboxui/internal/domain/runtime"
	"github.com/larffxx/singboxui/internal/tray"
)

// fakeTrayDriver records the models the menu bar was told to draw and keeps the
// options it was built with, so a test can choose an item the way the user would.
type fakeTrayDriver struct {
	mu    sync.Mutex
	opts  tray.Options
	dirs  []tray.Model
	hides int
}

func (d *fakeTrayDriver) Render(model tray.Model) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dirs = append(d.dirs, model)
	return nil
}

func (d *fakeTrayDriver) Hide() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.hides++
}

func (d *fakeTrayDriver) configure(opts tray.Options) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.opts = opts
}

func (d *fakeTrayDriver) options() tray.Options {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.opts
}

// statusCell is what the menu bar reads while a test walks through runtime
// states: the presenter reads it on its own goroutine, so the test cannot simply
// rebind a field.
type statusCell struct {
	value atomic.Value
}

func (c *statusCell) set(status domruntime.Status) { c.value.Store(status) }

func (c *statusCell) get() domruntime.Status {
	status, _ := c.value.Load().(domruntime.Status)
	return status
}

// rendered reports whether any render so far satisfies the predicate, and
// returns the last one that does.
func (d *fakeTrayDriver) rendered(pred func(tray.Model) bool) (tray.Model, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := len(d.dirs) - 1; i >= 0; i-- {
		if pred(d.dirs[i]) {
			return d.dirs[i], true
		}
	}
	return tray.Model{}, false
}

func (d *fakeTrayDriver) waitFor(t *testing.T, what string, pred func(tray.Model) bool) tray.Model {
	t.Helper()
	var found tray.Model
	waitUntil(t, what, func() bool {
		model, ok := d.rendered(pred)
		if ok {
			found = model
		}
		return ok
	})
	return found
}

func (d *fakeTrayDriver) hidden() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.hides
}

// selectItem chooses a menu item the way the menu bar does: through the callback
// the driver was built with.
func (d *fakeTrayDriver) selectItem(t *testing.T, id string) {
	t.Helper()
	pick := d.options().OnSelect
	if pick == nil {
		t.Fatal("the menu bar never received a selection callback")
	}
	pick(id)
}

// startHarness builds the application and runs the startup hook, which is where
// the menu bar appears.
func startHarness(t *testing.T, options ...func(*Deps)) *harness {
	t.Helper()
	h := newHarness(t, options...)
	h.app.OnStartup(context.Background())
	return h
}

// item finds a menu row by its identifier and fails the test when it is missing.
func item(t *testing.T, model tray.Model, id string) tray.Item {
	t.Helper()
	for _, row := range model.Items {
		if row.ID == id {
			return row
		}
	}
	t.Fatalf("the menu has no %q item: %+v", id, model.Items)
	return tray.Item{}
}

// rememberProfile makes a profile the one the menu bar starts, the way the
// window's profile screen does (spec §51).
func rememberProfile(t *testing.T, h *harness, id string) {
	t.Helper()
	last := id
	if payload := h.app.SettingsAPI.UpdateSettings(settings.UpdateInput{LastProfileID: &last}); payload.Error != nil {
		t.Fatalf("UpdateSettings failed: %+v", payload.Error)
	}
}

// TestMenuBarAppearsOnStartup: the icon appears with the window, and it shows
// the state the runtime is in (spec §10, §26).
func TestMenuBarAppearsOnStartup(t *testing.T) {
	h := startHarness(t)

	model := h.tray.waitFor(t, "the icon to appear", func(tray.Model) bool { return true })
	if model.Icon != tray.IconIdle {
		t.Errorf("icon = %q, want %q", model.Icon, tray.IconIdle)
	}
	status := model.Items[0]
	if status.Title != "Остановлен" || status.Enabled {
		t.Errorf("status line = %+v, want a disabled «Остановлен»", status)
	}
	for _, id := range []string{trayItemShowWindow, trayItemQuit} {
		if !item(t, model, id).Enabled {
			t.Errorf("%s is disabled, want it available", id)
		}
	}
	// Nothing has ever been started, so there is no profile the menu could start.
	if start := item(t, model, trayItemStart); start.Enabled {
		t.Error("Start is enabled although no profile can be started")
	}
	if stop := item(t, model, trayItemStop); stop.Enabled {
		t.Error("Stop is enabled although nothing runs")
	}
}

// TestMenuBarNamesTheProfileToStart: with a remembered profile the menu says
// which one it starts, and Start becomes available.
func TestMenuBarNamesTheProfileToStart(t *testing.T) {
	h := startHarness(t)
	id := h.createProfile(t, "Основной", "tun-basic")

	rememberProfile(t, h, id)
	h.app.tray.refreshed()

	model := h.tray.waitFor(t, "the menu to name the profile", func(model tray.Model) bool {
		return item(t, model, trayItemStart).Enabled
	})
	if got := item(t, model, trayItemStart).Title; got != "Запустить «Основной»" {
		t.Errorf("Start title = %q, want %q", got, "Запустить «Основной»")
	}
}

// TestMenuBarFollowsEveryState pins the menu of each lifecycle state: the icon,
// the words and what can be chosen (frontend/src/features/runtime/state.ts).
func TestMenuBarFollowsEveryState(t *testing.T) {
	cell := &statusCell{}
	cell.set(domruntime.Status{State: domruntime.StateStopped})
	h := startHarness(t, func(deps *Deps) { deps.RuntimeStatus = cell.get })
	profileID := h.createProfile(t, "Основной", "tun-basic")

	cases := []struct {
		name     string
		state    domruntime.State
		icon     tray.Icon
		status   string
		canStart bool
		canStop  bool
	}{
		{name: "stopped", state: domruntime.StateStopped, icon: tray.IconIdle, status: "Остановлен", canStart: true},
		{name: "starting", state: domruntime.StateStarting, icon: tray.IconBusy, status: "Запускается"},
		{name: "running", state: domruntime.StateRunning, icon: tray.IconActive, status: "Запущен — Основной", canStop: true},
		{name: "stopping", state: domruntime.StateStopping, icon: tray.IconBusy, status: "Останавливается"},
		{name: "failed", state: domruntime.StateFailed, icon: tray.IconFailed, status: "Ошибка", canStart: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cell.set(domruntime.Status{State: tc.state, ActiveProfileID: profileID})
			h.app.tray.refreshed()

			model := h.tray.waitFor(t, "the menu to show "+tc.name, func(model tray.Model) bool {
				return model.Icon == tc.icon && model.Items[0].Title == tc.status
			})
			if got := model.Items[0].Title; got != tc.status {
				t.Errorf("status line = %q, want %q", got, tc.status)
			}
			if got := item(t, model, trayItemStart).Enabled; got != tc.canStart {
				t.Errorf("Start enabled = %v, want %v", got, tc.canStart)
			}
			if got := item(t, model, trayItemStop).Enabled; got != tc.canStop {
				t.Errorf("Stop enabled = %v, want %v", got, tc.canStop)
			}
			if !item(t, model, trayItemShowWindow).Enabled || !item(t, model, trayItemQuit).Enabled {
				t.Errorf("Show window and Quit must stay available in state %s", tc.state)
			}
		})
	}
}

// TestMenuBarFollowsAProfileRename: the menu names what it would start, so a
// rename has to reach it (spec §46: the menu follows events, it does not poll).
func TestMenuBarFollowsAProfileRename(t *testing.T) {
	h := startHarness(t)
	id := h.createProfile(t, "Основной", "tun-basic")
	rememberProfile(t, h, id)

	h.app.tray.refreshed()
	h.tray.waitFor(t, "the menu to name the original profile", func(model tray.Model) bool {
		return item(t, model, trayItemStart).Title == "Запустить «Основной»"
	})

	if payload := h.app.ProfileAPI.RenameProfile(id, "Рабочий", ""); payload.Error != nil {
		t.Fatalf("RenameProfile failed: %+v", payload.Error)
	}

	h.tray.waitFor(t, "the menu to follow the rename", func(model tray.Model) bool {
		return item(t, model, trayItemStart).Title == "Запустить «Рабочий»"
	})
}

// TestMenuBarQuitAsksTheApplicationToQuit: quitting runs the shutdown sequence
// (the Wails quit hook), it does not merely remove an icon (spec §26).
func TestMenuBarQuitAsksTheApplicationToQuit(t *testing.T) {
	asked := make(chan struct{}, 1)
	h := startHarness(t, func(deps *Deps) {
		deps.Quit = func(context.Context) { asked <- struct{}{} }
	})

	h.tray.selectItem(t, trayItemQuit)

	select {
	case <-asked:
	default:
		t.Error("choosing Quit did not ask the application to quit")
	}
}

// TestMenuBarWithoutAWailsSessionDoesNothing: window-bound Wails calls panic
// outside a session, so a selection must not make one (spec §63).
func TestMenuBarWithoutAWailsSessionDoesNothing(t *testing.T) {
	h := newHarness(t, func(deps *Deps) {
		deps.ShowWindow = func(context.Context) { t.Error("Show window reached Wails without a session") }
		deps.Quit = func(context.Context) { t.Error("Quit reached Wails without a session") }
	})

	h.app.tray.selectItem(trayItemShowWindow)
	h.app.tray.selectItem(trayItemQuit)
}

// TestMenuBarStartAsksTheSupervisor is the whole path in one test: choosing
// Start runs the supervisor, the transition reaches the icon, and the failure
// leaves a menu that can be used again.
func TestMenuBarStartAsksTheSupervisor(t *testing.T) {
	h := startHarness(t)
	id := h.createProfile(t, "Основной", "tun-basic")
	rememberProfile(t, h, id)
	fakeBinary := placeFakeSingbox(t, h.dir, "sing-box")
	if source := h.app.BinaryAPI.SetBinarySource("custom", fakeBinary); source.Error != nil {
		t.Fatalf("SetBinarySource: %+v", source.Error)
	}

	h.app.tray.startRequest()

	// The start reaches the privileged launch, which this platform refuses: that
	// is the failure the menu has to survive.
	if h.plat.priv.startCount() == 0 {
		t.Fatal("the privileged launcher was never asked, so the start did not get that far")
	}
	// Renders coalesce — a transition that fails immediately may skip the busy
	// icon — but the failure is the state the user is left with.
	model := h.tray.waitFor(t, "the icon to show the failure", func(model tray.Model) bool {
		return model.Icon == tray.IconFailed
	})
	if got := model.Items[0].Title; got != "Ошибка" {
		t.Errorf("status line = %q, want %q", got, "Ошибка")
	}
	if stop := item(t, model, trayItemStop); stop.Enabled {
		t.Error("Stop is enabled after a failed start")
	}
	if start := item(t, model, trayItemStart); !start.Enabled {
		t.Error("Start is disabled after a failed start, so the menu cannot retry")
	}

	status := h.app.RuntimeAPI.GetRuntimeStatus()
	if status.Status.ActiveProfileID != id {
		t.Errorf("active profile = %q, want %q: the menu bar starts the profile it names",
			status.Status.ActiveProfileID, id)
	}
}

// TestMenuBarReportsItself: the driver is built with all three callbacks, and the
// ones the application acts on do not panic. A menu bar that never appears is
// otherwise invisible in the logs, which is exactly how the rewrite shipped
// without one and nobody could tell why.
func TestMenuBarReportsItself(t *testing.T) {
	h := startHarness(t)

	opts := h.tray.options()
	if opts.OnSelect == nil || opts.OnReady == nil || opts.OnProblem == nil {
		t.Fatalf("driver options = %+v, want OnSelect, OnReady and OnProblem", opts)
	}
	opts.OnReady()
	opts.OnProblem("no symbol named shield on this macOS")
}

// TestMenuBarDisappearsOnShutdown: the icon goes away with the application.
func TestMenuBarDisappearsOnShutdown(t *testing.T) {
	h := startHarness(t)

	h.app.OnShutdown(context.Background())

	if h.tray.hidden() == 0 {
		t.Error("the menu bar item was not removed during shutdown")
	}
}

// TestMenuBarIsOptional: a platform without a menu bar is an ordinary answer,
// not a failure (docs/architecture/feature-matrix.md row 26).
func TestMenuBarIsOptional(t *testing.T) {
	h := newHarness(t, func(deps *Deps) {
		deps.NewTray = func(tray.Options) (tray.Driver, error) { return nil, tray.ErrUnsupported }
	})

	h.app.OnStartup(context.Background())

	if h.app.tray.attached() {
		t.Error("a driver exists on a platform without a menu bar")
	}
}
