package share

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/larffxx/singboxui/internal/domain/apperr"
)

func TestBuildExact(t *testing.T) {
	tests := []struct {
		name     string
		outbound map[string]any
		want     string
	}{
		{
			name: "vless tls",
			outbound: map[string]any{
				"type": "vless", "tag": "Tokyo",
				"server": "vpn.example.com", "server_port": 443,
				"uuid":            "11111111-1111-1111-1111-111111111111",
				"network":         "tcp",
				"packet_encoding": "xudp",
				"tls":             map[string]any{"enabled": true, "server_name": "vpn.example.com"},
			},
			want: "vless://11111111-1111-1111-1111-111111111111@vpn.example.com:443?encryption=none&type=tcp&packetEncoding=xudp&security=tls&sni=vpn.example.com#Tokyo",
		},
		{
			name: "vless websocket tls",
			outbound: map[string]any{
				"type": "vless", "tag": "W",
				"server": "h.example.com", "server_port": 443, "uuid": "u",
				"network": "ws",
				"tls":     map[string]any{"enabled": true, "server_name": "h.example.com"},
				"transport": map[string]any{
					"type": "ws", "path": "/p", "host": "cdn.example.com",
				},
			},
			want: "vless://u@h.example.com:443?encryption=none&type=ws&security=tls&sni=h.example.com&path=%2Fp&host=cdn.example.com#W",
		},
		{
			name: "vless without tls",
			outbound: map[string]any{
				"type": "vless", "tag": "P", "server": "h", "server_port": 80, "uuid": "u", "network": "tcp",
			},
			want: "vless://u@h:80?encryption=none&type=tcp&security=none#P",
		},
		{
			name: "trojan tls",
			outbound: map[string]any{
				"type": "trojan", "tag": "TJ", "server": "tj.example.com", "server_port": 443,
				"password": "secret", "network": "tcp",
				"tls": map[string]any{"enabled": true, "server_name": "tj.example.com"},
			},
			want: "trojan://secret@tj.example.com:443?type=tcp&security=tls&sni=tj.example.com#TJ",
		},
		{
			name: "hysteria2 with obfs",
			outbound: map[string]any{
				"type": "hysteria2", "tag": "HY", "server": "hy.example.com", "server_port": 443, "password": "pw",
				"tls":  map[string]any{"enabled": true, "server_name": "hy.example.com", "insecure": true},
				"obfs": map[string]any{"type": "salamander", "password": "op"},
			},
			want: "hysteria2://pw@hy.example.com:443?sni=hy.example.com&insecure=1&obfs=salamander&obfs-password=op#HY",
		},
		{
			name: "tuic",
			outbound: map[string]any{
				"type": "tuic", "tag": "TU", "server": "tu.example.com", "server_port": 443,
				"uuid": "u1", "password": "p1",
				"tls":                map[string]any{"enabled": true, "server_name": "tu.example.com"},
				"udp_relay_mode":     "native",
				"congestion_control": "bbr",
			},
			want: "tuic://u1:p1@tu.example.com:443?sni=tu.example.com&udp_relay_mode=native&congestion_control=bbr#TU",
		},
		{
			name: "ipv6 host is bracketed",
			outbound: map[string]any{
				"type": "trojan", "tag": "V6", "server": "2001:db8::1", "server_port": 443,
				"password": "pw", "network": "tcp",
				"tls": map[string]any{"enabled": true},
			},
			want: "trojan://pw@[2001:db8::1]:443?type=tcp&security=tls#V6",
		},
		{
			name: "password is percent encoded",
			outbound: map[string]any{
				"type": "trojan", "tag": "E", "server": "h", "server_port": 443,
				"password": "p@ss:w/rd", "network": "tcp",
				"tls": map[string]any{"enabled": true},
			},
			want: "trojan://p%40ss%3Aw%2Frd@h:443?type=tcp&security=tls#E",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Build(tc.outbound)
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			if got != tc.want {
				t.Fatalf("Build =\n  %s\nwant\n  %s", got, tc.want)
			}
		})
	}
}

