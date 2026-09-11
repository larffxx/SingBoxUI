// Package apperr defines the stable, typed error model shared by every layer.
//
// The frontend branches on Code, never on message text (spec §33).
package apperr

import (
	"errors"
	"fmt"
	"strings"
)

// Code is a stable, machine-readable error category.
type Code string

const (
	CodeConfigInvalid             Code = "CONFIG_INVALID"
	CodeConfigCheckFailed         Code = "CONFIG_CHECK_FAILED"
	CodeConfigApplyFailed         Code = "CONFIG_APPLY_FAILED"
	CodeProfileNotFound           Code = "PROFILE_NOT_FOUND"
	CodeRevisionNotFound          Code = "REVISION_NOT_FOUND"
	CodeProfileNameConflict       Code = "PROFILE_NAME_CONFLICT"
	CodeProfileDeleteBlocked      Code = "PROFILE_DELETE_BLOCKED"
	CodeRuntimeAlreadyRunning     Code = "RUNTIME_ALREADY_RUNNING"
	CodeRuntimeNotRunning         Code = "RUNTIME_NOT_RUNNING"
	CodeRuntimeStartFailed        Code = "RUNTIME_START_FAILED"
	CodeRuntimeStopFailed         Code = "RUNTIME_STOP_FAILED"
	CodeRuntimeBusy               Code = "RUNTIME_BUSY"
	CodePrivilegeDenied           Code = "PRIVILEGE_DENIED"
	CodeBinaryNotFound            Code = "BINARY_NOT_FOUND"
	CodeBinaryDownloadFailed      Code = "BINARY_DOWNLOAD_FAILED"
	CodeBinaryChecksumFailed      Code = "BINARY_CHECKSUM_FAILED"
	CodeBinaryUnsupportedPlatform Code = "BINARY_UNSUPPORTED_PLATFORM"
	CodeBinaryInstallFailed       Code = "BINARY_INSTALL_FAILED"
	CodeBinarySourceInvalid       Code = "BINARY_SOURCE_INVALID"
	CodeBinaryUpdateCheckFailed   Code = "BINARY_UPDATE_CHECK_FAILED"
	CodeBinaryUpdateUnavailable   Code = "BINARY_UPDATE_UNAVAILABLE"
	CodeSettingsInvalid           Code = "SETTINGS_INVALID"
	CodeShareLinkInvalid          Code = "SHARE_LINK_INVALID"
	CodeDatabaseError             Code = "DATABASE_ERROR"
	CodeInvalidArgument           Code = "INVALID_ARGUMENT"
	CodeNotFound                  Code = "NOT_FOUND"
	CodeNetworkUnavailable        Code = "NETWORK_UNAVAILABLE"
	CodeUpdatingRestricted        Code = "UPDATING_RESTRICTED"
	CodeAppShuttingDown           Code = "APP_SHUTTING_DOWN"
	CodeInternal                  Code = "INTERNAL_ERROR"
)

// Error is the single error type crossing application boundaries.
type Error struct {
	Code      Code     `json:"code"`
	Message   string   `json:"message"`
	Details   []string `json:"details,omitempty"`
	Operation string   `json:"operation,omitempty"`
	cause     error
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString(string(e.Code))
	if e.Operation != "" {
		b.WriteString(" [")
		b.WriteString(e.Operation)
		b.WriteString("]")
	}
	if e.Message != "" {
		b.WriteString(": ")
		b.WriteString(e.Message)
	}
	if len(e.Details) > 0 {
		b.WriteString(" (")
		b.WriteString(strings.Join(e.Details, "; "))
		b.WriteString(")")
	}
	if e.cause != nil {
		b.WriteString(": ")
		b.WriteString(e.cause.Error())
	}
	return b.String()
}

func (e *Error) Unwrap() error { return e.cause }

// New builds an Error with no underlying cause.
func New(code Code, operation, message string) *Error {
	return &Error{Code: code, Operation: operation, Message: message}
}

// Newf is New with formatting.
func Newf(code Code, operation, format string, args ...any) *Error {
	return New(code, operation, fmt.Sprintf(format, args...))
}

// Wrap attaches a cause; the message stays human-readable.
func Wrap(code Code, operation, message string, cause error) *Error {
	return &Error{Code: code, Operation: operation, Message: message, cause: cause}
}

// WithDetails returns a copy carrying extra structured details.
func WithDetails(err *Error, details ...string) *Error {
	if err == nil {
		return nil
	}
	out := *err
	out.Details = append(append([]string{}, err.Details...), details...)
	return &out
}

// As extracts the typed error from a chain, if present.
func As(err error) (*Error, bool) {
	var target *Error
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}

// CodeOf returns the stable code for any error; unknown errors become CodeInternal.
func CodeOf(err error) Code {
	if err == nil {
		return ""
	}
	if typed, ok := As(err); ok {
		return typed.Code
	}
	return CodeInternal
}

// MessageOf returns a user-presentable message.
func MessageOf(err error) string {
	if err == nil {
		return ""
	}
	if typed, ok := As(err); ok {
		if typed.Message != "" {
			return typed.Message
		}
		return typed.Error()
	}
	return err.Error()
}

// DetailsOf returns structured details, if any.
func DetailsOf(err error) []string {
	if typed, ok := As(err); ok {
		return typed.Details
	}
	return nil
}

// IsCode reports whether err carries the given code.
func IsCode(err error, code Code) bool { return CodeOf(err) == code }

// From converts an arbitrary error into a typed one, preserving typed errors.
func From(operation string, code Code, err error) *Error {
	if err == nil {
		return nil
	}
	if typed, ok := As(err); ok {
		return typed
	}
	return Wrap(code, operation, err.Error(), err)
}
