package share

import (
	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/logging"
)

// Field-level failure reasons. They name the missing/malformed *field*, never
// its value, so credentials cannot leak through an error detail.
const (
	detailMissingHost     = "missing host"
	detailMissingPort     = "missing port"
	detailInvalidPort     = "invalid port"
	detailMissingUUID     = "missing UUID"
	detailMissingPassword = "missing password"
	detailMissingMethod   = "missing method"
	detailMalformedURL    = "malformed URL"
)

// invalidErr builds the SHARE_LINK_INVALID typed error returned by every
// parser/builder failure. Message and details are passed through the shared
// redaction helper as a defence in depth: even if a caller ever smuggles part
// of a link into a detail, the credential never survives into a log line.
func invalidErr(operation, message string, details ...string) *apperr.Error {
	safe := make([]string, 0, len(details))
	for _, d := range details {
		safe = append(safe, logging.RedactString(d))
	}
	err := apperr.New(apperr.CodeShareLinkInvalid, operation, logging.RedactString(message))
	return apperr.WithDetails(err, safe...)
}