func TestBuildShadowsocks(t *testing.T) {
	outbound := map[string]any{
		"type": "shadowsocks", "tag": "SS", "server": "ss.example.com", "server_port": 8388,
		"method": "aes-256-gcm", "password": "s3cr3t",
	}
	link, err := Build(outbound)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !strings.HasPrefix(link, "ss://") || !strings.HasSuffix(link, "#SS") {
		t.Fatalf("unexpected ss link shape: %s", link)
	}
	if !strings.Contains(link, "@ss.example.com:8388") {
		t.Fatalf("ss link lost the endpoint: %s", link)
	}
	userinfo := strings.TrimPrefix(strings.SplitN(link, "@", 2)[0], "ss://")
	decoded, ok := decodeBase64(userinfo)
	if !ok {
		t.Fatalf("ss userinfo is not base64: %q", userinfo)
	}
	if string(decoded) != "aes-256-gcm:s3cr3t" {
		t.Fatalf("ss userinfo decoded to %q, want method:password", decoded)
	}
}

func TestBuildVmessPayload(t *testing.T) {
	outbound := map[string]any{
		"type": "vmess", "tag": "VM", "server": "vm.example.com", "server_port": 8443,
		"uuid": "uuid-1", "alter_id": 2, "security": "chacha20-poly1305", "network": "ws",
		"tls":       map[string]any{"enabled": true, "server_name": "cdn.example.com", "utls": map[string]any{"fingerprint": "chrome"}},
		"transport": map[string]any{"type": "ws", "path": "/ray", "host": "cdn.example.com"},
	}
	link, err := Build(outbound)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !strings.HasPrefix(link, "vmess://") || !strings.HasSuffix(link, "#VM") {
		t.Fatalf("unexpected vmess link: %s", link)
	}
	body := strings.TrimSuffix(strings.TrimPrefix(link, "vmess://"), "#VM")
	decoded, ok := decodeBase64(body)
	if !ok {
		t.Fatalf("vmess payload is not base64: %q", body)
	}
	var payload map[string]any
	if err := json.Unmarshal(decoded, &payload); err != nil {
		t.Fatalf("vmess payload is not JSON: %v", err)
	}
	want := map[string]any{
		"v": "2", "ps": "VM", "add": "vm.example.com", "port": float64(8443),
		"id": "uuid-1", "aid": float64(2), "scy": "chacha20-poly1305",
		"net": "ws", "type": "none", "tls": "tls", "sni": "cdn.example.com",
		"fp": "chrome", "path": "/ray", "host": "cdn.example.com",
	}
	for k, v := range want {
		if payload[k] != v {
			t.Errorf("payload[%q] = %#v, want %#v", k, payload[k], v)
		}
	}
}

func TestBuildErrors(t *testing.T) {
	tests := []struct {
		name       string
		outbound   map[string]any
		wantDetail string
	}{
		{"nil outbound", nil, ""},
		{"missing type", map[string]any{"server": "h"}, "missing type"},
		{"unsupported type", map[string]any{"type": "wireguard", "server": "h", "server_port": 1}, "unsupported type: wireguard"},
		{"vless missing server", map[string]any{"type": "vless", "server_port": 443, "uuid": "u"}, detailMissingHost},
		{"vless missing port", map[string]any{"type": "vless", "server": "h", "uuid": "u"}, detailMissingPort},
		{"vless invalid port", map[string]any{"type": "vless", "server": "h", "server_port": 70000, "uuid": "u"}, detailInvalidPort},
		{"vless missing uuid", map[string]any{"type": "vless", "server": "h", "server_port": 443}, detailMissingUUID},
		{"trojan missing password", map[string]any{"type": "trojan", "server": "h", "server_port": 443}, detailMissingPassword},
		{"ss missing method", map[string]any{"type": "shadowsocks", "server": "h", "server_port": 8388, "password": "p"}, detailMissingMethod},
		{"ss missing password", map[string]any{"type": "shadowsocks", "server": "h", "server_port": 8388, "method": "m"}, detailMissingPassword},
		{"hysteria2 missing password", map[string]any{"type": "hysteria2", "server": "h", "server_port": 443}, detailMissingPassword},
		{"tuic missing uuid", map[string]any{"type": "tuic", "server": "h", "server_port": 443, "password": "p"}, detailMissingUUID},
		{"tuic missing password", map[string]any{"type": "tuic", "server": "h", "server_port": 443, "uuid": "u"}, detailMissingPassword},
		{"vmess missing uuid", map[string]any{"type": "vmess", "server": "h", "server_port": 443}, detailMissingUUID},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Build(tc.outbound)
			wantCode(t, err)
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
				t.Fatalf("details %v do not contain %q", apperr.DetailsOf(err), tc.wantDetail)
			}
		})
	}
}

