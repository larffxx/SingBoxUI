# ADR 013 — Routing rules that select a program by name (Windows)

## Context

ADR 011 gave macOS an application picker and answered the same question on Windows with
`Supported: false` and a sentence telling the user to write `process_name` by hand. That answer was
right about the mechanism and wrong about the product: the machine this application is used on most is
a Windows one, and the feature the user asked for — "эта программа через VPN, та напрямую" — is the
same feature on both platforms. The workaround was the raw editor, and the user's own configuration
still carries one of those blocks:

```json
{"process_name": ["Discord.exe", "Telegram.exe", "chrome.exe", "dota2.exe"],
 "action": "route", "outbound": "vless-reality"}
```

What sing-box 1.14.0 offers on Windows was measured against a running core before anything was
written (a `mixed` inbound on a spare port, no TUN, no elevation, the managed binary; the clients were
copies of `curl.exe` placed where the rule under test could see them):

* `process_name`, `process_path` and `process_path_regex` are all accepted by `sing-box check` and all
  three match. The name is matched against the file name and **case-insensitively**
  (`process_name: ["probename.exe"]` selected `PROBENAME.EXE`), and the path is the full native path
  **including `.exe`** — the log line is `router: found process path: C:\…\appdir\probe2.exe`.
* A `process_path_regex` anchored at a directory with a trailing backslash matches processes in its
  subdirectories, which is what makes a folder rule (a game library) possible.
* The path comes from the process that owns the connection, so an unelevated core sees every process
  of this user, and the application's own core runs as administrator (ADR 005) and sees everything.

Windows has no directory that could be enumerated the way macOS enumerates bundles. What it has was
measured on a real machine as well:

* **running programs**: 70 processes owned a top-level window with a title. Steam, Discord, Telegram,
  Chrome and a game launcher are all in that set even when they sit in the notification area, because a
  program that hides its window still owns it;
* **the Start Menu**: 215 shortcuts, 165 of which resolved through the shell to an existing image
  (1 was an advertised MSI link that resolves to `msiexec.exe`, 49 pointed at documents, folders,
  `.msc` consoles or nothing);
* **the registry** (`App Paths`): 58 values across the three views, mostly Windows components
  (`TabTip.exe`, `wab.exe`, `wmplayer.exe`), many still stored as `%ProgramFiles%\…`, and none of them
  carrying a version resource. It describes what an installer registered, not what the user starts.

The two sources above assemble into 139 rows on that machine, none of them inside the Windows
directory.

## Decision

The application screen gets its Windows catalog, and the catalog declares *how* a program is selected
rather than only which program it is.

* **The listing is the union of two sources**: the programs this machine runs — a process that owns a
  top-level window with a title, or one that is visible — and the programs its Start Menu offers,
  resolved by the shell itself (`IShellLinkW` + `IPersistFile::Load`, called through their vtables; the
  argument vector and the flags are fixed, and nothing goes through a shell or a PowerShell process).
  The listing reports what the user can start and what is running, which is what the macOS side reports
  through its bundles.
* **The condition is `process_name`**, the file name of the program's image, and it is generated next to
  the listing. A Windows program is a *file*, and its directory is not stable: Discord installs into
  `app-1.0.9180`, the next update into `app-1.0.9257`, and a rule anchored at a directory stops matching
  after an update. The name is what stays, helper processes of one program share it (Chrome's renderers
  are all `chrome.exe`), and the match is case-insensitive, so the name the filesystem reports is always
  the right one to write. `ProcessNameCondition` is the whole rule logic.
* **The descriptor names the condition.** `applications.Application` loses `ProcessPathRegex` and gains
  `MatchKey` + `MatchValue`: the frontend writes the pair as it stands, exactly as it wrote the macOS
  expression before. Which field identifies a program is a property of the operating system, and
  `internal/domain/applications` also carries the two constants (`process_name`,
  `process_path_regex`) so both platforms and the frontend refer to the same strings.
* **Two paths that produce the same condition are one row.** Chrome's renderers, the 32- and 64-bit
  launcher of one game and the versioned directories of one application all describe one program to the
  screen, and a generated rule cannot tell them apart anyway.
