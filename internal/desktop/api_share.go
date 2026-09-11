package desktop

import (
	"encoding/json"

	"github.com/larffxx/singboxui/internal/app/share"
	"github.com/larffxx/singboxui/internal/domain/apperr"
)

// ShareAPI is the share-link facade bound to the frontend (spec §31, §39).
// Parsing returns a candidate outbound the user previews before it is inserted
// into a configuration; credentials never reach the logs.
type ShareAPI struct{ app *App }

// ShareParsePayload carries one parsed link.
type ShareParsePayload struct {
	Parsed *share.Parsed `json:"parsed,omitempty"`
	Error  *apperr.Error `json:"error,omitempty"`
}

// ShareListPayload carries a batch of parsed links; individual failures are
// reported per line so a paste of twenty links does not fail as a whole.
type ShareListPayload struct {
	Parsed []share.Parsed `json:"parsed"`
	Errors []string       `json:"errors,omitempty"`
	Error  *apperr.Error  `json:"error,omitempty"`
}

// ShareBuildPayload carries a generated link.
type ShareBuildPayload struct {
	Link  string        `json:"link"`
	Error *apperr.Error `json:"error,omitempty"`
}

// SupportedShareSchemes returns the schemes the parser accepts.
func (a *ShareAPI) SupportedShareSchemes() []string { return share.Supported() }

// ParseShareLink parses a single share link into a candidate outbound.
func (a *ShareAPI) ParseShareLink(link string) ShareParsePayload {
	parsed, err := share.Parse(link)
	if err != nil {
		return ShareParsePayload{Error: fail("ShareAPI.ParseShareLink", err)}
	}
	return ShareParsePayload{Parsed: &parsed}
}

// ParseShareLinks parses pasted text, returning every link it could read plus a
// message per unusable line.
func (a *ShareAPI) ParseShareLinks(text string) ShareListPayload {
	parsed, errs := share.ParseList(text)
	out := ShareListPayload{Parsed: parsed}
	for _, err := range errs {
		if err == nil {
			continue
		}
		out.Errors = append(out.Errors, apperr.MessageOf(err))
	}
	return out
}

// BuildShareLink builds a share link from an outbound object supplied as JSON.
// The link is only produced for schemes the user may share; errors are typed.
func (a *ShareAPI) BuildShareLink(outboundJSON string) ShareBuildPayload {
	outbound := map[string]any{}
	if err := json.Unmarshal([]byte(outboundJSON), &outbound); err != nil {
		return ShareBuildPayload{Error: apperr.New(apperr.CodeShareLinkInvalid, "ShareAPI.BuildShareLink",
			"the outbound is not a JSON object")}
	}
	link, err := share.Build(outbound)
	if err != nil {
		return ShareBuildPayload{Error: fail("ShareAPI.BuildShareLink", err)}
	}
	return ShareBuildPayload{Link: link}
}
