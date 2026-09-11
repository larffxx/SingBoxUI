// Command fakesingbox is the fake sing-box executable the test suite runs
// (spec §67).
//
// It is a separate command rather than a test-only function because the code
// under test has to start a real child process to be worth testing: version
// probing, `check`, termination and foreign-process detection all depend on
// process semantics that an in-process stub cannot reproduce. Tests build this
// command on demand into a temporary directory, usually under the name
// "sing-box", and drive it with -scenario or FAKESINGBOX_SCENARIO.
package main

import (
	"os"

	"github.com/larffxx/singboxui/internal/singbox/faketest"
)

func main() {
	os.Exit(faketest.Main())
}
