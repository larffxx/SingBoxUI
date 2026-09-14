package profiles

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/larffxx/singboxui/internal/app/share"
	"github.com/larffxx/singboxui/internal/app/templates"
	"github.com/larffxx/singboxui/internal/domain/apperr"
)

// This file turns pasted share links into a complete configuration, which is what the create
// dialog offers next to a starter template (spec §39). A link describes one outbound; a profile
// needs inbounds, DNS and routing as well, so the document is generated on a skeleton the user
// picks (ShareLinkBases) with the parsed outbounds in place of its placeholder proxy.
//
// The skeletons are the shipped starters rather than a second copy of them: a generated profile
// is the starter the user would have picked, with the server filled in from the link. Editing a
// starter therefore changes what a link generates, which is the point of having one source.

// shareLinkPlaceholderTag is the outbound tag every share-link skeleton carries for this file to
// replace. It is spelled out here because the skeletons are template documents and the tag in
// them is the anchor; the test for this file fails if a declared base loses it, and
// templates_test.go keeps the starters themselves valid.
const shareLinkPlaceholderTag = "proxy"

// urlTestInterval is how often the automatic group re-measures the servers it holds, matching
// the selector starter.
const urlTestInterval = "5m"

// ShareLinkBase is one starting shape for a generated configuration.
type ShareLinkBase struct {
	ID string `json:"id"`
	// Name and Description are shown in the create dialog; the backend declares them so the
	// dialog carries no list of its own (ADR 013).
	Name        string `json:"name"`
	Description string `json:"description"`
	// RequiresPrivilege is true when starting the result needs administrator rights (the TUN
	// inbound), which the dialog has to say before the user picks it.
	RequiresPrivilege bool `json:"requiresPrivilege"`
	// TemplateID is the starter whose skeleton the configuration is generated on.
	TemplateID string `json:"templateId"`
}

// ShareLinkBases returns the shapes a generated configuration can take, in display order. The
// first one is the default.
func ShareLinkBases() []ShareLinkBase {
	return []ShareLinkBase{
		{
			ID: "tun", Name: "VPN (TUN device)",
			Description:       "System traffic goes through the server from the link, with a local mixed port as a fallback. Starting it needs administrator rights.",
			RequiresPrivilege: true,
			TemplateID:        "tun-vless-reality",
		},
		{
			ID: "local", Name: "Local port only (no TUN)",
			Description: "No TUN device: a local mixed port on 127.0.0.1 goes through the server from the link and system routing is left alone.",
			TemplateID:  "socks-local",
		},
	}
}

// ShareLinkInput is one request to generate a configuration from pasted links. An empty Base
// selects the first of ShareLinkBases.
type ShareLinkInput struct {
	Name        string
	Description string
	// Links is the pasted text: one link per line, comments and blank lines allowed.
	Links string
	Base  string
}

// ShareLinkConfig is the result of generating a configuration from links.
type ShareLinkConfig struct {
	// ConfigJSON is a complete sing-box configuration document.
	ConfigJSON string
	// Name is what the profile should be called when the user typed no name of their own: the
	// remarks of the first link.
	Name string
	// Kind is the protocol of the first link, for the dialog's summary.
	Kind string
	// Count is how many links the configuration carries.
	Count int
	// Warnings name the pasted lines that were skipped and the link parameters that had no
	// sing-box equivalent; credentials never appear in them.
	Warnings []string
}

// GenerateFromShareLinks builds a configuration from pasted links (spec §39). A paste in which
// one line of several is unusable still generates, and the unusable lines are reported as
// warnings, because that is what the user can act on; a paste with nothing usable is an error.
func GenerateFromShareLinks(in ShareLinkInput) (ShareLinkConfig, error) {
	const op = "profiles.GenerateFromShareLinks"
	base, err := shareLinkBase(in.Base)
	if err != nil {
		return ShareLinkConfig{}, err
	}
	parsed, failures := share.ParseList(in.Links)
	warnings := make([]string, 0, len(failures))
	for _, failure := range failures {
		warnings = append(warnings, share.FailureMessage(failure))
	}
	if len(parsed) == 0 {
		if len(warnings) == 0 {
			return ShareLinkConfig{}, apperr.New(apperr.CodeShareLinkInvalid, op,
				"no share link was given")
		}
		return ShareLinkConfig{}, apperr.WithDetails(apperr.New(apperr.CodeShareLinkInvalid, op,
			"none of the pasted lines is a usable share link"), warnings...)
	}

	document, err := skeleton(base.TemplateID)
	if err != nil {
		return ShareLinkConfig{}, err
	}
	taken := outboundTags(document)
	// The placeholder is replaced by the parsed outbounds, so its tag is free for a link to
	// take; every other tag in the skeleton is not.
	delete(taken, shareLinkPlaceholderTag)
	parsed = share.SetTags(parsed, func(tag string) bool { return taken[tag] })

	proxies := make([]any, 0, len(parsed)+1)
	for _, item := range parsed {
		for _, warning := range item.Warnings {
			warnings = append(warnings, commentFor(item, len(parsed), warning))
		}
		proxies = append(proxies, item.Outbound)
	}

	target := parsed[0].Tag
	if len(parsed) > 1 {
		// Several links: one automatic group picks among them, so the profile works without
		// the user choosing a server first. It leads the outbounds the way the selector
		// starter leads with its group.
		groupTag := uniqueTag(taken, "auto")
		proxies = append([]any{urlTestGroup(groupTag, parsed)}, proxies...)
		target = groupTag
	}
	document["outbounds"] = append(proxies, remainingOutbounds(document)...)
	retarget(document, target)

	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return ShareLinkConfig{}, apperr.Wrap(apperr.CodeInternal, op,
			"the generated configuration could not be encoded", err)
	}
	return ShareLinkConfig{
		ConfigJSON: string(encoded) + "\n",
		Name:       parsed[0].DisplayName,
		Kind:       parsed[0].Kind,
		Count:      len(parsed),
		Warnings:   warnings,
	}, nil
}

