// Command singboxui-priv is the narrow privileged helper of SingBoxUI.
//
// It performs exactly two operations - start one validated sing-box process and
// stop one recorded process - and nothing else (internal-contracts.md §3). Every
// argument is validated before any action is taken, no path is interpreted
// relative to a working directory, and no shell is involved anywhere: the
// platform's elevation mechanism starts this program with an argument array and
// the program execs sing-box the same way (spec §24, §83).
//
// The application starts it; a user never does. It holds no state: the process
// it supervises is described by the pid file of the request, and the exit code is
// recorded in the status file.
//
// Exit codes are the protocol described in internal/platform/privhelper:
//
//	0  the operation was performed
//	1  the operation was refused or its result is unknown
//	2  the invocation itself was invalid
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/larffxx/singboxui/internal/platform/privhelper"
)

func main() {
	// The helper's lifetime is the supervised child's lifetime: a termination
	// signal cancels the context below, which stops sing-box gracefully before
	// the helper exits (spec §62). Only these signals are installed; the helper
	// does not otherwise interpret signals.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := privhelper.Run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
