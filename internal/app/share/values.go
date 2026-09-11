package share

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"net/url"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// generic sing-box outbound value access
//
// A sing-box outbound is an open-ended JSON object (spec §36/§32), so the
// candidate model is a plain map[string]any. These helpers read values back
// regardless of whether the map came straight from a parser (native Go types)
// or from a JSON round trip (float64 / []any / json.Number).
// ---------------------------------------------------------------------------

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func asBool(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return isTrue(b)
	}
	return false
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asStrings(v any) []string {
	switch t := v.(type) {
	case []string:
		return append([]string(nil), t...)
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		return splitList(t)
	}
	return nil
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float32:
		if f := float64(n); f == math.Trunc(f) {
			return int(f), true
		}
	case float64:
		if n == math.Trunc(n) {
			return int(n), true
		}
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i), true
		}
		if f, err := n.Float64(); err == nil && f == math.Trunc(f) {
			return int(f), true
		}
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
			return i, true
		}
	}
	return 0, false
}

func intDefault(v any, def int) int {
	if n, ok := toInt(v); ok {
		return n
	}
	return def
}

func valueOr(v, def string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return def
}

func isTrue(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ---------------------------------------------------------------------------
// URL / base64 helpers
// ---------------------------------------------------------------------------

// orderedQuery builds a query string in insertion order so that built links
// are byte-for-byte stable (round-trip tests rely on it).
type orderedQuery struct {
	keys []string
	vals map[string]string
}

func newQuery() *orderedQuery { return &orderedQuery{vals: make(map[string]string)} }

func (q *orderedQuery) set(key, value string) {
	if _, ok := q.vals[key]; !ok {
		q.keys = append(q.keys, key)
	}
	q.vals[key] = value
}

func (q *orderedQuery) Encode() string {
	parts := make([]string, 0, len(q.keys))
	for _, k := range q.keys {
		v, ok := q.vals[k]
		if !ok {
			continue
		}
		parts = append(parts, queryEscape(k)+"="+queryEscape(v))
	}
	return strings.Join(parts, "&")
}

// queryEscape percent-encodes a fragment or query component; url.QueryEscape
// renders a space as '+', which some share-link readers do not decode.
func queryEscape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

// decodeFragment is a best-effort percent-decode of a raw fragment; the value
// is returned unchanged when it is not valid percent-encoding.
func decodeFragment(s string) string {
	if s == "" {
		return ""
	}
	if v, err := url.QueryUnescape(s); err == nil {
		return v
	}
	return s
}

// hostPort renders host:port, bracketing IPv6 literals.
func hostPort(host string, port int) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return "[" + host + "]:" + strconv.Itoa(port)
	}
	return host + ":" + strconv.Itoa(port)
}

// decodeBase64 accepts standard and URL-safe base64, with or without padding.
func decodeBase64(s string) ([]byte, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, false
	}
	s = strings.NewReplacer("-", "+", "_", "/").Replace(s)
	s = strings.TrimRight(s, "=")
	if rem := len(s) % 4; rem != 0 {
		s += strings.Repeat("=", 4-rem)
	}
	if len(s)%4 != 0 {
		return nil, false
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, false
	}
	return b, true
}

func encodeBase64(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}
