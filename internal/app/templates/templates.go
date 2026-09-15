// Package templates holds the profile starters (spec §44).
//
// A template is a complete, valid sing-box configuration used as the initial
// revision of a new profile, or applied on top of an existing one. Applying a
// template never rewrites history: it creates a new revision.
//
// Every template must pass `sing-box check` with the managed binary as shipped:
// a starter that the app itself rejects teaches the user nothing and, because a
// rejected revision is never started (spec §37, §38), leaves them with a profile
// that cannot come up. Templates_test.go runs the real validator over all of
// them when a binary is available, so a sing-box release that removes a field
// cannot silently break the starters (1.12 removed the legacy DNS server form,
// 1.13 the legacy inbound fields, 1.14 the fallback domain resolver).
//
// Placeholders are valid-shaped on purpose — a reality public key and a
// 2022-blake3 password are checked by sing-box before it starts, so a readable
// "REPLACE_ME" there would make the template unusable. Everything the user has
// to replace is named, and the DNS servers are written in the post-1.12 form
// (type/server/tls) with route.default_domain_resolver set, which sing-box 1.14
// requires before it will resolve a domain for a dial.
package templates

import (
	"fmt"

	"github.com/larffxx/singboxui/internal/domain/apperr"
)

// Template is a named starting configuration.
type Template struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Description explains what the template is for, in the user's words.
	Description string `json:"description"`
	// Config is a complete sing-box configuration document.
	Config string `json:"config"`
	// RequiresPrivilege is true when starting it needs administrator rights
	// (the TUN inbound), which the UI must surface before the apply prompt.
	RequiresPrivilege bool `json:"requiresPrivilege"`
}

