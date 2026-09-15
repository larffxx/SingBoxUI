// Package share parses and builds proxy share links in the formats users paste
// from subscription providers, and converts them to/from sing-box outbound
// objects (spec §39–§40).
//
// It is deliberately isolated from persistence: Parse returns a *candidate*
// outbound — a plain map[string]any in sing-box outbound shape — which the
// application layer may inspect, edit, store in a revision, or discard. This is
// the one place where a generic JSON object is legitimate, because sing-box
// outbounds are open-ended (spec §36/§32). Build is the inverse operation used
// for export.
//
// Supported link schemes: vless, vmess, trojan, ss (shadowsocks), hy2 and
// hysteria2, tuic. Aliases `shadowsocks://` and `hy2://` are accepted.
//
// Every failure is a typed *apperr.Error with Code == CodeShareLinkInvalid and
// a detail naming the missing or malformed field. Credentials are never placed
// in a message, a detail, a warning or a log line.
package share

import (
	"fmt"
	"strings"

	"github.com/larffxx/singboxui/internal/domain/apperr"
)

// Canonical sing-box outbound types / parsed kinds.
const (
	KindVless       = "vless"
	KindVmess       = "vmess"
	KindTrojan      = "trojan"
	KindShadowsocks = "shadowsocks"
	KindHysteria2   = "hysteria2"
	KindTuic        = "tuic"
)

const (
	opParse = "share.Parse"
	opBuild = "share.Build"
)

// Parsed is a parsed share link: the kind of proxy, the tag that must be
// assigned to the outbound, the candidate outbound itself and the remarks the
// link carried.
//
// DisplayName is the sanitised remarks as written in the link, before tag
// uniquification; Tag is the final, unique tag also stored in
// Outbound["tag"]. Warnings describe link parameters that had no equivalent
// sing-box field and were therefore not carried over.
type Parsed struct {
	Kind        string         `json:"kind"`
	Tag         string         `json:"tag"`
	Outbound    map[string]any `json:"outbound"`
	DisplayName string         `json:"displayName"`
	Warnings    []string       `json:"warnings,omitempty"`
}

// Supported returns the share-link schemes Parse accepts, in the canonical
// spelling. `shadowsocks` and `hy2` are accepted aliases of `ss` and
// `hysteria2`. A fresh slice is returned on every call.
func Supported() []string {
	return []string{KindVless, KindVmess, KindTrojan, "ss", KindHysteria2, KindTuic}
}

// Kind reports the canonical outbound type a link would produce, or "" when
// the scheme is not a supported share link. It inspects only the scheme and
// never fails, which lets a caller route a pasted blob before parsing it.
func Kind(link string) string {
	switch schemeOf(link) {
	case "vless":
		return KindVless
	case "vmess":
		return KindVmess
	case "trojan":
		return KindTrojan
	case "ss", "shadowsocks":
		return KindShadowsocks
	case "hy2", "hysteria2":
		return KindHysteria2
	case "tuic":
		return KindTuic
	}
	return ""
}

// Parse turns one share link into a candidate sing-box outbound. The tag is
// derived from the URL fragment (#remarks) and sanitised; use ParseWithUsed or
// SetTags when the tag must be unique inside a profile.
func Parse(link string) (Parsed, error) {
	return parseLink(link, nil)
}

// ParseWithUsed is Parse, but the returned tag is guaranteed not to collide
// with any tag for which used returns true (a "-2", "-3", … suffix is added as
// needed). used is read-only; the caller keeps ownership of its state.
func ParseWithUsed(link string, used func(string) bool) (Parsed, error) {
	return parseLink(link, used)
}

func parseLink(link string, used func(string) bool) (Parsed, error) {
	trimmed := strings.TrimSpace(link)
	if trimmed == "" {
		return Parsed{}, invalidErr(opParse, "share link is empty")
	}
	scheme := schemeOf(trimmed)
	if scheme == "" {
		return Parsed{}, invalidErr(opParse, "share link has no scheme",
			"expected one of: "+strings.Join(Supported(), ", "))
	}
	var (
		parsed Parsed
		err    error
	)
	switch scheme {
	case "vless":
		parsed, err = parseVless(trimmed)
	case "vmess":
		parsed, err = parseVmess(trimmed)
	case "trojan":
		parsed, err = parseTrojan(trimmed)
	case "ss", "shadowsocks":
		parsed, err = parseShadowsocks(trimmed)
	case "hy2", "hysteria2":
		parsed, err = parseHysteria2(trimmed)
	case "tuic":
		parsed, err = parseTuic(trimmed)
	default:
		return Parsed{}, invalidErr(opParse, "unsupported share link scheme",
			"scheme: "+scheme, "expected one of: "+strings.Join(Supported(), ", "))
	}
	if err != nil {
		return Parsed{}, err
	}
	parsed.Kind = Kind(trimmed)
	return finalizeTag(parsed, used), nil
}