func TestBuildNeverLeaksCredentials(t *testing.T) {
	const secret = "BuildSecretValue9"
	outbound := map[string]any{
		"type": "trojan", "server": "h",
		"password": secret,
		// server_port intentionally missing -> error
	}
	_, err := Build(outbound)
	wantCode(t, err)
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("Build error leaked a credential: %v", err)
	}
}

// TestBuildDoesNotMutateOutbound covers the "build on top of the parsed map"
// contract: exporting must leave the candidate object untouched, including
// fields the share-link format cannot represent.
func TestBuildDoesNotMutateOutbound(t *testing.T) {
	outbound := map[string]any{
		"type": "vless", "tag": "T", "server": "h", "server_port": 443, "uuid": "u",
		"network": "tcp", "observatory": map[string]any{"enable": true}, "customField": "kept",
	}
	before := deepCopyMap(t, outbound)
	if _, err := Build(outbound); err != nil {
		t.Fatalf("Build: %v", err)
	}
	after := deepCopyMap(t, outbound)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("Build mutated its input:\n before %#v\n after  %#v", before, after)
	}
}

func deepCopyMap(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

// TestBuildParseRoundTrip is the core guarantee: parsing a link, building it
// back into a link and parsing again yields the identical candidate outbound.
func TestBuildParseRoundTrip(t *testing.T) {
	links := map[string]string{
		"vless ws tls":       "vless://11111111-1111-1111-1111-111111111111@vpn.example.com:443?security=tls&sni=vpn.example.com&type=ws&path=%2Fws&host=edge.example.com#Tokyo",
		"vless reality":      "vless://22222222-2222-2222-2222-222222222222@r.example.com:443?security=reality&sni=www.microsoft.com&fp=chrome&pbk=PUBKEY&sid=abcd&flow=xtls-rprx-vision&type=tcp&alpn=h2,http%2F1.1#Reality",
		"vless plain":        "vless://33333333-3333-3333-3333-333333333333@plain.example.com:8080?type=tcp#Plain",
		"vless grpc":         "vless://44444444-4444-4444-4444-444444444444@g.example.com:443?security=tls&type=grpc&serviceName=svc#GRPC",
		"vless httpupgrade":  "vless://55555555-5555-5555-5555-555555555555@hu.example.com:443?security=tls&type=httpupgrade&path=%2Fup&host=cdn.example.com#HU",
		"vless http":         "vless://66666666-6666-6666-6666-666666666666@hh.example.com:443?security=tls&type=http&path=%2Fh2#H2",
		"trojan grpc tls":    "trojan://secret@tj.example.com:443?security=tls&sni=tj.example.com&type=grpc&serviceName=grpcsvc#TJ",
		"trojan default tls": "trojan://secret@tj.example.com:443#NoSec",
		"trojan tls none":    "trojan://secret@tj.example.com:443?security=none#NoTLS",
		"trojan ipv6":        "trojan://pw@[2001:db8::1]:443?security=tls&sni=v6.example.com#V6",
		"ss sip002":          "ss://" + encodeBase64("aes-256-gcm:s3cr3t") + "@ss.example.com:8388#SS",
		"ss legacy base64":   "ss://" + encodeBase64("aes-256-gcm:s3cr3t@ss.example.com:8388") + "#Legacy",
		"ss plugin":          "ss://" + encodeBase64("aes-256-gcm:s3cr3t") + "@ss.example.com:8388?plugin=v2ray-plugin%3Bmode%3Dwebsocket#PL",
		"hysteria2 full":     "hysteria2://pw@hy.example.com:443?sni=hy.example.com&insecure=1&obfs=salamander&obfs-password=op&alpn=h3#HY",
		"hysteria2 minimal":  "hysteria2://pw@hy.example.com:443#HY2",
		"tuic full":          "tuic://u-1:p-p@tu.example.com:443?sni=tu.example.com&congestion_control=bbr&udp_relay_mode=native&insecure=1&alpn=h3#TU",
		"tuic minimal":       "tuic://u-1:p-p@tu.example.com:443#TU2",
		"vmess plain":        vmessLink(t, map[string]any{"v": "2", "ps": "My VMess", "add": "vm.example.com", "port": "443", "id": "uuid-1", "aid": "0", "scy": "auto", "net": "tcp", "type": "none"}, "My VMess"),
		"vmess ws tls":       vmessLink(t, map[string]any{"v": "2", "ps": "WS", "add": "vm.example.com", "port": 8443, "id": "uuid-2", "aid": 0, "scy": "chacha20-poly1305", "net": "ws", "type": "none", "host": "cdn.example.com", "path": "/ray", "tls": "tls", "sni": "cdn.example.com", "fp": "chrome"}, "WS"),
		"vmess grpc":         vmessLink(t, map[string]any{"v": "2", "ps": "gRPC", "add": "vm.example.com", "port": 443, "id": "uuid-3", "net": "grpc", "type": "none", "path": "svcName", "tls": "tls"}, "gRPC"),
		"vmess httpupgrade":  vmessLink(t, map[string]any{"v": "2", "ps": "HU", "add": "vm.example.com", "port": 443, "id": "uuid-5", "net": "httpupgrade", "type": "none", "path": "/up", "host": "cdn.example.com", "tls": "tls"}, "HU"),
		"vmess tcp http":     vmessLink(t, map[string]any{"v": "2", "ps": "HTTP", "add": "vm.example.com", "port": 80, "id": "uuid-4", "net": "tcp", "type": "http", "path": "/x", "host": "h.example.com"}, "HTTP"),
	}
	for name, link := range links {
		t.Run(name, func(t *testing.T) {
			first := mustParse(t, link)

			built, err := Build(first.Outbound)
			if err != nil {
				t.Fatalf("Build(%#v): %v", first.Outbound, err)
			}
			second, err := Parse(built)
			if err != nil {
				t.Fatalf("re-parsing %q failed: %v", built, err)
			}
			if first.Kind != second.Kind {
				t.Fatalf("kind changed: %q -> %q", first.Kind, second.Kind)
			}
			if first.Tag != second.Tag {
				t.Fatalf("tag changed: %q -> %q (link %s)", first.Tag, second.Tag, built)
			}
			if !reflect.DeepEqual(first.Outbound, second.Outbound) {
				t.Fatalf("outbound changed across the round trip for %q:\n first  %#v\n second %#v", built, first.Outbound, second.Outbound)
			}
		})
	}
}

// TestBuildAcceptsTypeAliases checks the ss/hy2 aliases some configs use.
func TestBuildAcceptsTypeAliases(t *testing.T) {
	tests := []map[string]any{
		{"type": "ss", "tag": "S", "server": "h", "server_port": 8388, "method": "aes-256-gcm", "password": "p"},
		{"type": "hy2", "tag": "H", "server": "h", "server_port": 443, "password": "p", "tls": map[string]any{"enabled": true}},
	}
	for _, outbound := range tests {
		if _, err := Build(outbound); err != nil {
			t.Errorf("Build(%v): %v", outbound["type"], err)
		}
	}
}

// TestParseTransportVariants pins the transport mapping for every transport
// family the importers understand.
//
// The transport must not be repeated in the outbound's legacy `network` field:
// sing-box accepts only `tcp`/`udp` there, and a transport name makes it refuse
// the whole configuration ("outbounds[i].network: unknown network: grpc"), which
// the app reports as a rejected profile and never starts (spec §37, §38). The
// transport is expressed by `transport` alone.
func TestParseTransportVariants(t *testing.T) {
	tests := []struct {
		name           string
		link           string
		wantType       string
		wantPath       string
		wantHost       string
		wantHeaderHost string
		wantService    string
	}{
		{"default tcp", "vless://u@h:443?security=tls#x", "", "", "", "", ""},
		{"ws", "vless://u@h:443?security=tls&type=ws&path=%2Fa&host=cdn.example.com#x", "ws", "/a", "", "cdn.example.com", ""},
		{"ws default path", "vless://u@h:443?security=tls&type=ws#x", "ws", "/", "", "", ""},
		{"httpupgrade", "vless://u@h:443?security=tls&type=httpupgrade&path=%2Fb&host=cdn.example.com#x", "httpupgrade", "/b", "cdn.example.com", "", ""},
		{"http", "vless://u@h:443?security=tls&type=http&path=%2Fc#x", "http", "/c", "", "", ""},
		{"grpc", "vless://u@h:443?security=tls&type=grpc&serviceName=svc#x", "grpc", "", "", "", "svc"},
		{"tcp header http", "vless://u@h:80?type=tcp&headerType=http&path=%2Fd#x", "http", "/d", "", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := mustParse(t, tc.link)
			if got, ok := p.Outbound["network"]; ok {
				t.Errorf("network = %v, want the field left out: sing-box 1.14 accepts only tcp/udp there and refuses the configuration otherwise", got)
			}
			tr, _ := p.Outbound["transport"].(map[string]any)
			if tc.wantType == "" {
				if tr != nil {
					t.Fatalf("expected no transport, got %#v", tr)
				}
				return
			}
			if tr == nil {
				t.Fatalf("expected transport %q, got none", tc.wantType)
			}
			if tr["type"] != tc.wantType {
				t.Errorf("transport.type = %v, want %v", tr["type"], tc.wantType)
			}
			if tc.wantPath != "" && tr["path"] != tc.wantPath {
				t.Errorf("transport.path = %v, want %v", tr["path"], tc.wantPath)
			}
			if tc.wantHost != "" && tr["host"] != tc.wantHost {
				t.Errorf("transport.host = %v, want %v", tr["host"], tc.wantHost)
			}
			// A websocket transport carries the Host header in `headers`, because
			// sing-box 1.14 removed the flat `host` field from it.
			headers, _ := tr["headers"].(map[string]any)
			if tc.wantHeaderHost != "" {
				if headers["Host"] != tc.wantHeaderHost {
					t.Errorf("transport.headers.Host = %v, want %v", headers["Host"], tc.wantHeaderHost)
				}
				if _, present := tr["host"]; present {
					t.Errorf("transport.host is still written for ws: sing-box refuses the configuration with `transport.host: unknown field`")
				}
			}
			if tc.wantService != "" && tr["service_name"] != tc.wantService {
				t.Errorf("transport.service_name = %v, want %v", tr["service_name"], tc.wantService)
			}
		})
	}
}

