package settings

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/domain/settings"
	"github.com/larffxx/singboxui/internal/events"
	"github.com/larffxx/singboxui/internal/platform"
)

// fakeStore is an in-memory ProfileStore. It counts writes so a test can prove
// that a rejected update never reaches persistence.
type fakeStore struct {
	mu       sync.Mutex
	values   settings.Settings
	loadErr  error
	saveErr  error
	saveCall int
	saved    []settings.Settings
}

func newFakeStore(values settings.Settings) *fakeStore {
	return &fakeStore{values: values}
}

func (s *fakeStore) LoadSettings(context.Context) (settings.Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return settings.Settings{}, s.loadErr
	}
	return s.values, nil
}

func (s *fakeStore) SaveSettings(_ context.Context, value settings.Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saveCall++
	if s.saveErr != nil {
		return s.saveErr
	}
	s.values = value
	s.saved = append(s.saved, value)
	return nil
}

func (s *fakeStore) writes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveCall
}

// fakeAutostart is the platform port, recording every call it receives.
type fakeAutostart struct {
	supported   bool
	enabled     bool
	legacyEntry string

	enabledCalls  []string
	disableCalls  int
	removeLegacy  int
	enableErr     error
	disableErr    error
	enabledErr    error
	removeLegacyE error
}

func (a *fakeAutostart) Supported() bool { return a.supported }

func (a *fakeAutostart) Enabled() (bool, error) {
	if a.enabledErr != nil {
		return false, a.enabledErr
	}
	return a.enabled, nil
}

func (a *fakeAutostart) Enable(execPath string) error {
	a.enabledCalls = append(a.enabledCalls, execPath)
	if a.enableErr != nil {
		return a.enableErr
	}
	a.enabled = true
	return nil
}

func (a *fakeAutostart) Disable() error {
	a.disableCalls++
	if a.disableErr != nil {
		return a.disableErr
	}
	a.enabled = false
	return nil
}

func (a *fakeAutostart) LegacyEntry() string { return a.legacyEntry }

func (a *fakeAutostart) RemoveLegacy() error {
	a.removeLegacy++
	if a.removeLegacyE != nil {
		return a.removeLegacyE
	}
	a.legacyEntry = ""
	return nil
}

type recordedEvent struct {
	name    string
	payload any
}

type fakeEmitter struct {
	mu     sync.Mutex
	events []recordedEvent
}

func (e *fakeEmitter) Emit(name string, payload any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, recordedEvent{name: name, payload: payload})
}

func (e *fakeEmitter) all() []recordedEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]recordedEvent(nil), e.events...)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func testPaths() platform.Paths {
	return platform.Paths{
		DataDir: "/tmp/singboxui-test",
		LogPath: "/tmp/singboxui-test/logs/app.log",
	}
}

func boolPtr(v bool) *bool                                     { return &v }
func strPtr(v string) *string                                  { return &v }
func sourcePtr(v settings.BinarySource) *settings.BinarySource { return &v }
func themePtr(v settings.Theme) *settings.Theme                { return &v }
func levelPtr(v settings.LogLevel) *settings.LogLevel          { return &v }

func TestGetReportsStoredValuesPlatformStateAndPaths(t *testing.T) {
	t.Parallel()

	store := newFakeStore(settings.Settings{
		AutoConnect:          true,
		BinarySource:         settings.BinaryManaged,
		ManagedStableChannel: true,
		UpdateCheckEnabled:   true,
		Theme:                settings.ThemeDark,
		LogLevel:             settings.LogWarn,
		LastProfileID:        "p-1",
	})
	autostart := &fakeAutostart{supported: true, enabled: true, legacyEntry: "com.larffxx.singboxui.plist"}
	svc := New(Deps{
		Store:     store,
		Autostart: autostart,
		Logger:    discardLogger(),
		Paths:     testPaths(),
		BinaryPath: func(context.Context) (string, error) {
			return "/tmp/bin/sing-box", nil
		},
	})

	state, err := svc.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() = %v, want nil", err)
	}
	if state.Values.Theme != settings.ThemeDark || state.Values.LogLevel != settings.LogWarn {
		t.Errorf("Values = %+v, want the stored theme and log level", state.Values)
	}
	if !state.Autostart.Supported || !state.Autostart.Enabled {
		t.Errorf("Autostart = %+v, want supported and enabled", state.Autostart)
	}
	if state.Autostart.LegacyEntry != "com.larffxx.singboxui.plist" {
		t.Errorf("LegacyEntry = %q, want the platform entry", state.Autostart.LegacyEntry)
	}
	if state.DataDir != testPaths().DataDir || state.LogPath != testPaths().LogPath {
		t.Errorf("paths = %q/%q, want %q/%q", state.DataDir, state.LogPath, testPaths().DataDir, testPaths().LogPath)
	}
	if state.BinaryPath != "/tmp/bin/sing-box" {
		t.Errorf("BinaryPath = %q, want the resolved executable", state.BinaryPath)
	}
}

