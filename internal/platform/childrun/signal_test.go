package childrun

import (
	"os"
	"os/signal"
	"syscall"
)

// terminationSignal is the signal a graceful stop sends on this system. On
// Windows the process is ended without a signal, so the stand-in sing-box also
// handles the forced paths through modeStubborn.
func terminationSignal() os.Signal { return syscall.SIGTERM }

// ignoreTermination makes the stand-in sing-box immune to a graceful stop, so the
// escalation to a kill can be tested.
func ignoreTermination() { signal.Ignore(terminationSignal()) }
