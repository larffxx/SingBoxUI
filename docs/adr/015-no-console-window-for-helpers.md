# ADR 015 — No console window for a helper process

## Context

Windows gives a console program a console of its own when the process that starts it has none.
SingBoxUI and its privileged helper are both GUI-subsystem binaries — deliberately so: the helper
would otherwise sit on the user's desktop with a black window for as long as the tunnel lives
(ADR 005). The consequence was not considered: every console program the two of them run got a console
of its own, and with Windows 11's default terminal host that console is a **Windows Terminal window**
that opens on the desktop for the lifetime of a helper that lasts milliseconds.

Measured on Windows 11 by sampling top-level windows from a windowless process while each command ran
(`internal/platform/console` records the flags and the measurement is described here):

| Command | Started as the code did | With the fix |
|---|---|---|
| `sing-box version` (binary probe) | terminal window, title = the command line | no window |
| `sing-box check -c …` (every revision save) | terminal window | no window |
| `tasklist …` (foreign-process search, every start) | terminal window | no window |
| `taskkill …` (every stop) | terminal window | no window |
| the core itself (`childrun`) | **no window** — `HideWindow` was already set | unchanged |

Every one of those five was reproduced with the flags and without them: a probe linked
`-H=windowsgui` (the same subsystem as the app) ran the application's own `singbox.Probe`,
`singbox.Check` and `singbox.Foreign` in a loop and reported every window that appeared during each
call — three rounds, each of the three calls, with a window every time before the change and no window
at all after it.

The core was already right, and that is what makes this a rule rather than four patches: `childrun`
sets `HideWindow` together with `CREATE_NEW_PROCESS_GROUP`, and the measurement shows a hidden window
attribute is enough for a process that keeps a console. What the four fixed call sites lacked was the
larger of the two flags.

macOS has nothing to fix: a process started there gets no window at all, which is why the problem only
ever appeared on one platform.

## Decision

Every console program started by the application or its helper is started without a console.

* `internal/platform/console.Windowless(cmd)` adds `CREATE_NO_WINDOW` and keeps `HideWindow` true. It
  **merges** into `SysProcAttr` rather than replacing it, so a caller that needs flags of its own —
  `childrun` wants a process group for `GenerateConsoleCtrlEvent` — keeps them.
* The call sites are the four that run helper programs: the version probe (`singbox/binary.go`), the
  validator (`singbox/validator.go`), the process search on Windows (`singbox/process_windows.go`) and
  the stop (`platform/childrun/process_windows.go`, `taskkillCommand`).
* `Windowless` is a no-op on other platforms (`console_other.go`), so the call sites need no build tags
  and the rule reads the same everywhere.
* The core's own launch is deliberately **not** changed: it keeps its hidden console, which the
  measurement shows is invisible, and the graceful-stop path that targets its process group stays
  intact.
* Tests pin the flags (set on a command without attributes, merged into one that has them) and the part
  that is easy to get wrong: a windowless child still receives its handles, so output capture and exit
  statuses behave exactly as before.

## Alternatives

* **Set the flags at each call site by hand.** Four copies of the same two-field struct, each of which
  a future helper program would have to remember. Rejected: the rule is one function.
* **`CREATE_NO_WINDOW` for the core as well.** It would remove the core's console entirely and with it
  the process group the graceful stop targets, for no visible gain — the core's console is hidden
  already. Rejected.
* **`HideWindow` alone.** It is what the core does, and the measurement shows it is enough *for a
  process that keeps its console*. For these four helpers the terminal host created a window anyway, so
  the smaller flag would have left the bug in place.
* **A shell wrapper or a hidden `cmd.exe`.** A windowless parent still starts the child windowless, and
  the wrapper would put a process between the code and the program it wants to talk to. Rejected.
* **Leave it, and treat the flashes as cosmetic.** They are not: a save flashes a terminal window while
  the user is looking at the editor, and the window carries the full command line in its title — paths
  and configuration files of the user, on a shared screen.

## Consequences

* `internal/platform/console` is the only place that knows how a helper program is started silently;
  a new `exec.Command` for a console program has one thing to add, and the package doc says why.
* The measurement of the four sites lives in this ADR and in the project skill, because it is the kind
  of fact that is expensive to re-derive (a GUI-subsystem probe, a window sampler, and a terminal host
  that changes between Windows versions).
* The helper's `taskkill` still prints its output where it always did — the flag changes the console,
  not the handles, which the tests assert.
* Nothing about privilege, process groups or the stop path changes: the flags are additive, and the
  core's own launch is untouched.
