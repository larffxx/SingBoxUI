// Package config contains pure sing-box configuration helpers: parsing that
// preserves every field, early structural validation and small queries.
//
// Persistence never re-serialises user JSON: revisions store the exact bytes the
// user produced, so unknown and future sing-box options always survive (spec §36).
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Result is the outcome of structural validation.
type Result struct {
	OK       bool     `json:"ok"`
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
}

// Parse decodes a configuration preserving number literals.
func Parse(raw []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var root map[string]any
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	// Reject trailing content so a truncated paste cannot pass validation.
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("invalid JSON: unexpected trailing data")
	}
	return root, nil
}

// Pretty re-indents a configuration object. Used for display and export only,
// never to replace stored revision bytes.
func Pretty(raw []byte) ([]byte, error) {
	root, err := Parse(raw)
	if err != nil {
		return nil, err
	}
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// Marshal renders a configuration map back to JSON.
func Marshal(root map[string]any) ([]byte, error) {
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// remoteOutboundTypes require server/server_port credentials.
var remoteOutboundTypes = map[string]bool{
	"shadowsocks": true, "vmess": true, "vless": true, "trojan": true,
	"socks": true, "http": true, "wireguard": true, "hysteria": true,
	"hysteria2": true, "tuic": true, "anytls": true, "snell": true,
	"naive": true, "shadowtls": true, "tor": true, "ssh": true,
}

// Validate performs early structural validation. It is deliberately shallow:
// the authoritative semantic validator is `sing-box check` (spec §37).
func Validate(raw []byte) Result {
	res := Result{OK: true, Errors: []string{}, Warnings: []string{}}
	root, err := Parse(raw)
	if err != nil {
		res.OK = false
		res.Errors = append(res.Errors, err.Error())
		return res
	}

	for _, section := range []string{"inbounds", "outbounds", "endpoints"} {
		raw, ok := root[section]
		if !ok {
			continue
		}
		entries, ok := raw.([]any)
		if !ok {
			res.Errors = append(res.Errors, section+" must be an array")
			continue
		}
		seen := map[string]int{}
		for i, entry := range entries {
			obj, ok := entry.(map[string]any)
			if !ok {
				res.Errors = append(res.Errors, fmt.Sprintf("%s[%d] must be an object", section, i))
				continue
			}
			tag, _ := obj["tag"].(string)
			if strings.TrimSpace(tag) == "" {
				res.Errors = append(res.Errors, fmt.Sprintf("%s[%d]: missing tag", section, i))
			} else if prev, dup := seen[tag]; dup {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: duplicate tag %q (indexes %d and %d)", section, tag, prev, i))
			} else {
				seen[tag] = i
			}
			if _, ok := obj["type"].(string); !ok {
				res.Errors = append(res.Errors, fmt.Sprintf("%s %q: missing type", section, tag))
				continue
			}
			if section == "outbounds" {
				validateOutbound(obj, tag, &res)
			}
		}
	}

	if raw, ok := root["outbounds"]; ok {
		if entries, ok := raw.([]any); ok && len(entries) == 0 {
			res.Errors = append(res.Errors, "outbounds must be a non-empty array")
		}
	}

	if raw, ok := root["route"]; ok {
		route, ok := raw.(map[string]any)
		if !ok {
			res.Errors = append(res.Errors, "route must be an object")
		} else if rulesRaw, ok := route["rules"]; ok {
			rules, ok := rulesRaw.([]any)
			if !ok {
				res.Errors = append(res.Errors, "route.rules must be an array")
			} else {
				for i, rule := range rules {
					if _, ok := rule.(map[string]any); !ok {
						res.Errors = append(res.Errors, fmt.Sprintf("route.rules[%d] must be an object", i))
					}
				}
			}
		}
	}

	if raw, ok := root["dns"]; ok {
		if dns, ok := raw.(map[string]any); ok {
			if serversRaw, ok := dns["servers"]; ok {
				servers, ok := serversRaw.([]any)
				if !ok {
					res.Errors = append(res.Errors, "dns.servers must be an array")
				} else {
					for i, server := range servers {
						obj, ok := server.(map[string]any)
						if !ok {
							res.Errors = append(res.Errors, fmt.Sprintf("dns.servers[%d] must be an object", i))
							continue
						}
						if _, ok := obj["tag"].(string); !ok {
							res.Warnings = append(res.Warnings, fmt.Sprintf("dns.servers[%d]: missing tag", i))
						}
					}
				}
			}
		} else {
			res.Errors = append(res.Errors, "dns must be an object")
		}
	}

	if hasTUN(root) {
		res.Warnings = append(res.Warnings, "configuration contains a tun inbound: starting it requires administrator privileges")
	}

	res.OK = len(res.Errors) == 0
	return res
}

func validateOutbound(obj map[string]any, tag string, res *Result) {
	typ, _ := obj["type"].(string)
	label := fmt.Sprintf("outbound %q (%s)", tag, typ)
	if remoteOutboundTypes[typ] {
		server, _ := obj["server"].(string)
		if strings.TrimSpace(server) == "" {
			res.Errors = append(res.Errors, label+": missing server")
		}
		if _, ok := obj["server_port"]; !ok {
			res.Errors = append(res.Errors, label+": missing server_port")
		}
	}
	switch typ {
	case "selector", "urltest":
		if list, ok := obj["outbounds"].([]any); !ok || len(list) == 0 {
			res.Errors = append(res.Errors, label+": missing outbounds list")
		}
	case "vless":
		if s, _ := obj["uuid"].(string); strings.TrimSpace(s) == "" {
			res.Errors = append(res.Errors, label+": missing uuid")
		}
	case "vmess":
		if s, _ := obj["uuid"].(string); strings.TrimSpace(s) == "" {
			res.Errors = append(res.Errors, label+": missing uuid")
		}
	case "trojan", "naive":
		if s, _ := obj["password"].(string); strings.TrimSpace(s) == "" {
			res.Errors = append(res.Errors, label+": missing password")
		}
	case "shadowsocks":
		if s, _ := obj["method"].(string); strings.TrimSpace(s) == "" {
			res.Errors = append(res.Errors, label+": missing method")
		}
		if s, _ := obj["password"].(string); strings.TrimSpace(s) == "" {
			res.Errors = append(res.Errors, label+": missing password")
		}
	case "wireguard":
		if s, _ := obj["private_key"].(string); strings.TrimSpace(s) == "" {
			res.Errors = append(res.Errors, label+": missing private_key")
		}
	case "tuic", "hysteria2":
		_, hasUUID := obj["uuid"]
		_, hasPassword := obj["password"]
		if !hasUUID && !hasPassword {
			res.Errors = append(res.Errors, label+": missing uuid/password")
		}
	}
}

// HasTUN reports whether the configuration needs a privileged runtime.
func HasTUN(raw []byte) bool {
	root, err := Parse(raw)
	if err != nil {
		return false
	}
	return hasTUN(root)
}

func hasTUN(root map[string]any) bool {
	for _, section := range []string{"inbounds", "endpoints"} {
		entries, ok := root[section].([]any)
		if !ok {
			continue
		}
		for _, entry := range entries {
			obj, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			if typ, _ := obj["type"].(string); typ == "tun" {
				return true
			}
		}
	}
	return false
}

// Tags lists the tags of a section, sorted, for UI suggestions.
func Tags(raw []byte, section string) []string {
	root, err := Parse(raw)
	if err != nil {
		return nil
	}
	entries, ok := root[section].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if obj, ok := entry.(map[string]any); ok {
			if tag, ok := obj["tag"].(string); ok && tag != "" {
				out = append(out, tag)
			}
		}
	}
	sort.Strings(out)
	return out
}
