package desktop

// The error object is what the interface renders and what a user pastes when
// something fails. It crosses the process boundary as JSON, and a typed error
// carries only its own message there — the cause it wraps is unexported and does
// not survive encoding. Without adding the chain, every wrapped failure reads as
// "could not start sing-box" and the reason it was refused is lost.

import (
	"errors"
	"slices"
	"testing"

	"github.com/larffxx/singboxui/internal/domain/apperr"
)

func TestFailCarriesTheWrappedReason(t *testing.T) {
	reason := "--log must not contain \"..\", \".\" or a trailing separator"
	inner := apperr.Wrap(apperr.CodeInvalidArgument, "privhelper.validate",
		"the privileged helper refused the request as invalid", errors.New(reason))
	outer := apperr.Wrap(apperr.CodeRuntimeStartFailed, "runtime.StartProfile",
		"could not start sing-box", inner)

	got := fail("RuntimeAPI.Start", outer)
	if got == nil {
		t.Fatal("fail() = nil, want the error object")
	}
	if got.Code != apperr.CodeRuntimeStartFailed {
		t.Errorf("Code = %s, want the code of the typed error kept", got.Code)
	}
	want := []string{
		"the privileged helper refused the request as invalid",
		reason,
	}
	for _, entry := range want {
		if !slices.Contains(got.Details, entry) {
			t.Errorf("Details = %q, want it to carry %q", got.Details, entry)
		}
	}
	// Every reason appears once, however often the chain repeats it.
	duplicated := apperr.Wrap(apperr.CodeRuntimeStartFailed, "op", "could not start sing-box", errors.New("same reason"))
	duplicated = apperr.WithDetails(duplicated, "same reason")
	if details := fail("op", duplicated).Details; len(details) != 1 {
		t.Errorf("Details = %q, want a reason already present not repeated", details)
	}
	// The error the caller passed is not mutated: it may still be logged elsewhere.
	if len(outer.Details) != 0 {
		t.Errorf("the original error gained details: %q", outer.Details)
	}
}

func TestFailKeepsUntypedErrorsTyped(t *testing.T) {
	if got := fail("op", nil); got != nil {
		t.Errorf("fail(nil) = %+v, want nil", got)
	}
	got := fail("ProfileAPI.List", errors.New("boom"))
	if got.Code != apperr.CodeInternal {
		t.Errorf("Code = %s, want INTERNAL_ERROR", got.Code)
	}
	if got.Message != "boom" || got.Operation != "ProfileAPI.List" {
		t.Errorf("error = %+v, want the message kept and the operation named", got)
	}
}
