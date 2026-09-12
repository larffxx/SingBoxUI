package share

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/larffxx/singboxui/internal/domain/apperr"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// dig returns the value at a dotted path inside nested map[string]any values.
func dig(m map[string]any, path string) any {
	var cur any = m
	for _, part := range strings.Split(path, ".") {
		next, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = next[part]
	}
	return cur
}

func vmessLink(t *testing.T, payload map[string]any, fragment string) string {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal vmess payload: %v", err)
	}
	link := "vmess://" + encodeBase64(string(data))
	if fragment != "" {
		link += "#" + fragment
	}
	return link
}

func mustParse(t *testing.T, link string) Parsed {
	t.Helper()
	parsed, err := Parse(link)
	if err != nil {
		t.Fatalf("Parse(%q) returned error: %v", link, err)
	}
	return parsed
}

func wantCode(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected a %s error, got nil", apperr.CodeShareLinkInvalid)
	}
	if code := apperr.CodeOf(err); code != apperr.CodeShareLinkInvalid {
		t.Fatalf("expected code %s, got %s (%v)", apperr.CodeShareLinkInvalid, code, err)
	}
}

// ---------------------------------------------------------------------------
// Supported / Kind
// ---------------------------------------------------------------------------

func TestSupported(t *testing.T) {
	want := []string{"vless", "vmess", "trojan", "ss", "hysteria2", "tuic"}
	if got := Supported(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Supported() = %v, want %v", got, want)
	}
	got := Supported()
	got[0] = "mutated"
	if Supported()[0] != "vless" {
		t.Fatal("Supported must return a fresh slice each call")
	}
}