func TestGetSurfacesAutostartAndBinaryPathProblemsWithoutFailing(t *testing.T) {
	t.Parallel()

	store := newFakeStore(settings.Default())
	autostart := &fakeAutostart{supported: true, enabledErr: errors.New("launchctl not reachable")}
	svc := New(Deps{
		Store:     store,
		Autostart: autostart,
		Logger:    discardLogger(),
		Paths:     testPaths(),
		BinaryPath: func(context.Context) (string, error) {
			return "", apperr.New(apperr.CodeBinaryNotFound, "settings.binaryPath", "no binary")
		},
	})

	state, err := svc.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() = %v, want nil: an unreadable autostart entry is a UI state, not a failure", err)
	}
	if !strings.Contains(state.Autostart.Error, "launchctl") {
		t.Errorf("Autostart.Error = %q, want the platform message", state.Autostart.Error)
	}
	if state.Autostart.Enabled {
		t.Error("Autostart.Enabled = true, want false when the state could not be read")
	}
	if state.BinaryPath != "" {
		t.Errorf("BinaryPath = %q, want empty when resolution failed", state.BinaryPath)
	}
}

func TestGetPropagatesStoreFailure(t *testing.T) {
	t.Parallel()

	store := newFakeStore(settings.Default())
	store.loadErr = errors.New("database is closed")
	svc := New(Deps{Store: store, Logger: discardLogger(), Paths: testPaths()})

	if _, err := svc.Get(context.Background()); err == nil {
		t.Fatal("Get() = nil, want the store error")
	}
}

