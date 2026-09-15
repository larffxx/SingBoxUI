// Command singboxui is the desktop entry point and the composition root: it is
// the only place that knows about Wails, the concrete OS adapter and SQLite
// (spec §63). Everything below it is built explicitly and passed in.
//
// The main package lives at the repository root because Wails v2 resolves its
// project directory - and therefore wails.json, the asset directory and the
// TypeScript bindings - from the directory that holds main.go. The layout of
// spec §6 is kept otherwise; the deviation is recorded in docs/adr/001-wails-v2.md.
package main

import (
	"context"
	"embed"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"github.com/larffxx/singboxui/internal/desktop"
	"github.com/larffxx/singboxui/internal/logging"
	"github.com/larffxx/singboxui/internal/platform"
	"github.com/larffxx/singboxui/internal/singbox"
	"github.com/larffxx/singboxui/internal/storage/sqlite"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "0.1.0-dev"

// assets embeds the production frontend build so the desktop binary is
// self-contained: no Node runtime and no asset directory are needed at runtime
// (spec §73).
//
//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "singboxui: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	plat, err := platform.New()
	if err != nil {
		return err
	}
	paths := plat.Paths()
	if err := plat.EnsureDirs(); err != nil {
		return err
	}

	logger, logCloser, err := logging.Setup(logging.Options{
		Level: logLevel(),
		Path:  logPath(paths.LogPath),
	})
	if err != nil {
		return err
	}
	defer logCloser.Close()

	// The root context is cancelled by the shutdown sequence; every background
	// worker derives from it, so none of them can outlive the application
	// (spec §62).
	rootCtx, cancelRoot := context.WithCancel(context.Background())
	defer cancelRoot()

	store, err := sqlite.Open(rootCtx, paths.DBPath)
	if err != nil {
		return err
	}
	// The store is closed by the shutdown sequence; closing it here as well
	// would race with the supervisor's final persistence.

	httpClient := &http.Client{Timeout: 60 * time.Second}

	application := desktop.New(desktop.Deps{
		Logger:     logger,
		Store:      store,
		Platform:   plat,
		Privilege:  plat.PrivilegeRunner(),
		Releases:   singbox.NewClient(httpClient, "", userAgent(), logger),
		HTTPClient: httpClient,
		ExecPath:   executablePath(),
		Version:    version,
		GOOS:       runtime.GOOS,
		GOARCH:     runtime.GOARCH,
	})

	// Closing the window quits the application (spec §10). A signal in a
	// developer session must behave the same way: stop the runtime, release
	// persistence, then exit.
	go func() {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
		<-signals
		application.OnShutdown(context.Background())
		os.Exit(0)
	}()

	logger.Info("starting singboxui", "version", version, "os", runtime.GOOS, "arch", runtime.GOARCH)

	return wails.Run(&options.App{
		Title:            "SingBoxUI",
		Width:            1200,
		Height:           800,
		MinWidth:         960,
		MinHeight:        640,
		BackgroundColour: &options.RGBA{R: 16, G: 17, B: 20, A: 1},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        application.OnStartup,
		OnShutdown:       application.OnShutdown,
		Bind: []interface{}{
			application.ProfileAPI,
			application.ConfigAPI,
			application.RuntimeAPI,
			application.BinaryAPI,
			application.SettingsAPI,
			application.ShareAPI,
			application.TrafficAPI,
			application.AppsAPI,
		},
		Mac: &mac.Options{
			TitleBar:             mac.TitleBarHiddenInset(),
			WebviewIsTransparent: false,
			About: &mac.AboutInfo{
				Title:   "SingBoxUI",
				Message: "Configure and control sing-box.\n\nVersion " + version,
			},
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			DisableWindowIcon:    false,
		},
	})
}

// logLevel reads the optional environment override; settings-level verbosity is
// applied to the runtime process, not to the GUI's own logger.
func logLevel() string {
	if v := os.Getenv("SINGBOXUI_LOG_LEVEL"); v != "" {
		return v
	}
	return "info"
}

// logPath allows a developer session to log to stderr instead of the data dir.
func logPath(defaultPath string) string {
	if os.Getenv("SINGBOXUI_LOG_STDERR") == "1" {
		return ""
	}
	return defaultPath
}

func userAgent() string {
	return fmt.Sprintf("SingBoxUI/%s (%s; %s)", version, runtime.GOOS, runtime.GOARCH)
}

func executablePath() string {
	path, err := os.Executable()
	if err != nil {
		return ""
	}
	return path
}