// TestBuildFromJSONDecodedOutbound covers the realistic application path where
// the candidate outbound has been persisted/reloaded through JSON: numbers
// arrive as float64, arrays as []any, booleans as bool.
func TestBuildFromJSONDecodedOutbound(t *testing.T) {
	links := []string{
		"vless://u@vpn.example.com:443?security=tls&sni=vpn.example.com&type=ws&path=%2Fws&host=cdn.example.com#Tokyo",
		"trojan://secret@tj.example.com:443?security=tls&type=grpc&serviceName=svc#TJ",
		"tuic://u-1:p-p@tu.example.com:443?sni=tu.example.com&congestion_control=bbr&udp_relay_mode=native&insecure=1&alpn=h3#TU",
		"hysteria2://pw@hy.example.com:443?sni=hy.example.com&obfs=salamander&obfs-password=op#HY",
		"vless://u@r.example.com:443?security=reality&sni=www.microsoft.com&fp=chrome&pbk=PUBKEY&sid=abcd&flow=xtls-rprx-vision&type=tcp&alpn=h2#Reality",
	}
	for _, link := range links {
		t.Run(Kind(link), func(t *testing.T) {
			first := mustParse(t, link)
			data, err := json.Marshal(first.Outbound)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var decoded map[string]any
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			built, err := Build(decoded)
			if err != nil {
				t.Fatalf("Build from JSON-decoded outbound: %v", err)
			}
			second, err := Parse(built)
			if err != nil {
				t.Fatalf("re-parsing %q failed: %v", built, err)
			}
			if !reflect.DeepEqual(first.Outbound, second.Outbound) {
				t.Fatalf("JSON round trip changed the outbound for %q:\n first  %#v\n second %#v", built, first.Outbound, second.Outbound)
			}
		})
	}
}