func TestUpdateTouchesOnlyTheProvidedFields(t *testing.T) {
	t.Parallel()

	initial := settings.Settings{
		AutoStartApplication: false,
		AutoConnect:          true,
		BinarySource:         settings.BinaryManaged,
		ManagedStableChannel: true,
		UpdateCheckEnabled:   true,
		Theme:                settings.ThemeSystem,
		LogLevel:             settings.LogInfo,
		LastProfileID:        "",
	}

	tests := []struct {
		name  string
		input UpdateInput
		check func(t *testing.T, got settings.Settings)
	}{
		{
			name:  "theme only",
			input: UpdateInput{Theme: themePtr(settings.ThemeDark)},
			check: func(t *testing.T, got settings.Settings) {
				if got.Theme != settings.ThemeDark {
					t.Errorf("Theme = %q, want dark", got.Theme)
				}
				if got.LogLevel != settings.LogInfo {
					t.Errorf("LogLevel = %q, want the untouched info", got.LogLevel)
				}
				if !got.AutoConnect {
					t.Error("AutoConnect was reset by a theme-only update")
				}
			},
		},
		{
			name:  "log level and update check",
			input: UpdateInput{LogLevel: levelPtr(settings.LogError), UpdateCheckEnabled: boolPtr(false)},
			check: func(t *testing.T, got settings.Settings) {
				if got.LogLevel != settings.LogError {
					t.Errorf("LogLevel = %q, want error", got.LogLevel)
				}
				if got.UpdateCheckEnabled {
					t.Error("UpdateCheckEnabled = true, want the explicit false")
				}
				if !got.ManagedStableChannel {
					t.Error("ManagedStableChannel was reset by an unrelated update")
				}
			},
		},
		{
			name:  "switching to a custom binary",
			input: UpdateInput{BinarySource: sourcePtr(settings.BinaryCustom), CustomBinaryPath: strPtr("/opt/sing-box")},
			check: func(t *testing.T, got settings.Settings) {
				if got.BinarySource != settings.BinaryCustom || got.CustomBinaryPath != "/opt/sing-box" {
					t.Errorf("binary selection = %q/%q, want custom /opt/sing-box", got.BinarySource, got.CustomBinaryPath)
				}
			},
		},
		{
			name:  "auto connect only",
			input: UpdateInput{AutoConnect: boolPtr(false)},
			check: func(t *testing.T, got settings.Settings) {
				if got.AutoConnect {
					t.Error("AutoConnect = true, want the explicit false")
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := newFakeStore(initial)
			svc := New(Deps{Store: store, Logger: discardLogger(), Paths: testPaths()})

			state, err := svc.Update(context.Background(), tc.input)
			if err != nil {
				t.Fatalf("Update() = %v, want nil", err)
			}
			tc.check(t, state.Values)
			if store.writes() != 1 {
				t.Errorf("store writes = %d, want exactly 1", store.writes())
			}
		})
	}
}

func TestUpdateTrimsWhitespaceFromTextFields(t *testing.T) {
	t.Parallel()

	store := newFakeStore(settings.Default())
	svc := New(Deps{
		Store:     store,
		Logger:    discardLogger(),
		Paths:     testPaths(),
		Autostart: &fakeAutostart{supported: true},
	})

	state, err := svc.Update(context.Background(), UpdateInput{
		BinarySource:     sourcePtr(settings.BinaryCustom),
		CustomBinaryPath: strPtr("  /opt/sing-box  "),
		LastProfileID:    strPtr("\t p-9 \n"),
	})
	if err != nil {
		t.Fatalf("Update() = %v, want nil", err)
	}
	if state.Values.CustomBinaryPath != "/opt/sing-box" {
		t.Errorf("CustomBinaryPath = %q, want the trimmed path", state.Values.CustomBinaryPath)
	}
	if state.Values.LastProfileID != "p-9" {
		t.Errorf("LastProfileID = %q, want the trimmed id", state.Values.LastProfileID)
	}
}

func TestUpdateRejectsInvalidValuesBeforeWriting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input UpdateInput
	}{
		{name: "unknown theme", input: UpdateInput{Theme: themePtr(settings.Theme("neon"))}},
		{name: "unknown log level", input: UpdateInput{LogLevel: levelPtr(settings.LogLevel("trace"))}},
		{name: "unknown binary source", input: UpdateInput{BinarySource: sourcePtr(settings.BinarySource("system"))}},
		{name: "custom source with a blank path", input: UpdateInput{BinarySource: sourcePtr(settings.BinaryCustom), CustomBinaryPath: strPtr("   ")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := newFakeStore(settings.Default())
			svc := New(Deps{Store: store, Logger: discardLogger(), Paths: testPaths()})

			_, err := svc.Update(context.Background(), tc.input)
			if err == nil {
				t.Fatal("Update() = nil, want a rejection")
			}
			if code := apperr.CodeOf(err); code != apperr.CodeInvalidArgument {
				t.Errorf("code = %s, want %s", code, apperr.CodeInvalidArgument)
			}
			if store.writes() != 0 {
				t.Errorf("store writes = %d, want 0: an invalid update must not be persisted", store.writes())
			}
		})
	}
}

func TestUpdatePropagatesStoreWriteFailure(t *testing.T) {
	t.Parallel()

	store := newFakeStore(settings.Default())
	store.saveErr = errors.New("disk is full")
	autostart := &fakeAutostart{supported: true}
	svc := New(Deps{Store: store, Autostart: autostart, Logger: discardLogger(), Paths: testPaths()})

	_, err := svc.Update(context.Background(), UpdateInput{Theme: themePtr(settings.ThemeDark)})
	if err == nil {
		t.Fatal("Update() = nil, want the store error")
	}
	if len(autostart.enabledCalls) != 0 {
		t.Error("autostart was applied even though the settings were not saved")
	}
}

