# ADR 012 — Stopping a sing-box the application did not start

## Context

The core is launched through the narrow privileged helper (ADR 005): the app asks for one specific
launch, the helper supervises the child and records its pid and start time next to the log and the
exit status, and stopping means signalling a process whose handle the app still holds.

That model has one hole, and it was hit in practice: a core can outlive the application. An instance
that is closing sends a graceful stop and gives up after a bounded wait, the window is gone —
and the child keeps running as `root`. Nothing holds a handle to it any more. The next start then
fails in a way that explains nothing:

```
FATAL[0000] start service: start inbound/mixed[mixed-in]: listen tcp 127.0.0.1:2080: bind: address already in use
```

The message the user is shown is worse than the log line. The run directory is per revision, so the
surviving process and the new one write to the *same* `run/<revision>/sing-box.log`; the startup
failure reports the tail of that file, which the old process is still filling with lines about the
traffic it is happily carrying. The user sees "RUNTIME_START_FAILED" followed by a few hundred NUL
bytes and a successful connection to `17.253.52.59:443`.

The pieces needed to answer this properly already existed:

* every launch records `run/<revision>/sing-box.pid` with pid, start time and binary path
  (internal-contracts.md §3), and the record is what `childrun.StopVerified` uses to make sure a
  recycled pid is never signalled;
* the privileged helper has performed `stop` from the beginning — `ParseStopArgs`, `StopRequest`,
  `StopArgv` — it simply had no caller for a process this instance did not start;
* `internal/singbox.Foreign` was written for exactly this situation, with a doc comment saying the
  supervisor "subtracts what it owns and uses the remainder to warn the user (spec §26, §27)", and
  then was never wired to anything.

## Decision

A core the application does not own is *found*, *named* and *stoppable*, and it makes a start fail
loudly instead of quietly.

* **Found by its record.** `Supervisor.ForeignProcesses()` reads every `run/<revision>/sing-box.pid`
  in the data directory, keeps the entries whose process is still running, and drops the one this
  supervisor started. No process enumeration and no file lock: the answer is the same bookkeeping
  the launch already writes. "Still running" means the record's *identity* still matches the kernel
  — pid **and** start time, the same check a stop performs (`childrun.PIDFile.Matches`) — because a
  pid is recycled long before a run directory is tidied up.
* **Named by the search.** `singbox.Foreign` is now filtered to processes that were launched to run
  a configuration (`sing-box run …`) by reading their command line: a `sing-box check` shares the
  executable name but neither the TUN device nor the ports, so it must never make the application
  refuse to start. Each platform reads the argument vector its own way — `ps` on POSIX, and
  `NtQueryInformationProcess(ProcessCommandLineInformation)` on Windows, where `tasklist` reports
  image names only and the CIM route answers with an empty command line for exactly the process that
  matters (the core the application itself starts as administrator). The call needs the limited query
  right that Windows grants for every process, so the filter is the same one on every machine and no
  process is reported merely for being unreadable.
* **Stoppable through the same narrow helper.** The `privilege.Runner` port gains one operation,
  `Stop(ctx, pidPath, force)`. `privrun.Runner.Stop` first tries to signal the process with the
  rights the application already has — an unelevated core then needs no elevation prompt — and
  escalates to the helper only when the kernel answers `ErrNotPermitted`, which is exactly the case
  of the root child that caused this ADR. The helper re-validates that the pid file is inside the
  data directory, and `childrun.StopVerified` verifies pid *and* start time before it signals
  anything, so the new operation cannot be aimed at a process the application never recorded.
  A port that stops a process by *record*, not by pid, keeps the boundary as narrow as it was: the
  application can never ask the helper to kill an arbitrary process.
* **Refused, with the pids, before anything is launched.** `startLocked` asks the supervisor's own
  records first and the machine-wide search second, and refuses with `RUNTIME_ALREADY_RUNNING`:
  "sing-box is already running outside the application (pid 4242): stop it and start again", with the
  pids and revisions in the details. A process started by hand is named as such, because it has no
  record and only the user can stop it. The search is a diagnostic, never a permission: a machine
  where `pgrep` or `ps` is unavailable (or that answers with an error) starts normally.
* **Shown where the user is.** `RuntimeAPI.GetRuntimeStatus` carries `foreignProcesses`, the runtime
  screen renders a warning naming each process with its revision and start time, and
  `RuntimeAPI.StopForeignProcesses` stops them all — the same one-click recovery the user had to
  perform by hand with `sudo kill`.

## Alternatives

* **Stop the foreign process from the unprivileged app** (`kill(2)`) — impossible for the actual
  case: the child runs as `root`, and an app that could signal it would not need the helper at all.
  The direct attempt is kept only as the fast path for an unelevated core.
* **Refuse to start whenever *any* sing-box process exists** — a running `sing-box check` would then
  block a start, which is wrong and would be reported as a bug. Hence the command-line filter.
* **Kill by pid instead of by record** — the helper would have to accept an arbitrary pid, which
  widens the privilege boundary from "one recorded sing-box" to "any process" (ADR 005). Rejected.
* **Make quitting more forceful instead of recovering afterwards** (escalate to `SIGKILL` earlier,
  wait longer) — it narrows the window without closing it, cannot help a process that outlived a
  crash, and the recovery path is needed either way. The shutdown path is unchanged here.
* **Put a per-launch log file next to the shared one** — would remove the mixed log tail, but it
  leaves two cores fighting over the TUN device and the ports, which is the actual failure. Not
  needed once the start is refused: the confusing tail only ever came from the second process.

