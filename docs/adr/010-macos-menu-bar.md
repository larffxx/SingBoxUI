# ADR 010 — The macOS menu bar item

## Context

The legacy prototype put an AWT `SystemTray` icon into the notification area (start/stop/quit plus a
3-second refresh watcher), and the rewrite dropped it deliberately (feature matrix row 26). What that
left is an application whose only surface is its window: with the window minimised or behind other
windows there is no way to see whether sing-box is running, and no way to stop it, without bringing
the window forward.

On macOS that surface is a status item in the menu bar (`NSStatusBar`), which only exists while the
process does — which is exactly the lifetime the rewrite guarantees (§10: quitting means quitting,
ADR 004).

The usual route — a Go systray library (`getlantern/systray` and its forks) — does not fit this
process. Those libraries define an Objective-C class called `AppDelegate`, and Wails v2 defines its
own `AppDelegate` (`v2/internal/frontend/desktop/darwin/AppDelegate.h`): two implementations of one
class name in a single binary is undefined behaviour, and the fork most often recommended for Wails
exists only to rename that class. They also own the application event loop, which Wails already owns.

## Decision

The menu bar belongs to `internal/tray`; what it shows belongs to `internal/desktop/tray.go`.

* `internal/tray` is platform-shaped and application-free: a `Model` (icon name, tooltip, menu rows
  carrying an identifier) and a `Driver` (`Render`, `Hide`). A driver maps the model onto its
  platform; `New` returns `tray.ErrUnsupported` where there is no menu bar, so Windows, Linux and
  non-cgo builds simply have none — an expected answer, never a failure (spec §63).
* The macOS driver is one small Objective-C file compiled through cgo (`tray_darwin.m`): a single
  `NSStatusItem`, template images from SF Symbols, a menu built from the model's JSON, and a target
  object that reports the chosen item's identifier back into Go. It defines no application delegate
  and touches no application state, so nothing of Wails' is reused or replaced.
* Rendering is whole-model and idempotent, and is dispatched to the main thread: Cocoa requires
  user-interface work there, while Wails runs the startup hook on a goroutine.
* The driver reports back: once, when the status item is in the menu bar ("the menu bar icon is in
  place"), and with a reason every time it cannot draw what it was given (a refused model, a symbol
  this macOS does not have). A menu bar that silently does not appear is the failure this ADR is
  answering, so it is not allowed to be silent.
* The words are the window's words (`frontend/src/features/runtime/state.ts`):
  Запущен / Запускается / Останавливается / Ошибка / Остановлен, «Показать окно», «Запустить»,
  «Остановить», «Завершить SingBoxUI».
* The menu follows the events the services already emit — `runtime:status`, `profiles:changed`
  (ADR 007) — through a coalescing worker. It never polls, and it never renders on the goroutine that
  changed the state, because that goroutine is inside the emitting service.
* Quit asks Wails to quit, so the mandatory shutdown sequence stops sing-box before the process exits
  (ADR 004, ADR 008, spec §26).
* §10 is unchanged: closing the window still quits the application. The icon is a second view of the
  running application, not a background mode.

## Alternatives

* **A third-party systray library** — the `AppDelegate` collision above, an extra dependency for a
  surface of ~200 lines, and an event loop that fights Wails'. Rejected.
* **Wails v3, whose tray API is built in** — a framework migration for a menu icon; ADR 001 pins
  Wails v2 for the rewrite. Rejected as disproportionate.
* **No icon at all (the status quo, feature matrix row 26)** — the state of the VPN stays invisible
  while the window is, and stopping it means finding the window first. Rejected.
* **A menu bar icon that keeps the application alive after the window closes** — would overturn §10
  and the "quit means quit" lifecycle of the supervisor (ADR 004). Rejected; the icon may be
  revisited if that decision ever changes.

## Consequences

* macOS gets the icon; Windows keeps none until a driver for it exists (the stub reports
  `ErrUnsupported`, so nothing breaks there and no Windows build links a menu bar).
* The darwin build needs cgo (Xcode Command Line Tools, which Wails already requires for macOS). A
  `darwin && !cgo` build compiles without a menu bar.
* The labels live in Go and mirror the frontend's wording: a wording change is a two-place change
  until the shell and the window share one source of strings.
* Nothing in the test suite creates a real status item: `Deps.NewTray` is the seam for the driver,
  `Deps.RuntimeStatus`, `Deps.ShowWindow` and `Deps.Quit` are the seams for what it shows and what it
  calls, and the presenter is driven through a recording driver that also plays the menu bar's own
  callback.