func TestUpdateAppliesAutostartThroughThePlatformPort(t *testing.T) {
	t.Parallel()

	t.Run("enabling passes the executable path", func(t *testing.T) {
		t.Parallel()
		store := newFakeStore(settings.Default())
		autostart := &fakeAutostart{supported: true}
		svc := New(Deps{
			Store:     store,
			Autostart: autostart,
			Logger:    discardLogger(),
			Paths:     testPaths(),
			ExecPath:  "/Applications/SingBoxUI.app/Contents/MacOS/SingBoxUI",
		})

		state, err := svc.Update(context.Background(), UpdateInput{AutoStartApplication: boolPtr(true)})
		if err != nil {
			t.Fatalf("Update() = %v, want nil", err)
		}
		want := []string{"/Applications/SingBoxUI.app/Contents/MacOS/SingBoxUI"}
		if len(autostart.enabledCalls) != 1 || autostart.enabledCalls[0] != want[0] {
			t.Errorf("Enable calls = %q, want %q", autostart.enabledCalls, want)
		}
		if autostart.disableCalls != 0 {
			t.Errorf("Disable calls = %d, want 0", autostart.disableCalls)
		}
		if !state.Values.AutoStartApplication || !state.Autostart.Enabled {
			t.Errorf("state = %+v, want autostart on in both the settings and the platform report", state)
		}
		if got := store.values.AutoStartApplication; !got {
			t.Error("the autostart flag was not persisted")
		}
	})

	t.Run("disabling removes the entry", func(t *testing.T) {
		t.Parallel()
		initial := settings.Default()
		initial.AutoStartApplication = true
		store := newFakeStore(initial)
		autostart := &fakeAutostart{supported: true, enabled: true}
		svc := New(Deps{Store: store, Autostart: autostart, Logger: discardLogger(), Paths: testPaths()})

		state, err := svc.Update(context.Background(), UpdateInput{AutoStartApplication: boolPtr(false)})
		if err != nil {
			t.Fatalf("Update() = %v, want nil", err)
		}
		if autostart.disableCalls != 1 {
			t.Errorf("Disable calls = %d, want 1", autostart.disableCalls)
		}
		if len(autostart.enabledCalls) != 0 {
			t.Errorf("Enable calls = %q, want none", autostart.enabledCalls)
		}
		if state.Values.AutoStartApplication || state.Autostart.Enabled {
			t.Errorf("state = %+v, want autostart off", state)
		}
	})

	t.Run("no autostart field means no platform call", func(t *testing.T) {
		t.Parallel()
		store := newFakeStore(settings.Default())
		autostart := &fakeAutostart{supported: true}
		svc := New(Deps{Store: store, Autostart: autostart, Logger: discardLogger(), Paths: testPaths()})

		if _, err := svc.Update(context.Background(), UpdateInput{AutoConnect: boolPtr(true)}); err != nil {
			t.Fatalf("Update() = %v, want nil", err)
		}
		if len(autostart.enabledCalls) != 0 || autostart.disableCalls != 0 {
			t.Errorf("autostart was touched by an unrelated update: enable=%q disable=%d",
				autostart.enabledCalls, autostart.disableCalls)
		}
	})
}

func TestUpdateAutostartEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		autostart  *fakeAutostart
		execPath   string
		enabled    bool
		wantCode   apperr.Code
		wantEnable int
	}{
		{
			name:      "enabling on a platform without support is rejected",
			autostart: &fakeAutostart{supported: false},
			execPath:  "/bin/app",
			enabled:   true,
			wantCode:  apperr.CodeInvalidArgument,
		},
		{
			name:      "enabling without an application path is refused",
			autostart: &fakeAutostart{supported: true},
			execPath:  "",
			enabled:   true,
			wantCode:  apperr.CodeInternal,
		},
		{
			name:       "a failing platform call is wrapped as internal",
			autostart:  &fakeAutostart{supported: true, enableErr: errors.New("launchd refused")},
			execPath:   "/bin/app",
			enabled:    true,
			wantCode:   apperr.CodeInternal,
			wantEnable: 1,
		},
		{
			name:      "disabling on an unsupported platform is a no-op",
			autostart: &fakeAutostart{supported: false},
			execPath:  "/bin/app",
			enabled:   false,
			wantCode:  "",
		},
		{
			name:      "disabling surfaces a platform failure",
			autostart: &fakeAutostart{supported: true, disableErr: errors.New("launchctl bootout failed")},
			execPath:  "/bin/app",
			enabled:   false,
			wantCode:  apperr.CodeInternal,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := newFakeStore(settings.Default())
			svc := New(Deps{
				Store:     store,
				Autostart: tc.autostart,
				Logger:    discardLogger(),
				Paths:     testPaths(),
				ExecPath:  tc.execPath,
			})

			_, err := svc.Update(context.Background(), UpdateInput{AutoStartApplication: boolPtr(tc.enabled)})
			if tc.wantCode == "" {
				if err != nil {
					t.Fatalf("Update() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("Update() = nil, want an error")
			}
			if code := apperr.CodeOf(err); code != tc.wantCode {
				t.Errorf("code = %s, want %s (err = %v)", code, tc.wantCode, err)
			}
			if got := len(tc.autostart.enabledCalls); got != tc.wantEnable {
				t.Errorf("Enable calls = %d, want %d", got, tc.wantEnable)
			}
			// The request is stored even when the operating system refused it,
			// so the UI can show the switch in the state the user asked for
			// together with the error.
			if store.values.AutoStartApplication != tc.enabled {
				t.Errorf("persisted AutoStartApplication = %v, want the requested %v",
					store.values.AutoStartApplication, tc.enabled)
			}
		})
	}
}

func TestSetAutostartDelegatesToUpdate(t *testing.T) {
	t.Parallel()

	store := newFakeStore(settings.Default())
	autostart := &fakeAutostart{supported: true}
	svc := New(Deps{
		Store:     store,
		Autostart: autostart,
		Logger:    discardLogger(),
		Paths:     testPaths(),
		ExecPath:  "/bin/app",
	})

	state, err := svc.SetAutostart(context.Background(), true)
	if err != nil {
		t.Fatalf("SetAutostart() = %v, want nil", err)
	}
	if !state.Values.AutoStartApplication {
		t.Error("SetAutostart(true) did not persist the flag")
	}
	if len(autostart.enabledCalls) != 1 {
		t.Errorf("Enable calls = %q, want one call", autostart.enabledCalls)
	}
	if _, err := svc.SetAutostart(context.Background(), false); err != nil {
		t.Fatalf("SetAutostart(false) = %v, want nil", err)
	}
	if autostart.disableCalls != 1 {
		t.Errorf("Disable calls = %d, want 1", autostart.disableCalls)
	}
}

func TestUpdateClearsLastProfileThatNoLongerExists(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		exists  bool
		wantID  string
		wantErr bool
	}{
		{name: "existing profile is kept", exists: true, wantID: "p-1"},
		{name: "deleted profile is dropped", exists: false, wantID: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := newFakeStore(settings.Default())
			svc := New(Deps{
				Store:     store,
				Logger:    discardLogger(),
				Paths:     testPaths(),
				Autostart: &fakeAutostart{supported: true},
				ProfileExists: func(context.Context, string) (bool, error) {
					return tc.exists, nil
				},
			})

			state, err := svc.Update(context.Background(), UpdateInput{LastProfileID: strPtr("p-1")})
			if err != nil {
				t.Fatalf("Update() = %v, want nil", err)
			}
			if state.Values.LastProfileID != tc.wantID {
				t.Errorf("LastProfileID = %q, want %q", state.Values.LastProfileID, tc.wantID)
			}
		})
	}
}