func TestKind(t *testing.T) {
	tests := []struct {
		name string
		link string
		want string
	}{
		{"vless", "vless://uuid@example.com:443", KindVless},
		{"vmess", "vmess://eyJ2IjoiMiJ9", KindVmess},
		{"trojan", "trojan://pw@example.com:443", KindTrojan},
		{"ss", "ss://YWVzOnB3@example.com:8388", KindShadowsocks},
		{"shadowsocks alias", "shadowsocks://x@example.com:8388", KindShadowsocks},
		{"hy2 alias", "hy2://pw@example.com:443", KindHysteria2},
		{"hysteria2", "hysteria2://pw@example.com:443", KindHysteria2},
		{"tuic", "tuic://uuid:pw@example.com:443", KindTuic},
		{"uppercase scheme", "VLESS://uuid@example.com:443", KindVless},
		{"surrounding whitespace", "  trojan://pw@example.com:443  ", KindTrojan},
		{"unsupported scheme", "http://example.com", ""},
		{"ssr not supported", "ssr://abc", ""},
		{"no scheme", "example.com", ""},
		{"empty", "", ""},
		{"scheme only", "vless://", KindVless},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Kind(tc.link); got != tc.want {
				t.Fatalf("Kind(%q) = %q, want %q", tc.link, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Parse — valid links
// ---------------------------------------------------------------------------

func TestParseValid(t *testing.T) {
	tests := []struct {
		name        string
		link        string
		wantKind    string
		wantTag     string
		wantDisplay string
		wantFields  map[string]any
	}{
		{
			name:        "vless websocket tls",
			link:        "vless://11111111-1111-1111-1111-111111111111@vpn.example.com:443?security=tls&sni=vpn.example.com&type=ws&path=%2Fws&host=edge.example.com#Tokyo%20Node",
			wantKind:    KindVless,
			wantTag:     "Tokyo Node",
			wantDisplay: "Tokyo Node",
			wantFields: map[string]any{
				"type":                   "vless",
				"server":                 "vpn.example.com",
				"server_port":            443,
				"uuid":                   "11111111-1111-1111-1111-111111111111",
				"packet_encoding":        "xudp",
				"tls.enabled":            true,
				"tls.server_name":        "vpn.example.com",
				"transport.type":         "ws",
				"transport.path":         "/ws",
				"transport.headers.Host": "edge.example.com",
				"tag":                    "Tokyo Node",
			},
		},
		{
			name:        "vless reality tcp with flow and utls",
			link:        "vless://22222222-2222-2222-2222-222222222222@r.example.com:443?security=reality&sni=www.microsoft.com&fp=chrome&pbk=PUBKEY&sid=abcd&flow=xtls-rprx-vision&type=tcp&alpn=h2,http/1.1#Reality",
			wantKind:    KindVless,
			wantTag:     "Reality",
			wantDisplay: "Reality",
			wantFields: map[string]any{
				"server_port":            443,
				"flow":                   "xtls-rprx-vision",
				"tls.enabled":            true,
				"tls.server_name":        "www.microsoft.com",
				"tls.utls.enabled":       true,
				"tls.utls.fingerprint":   "chrome",
				"tls.reality.enabled":    true,
				"tls.reality.public_key": "PUBKEY",
				"tls.reality.short_id":   "abcd",
			},
		},
		{
			name:        "vless without tls has no tls object",
			link:        "vless://33333333-3333-3333-3333-333333333333@plain.example.com:8080?type=tcp#Plain",
			wantKind:    KindVless,
			wantTag:     "Plain",
			wantDisplay: "Plain",
			wantFields: map[string]any{
				"tls.enabled": nil,
				"tls":         nil,
			},
		},
		{
			name:        "trojan grpc tls",
			link:        "trojan://secret@tj.example.com:443?security=tls&sni=tj.example.com&type=grpc&serviceName=grpcsvc#TJ",
			wantKind:    KindTrojan,
			wantTag:     "TJ",
			wantDisplay: "TJ",
			wantFields: map[string]any{
				"password":               "secret",
				"tls.enabled":            true,
				"tls.server_name":        "tj.example.com",
				"transport.type":         "grpc",
				"transport.service_name": "grpcsvc",
			},
		},
		{
			name:        "trojan defaults to tls",
			link:        "trojan://secret@tj.example.com:443#NoSec",
			wantKind:    KindTrojan,
			wantTag:     "NoSec",
			wantDisplay: "NoSec",
			wantFields: map[string]any{
				"tls.enabled": true,
			},
		},
		{
			name:        "trojan security none disables tls",
			link:        "trojan://secret@tj.example.com:443?security=none#NoTLS",
			wantKind:    KindTrojan,
			wantTag:     "NoTLS",
			wantDisplay: "NoTLS",
			wantFields: map[string]any{
				"tls": nil,
			},
		},
		{
			name:        "ss sip002 base64 userinfo",
			link:        "ss://" + encodeBase64("aes-256-gcm:s3cr3t") + "@ss.example.com:8388#SS%20Node",
			wantKind:    KindShadowsocks,
			wantTag:     "SS Node",
			wantDisplay: "SS Node",
			wantFields: map[string]any{
				"method":      "aes-256-gcm",
				"password":    "s3cr3t",
				"server":      "ss.example.com",
				"server_port": 8388,
			},
		},
		{
			name:        "ss legacy fully base64",
			link:        "ss://" + encodeBase64("aes-256-gcm:s3cr3t@ss.example.com:8388") + "#Legacy",
			wantKind:    KindShadowsocks,
			wantTag:     "Legacy",
			wantDisplay: "Legacy",
			wantFields: map[string]any{
				"method":      "aes-256-gcm",
				"password":    "s3cr3t",
				"server":      "ss.example.com",
				"server_port": 8388,
			},
		},
		{
			name:        "ss plaintext percent-encoded userinfo",
			link:        "ss://aes-256-gcm%3As3cr3t@ss.example.com:8388#Plain",
			wantKind:    KindShadowsocks,
			wantTag:     "Plain",
			wantDisplay: "Plain",
			wantFields: map[string]any{
				"method":   "aes-256-gcm",
				"password": "s3cr3t",
			},
		},
		{
			name:        "ss with v2ray plugin",
			link:        "ss://" + encodeBase64("aes-256-gcm:s3cr3t") + "@ss.example.com:8388?plugin=v2ray-plugin%3Bmode%3Dwebsocket#PL",
			wantKind:    KindShadowsocks,
			wantTag:     "PL",
			wantDisplay: "PL",
			wantFields: map[string]any{
				"plugin":      "v2ray-plugin",
				"plugin_opts": "mode=websocket",
			},
		},
		{
			name:        "hysteria2 full",
			link:        "hysteria2://pw@hy.example.com:443?sni=hy.example.com&insecure=1&obfs=salamander&obfs-password=op#HY",
			wantKind:    KindHysteria2,
			wantTag:     "HY",
			wantDisplay: "HY",
			wantFields: map[string]any{
				"password":        "pw",
				"tls.enabled":     true,
				"tls.server_name": "hy.example.com",
				"tls.insecure":    true,
				"obfs.type":       "salamander",
				"obfs.password":   "op",
			},
		},
		{
			name:        "hy2 alias",
			link:        "hy2://pw@hy.example.com:443#Short",
			wantKind:    KindHysteria2,
			wantTag:     "Short",
			wantDisplay: "Short",
			wantFields: map[string]any{
				"tls.enabled": true,
			},
		},
		{
			name:        "tuic full",
			link:        "tuic://aaaabbbb-0000-1111-2222-333344445555:pp@tu.example.com:443?sni=tu.example.com&congestion_control=bbr&udp_relay_mode=native&insecure=1&alpn=h3#TU",
			wantKind:    KindTuic,
			wantTag:     "TU",
			wantDisplay: "TU",
			wantFields: map[string]any{
				"uuid":               "aaaabbbb-0000-1111-2222-333344445555",
				"password":           "pp",
				"tls.enabled":        true,
				"tls.server_name":    "tu.example.com",
				"tls.insecure":       true,
				"congestion_control": "bbr",
				"udp_relay_mode":     "native",
			},
		},
		{
			name:        "ipv6 literal host",
			link:        "trojan://pw@[2001:db8::1]:443?security=tls#V6",
			wantKind:    KindTrojan,
			wantTag:     "V6",
			wantDisplay: "V6",
			wantFields: map[string]any{
				"server":      "2001:db8::1",
				"server_port": 443,
			},
		},
		{
			name:        "missing fragment falls back to protocol default tag",
			link:        "vless://44444444-4444-4444-4444-444444444444@h.example.com:443?security=tls",
			wantKind:    KindVless,
			wantTag:     "vless",
			wantDisplay: "vless",
			wantFields: map[string]any{
				"server_port": 443,
			},
		},
		{
			name:        "vmess plain",
			link:        vmessLink(t, map[string]any{"v": "2", "ps": "My VMess", "add": "vm.example.com", "port": "443", "id": "uuid-1", "aid": "0", "scy": "auto", "net": "tcp", "type": "none"}, ""),
			wantKind:    KindVmess,
			wantTag:     "My VMess",
			wantDisplay: "My VMess",
			wantFields: map[string]any{
				"server":      "vm.example.com",
				"server_port": 443,
				"uuid":        "uuid-1",
				"alter_id":    0,
				"security":    "auto",
				"tls":         nil,
			},
		},
		{
			name:        "vmess websocket tls",
			link:        vmessLink(t, map[string]any{"v": "2", "ps": "WS", "add": "vm.example.com", "port": 8443, "id": "uuid-2", "aid": 0, "scy": "chacha20-poly1305", "net": "ws", "type": "none", "host": "cdn.example.com", "path": "/ray", "tls": "tls", "sni": "cdn.example.com", "fp": "chrome"}, "WS"),
			wantKind:    KindVmess,
			wantTag:     "WS",
			wantDisplay: "WS",
			wantFields: map[string]any{
				"server_port":            8443,
				"security":               "chacha20-poly1305",
				"tls.enabled":            true,
				"tls.server_name":        "cdn.example.com",
				"tls.utls.fingerprint":   "chrome",
				"transport.type":         "ws",
				"transport.path":         "/ray",
				"transport.headers.Host": "cdn.example.com",
			},
		},
		{
			name:        "vmess tcp http header type becomes http transport",
			link:        vmessLink(t, map[string]any{"v": "2", "ps": "HTTP", "add": "vm.example.com", "port": 80, "id": "uuid-3", "net": "tcp", "type": "http", "path": "/x", "host": "h.example.com"}, ""),
			wantKind:    KindVmess,
			wantTag:     "HTTP",
			wantDisplay: "HTTP",
			wantFields: map[string]any{
				"transport.type": "http",
				"transport.path": "/x",
				"transport.host": "h.example.com",
			},
		},
		{
			name:        "vmess fragment overrides ps",
			link:        vmessLink(t, map[string]any{"v": "2", "ps": "Remarks", "add": "vm.example.com", "port": 443, "id": "uuid-4", "net": "tcp"}, "Fragment"),
			wantKind:    KindVmess,
			wantTag:     "Fragment",
			wantDisplay: "Fragment",
			wantFields: map[string]any{
				"tag": "Fragment",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parsed := mustParse(t, tc.link)
			if parsed.Kind != tc.wantKind {
				t.Fatalf("Kind = %q, want %q", parsed.Kind, tc.wantKind)
			}
			if parsed.Tag != tc.wantTag {
				t.Fatalf("Tag = %q, want %q", parsed.Tag, tc.wantTag)
			}
			if parsed.DisplayName != tc.wantDisplay {
				t.Fatalf("DisplayName = %q, want %q", parsed.DisplayName, tc.wantDisplay)
			}
			if parsed.Outbound == nil {
				t.Fatal("Outbound is nil")
			}
			for path, want := range tc.wantFields {
				if got := dig(parsed.Outbound, path); !reflect.DeepEqual(got, want) {
					t.Errorf("outbound[%s] = %#v, want %#v", path, got, want)
				}
			}
			if got := parsed.Outbound["tag"]; got != tc.wantTag {
				t.Errorf("outbound tag = %#v, want %q", got, tc.wantTag)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Parse — invalid links
// ---------------------------------------------------------------------------

func TestParseInvalid(t *testing.T) {
	tests := []struct {
		name       string
		link       string
		wantDetail string
	}{
		{"empty", "", ""},
		{"whitespace only", "   ", ""},
		{"no scheme", "example.com", "expected one of"},
		{"unsupported scheme", "http://example.com", "scheme: http"},
		{"ssr rejected", "ssr://abc", "scheme: ssr"},
		{"vless missing host", "vless://uuid@:443?security=tls", detailMissingHost},
		{"vless missing port", "vless://uuid@example.com?security=tls", detailMissingPort},
		{"vless invalid port", "vless://uuid@example.com:0", detailInvalidPort},
		{"vless missing uuid", "vless://example.com:443?security=tls", detailMissingUUID},
		{"trojan missing host", "trojan://pw@:443", detailMissingHost},
		{"trojan missing password", "trojan://@example.com:443", detailMissingPassword},
		{"trojan missing port", "trojan://pw@example.com", detailMissingPort},
		{"hysteria2 missing password", "hysteria2://@example.com:443", detailMissingPassword},
		{"hysteria2 missing host", "hysteria2://pw@:443", detailMissingHost},
		{"hysteria2 missing port", "hysteria2://pw@example.com", detailMissingPort},
		{"tuic missing password", "tuic://uuid@example.com:443", detailMissingPassword},
		{"tuic missing host", "tuic://uuid:pw@:443", detailMissingHost},
		{"tuic missing uuid", "tuic://:pw@example.com:443", detailMissingUUID},
		{"vmess invalid base64", "vmess://!!!not-base64!!!", "invalid base64"},
		{"vmess missing payload", "vmess://", "missing payload"},
		{"vmess payload not json", "vmess://" + encodeBase64("not json"), "invalid JSON"},
		{"vmess missing host", vmessLink(t, map[string]any{"port": 443, "id": "u"}, ""), detailMissingHost},
		{"vmess missing port", vmessLink(t, map[string]any{"add": "h", "id": "u"}, ""), detailMissingPort},
		{"vmess missing uuid", vmessLink(t, map[string]any{"add": "h", "port": 443}, ""), detailMissingUUID},
		{"ss malformed base64", "ss://!!!not-base64!!!", "invalid credentials"},
		{"ss missing host", "ss://" + encodeBase64("aes-256-gcm:pw") + "@", detailMissingHost},
		{"ss missing port", "ss://" + encodeBase64("aes-256-gcm:pw") + "@example.com", detailMissingPort},
		{"ss missing method", "ss://" + encodeBase64(":pw") + "@example.com:8388", detailMissingMethod},
		{"ss missing password", "ss://" + encodeBase64("aes-256-gcm:") + "@example.com:8388", detailMissingPassword},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := Parse(tc.link)
			wantCode(t, err)
			if !reflect.DeepEqual(parsed, Parsed{}) {
				t.Errorf("expected zero Parsed on error, got %#v", parsed)
			}
			if tc.wantDetail == "" {
				return
			}
			found := false
			for _, d := range apperr.DetailsOf(err) {
				if strings.Contains(d, tc.wantDetail) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("details %v do not contain %q (error: %v)", apperr.DetailsOf(err), tc.wantDetail, err)
			}
		})
	}
}

// TestParseNeverLeaksCredentials pins the security requirement: a malformed link
// that still carries a credential must produce an error that names the missing
// field without echoing any part of the link.
func TestParseNeverLeaksCredentials(t *testing.T) {
	const secret = "SuperSecretPassword123"
	tests := []struct {
		name string
		link string
	}{
		{"vless missing port", "vless://" + secret + "@example.com?security=tls"},
		{"trojan missing port", "trojan://" + secret + "@example.com"},
		{"hysteria2 missing port", "hysteria2://" + secret + "@example.com"},
		{"tuic missing port", "tuic://" + secret + ":pw@example.com"},
		{"ss missing port", "ss://" + encodeBase64("aes-256-gcm:"+secret) + "@example.com"},
		{"unsupported with secret", "http://user:" + secret + "@example.com:443/path"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.link)
			wantCode(t, err)
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("error leaked a credential: %v", err)
			}
			for _, d := range apperr.DetailsOf(err) {
				if strings.Contains(d, secret) {
					t.Fatalf("details leaked a credential: %q", d)
				}
			}
		})
	}
}

func TestParseWarningsReportUnmappedParameters(t *testing.T) {
	secret := "ShouldNotLeakValue"
	link := "vless://uuid@example.com:443?security=tls&mystery=" + secret + "#W"
	parsed := mustParse(t, link)
	if len(parsed.Warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one", parsed.Warnings)
	}
	warning := parsed.Warnings[0]
	if !strings.Contains(warning, `"mystery"`) {
		t.Fatalf("warning %q should name the unmapped parameter", warning)
	}
	if strings.Contains(warning, secret) {
		t.Fatalf("warning leaked the parameter value: %q", warning)
	}
}

func TestParseKnownParametersProduceNoWarnings(t *testing.T) {
	link := "vless://uuid@example.com:443?security=tls&sni=a.example.com&type=ws&path=%2Fp&host=h.example.com&fp=chrome&alpn=h2&packetEncoding=xudp&flow=xtls-rprx-vision#W"
	parsed := mustParse(t, link)
	if len(parsed.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", parsed.Warnings)
	}
}

// ---------------------------------------------------------------------------
// Tags
// ---------------------------------------------------------------------------

func TestTagSanitisation(t *testing.T) {
	tests := []struct {
		name string
		link string
		want string
	}{
		{"spaces preserved and collapsed", "vless://uuid@h:443#My%20%20Node", "My Node"},
		{"control characters dropped", "vless://uuid@h:443#a%0Ab%09c", "a b c"},
		{"empty fragment default", "trojan://pw@h:443#", "trojan"},
		{"whitespace fragment default", "trojan://pw@h:443#%20%20", "trojan"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parsed := mustParse(t, tc.link)
			if parsed.Tag != tc.want {
				t.Fatalf("Tag = %q, want %q", parsed.Tag, tc.want)
			}
		})
	}
}

func TestParseWithUsedUniquifiesTag(t *testing.T) {
	link := "vless://uuid@h:443#Node"
	used := map[string]bool{"Node": true, "Node-2": true}
	parsed, err := ParseWithUsed(link, func(tag string) bool { return used[tag] })
	if err != nil {
		t.Fatalf("ParseWithUsed: %v", err)
	}
	if parsed.Tag != "Node-3" {
		t.Fatalf("Tag = %q, want Node-3", parsed.Tag)
	}
	if parsed.Outbound["tag"] != "Node-3" {
		t.Fatalf("outbound tag = %#v, want Node-3", parsed.Outbound["tag"])
	}
	if parsed.DisplayName != "Node" {
		t.Fatalf("DisplayName = %q, want Node", parsed.DisplayName)
	}
}

func TestSetTagsUniquifiesAgainstExternalAndWithinSlice(t *testing.T) {
	a := mustParse(t, "vless://u@h:443#Node")
	b := mustParse(t, "trojan://pw@h:443#Node")
	c := mustParse(t, "tuic://u:pw@h:443?") // default tag "tuic"

	items := SetTags([]Parsed{a, b, c}, func(tag string) bool { return tag == "Node" })
	if items[0].Tag != "Node-2" {
		t.Errorf("first tag = %q, want Node-2", items[0].Tag)
	}
	if items[1].Tag != "Node-3" {
		t.Errorf("second tag = %q, want Node-3", items[1].Tag)
	}
	if items[2].Tag != "tuic" {
		t.Errorf("third tag = %q, want tuic", items[2].Tag)
	}
	for i, item := range items {
		if item.Outbound["tag"] != item.Tag {
			t.Errorf("item %d outbound tag = %#v, want %q", i, item.Outbound["tag"], item.Tag)
		}
	}
	if items[0].DisplayName != "Node" || items[1].DisplayName != "Node" {
		t.Errorf("DisplayName should keep the original remarks: %q / %q", items[0].DisplayName, items[1].DisplayName)
	}
}

// ---------------------------------------------------------------------------
// ParseList
// ---------------------------------------------------------------------------

func TestParseList(t *testing.T) {
	text := strings.Join([]string{
		"# a pasted bundle",
		"vless://11111111-1111-1111-1111-111111111111@a.example.com:443?security=tls#One",
		"trojan://",
		"vless://22222222-2222-2222-2222-222222222222@b.example.com:443?security=tls#One",
		"http://not-a-share-link",
		"ss://!!!not-base64",
	}, "\n")
	parsed, errs := ParseList(text)
	if len(parsed) != 2 {
		t.Fatalf("parsed %d links, want 2 (%v)", len(parsed), parsed)
	}
	if len(errs) != 3 {
		t.Fatalf("got %d errors, want 3 (%v)", len(errs), errs)
	}
	if parsed[0].Tag != "One" || parsed[1].Tag != "One-2" {
		t.Fatalf("tags = %q / %q, want One / One-2", parsed[0].Tag, parsed[1].Tag)
	}
	for _, err := range errs {
		wantCode(t, err)
	}
	if !strings.Contains(strings.Join(apperr.DetailsOf(errs[0]), "; "), "line 3") {
		t.Fatalf("first error should point at line 3: %v", errs[0])
	}
}

func TestParseListHandlesWhitespaceSeparatedAndComments(t *testing.T) {
	text := "vless://u@h:443?security=tls#A  trojan://pw@h2:443#B\n// comment line\nhysteria2://pw@h3:443#C"
	parsed, errs := ParseList(text)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(parsed) != 3 {
		t.Fatalf("parsed %d links, want 3", len(parsed))
	}
	want := []string{"A", "B", "C"}
	for i, item := range parsed {
		if item.Tag != want[i] {
			t.Errorf("tag[%d] = %q, want %q", i, item.Tag, want[i])
		}
	}
}

func TestParseListEmpty(t *testing.T) {
	parsed, errs := ParseList("   \n# only comments\n")
	if len(parsed) != 0 || len(errs) != 0 {
		t.Fatalf("expected empty result, got %d links and %d errors", len(parsed), len(errs))
	}
}
