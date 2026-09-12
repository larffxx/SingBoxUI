package share

// The import dialog hands a parsed outbound straight into a profile revision, and
// the app refuses to start a configuration the installed sing-box rejects
// (spec §37, §38). A parsed link that carries a field sing-box has since removed
// therefore produces a profile that cannot come up at all, and the user is told
// "sing-box rejected the configuration" with nothing to act on — which is exactly
// what a vless link with `type=grpc` did before the legacy `network` field was
// dropped from the generated outbound.
//
// The parser is the only part of the application that writes outbound fields
// sing-box may have removed, so it is validated against the real binary here, the
// same way internal/app/templates validates its starters. Both tests skip when no
// sing-box is installed rather than failing.

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/larffxx/singboxui/internal/platform"
	"github.com/larffxx/singboxui/internal/singbox"
)

// validatorBinary returns the sing-box the regression test validates with:
// SINGBOXUI_TEST_SINGBOX wins, then the managed installation, then PATH. An empty
// result means the test is skipped.
func validatorBinary(t *testing.T) string {
	t.Helper()
	if explicit := strings.TrimSpace(os.Getenv("SINGBOXUI_TEST_SINGBOX")); explicit != "" {
		if !singbox.IsExecutable(explicit) {
			t.Fatalf("SINGBOXUI_TEST_SINGBOX=%s is not an executable file", explicit)
		}
		return explicit
	}
	if plat, err := platform.New(); err == nil {
		matches, _ := filepath.Glob(filepath.Join(plat.Paths().BinDir, "*", singbox.ExecutableName(runtime.GOOS)))
		sort.Sort(sort.Reverse(sort.StringSlice(matches)))
		for _, candidate := range matches {
			if singbox.IsExecutable(candidate) {
				return candidate
			}
		}
	}
	if found, err := exec.LookPath("sing-box"); err == nil {
		return found
	}
	return ""
}

// importLinks are one link per protocol and transport family the importer
// understands. They are synthetic: the point is the shape of the outbound, not
// the endpoint. The vless grpc link mirrors the report that started this file.
func importLinks(t *testing.T) []struct{ name, link string } {
	t.Helper()
	return []struct{ name, link string }{
		{"vless grpc reality", "vless://0ffdc49e-7ed1-4fc4-8730-df5711b338d3@203.0.113.10:443?encryption=none&security=reality&sni=www.microsoft.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=39fd2783&type=grpc&serviceName=m-9f2k#vless-grpc"},
		{"vless ws tls", "vless://11111111-1111-1111-1111-111111111111@vpn.example.com:443?security=tls&sni=vpn.example.com&type=ws&path=%2Fws&host=edge.example.com#vless-ws"},
		{"vless httpupgrade", "vless://22222222-2222-2222-2222-222222222222@vpn.example.com:443?security=tls&type=httpupgrade&path=%2Fup#vless-hu"},
		{"vless plain tcp", "vless://33333333-3333-3333-3333-333333333333@vpn.example.com:8080?type=tcp#vless-tcp"},
		{"vless http transport", "vless://77777777-7777-7777-7777-777777777777@vpn.example.com:443?security=tls&type=http&path=%2Fh2&host=h.example.com#vless-http"},
		{"trojan ws tls", "trojan://secret@tj.example.com:443?security=tls&type=ws&path=%2Fws&host=edge.example.com#trojan-ws"},
		{"trojan grpc tls", "trojan://secret@tj.example.com:443?security=tls&sni=tj.example.com&type=grpc&serviceName=grpcsvc#trojan-grpc"},
		{"ss 2022", "ss://" + encodeBase64("2022-blake3-aes-128-gcm:AAAAAAAAAAAAAAAAAAAAAA==") + "@ss.example.com:8388#ss"},
		{"ss legacy base64", "ss://" + encodeBase64("aes-256-gcm:s3cr3t@ss.example.com:8388") + "#ss-legacy"},
		{"hysteria2 plain", "hysteria2://pw@hy.example.com:8443?sni=hy.example.com#hy2-plain"},
		{"hysteria2 salamander", "hysteria2://0fb42e570cc98a0d93795c1f165c907b@hy.example.com:443?obfs=salamander&obfs-password=d6c1bc0ae78cb39bd1cb101065da5605&sni=www.microsoft.com&insecure=1#hy2"},
		{"tuic", "tuic://44444444-4444-4444-4444-444444444444:pw@tuic.example.com:443?sni=tuic.example.com#tuic"},
		{"vmess grpc tls", vmessLink(t, map[string]any{"v": "2", "ps": "grpc", "add": "vm.example.com", "port": 443, "id": "55555555-5555-5555-5555-555555555555", "aid": 0, "scy": "auto", "net": "grpc", "path": "svc", "tls": "tls", "sni": "vm.example.com"}, "vmess-grpc")},
		{"vmess ws tls", vmessLink(t, map[string]any{"v": "2", "ps": "ws", "add": "vm.example.com", "port": 8443, "id": "66666666-6666-6666-6666-666666666666", "aid": 0, "scy": "auto", "net": "ws", "path": "/ray", "host": "cdn.example.com", "tls": "tls", "sni": "cdn.example.com"}, "vmess-ws")},
		{"vmess http header", vmessLink(t, map[string]any{"v": "2", "ps": "http", "add": "vm.example.com", "port": 80, "id": "88888888-8888-8888-8888-888888888888", "aid": 0, "scy": "auto", "net": "tcp", "type": "http", "path": "/x", "host": "h.example.com"}, "vmess-http")},
	}
}