* **What is never offered**: everything inside the Windows directory (the shell, the services, the MMC
  consoles and the installers live there), `msiexec.exe` (what an advertised shortcut resolves to), and
  this application's own data directory and executables — a rule that routes the tunnel's own core, or
  the window that manages it, is never what the screen is for.
* **The name the row shows** is the one the file gives itself in its version resource
  (`FileDescription`, then `ProductName`, read through the translation table so a localised
  installation still has a name), and the file name without its extension when there is none.
* **The listing keeps the macOS answers**: a 2-minute cache, a source that fails is only reported when
  nothing at all could be listed, and the entry type is the same flat descriptor the routing tab and the
  rule editor already understand.

The registry is not a source. It describes installers and Windows components, and the entries that do
name a real program are the ones the Start Menu already offers.

## Alternatives

* **`App Paths` as a third source** — measured: 58 values, mostly Windows components, many unexpanded,
  none with a name to show. It would add rows like "Windows Media Player" to a list of programs and
  would need its own environment expansion. Rejected.
* **The uninstall keys** (`…\Uninstall`) — a `DisplayName` and an `InstallLocation`, no executable:
  the same objection ADR 011 raised against the registry, now with numbers. Rejected.
* **Resolving `.lnk` files by parsing the format** — the format carries a target path, an item
  identifier list and an *advertised* form for installer-created shortcuts; parsing it means
  reimplementing the shell's resolver, and the advertised form is exactly the case a parser gets wrong.
  The shell is asked instead. Rejected.
* **Anchoring a `process_path_regex` at the program's directory** (the macOS shape) — the expression is
  what the rule editor is for, but as the *generated* condition it breaks on the first update of an
  application that installs into a versioned directory, which on Windows is most Electron applications
  and every game launcher. Rejected as the default; the rule editor still offers the field, and the card
  says so.
* **Only programs with a visible window** — a program minimised into the notification area (Steam,
  Discord, Telegram) shows no visible window and is exactly the program a user wants to route. The
  window is the signal, its visibility is not.
* **Every running process** — services, updaters, crash handlers and CEF helpers would be most of the
  list; measured, a visible-window-only list drops whole products (`steam.exe` has no visible window).
  The union of the two sources above is what is offered instead, with the search box to cut it down.
* **Filtering by a heuristic name list** (`*helper*.exe`, `crashpad*`, `*agent.exe`) — it would hide the
  rows the user came for as soon as a product names its main program helpfully. The two sources decide,
  not a pattern list.
* **Listing without a `MatchKey`** (the frontend choosing the field per platform) — the card would have
  to know which platform it runs on and derive the rule from the platform. The listing already knows;
  the rule logic stays where it was, in the backend next to the expression. Rejected.

## Consequences

* Adding a program is one click on Windows too, and what lands in the configuration is a rule the user
  can read and edit: `{"action": "route", "outbound": "…", "process_name": ["chrome.exe"]}`.
* A name is not unique. Two programs whose images share a file name (two different `launcher.exe`)
  produce the same condition and therefore one row; the rule cannot tell them apart, and neither can the
  screen. The path conditions remain in the rule editor for the cases where this matters.
* The Start Menu is not the list of installed programs — a program installed without a shortcut (Steam on
  the machine this was written on) is only offered while it runs. That is why the running source is not
  optional, and why the search box searches the path as well as the name.
* The listing is machine-specific and read-only: nothing is persisted, and no rule is written until the
  user clicks a button, exactly as on macOS.
* `internal/platform/windows` now calls the shell's COM objects and reads version resources. The three
  sources are function fields of the catalog, so the suite describes a machine instead of inspecting the
  one it runs on, and the machine itself is covered by its own tests (the real Start Menu, a shortcut the
  test writes and reads back through the shell, a real image path, a real version resource).
* The macOS descriptor is unchanged in everything the user sees: the same expression, anchored at a
  resolved bundle path, is now carried in `MatchValue` under `MatchKey: process_path_regex`.
* Windows programs are listed without a `BundleID` — the field stays empty there, as the value type
  anticipated.
