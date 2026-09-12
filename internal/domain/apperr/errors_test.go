package apperr

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestErrorStringFormatting(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("boom")
	tests := []struct {
		name string
		err  *Error
		want string
	}{
		{
			name: "code only",
			err:  &Error{Code: CodeInternal},
			want: "INTERNAL_ERROR",
		},
		{
			name: "operation only",
			err:  &Error{Code: CodeNotFound, Operation: "get-profile"},
			want: "NOT_FOUND [get-profile]",
		},
		{
			name: "message only",
			err:  &Error{Code: CodeNotFound, Message: "no such profile"},
			want: "NOT_FOUND: no such profile",
		},
		{
			name: "operation and message",
			err:  &Error{Code: CodeProfileNotFound, Operation: "load", Message: "missing"},
			want: "PROFILE_NOT_FOUND [load]: missing",
		},
		{
			name: "details are joined with semicolons",
			err:  &Error{Code: CodeConfigInvalid, Message: "invalid", Details: []string{"one", "two"}},
			want: "CONFIG_INVALID: invalid (one; two)",
		},
		{
			name: "cause only",
			err:  &Error{Code: CodeInternal, cause: sentinel},
			want: "INTERNAL_ERROR: boom",
		},
		{
			name: "every field",
			err: &Error{
				Code:      CodeConfigApplyFailed,
				Operation: "apply",
				Message:   "write failed",
				Details:   []string{"line 3"},
				cause:     sentinel,
			},
			want: "CONFIG_APPLY_FAILED [apply]: write failed (line 3): boom",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.err.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestUnwrapAndErrorsIs(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("root cause")
	wrapped := Wrap(CodeDatabaseError, "query", "select failed", sentinel)
	if wrapped.Unwrap() != sentinel {
		t.Errorf("Unwrap() = %v, want the sentinel", wrapped.Unwrap())
	}
	if !errors.Is(wrapped, sentinel) {
		t.Error("errors.Is(wrapped, sentinel) = false, want true")
	}
	// A second layer of wrapping must still expose the cause.
	outer := fmt.Errorf("outer: %w", wrapped)
	if !errors.Is(outer, sentinel) {
		t.Error("errors.Is(outer, sentinel) = false, want true after double wrapping")
	}

	plain := New(CodeNotFound, "get", "nope")
	if plain.Unwrap() != nil {
		t.Errorf("Unwrap() of New() = %v, want nil", plain.Unwrap())
	}
}

func TestNewfFormatsTheMessage(t *testing.T) {
	t.Parallel()

	got := Newf(CodeInvalidArgument, "set", "bad value %d: %s", 7, "too small")
	want := "INVALID_ARGUMENT [set]: bad value 7: too small"
	if got.Error() != want {
		t.Errorf("Newf().Error() = %q, want %q", got.Error(), want)
	}
	if got.Code != CodeInvalidArgument || got.Operation != "set" {
		t.Errorf("Newf() = %+v, want code and operation preserved", got)
	}
}

func TestWithDetailsCopiesInsteadOfMutating(t *testing.T) {
	t.Parallel()

	base := New(CodeConfigInvalid, "validate", "bad")
	base.Details = []string{"first"}

	got := WithDetails(base, "second", "third")
	if got == base {
		t.Fatal("WithDetails returned the same pointer; callers must observe a copy")
	}
	want := []string{"first", "second", "third"}
	if len(got.Details) != len(want) {
		t.Fatalf("Details = %v, want %v", got.Details, want)
	}
	for i := range want {
		if got.Details[i] != want[i] {
			t.Errorf("Details[%d] = %q, want %q", i, got.Details[i], want[i])
		}
	}
	// The original must be untouched, both in length and backing array.
	if len(base.Details) != 1 || base.Details[0] != "first" {
		t.Errorf("WithDetails mutated the input: %v", base.Details)
	}
	if got.Code != base.Code || got.Operation != base.Operation || got.Message != base.Message {
		t.Errorf("WithDetails lost a field: %+v", got)
	}

	if WithDetails(nil, "x") != nil {
		t.Error("WithDetails(nil, ...) != nil, want nil")
	}

	// A cause must survive the copy.
	cause := errors.New("disk full")
	copied := WithDetails(Wrap(CodeInternal, "op", "msg", cause), "d")
	if copied.Unwrap() != cause {
		t.Errorf("copied.Unwrap() = %v, want the cause to be preserved", copied.Unwrap())
	}
}

func TestAsAndCodeOf(t *testing.T) {
	t.Parallel()

	typed := New(CodeProfileNameConflict, "create", "duplicate")

	got, ok := As(typed)
	if !ok || got != typed {
		t.Errorf("As(typed) = (%v, %v), want (%v, true)", got, ok, typed)
	}

	wrapped := fmt.Errorf("outer: %w", typed)
	got, ok = As(wrapped)
	if !ok || got != typed {
		t.Errorf("As(wrapped) = (%v, %v), want the inner typed error", got, ok)
	}

	if got, ok := As(errors.New("plain")); ok || got != nil {
		t.Errorf("As(plain) = (%v, %v), want (nil, false)", got, ok)
	}
	if got, ok := As(nil); ok || got != nil {
		t.Errorf("As(nil) = (%v, %v), want (nil, false)", got, ok)
	}

	tests := []struct {
		name string
		err  error
		want Code
	}{
		{name: "typed", err: typed, want: CodeProfileNameConflict},
		{name: "wrapped typed", err: wrapped, want: CodeProfileNameConflict},
		{name: "untyped becomes internal", err: errors.New("plain"), want: CodeInternal},
		{name: "nil is empty", err: nil, want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := CodeOf(tc.err); got != tc.want {
				t.Errorf("CodeOf() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMessageOf(t *testing.T) {
	t.Parallel()

	typed := New(CodeNotFound, "get", "gone")
	if got := MessageOf(typed); got != "gone" {
		t.Errorf("MessageOf(typed) = %q, want %q", got, "gone")
	}

	// An empty Message falls back to the full rendered error so the UI never
	// shows a blank string.
	empty := New(CodeNotFound, "get", "")
	if got := MessageOf(empty); got != empty.Error() {
		t.Errorf("MessageOf(empty) = %q, want %q", got, empty.Error())
	}

	if got := MessageOf(errors.New("boom")); got != "boom" {
		t.Errorf("MessageOf(plain) = %q, want %q", got, "boom")
	}
	if got := MessageOf(nil); got != "" {
		t.Errorf("MessageOf(nil) = %q, want empty", got)
	}
	if got := MessageOf(fmt.Errorf("outer: %w", typed)); got != "gone" {
		t.Errorf("MessageOf(wrapped) = %q, want the typed message %q", got, "gone")
	}
}

func TestDetailsOf(t *testing.T) {
	t.Parallel()

	withDetails := WithDetails(New(CodeInternal, "op", "m"), "a", "b")
	got := DetailsOf(withDetails)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("DetailsOf() = %v, want [a b]", got)
	}
	if DetailsOf(errors.New("plain")) != nil {
		t.Error("DetailsOf(plain) != nil, want nil")
	}
	if DetailsOf(nil) != nil {
		t.Error("DetailsOf(nil) != nil, want nil")
	}
}

func TestIsCode(t *testing.T) {
	t.Parallel()

	busy := New(CodeRuntimeBusy, "start", "busy")
	if !IsCode(busy, CodeRuntimeBusy) {
		t.Error("IsCode(busy, CodeRuntimeBusy) = false, want true")
	}
	if IsCode(busy, CodeInternal) {
		t.Error("IsCode(busy, CodeInternal) = true, want false")
	}
	// An untyped error is reported as the internal code.
	if !IsCode(errors.New("plain"), CodeInternal) {
		t.Error("IsCode(plain, CodeInternal) = false, want true")
	}
	// nil maps to the empty code.
	if !IsCode(nil, "") {
		t.Error(`IsCode(nil, "") = false, want true`)
	}
}

func TestFromPreservesTypedErrorsAndWrapsOthers(t *testing.T) {
	t.Parallel()

	if got := From("op", CodeInternal, nil); got != nil {
		t.Errorf("From(nil) = %v, want nil", got)
	}

	plain := errors.New("boom")
	got := From("load", CodeConfigInvalid, plain)
	if got == nil {
		t.Fatal("From(plain) = nil, want a typed error")
	}
	if got.Code != CodeConfigInvalid || got.Operation != "load" || got.Message != "boom" {
		t.Errorf("From(plain) = %+v, want code/operation/message filled in", got)
	}
	if got.Unwrap() != plain || !errors.Is(got, plain) {
		t.Errorf("From(plain) did not carry the cause: %v", got.Unwrap())
	}

	typed := New(CodeProfileNotFound, "x", "y")
	if from := From("other", CodeInternal, typed); from != typed {
		t.Errorf("From(typed) = %p, want the same pointer %p", from, typed)
	}
	wrapped := fmt.Errorf("outer: %w", typed)
	if from := From("other", CodeInternal, wrapped); from != typed {
		t.Errorf("From(wrapped typed) = %p, want the inner typed error %p", from, typed)
	}
}

// TestExplainRendersTheWholeChain pins the diagnostic form: a typed error carries
// only its own message across the process boundary, so the reason a call failed is
// only visible if the chain is rendered into one line for the log and the status.
func TestExplainRendersTheWholeChain(t *testing.T) {
	t.Parallel()

	inner := errors.New("--log must not contain \"..\", \".\" or a trailing separator")
	middle := Wrap(CodeRuntimeStartFailed, "privrun",
		"the privileged helper refused the request as invalid", inner)
	outer := Wrap(CodeRuntimeStartFailed, "runtime.StartProfile", "could not start sing-box", middle)

	got := Explain(outer)
	for _, want := range []string{
		"could not start sing-box",
		"the privileged helper refused the request as invalid",
		"--log must not contain",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Explain() = %q, want it to carry %q", got, want)
		}
	}
	if causes := CauseMessages(outer); len(causes) != 2 {
		t.Errorf("CauseMessages() = %q, want the two wrapped reasons", causes)
	}
	if causes := CauseMessages(inner); len(causes) != 0 {
		t.Errorf("CauseMessages(a plain error) = %q, want none", causes)
	}
}