func TestUpdateSurfacesProfileLookupFailure(t *testing.T) {
	t.Parallel()

	store := newFakeStore(settings.Default())
	svc := New(Deps{
		Store:     store,
		Logger:    discardLogger(),
		Paths:     testPaths(),
		Autostart: &fakeAutostart{supported: true},
		ProfileExists: func(context.Context, string) (bool, error) {
			return false, errors.New("database is locked")
		},
	})

	if _, err := svc.Update(context.Background(), UpdateInput{LastProfileID: strPtr("p-1")}); err == nil {
		t.Fatal("Update() = nil, want the lookup error")
	}
	if store.writes() != 0 {
		t.Errorf("store writes = %d, want 0", store.writes())
	}
}

func TestRemoveLegacyAutostart(t *testing.T) {
	t.Parallel()

	t.Run("removes the entry left by the prototype", func(t *testing.T) {
		t.Parallel()
		store := newFakeStore(settings.Default())
		autostart := &fakeAutostart{supported: true, legacyEntry: "SingBoxUI.plist"}
		svc := New(Deps{Store: store, Autostart: autostart, Logger: discardLogger(), Paths: testPaths()})

		state, err := svc.RemoveLegacyAutostart(context.Background())
		if err != nil {
			t.Fatalf("RemoveLegacyAutostart() = %v, want nil", err)
		}
		if autostart.removeLegacy != 1 {
			t.Errorf("RemoveLegacy calls = %d, want 1", autostart.removeLegacy)
		}
		if state.Autostart.LegacyEntry != "" {
			t.Errorf("LegacyEntry = %q, want empty after removal", state.Autostart.LegacyEntry)
		}
	})

	t.Run("a platform failure is typed as internal", func(t *testing.T) {
		t.Parallel()
		store := newFakeStore(settings.Default())
		autostart := &fakeAutostart{supported: true, legacyEntry: "SingBoxUI.plist", removeLegacyE: errors.New("permission denied")}
		svc := New(Deps{Store: store, Autostart: autostart, Logger: discardLogger(), Paths: testPaths()})

		_, err := svc.RemoveLegacyAutostart(context.Background())
		if err == nil {
			t.Fatal("RemoveLegacyAutostart() = nil, want an error")
		}
		if code := apperr.CodeOf(err); code != apperr.CodeInternal {
			t.Errorf("code = %s, want %s", code, apperr.CodeInternal)
		}
		if !strings.Contains(apperr.MessageOf(err), "previous autostart entry") {
			t.Errorf("message = %q, want it to name the legacy entry", apperr.MessageOf(err))
		}
	})

	t.Run("no autostart adapter simply reports the state", func(t *testing.T) {
		t.Parallel()
		store := newFakeStore(settings.Default())
		svc := New(Deps{Store: store, Logger: discardLogger(), Paths: testPaths()})

		if _, err := svc.RemoveLegacyAutostart(context.Background()); err != nil {
			t.Fatalf("RemoveLegacyAutostart() = %v, want nil", err)
		}
	})
}

type fakeConnector struct {
	calls []string
	err   error
}

func (c *fakeConnector) StartProfile(_ context.Context, profileID string) error {
	c.calls = append(c.calls, profileID)
	return c.err
}