// TestParsedLinksPassTheManagedValidator is the regression test for the shipped
// failure: every link the import accepts must produce an outbound the installed
// sing-box decodes. A profile built from a rejected outbound can never start, and
// the app reports it as a rejected configuration.
func TestParsedLinksPassTheManagedValidator(t *testing.T) {
	binary := validatorBinary(t)
	if binary == "" {
		t.Skip("no sing-box binary found; set SINGBOXUI_TEST_SINGBOX to validate the parsed links")
	}
	t.Logf("validating parsed links with %s", binary)

	for _, tc := range importLinks(t) {
		t.Run(tc.name, func(t *testing.T) {
			parsed := mustParse(t, tc.link)

			// The smallest configuration that exercises the outbound: nothing
			// needs a privilege, so this runs everywhere the binary does.
			outbound := map[string]any{}
			for key, value := range parsed.Outbound {
				outbound[key] = value
			}
			outbound["tag"] = "proxy"
			config := map[string]any{
				"log":       map[string]any{"level": "info"},
				"outbounds": []any{outbound, map[string]any{"type": "direct", "tag": "direct"}},
				"route":     map[string]any{"final": "proxy"},
			}
			raw, err := json.Marshal(config)
			if err != nil {
				t.Fatalf("encode the configuration: %v", err)
			}
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatalf("write the configuration: %v", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			check, err := singbox.Check(ctx, binary, path, 30*time.Second)
			if err != nil {
				t.Fatalf("sing-box could not check the outbound built from %s: %v\n%s", tc.link, err, check.Output)
			}
			if !check.OK {
				t.Fatalf("sing-box rejected the outbound built from %s:\n%s", tc.link, check.Output)
			}
		})
	}
}

// TestParsedOutboundsAvoidTheLegacyNetworkField needs no binary: it states the
// rule the parser must keep whatever sing-box is installed. An outbound `network`
// accepts tcp/udp only, so a transport name there is refused outright; leaving the
// field out is also what makes UDP work, because `network: "tcp"` restricts the
// outbound to TCP.
func TestParsedOutboundsAvoidTheLegacyNetworkField(t *testing.T) {
	for _, tc := range importLinks(t) {
		t.Run(tc.name, func(t *testing.T) {
			parsed := mustParse(t, tc.link)
			got, present := parsed.Outbound["network"]
			if !present {
				return
			}
			panicList := []string{"tcp", "udp"}
			for _, allowed := range panicList {
				if got == allowed {
					return
				}
			}
			t.Errorf("outbounds carry network=%v: sing-box refuses a transport name there (\"unknown network\") and the profile never starts; write the transport in `transport` only", got)
		})
	}
}