// SetTags assigns every item a unique, sanitised tag and mirrors it into
// Outbound["tag"], consulting used for names already taken outside the slice
// (for example by other outbounds in the profile). The slice is modified and
// returned for convenience.
func SetTags(items []Parsed, used func(string) bool) []Parsed {
	taken := make(map[string]bool, len(items))
	isUsed := func(tag string) bool {
		if taken[tag] {
			return true
		}
		return used != nil && used(tag)
	}
	for i := range items {
		def := defaultTag(items[i].Kind)
		base := items[i].DisplayName
		if base == "" {
			base = items[i].Tag
		}
		base = sanitizeTag(base, def)
		tag := uniqueTag(base, isUsed)
		taken[tag] = true
		if items[i].Outbound == nil {
			items[i].Outbound = map[string]any{}
		}
		items[i].Outbound["tag"] = tag
		items[i].Tag = tag
		items[i].DisplayName = base
	}
	return items
}

// ParseList parses several links pasted at once (one per line or separated by
// whitespace). Lines starting with '#' or '//' are treated as comments and
// skipped. Successful links are returned in order with unique tags; each failed
// link contributes one error carrying its 1-based source line in the details.
func ParseList(text string) ([]Parsed, []error) {
	var (
		parsed []Parsed
		errs   []error
	)
	for lineNo, line := range strings.Split(text, "\n") {
		for _, field := range strings.Fields(line) {
			// A field starting with "#" or "//" is a comment; the rest of
			// the line is a comment too when the marker is inline.
			if strings.HasPrefix(field, "#") || strings.HasPrefix(field, "//") {
				break
			}
			item, err := parseLink(field, nil)
			if err != nil {
				errs = append(errs, withLine(err, lineNo+1))
				continue
			}
			parsed = append(parsed, item)
		}
	}
	return SetTags(parsed, nil), errs
}

// ---------------------------------------------------------------------------
// tag handling
// ---------------------------------------------------------------------------

func finalizeTag(p Parsed, used func(string) bool) Parsed {
	def := defaultTag(p.Kind)
	display := sanitizeTag(p.Tag, def)
	tag := uniqueTag(display, used)
	if p.Outbound == nil {
		p.Outbound = map[string]any{}
	}
	p.Outbound["tag"] = tag
	p.Tag = tag
	p.DisplayName = display
	return p
}

func uniqueTag(base string, used func(string) bool) string {
	if used == nil || !used(base) {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !used(candidate) {
			return candidate
		}
	}
}

// sanitizeTag turns remarks into a safe sing-box tag: control characters are
// dropped, whitespace is collapsed and the result is bounded in length. An
// empty or unusable remark falls back to the protocol default.
func sanitizeTag(raw, def string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return def
	}
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return def
	}
	if r := []rune(s); len(r) > 100 {
		s = strings.TrimSpace(string(r[:100]))
	}
	return s
}

func defaultTag(kind string) string {
	switch kind {
	case KindVless:
		return "vless"
	case KindVmess:
		return "vmess"
	case KindTrojan:
		return "trojan"
	case KindShadowsocks:
		return "ss"
	case KindHysteria2:
		return "hysteria2"
	case KindTuic:
		return "tuic"
	}
	return "outbound"
}

func schemeOf(link string) string {
	s := strings.TrimSpace(link)
	i := strings.Index(s, "://")
	if i <= 0 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(s[:i]))
}

// withLine decorates a typed error with the source line of a bulk paste.
func withLine(err error, line int) error {
	if typed, ok := apperr.As(err); ok {
		return apperr.WithDetails(typed, fmt.Sprintf("line %d", line))
	}
	return err
}
