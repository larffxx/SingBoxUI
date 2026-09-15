// Package console keeps helper programs from putting a console window on the desktop.
//
// Windows gives a console program a console of its own when it is started by a process that has
// none, and both the application and its privileged helper are GUI-subsystem binaries. Every
// `sing-box version`, `sing-box check`, `tasklist` and `taskkill` they run therefore opened a
// console: on Windows 11 with Windows Terminal as the terminal host that is a terminal window
// carrying the command line in its title, appearing and disappearing while the user works —
// measured for all four (see the reference in the project skill). The core itself is not
// affected, because `childrun` starts it with a hidden window in a process group of its own.
//
// macOS has nothing to fix here: a process started there gets no window at all.
//
// A windowless start changes nothing else about the child: it still receives the handles it is
// given, so captured output, exit codes and the OpenProcess probes of the stop path all behave
// exactly as before.
package console

import "os/exec"

// Windowless starts cmd without a console of its own. Call it immediately after the command
// has been built, before it is started or output is wired up; it is a no-op on platforms that
// have no console windows to keep away (see console_windows.go and console_other.go).
func Windowless(cmd *exec.Cmd) { windowless(cmd) }