// All returns every built-in template, in display order.
func All() []Template {
	return []Template{
		{
			ID: "empty", Name: "Empty",
			Description: "Minimal configuration: one direct outbound, no inbounds. The starting point for a hand-written config.",
			Config: `{
  "log": {
    "level": "info",
    "timestamp": true
  },
  "outbounds": [
    {
      "type": "direct",
      "tag": "direct"
    }
  ]
}
`,
		},
		{
			ID: "tun-basic", Name: "TUN basic",
			Description:       "TUN inbound with a mixed-port fallback and direct routing. The VPN is on, but nothing is proxied yet.",
			RequiresPrivilege: true,
			Config: `{
  "log": {
    "level": "info",
    "timestamp": true
  },
  "dns": {
    "servers": [
      {
        "type": "https",
        "tag": "cloudflare",
        "server": "1.1.1.1",
        "server_port": 443,
        "path": "/dns-query",
        "tls": {
          "enabled": true,
          "server_name": "1.1.1.1"
        }
      },
      {
        "type": "local",
        "tag": "local"
      }
    ],
    "final": "cloudflare",
    "strategy": "prefer_ipv4"
  },
  "inbounds": [
    {
      "type": "tun",
      "tag": "tun-in",
      "address": [
        "172.19.0.1/30"
      ],
      "mtu": 9000,
      "auto_route": true,
      "strict_route": true,
      "stack": "system"
    },
    {
      "type": "mixed",
      "tag": "mixed-in",
      "listen": "127.0.0.1",
      "listen_port": 2080
    }
  ],
  "outbounds": [
    {
      "type": "direct",
      "tag": "direct"
    }
  ],
  "route": {
    "rules": [
      {
        "action": "sniff"
      },
      {
        "protocol": "dns",
        "action": "hijack-dns"
      },
      {
        "ip_is_private": true,
        "outbound": "direct"
      }
    ],
    "final": "direct",
    "auto_detect_interface": true,
    "default_domain_resolver": {
      "server": "cloudflare"
    }
  }
}
`,
		},
		{
			ID: "tun-vless-reality", Name: "TUN + VLESS Reality",
			Description:       "TUN inbound routed through a VLESS outbound with Reality TLS. Fill in the server address, port, UUID and public key.",
			RequiresPrivilege: true,
			Config: `{
  "log": {
    "level": "info",
    "timestamp": true
  },
  "dns": {
    "servers": [
      {
        "type": "https",
        "tag": "remote",
        "server": "1.1.1.1",
        "server_port": 443,
        "path": "/dns-query",
        "tls": {
          "enabled": true,
          "server_name": "1.1.1.1"
        },
        "detour": "proxy"
      },
      {
        "type": "local",
        "tag": "local"
      }
    ],
    "final": "remote",
    "strategy": "prefer_ipv4"
  },
  "inbounds": [
    {
      "type": "tun",
      "tag": "tun-in",
      "address": [
        "172.19.0.1/30"
      ],
      "mtu": 9000,
      "auto_route": true,
      "strict_route": true,
      "stack": "system"
    },
    {
      "type": "mixed",
      "tag": "mixed-in",
      "listen": "127.0.0.1",
      "listen_port": 2080
    }
  ],
  "outbounds": [
    {
      "type": "vless",
      "tag": "proxy",
      "server": "REPLACE_WITH_SERVER",
      "server_port": 443,
      "uuid": "REPLACE_WITH_UUID",
      "flow": "xtls-rprx-vision",
      "tls": {
        "enabled": true,
        "server_name": "REPLACE_WITH_SNI",
        "utls": {
          "enabled": true,
          "fingerprint": "chrome"
        },
        "reality": {
          "enabled": true,
          "public_key": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
          "short_id": ""
        }
      }
    },
    {
      "type": "direct",
      "tag": "direct"
    }
  ],
  "route": {
    "rules": [
      {
        "action": "sniff"
      },
      {
        "protocol": "dns",
        "action": "hijack-dns"
      },
      {
        "ip_is_private": true,
        "outbound": "direct"
      }
    ],
    "final": "proxy",
    "auto_detect_interface": true,
    "default_domain_resolver": {
      "server": "remote"
    }
  }
}
`,
		},
		{
			ID: "socks-local", Name: "SOCKS local proxy",
			Description: "No TUN device: a local SOCKS/mixed port routed through a Shadowsocks outbound. Useful for testing a server without touching system routing.",
			Config: `{
  "log": {
    "level": "info",
    "timestamp": true
  },
  "inbounds": [
    {
      "type": "mixed",
      "tag": "mixed-in",
      "listen": "127.0.0.1",
      "listen_port": 2080
    }
  ],
  "outbounds": [
    {
      "type": "shadowsocks",
      "tag": "proxy",
      "server": "REPLACE_WITH_SERVER",
      "server_port": 8388,
      "method": "2022-blake3-aes-128-gcm",
      "password": "AAAAAAAAAAAAAAAAAAAAAA=="
    },
    {
      "type": "direct",
      "tag": "direct"
    }
  ],
  "route": {
    "rules": [
      {
        "action": "sniff"
      }
    ],
    "final": "proxy"
  }
}
`,
		},
		{
			ID: "selector", Name: "Selector-based profile",
			Description: "Two proxied outbounds behind a selector plus a urltest group, so the active server can be switched while the VPN is running.",
			Config: `{
  "log": {
    "level": "info",
    "timestamp": true
  },
  "dns": {
    "servers": [
      {
        "type": "https",
        "tag": "remote",
        "server": "1.1.1.1",
        "server_port": 443,
        "path": "/dns-query",
        "tls": {
          "enabled": true,
          "server_name": "1.1.1.1"
        },
        "detour": "select"
      }
    ],
    "final": "remote",
    "strategy": "prefer_ipv4"
  },
  "inbounds": [
    {
      "type": "mixed",
      "tag": "mixed-in",
      "listen": "127.0.0.1",
      "listen_port": 2080
    }
  ],
  "outbounds": [
    {
      "type": "selector",
      "tag": "select",
      "outbounds": [
        "auto",
        "server-a",
        "server-b",
        "direct"
      ],
      "default": "auto"
    },
    {
      "type": "urltest",
      "tag": "auto",
      "outbounds": [
        "server-a",
        "server-b"
      ],
      "url": "https://www.gstatic.com/generate_204",
      "interval": "5m",
      "tolerance": 50
    },
    {
      "type": "vless",
      "tag": "server-a",
      "server": "REPLACE_WITH_SERVER_A",
      "server_port": 443,
      "uuid": "REPLACE_WITH_UUID",
      "tls": {
        "enabled": true,
        "server_name": "REPLACE_WITH_SNI",
        "utls": {
          "enabled": true,
          "fingerprint": "chrome"
        }
      }
    },
    {
      "type": "trojan",
      "tag": "server-b",
      "server": "REPLACE_WITH_SERVER_B",
      "server_port": 443,
      "password": "REPLACE_WITH_PASSWORD",
      "tls": {
        "enabled": true,
        "server_name": "REPLACE_WITH_SNI"
      }
    },
    {
      "type": "direct",
      "tag": "direct"
    }
  ],
  "route": {
    "rules": [
      {
        "action": "sniff"
      }
    ],
    "final": "select",
    "default_domain_resolver": {
      "server": "remote"
    }
  }
}
`,
		},
	}
}

// Get returns a template by id.
func Get(id string) (Template, error) {
	for _, t := range All() {
		if t.ID == id {
			return t, nil
		}
	}
	return Template{}, apperr.Newf(apperr.CodeNotFound, "templates.Get", "unknown template %q", id)
}

// IDs lists the available template identifiers.
func IDs() []string {
	all := All()
	out := make([]string, 0, len(all))
	for _, t := range all {
		out = append(out, t.ID)
	}
	return out
}

// MustGet is Get for compile-time constants; it panics on an unknown id.
func MustGet(id string) Template {
	t, err := Get(id)
	if err != nil {
		panic(fmt.Sprintf("templates: %s", err))
	}
	return t
}