// shareLinkBase resolves the requested base, defaulting to the first one declared.
func shareLinkBase(id string) (ShareLinkBase, error) {
	bases := ShareLinkBases()
	wanted := strings.TrimSpace(id)
	if wanted == "" {
		return bases[0], nil
	}
	for _, base := range bases {
		if base.ID == wanted {
			return base, nil
		}
	}
	return ShareLinkBase{}, apperr.Newf(apperr.CodeInvalidArgument, "profiles.ShareLinkBase",
		"unknown base %q", wanted)
}

// skeleton decodes a starter into a document that can be edited. A generic JSON object is
// legitimate here for the same reason it is in the share package: sing-box configurations are
// open-ended, and this one is handed back to the user as JSON.
func skeleton(templateID string) (map[string]any, error) {
	const op = "profiles.Skeleton"
	tpl, err := templates.Get(templateID)
	if err != nil {
		return nil, err
	}
	document := map[string]any{}
	if err := json.Unmarshal([]byte(tpl.Config), &document); err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, op,
			"the starter this base is built on is not a JSON object", err)
	}
	if _, ok := document["outbounds"].([]any); !ok {
		return nil, apperr.Newf(apperr.CodeInternal, op,
			"the starter %q has no outbounds array", templateID)
	}
	return document, nil
}

// outboundTags collects every outbound tag in the document, which is what a parsed tag may not
// collide with.
func outboundTags(document map[string]any) map[string]bool {
	tags := map[string]bool{}
	for _, item := range asAnySlice(document["outbounds"]) {
		if object, ok := item.(map[string]any); ok {
			if tag, ok := object["tag"].(string); ok && tag != "" {
				tags[tag] = true
			}
		}
	}
	return tags
}

// remainingOutbounds returns the outbounds of the skeleton except the placeholder, keeping
// their order. They follow the proxies, so the document still ends with `direct`.
func remainingOutbounds(document map[string]any) []any {
	out := make([]any, 0, 4)
	for _, item := range asAnySlice(document["outbounds"]) {
		if object, ok := item.(map[string]any); ok {
			if tag, ok := object["tag"].(string); ok && tag == shareLinkPlaceholderTag {
				continue
			}
		}
		out = append(out, item)
	}
	return out
}

// urlTestGroup builds the group that picks among several links, in the shape the selector
// starter uses.
func urlTestGroup(tag string, parsed []share.Parsed) map[string]any {
	tags := make([]any, 0, len(parsed))
	for _, item := range parsed {
		tags = append(tags, item.Tag)
	}
	return map[string]any{
		"type":      "urltest",
		"tag":       tag,
		"outbounds": tags,
		"url":       "https://www.gstatic.com/generate_204",
		"interval":  urlTestInterval,
		"tolerance": 50,
	}
}

// retarget points the document at the outbound that now carries the traffic: the final outbound
// of the route, the rules that named the placeholder, and the DNS server that was resolved
// through it. Only the placeholder is replaced — everything else in the skeleton is left as the
// starter wrote it, including rules that deliberately route elsewhere.
func retarget(document map[string]any, target string) {
	if route, ok := document["route"].(map[string]any); ok {
		if final, ok := route["final"].(string); ok && final == shareLinkPlaceholderTag {
			route["final"] = target
		}
		for _, item := range asAnySlice(route["rules"]) {
			rule, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if outbound, ok := rule["outbound"].(string); ok && outbound == shareLinkPlaceholderTag {
				rule["outbound"] = target
			}
		}
	}
	dnsSection, ok := document["dns"].(map[string]any)
	if !ok {
		return
	}
	for _, item := range asAnySlice(dnsSection["servers"]) {
		server, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if detour, ok := server["detour"].(string); ok && detour == shareLinkPlaceholderTag {
			server["detour"] = target
		}
	}
}

// uniqueTag returns tag, or tag-2, tag-3 … when it is already taken, and records the answer.
func uniqueTag(taken map[string]bool, tag string) string {
	if !taken[tag] {
		taken[tag] = true
		return tag
	}
	for i := 2; ; i++ {
		candidate := tag + "-" + strconv.Itoa(i)
		if !taken[candidate] {
			taken[candidate] = true
			return candidate
		}
	}
}

// commentFor prefixes a warning with the link it came from, so a paste of several links does
// not leave the user guessing which one carries an unsupported parameter.
func commentFor(item share.Parsed, total int, warning string) string {
	if total <= 1 {
		return warning
	}
	return item.Tag + ": " + warning
}

// asAnySlice reads a JSON array field, tolerating a document that carries something else.
func asAnySlice(value any) []any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	return items
}
