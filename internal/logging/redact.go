// Package logging configures structured logging and centralises secret redaction.
package logging

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Redacted is the placeholder replacing every secret value.
const Redacted = "[REDACTED]"

// sensitiveKeys are configuration keys whose values are credentials.
var sensitiveKeys = map[string]bool{
	"password":         true,
	"private_key":      true,
	"pre_shared_key":   true,
	"uuid":             true,
	"token":            true,
	"authorization":    true,
	"auth_str":         true,
	"obfs_password":    true,
	"secret":           true,
	"public_key":       false, // public keys are not secrets; kept for readability
	"subscription_url": true,
	"share_link":       true,
}

// shareLinkRe matches proxy share URLs, whose body contains credentials.
var shareLinkRe = regexp.MustCompile(`(?i)\b(vless|vmess|trojan|ss|ssr|hy2|hysteria2|tuic)://[^\s"'<>]+`)

// RedactJSON returns the input with every credential value replaced.
// Invalid JSON is redacted textually so nothing leaks through a parse failure.
func RedactJSON(raw []byte) []byte {
	var root any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&root); err != nil {
		return []byte(RedactString(string(raw)))
	}
	redacted := redactValue(root)
	out, err := json.Marshal(redacted)
	if err != nil {
		return []byte(RedactString(string(raw)))
	}
	return out
}

func redactValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			if sensitiveKeys[strings.ToLower(key)] {
				out[key] = Redacted
				continue
			}
			out[key] = redactValue(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = redactValue(item)
		}
		return out
	default:
		return value
	}
}

// RedactString removes share links and obvious credentials from free text.
func RedactString(s string) string {
	out := shareLinkRe.ReplaceAllString(s, "$1://"+Redacted)
	out = strings.ReplaceAll(out, "\n", " ")
	return out
}

// RedactValue is a convenience for single scalars (optionally secret).
func RedactValue(key, value string) string {
	if sensitiveKeys[strings.ToLower(key)] {
		return Redacted
	}
	return RedactString(value)
}