func TestAutoConnectOnStartup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		values     settings.Settings
		startErr   error
		wantStarts []string
		wantNotice bool
		wantCode   string
	}{
		{
			name:       "disabled does nothing",
			values:     settings.Settings{AutoConnect: false, LastProfileID: "p-1", BinarySource: settings.BinaryManaged, Theme: settings.ThemeSystem, LogLevel: settings.LogInfo},
			wantStarts: nil,
		},
		{
			name:       "enabled but no remembered profile does nothing",
			values:     settings.Settings{AutoConnect: true, LastProfileID: "", BinarySource: settings.BinaryManaged, Theme: settings.ThemeSystem, LogLevel: settings.LogInfo},
			wantStarts: nil,
		},
		{
			name:       "enabled with a profile starts exactly that profile",
			values:     settings.Settings{AutoConnect: true, LastProfileID: "p-7", BinarySource: settings.BinaryManaged, Theme: settings.ThemeSystem, LogLevel: settings.LogInfo},
			wantStarts: []string{"p-7"},
		},
		{
			name:       "a start failure is reported as a persistent notice",
			values:     settings.Settings{AutoConnect: true, LastProfileID: "p-7", BinarySource: settings.BinaryManaged, Theme: settings.ThemeSystem, LogLevel: settings.LogInfo},
			startErr:   apperr.New(apperr.CodeRuntimeStartFailed, "runtime.Start", "sing-box exited with status 1"),
			wantStarts: []string{"p-7"},
			wantNotice: true,
			wantCode:   string(apperr.CodeRuntimeStartFailed),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := newFakeStore(tc.values)
			emitter := &fakeEmitter{}
			svc := New(Deps{Store: store, Emitter: emitter, Logger: discardLogger(), Paths: testPaths()})
			connector := &fakeConnector{err: tc.startErr}

			if err := svc.AutoConnectOnStartup(context.Background(), connector); err != nil {
				t.Fatalf("AutoConnectOnStartup() = %v, want nil: a failed auto-connect is surfaced, not returned", err)
			}
			if len(connector.calls) != len(tc.wantStarts) {
				t.Fatalf("StartProfile calls = %q, want %q", connector.calls, tc.wantStarts)
			}
			for i, want := range tc.wantStarts {
				if connector.calls[i] != want {
					t.Errorf("StartProfile[%d] = %q, want %q", i, connector.calls[i], want)
				}
			}

			notices := 0
			for _, e := range emitter.all() {
				if e.name != events.AppNotice {
					t.Errorf("unexpected event %q", e.name)
					continue
				}
				notices++
				notice, ok := e.payload.(events.Notice)
				if !ok {
					t.Fatalf("notice payload = %T, want events.Notice", e.payload)
				}
				if !notice.Persistent {
					t.Error("notice is not persistent; a failed auto-connect must not fade away")
				}
				if notice.Level != "error" {
					t.Errorf("notice level = %q, want error", notice.Level)
				}
				if notice.Code != tc.wantCode {
					t.Errorf("notice code = %q, want %q", notice.Code, tc.wantCode)
				}
				if notice.Message == "" || notice.Title == "" || notice.OccurredAt == "" {
					t.Errorf("notice is missing user-facing fields: %+v", notice)
				}
				if notice.Operation != "settings.AutoConnectOnStartup" {
					t.Errorf("notice operation = %q, want settings.AutoConnectOnStartup", notice.Operation)
				}
			}
			if tc.wantNotice && notices != 1 {
				t.Errorf("notices = %d, want exactly 1", notices)
			}
			if !tc.wantNotice && notices != 0 {
				t.Errorf("notices = %d, want none", notices)
			}
		})
	}
}

func TestAutoConnectOnStartupPropagatesStoreFailure(t *testing.T) {
	t.Parallel()

	store := newFakeStore(settings.Default())
	store.loadErr = errors.New("database is closed")
	svc := New(Deps{Store: store, Logger: discardLogger(), Paths: testPaths()})
	connector := &fakeConnector{}

	err := svc.AutoConnectOnStartup(context.Background(), connector)
	if err == nil {
		t.Fatal("AutoConnectOnStartup() = nil, want the store error")
	}
	if len(connector.calls) != 0 {
		t.Errorf("StartProfile calls = %q, want none", connector.calls)
	}
}

func TestNewToleratesMissingOptionalDependencies(t *testing.T) {
	t.Parallel()

	store := newFakeStore(settings.Settings{})
	// No logger, no emitter, no autostart adapter: the constructor must fill in
	// safe defaults rather than panicking on the first call.
	svc := New(Deps{Store: store, Paths: testPaths()})

	state, err := svc.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() = %v, want nil", err)
	}
	if state.Autostart.Supported {
		t.Error("Autostart.Supported = true without an adapter")
	}
	if state.Values.BinarySource != settings.BinaryManaged {
		t.Errorf("BinarySource = %q, want the manager default", state.Values.BinarySource)
	}
}