## Consequences

* The failure mode that produced "RUNTIME_START_FAILED + a log tail of somebody else's traffic" now
  produces one sentence naming the pid, and one button.
* The identity check is not theoretical. The first scan of a real data directory found a record from
  the previous day whose pid had been recycled by `SafariLaunchAgent`: "the pid is alive" would have
  refused every start of that profile, and the stop would have failed with an identity mismatch the
  user could do nothing about. `PIDFile.Matches` is what makes the answer the same one a stop would
  act on.
* `Deps.Foreign` is a seam: the tests never search the machine, so a developer running a core does
  not change the outcome of the suite, and the search itself is covered with a fixture.
* A process started by hand is reported but not stoppable from the UI. That is the price of keeping
  the helper narrow; the message says so, and the user stops it wherever it was started.
* Stopping is per record, so a record that was deleted (or that describes a process that already
  exited) leaves a core the application can name but not end. Nothing new is introduced by that; it
  is the state the application was already in before this ADR, minus the explanation.

## Amendment — the refusal is the kernel's answer, on Windows too

The decision above was written where a kernel answers `ErrNotPermitted` — `kill(2)` with `EPERM` on
macOS. On Windows nothing did. A stop of an elevated core ended in `taskkill`, which prints a localised
"Access is denied" that was reported as a plain failure, so `privrun.Runner.Stop` never escalated to the
helper and the runtime screen could not stop a core the application had itself started as administrator
— which is every TUN launch on that platform, because Windows needs the elevation for the TUN device
(internal-contracts.md §3). The feature of this ADR was therefore missing exactly where the application
is used.

`childrun` now asks the kernel before it signals anything: `OpenProcess` is the question,
`ERROR_ACCESS_DENIED` is `ErrNotPermitted`, a pid that owns no process is `ErrProcessGone`, and the
text `taskkill` prints is no longer read for a reason. The order is unchanged — the direct attempt
stays the fast path for a core this user owns, and the helper is asked only after the refusal.

The rights asked for are the rights the stop needs, and that first proved to be the difference between
a probe that answers and a probe that works. `taskkill /T /F` identifies a process before it ends it,
so it asks for `PROCESS_TERMINATE` *and* `PROCESS_QUERY_INFORMATION`; asking for the terminate right
alone answers `permitted` for a process at a higher integrity level and the stop then fails with the
`Access is denied` this amendment set out to replace — which is what happened on a real machine:

    childrun: taskkill 31760 (/PID 31760 /T /F): exit status 128:
    ERROR: The process with PID 31760 (child process of PID 6900) could not be terminated. Reason: Access is denied.

Measured on Windows 11 against a core running as administrator, from the unprivileged application:
`PROCESS_TERMINATE` alone opened it, while `PROCESS_TERMINATE|PROCESS_QUERY_INFORMATION` and
`PROCESS_ALL_ACCESS` both answered `Access is denied` — the mask `taskkill` uses is refused for exactly
the core Windows needs elevation to start, so the escalation is what stops it, not the fast path. A
process of this user that is *not* elevated answers `permitted` to the same mask, so the fast path
remains the common case and no elevation prompt appears for a core this user started without one; a
`SYSTEM` process is refused, as before.

Verified on Windows: a start-and-exit pid answers `ErrProcessGone`; the production helper binary, built
as it ships, ends a recorded process and reports it (`singboxui-priv: stopped pid …`), including the
elevated core that produced the failure above; the mask is pinned by a test that fails if the query
right is dropped again; the refusal is injected through the same variable the implementation uses
(`openForTermination`) and asserted to reach the caller untouched, which is the contract the escalation
acts on; and the process a refusal describes is left running. What the suite cannot create for itself
is an elevation prompt, so the escalated stop itself is covered by the helper's own tests and by the
machine.

## Amendment — the search reads command lines on Windows too

The filter of the decision above ("a `sing-box check` must never make the application refuse to start")
was written for POSIX, where `ps` answers with the argument vector. On Windows the search stopped at
`tasklist`, which reports image names only, and the amendment to the code that made the mask right left
that open: the application's own binary probe — `sing-box check` and `sing-box version`, run by the
binary adapter at startup — was reported as a foreign core, and a start was refused with
`RUNTIME_ALREADY_RUNNING` because of a process that held neither the TUN device nor a port. It was
observed on a real machine, in the log of an auto-connect that gave up on exactly that:

    sing-box (pid 13048) is running a configuration that SingBoxUI did not start: stop it and start again

`NtQueryInformationProcess(ProcessCommandLineInformation)` answers with the full command line of any
process, including one running as administrator, and needs nothing but
`PROCESS_QUERY_LIMITED_INFORMATION`, which Windows grants for every process of every user. The Windows
enumeration therefore reads each candidate's command line and applies the same `run` filter as POSIX
does, in the same package, with the platform difference confined to the two files that do the reading
(`process_posix.go`, `process_windows.go`). `Get-CimInstance Win32_Process` is *not* an alternative: it
answers with an empty command line for the elevated core, which is the process the filter has to judge.

Verified on Windows: the fixture (a copy of the test binary named `sing-box.exe`, so `tasklist` sees it
like any core) is enumerated when it is started with `run` and left out when it is started with `check`,
and both tests run on that platform now instead of being skipped; the command line of a live process is
read and asserted against the arguments the fixture was started with; a pid that cannot exist answers an
error rather than an empty line; and the full local suite passes with the same result as before.
