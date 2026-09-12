package templates

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	domainconfig "github.com/larffxx/singboxui/internal/domain/config"
	"github.com/larffxx/singboxui/internal/platform"
	"github.com/larffxx/singboxui/internal/singbox"
)

// wantIDs is the catalogue in display order. The UI renders the list in this
// order and the first template is the default selection, so the order is part
// of the contract.
var wantIDs = []string{"empty", "tun-basic", "tun-vless-reality", "socks-local", "selector"}

func templateByID(t *testing.T, id string) Template {
	t.Helper()
	tpl, err := Get(id)
	if err != nil {
		t.Fatalf("Get(%q) = %v, want the template", id, err)
	}
	return tpl
}

// lookup walks a parsed configuration with string keys for objects and ints for
// array indexes.
func lookup(t *testing.T, root map[string]any, path []any) (any, bool) {
	t.Helper()
	var current any = root
	for i, step := range path {
		switch key := step.(type) {
		case string:
			obj, ok := current.(map[string]any)
			if !ok {
				return nil, false
			}
			value, ok := obj[key]
			if !ok {
				return nil, false
			}
			current = value
		case int:
			list, ok := current.([]any)
			if !ok || key < 0 || key >= len(list) {
				return nil, false
			}
			current = list[key]
		default:
			t.Fatalf("unsupported path step %T at index %d", step, i)
		}
	}
	return current, true
}

func formatValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case bool:
		return strconv.FormatBool(typed)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, formatValue(item))
		}
		return strings.Join(parts, ",")
	case nil:
		return "null"
	default:
		return strings.TrimSpace(strings.ReplaceAll(strings.TrimSpace(jsonish(typed)), "\n", " "))
	}
}

func jsonish(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "?"
	}
	return string(raw)
}

func TestCatalogIsStableAndOrdered(t *testing.T) {
	if got := IDs(); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("IDs() = %v, want %v", got, wantIDs)
	}

	all := All()
	if len(all) != len(wantIDs) {
		t.Fatalf("len(All()) = %d, want %d", len(all), len(wantIDs))
	}

	seen := map[string]bool{}
	for i, tpl := range all {
		if tpl.ID != wantIDs[i] {
			t.Errorf("All()[%d].ID = %q, want %q", i, tpl.ID, wantIDs[i])
		}
		if seen[tpl.ID] {
			t.Errorf("duplicate template id %q", tpl.ID)
		}
		seen[tpl.ID] = true

		if strings.TrimSpace(tpl.Name) == "" {
			t.Errorf("%s: Name is empty", tpl.ID)
		}
		if len(strings.TrimSpace(tpl.Description)) < 20 {
			t.Errorf("%s: Description %q is too short to explain the template", tpl.ID, tpl.Description)
		}
		if strings.TrimSpace(tpl.Config) == "" {
			t.Errorf("%s: Config is empty", tpl.ID)
		}
		if !strings.HasSuffix(tpl.Config, "\n") {
			t.Errorf("%s: Config should end with a newline", tpl.ID)
		}
		if got, err := Get(tpl.ID); err != nil || got != tpl {
			t.Errorf("Get(%q) = (%+v, %v), want the template from All()", tpl.ID, got, err)
		}
	}
}

func TestEveryTemplateIsAValidSingBoxConfiguration(t *testing.T) {
	for _, tpl := range All() {
		t.Run(tpl.ID, func(t *testing.T) {
			root, err := domainconfig.Parse([]byte(tpl.Config))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			result := domainconfig.Validate([]byte(tpl.Config))
			if !result.OK {
				t.Fatalf("Validate() = %+v, want OK", result)
			}
			if len(result.Errors) != 0 {
				t.Errorf("Validate() errors = %v, want none", result.Errors)
			}
			// A TUN configuration must carry the warning that drives the admin
			// prompt; everything else must be warning-free.
			hasTUN := domainconfig.HasTUN([]byte(tpl.Config))
			if hasTUN {
				if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "administrator") {
					t.Errorf("Validate() warnings = %v, want the administrator-privilege notice", result.Warnings)
				}
			} else if len(result.Warnings) != 0 {
				t.Errorf("Validate() warnings = %v, want none", result.Warnings)
			}

			outbounds, ok := root["outbounds"].([]any)
			if !ok || len(outbounds) == 0 {
				t.Fatalf("outbounds = %v, want a non-empty array", root["outbounds"])
			}
			for _, section := range []string{"inbounds", "outbounds"} {
				tags := domainconfig.Tags([]byte(tpl.Config), section)
				if len(tags) != len(uniqueStrings(tags)) {
					t.Errorf("%s tags %v contain duplicates", section, tags)
				}
			}

			// The privilege flag drives the admin prompt in the UI; it must
			// agree with the config the template ships.
			if want := domainconfig.HasTUN([]byte(tpl.Config)); tpl.RequiresPrivilege != want {
				t.Errorf("RequiresPrivilege = %v, but HasTUN = %v", tpl.RequiresPrivilege, want)
			}
		})
	}
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

// TestTemplateGoldenStructure pins the generated document structurally: the
// exact set of fields the application depends on, not the formatting.
func TestTemplateGoldenStructure(t *testing.T) {
	probes := []struct {
		name string
		id   string
		path []any
		want string
		// missing inverts the probe: the path must not exist at all. It covers
		// the fields that have to stay out of a generated configuration, such as
		// the legacy forms sing-box refuses to start with.
		missing bool
	}{
		{name: "empty log level", id: "empty", path: []any{"log", "level"}, want: "info"},
		{name: "empty timestamp", id: "empty", path: []any{"log", "timestamp"}, want: "true"},
		{name: "empty single direct outbound", id: "empty", path: []any{"outbounds", 0, "type"}, want: "direct"},
		{name: "empty direct tag", id: "empty", path: []any{"outbounds", 0, "tag"}, want: "direct"},

		{name: "tun inbound type", id: "tun-basic", path: []any{"inbounds", 0, "type"}, want: "tun"},
		{name: "tun tag", id: "tun-basic", path: []any{"inbounds", 0, "tag"}, want: "tun-in"},
		{name: "tun interface name is left to sing-box", id: "tun-basic", path: []any{"inbounds", 0, "interface_name"}, missing: true},
		{name: "tun address", id: "tun-basic", path: []any{"inbounds", 0, "address"}, want: "172.19.0.1/30"},
		{name: "tun mtu", id: "tun-basic", path: []any{"inbounds", 0, "mtu"}, want: "9000"},
		{name: "tun auto route", id: "tun-basic", path: []any{"inbounds", 0, "auto_route"}, want: "true"},
		{name: "tun strict route", id: "tun-basic", path: []any{"inbounds", 0, "strict_route"}, want: "true"},
		{name: "tun stack", id: "tun-basic", path: []any{"inbounds", 0, "stack"}, want: "system"},
		{name: "mixed inbound type", id: "tun-basic", path: []any{"inbounds", 1, "type"}, want: "mixed"},
		{name: "mixed inbound listen", id: "tun-basic", path: []any{"inbounds", 1, "listen"}, want: "127.0.0.1"},
		{name: "mixed inbound port", id: "tun-basic", path: []any{"inbounds", 1, "listen_port"}, want: "2080"},
		{name: "tun-basic route final", id: "tun-basic", path: []any{"route", "final"}, want: "direct"},
		{name: "tun-basic auto detect interface", id: "tun-basic", path: []any{"route", "auto_detect_interface"}, want: "true"},
		{name: "tun-basic dns final", id: "tun-basic", path: []any{"dns", "final"}, want: "cloudflare"},
		{name: "tun-basic dns server has no detour", id: "tun-basic", path: []any{"dns", "servers", 0, "detour"}, missing: true},
		{name: "tun-basic dns server type", id: "tun-basic", path: []any{"dns", "servers", 0, "type"}, want: "https"},
		{name: "tun-basic dns server address", id: "tun-basic", path: []any{"dns", "servers", 0, "server"}, want: "1.1.1.1"},
		{name: "tun-basic dns server port", id: "tun-basic", path: []any{"dns", "servers", 0, "server_port"}, want: "443"},
		{name: "tun-basic dns server path", id: "tun-basic", path: []any{"dns", "servers", 0, "path"}, want: "/dns-query"},
		{name: "tun-basic dns server tls", id: "tun-basic", path: []any{"dns", "servers", 0, "tls", "enabled"}, want: "true"},
		{name: "tun-basic dns server tls name", id: "tun-basic", path: []any{"dns", "servers", 0, "tls", "server_name"}, want: "1.1.1.1"},
		{name: "tun-basic local server type", id: "tun-basic", path: []any{"dns", "servers", 1, "type"}, want: "local"},
		{name: "tun-basic default domain resolver", id: "tun-basic", path: []any{"route", "default_domain_resolver", "server"}, want: "cloudflare"},
		{name: "tun-basic rule 3 keeps private traffic direct", id: "tun-basic", path: []any{"route", "rules", 2, "outbound"}, want: "direct"},
		{name: "tun-basic rule 3 matches private ip", id: "tun-basic", path: []any{"route", "rules", 2, "ip_is_private"}, want: "true"},
		{name: "tun-basic dns rule hijacks", id: "tun-basic", path: []any{"route", "rules", 1, "action"}, want: "hijack-dns"},

		{name: "reality inbound is tun", id: "tun-vless-reality", path: []any{"inbounds", 0, "type"}, want: "tun"},
		{name: "reality tun interface name is left to sing-box", id: "tun-vless-reality", path: []any{"inbounds", 0, "interface_name"}, missing: true},
		{name: "reality outbound type", id: "tun-vless-reality", path: []any{"outbounds", 0, "type"}, want: "vless"},
		{name: "reality outbound tag", id: "tun-vless-reality", path: []any{"outbounds", 0, "tag"}, want: "proxy"},
		{name: "reality server port", id: "tun-vless-reality", path: []any{"outbounds", 0, "server_port"}, want: "443"},
		{name: "reality flow", id: "tun-vless-reality", path: []any{"outbounds", 0, "flow"}, want: "xtls-rprx-vision"},
		{name: "reality tls enabled", id: "tun-vless-reality", path: []any{"outbounds", 0, "tls", "enabled"}, want: "true"},
		{name: "reality tls utls fingerprint", id: "tun-vless-reality", path: []any{"outbounds", 0, "tls", "utls", "fingerprint"}, want: "chrome"},
		{name: "reality enabled", id: "tun-vless-reality", path: []any{"outbounds", 0, "tls", "reality", "enabled"}, want: "true"},
		{name: "reality short id is empty", id: "tun-vless-reality", path: []any{"outbounds", 0, "tls", "reality", "short_id"}, want: ""},
		{name: "reality route final", id: "tun-vless-reality", path: []any{"route", "final"}, want: "proxy"},
		{name: "reality dns final", id: "tun-vless-reality", path: []any{"dns", "final"}, want: "remote"},
		{name: "reality dns goes through the proxy", id: "tun-vless-reality", path: []any{"dns", "servers", 0, "detour"}, want: "proxy"},
		{name: "reality dns server type", id: "tun-vless-reality", path: []any{"dns", "servers", 0, "type"}, want: "https"},
		{name: "reality dns server address", id: "tun-vless-reality", path: []any{"dns", "servers", 0, "server"}, want: "1.1.1.1"},
		{name: "reality dns tls name", id: "tun-vless-reality", path: []any{"dns", "servers", 0, "tls", "server_name"}, want: "1.1.1.1"},
		{name: "reality local server type", id: "tun-vless-reality", path: []any{"dns", "servers", 1, "type"}, want: "local"},
		{name: "reality default domain resolver", id: "tun-vless-reality", path: []any{"route", "default_domain_resolver", "server"}, want: "remote"},

		{name: "socks single inbound", id: "socks-local", path: []any{"inbounds", 0, "type"}, want: "mixed"},
		{name: "socks listen port", id: "socks-local", path: []any{"inbounds", 0, "listen_port"}, want: "2080"},
		{name: "socks outbound type", id: "socks-local", path: []any{"outbounds", 0, "type"}, want: "shadowsocks"},
		{name: "socks outbound port", id: "socks-local", path: []any{"outbounds", 0, "server_port"}, want: "8388"},
		{name: "socks method", id: "socks-local", path: []any{"outbounds", 0, "method"}, want: "2022-blake3-aes-128-gcm"},
		{name: "socks route final", id: "socks-local", path: []any{"route", "final"}, want: "proxy"},

		{name: "selector outbound type", id: "selector", path: []any{"outbounds", 0, "type"}, want: "selector"},
		{name: "selector members", id: "selector", path: []any{"outbounds", 0, "outbounds"}, want: "auto,server-a,server-b,direct"},
		{name: "selector default", id: "selector", path: []any{"outbounds", 0, "default"}, want: "auto"},
		{name: "urltest type", id: "selector", path: []any{"outbounds", 1, "type"}, want: "urltest"},
		{name: "urltest members", id: "selector", path: []any{"outbounds", 1, "outbounds"}, want: "server-a,server-b"},
		{name: "urltest url", id: "selector", path: []any{"outbounds", 1, "url"}, want: "https://www.gstatic.com/generate_204"},
		{name: "urltest interval", id: "selector", path: []any{"outbounds", 1, "interval"}, want: "5m"},
		{name: "urltest tolerance", id: "selector", path: []any{"outbounds", 1, "tolerance"}, want: "50"},
		{name: "selector server a type", id: "selector", path: []any{"outbounds", 2, "type"}, want: "vless"},
		{name: "selector server b is trojan", id: "selector", path: []any{"outbounds", 3, "type"}, want: "trojan"},
		{name: "selector direct is last", id: "selector", path: []any{"outbounds", 4, "type"}, want: "direct"},
		{name: "selector route final", id: "selector", path: []any{"route", "final"}, want: "select"},
		{name: "selector dns follows the selector", id: "selector", path: []any{"dns", "servers", 0, "detour"}, want: "select"},
		{name: "selector dns server type", id: "selector", path: []any{"dns", "servers", 0, "type"}, want: "https"},
		{name: "selector dns server address", id: "selector", path: []any{"dns", "servers", 0, "server"}, want: "1.1.1.1"},
		{name: "selector default domain resolver", id: "selector", path: []any{"route", "default_domain_resolver", "server"}, want: "remote"},
	}

	for _, probe := range probes {
		t.Run(probe.name, func(t *testing.T) {
			root, err := domainconfig.Parse([]byte(templateByID(t, probe.id).Config))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			value, ok := lookup(t, root, probe.path)
			if probe.missing {
				if ok {
					t.Fatalf("%s: path %v must not be set, found %q", probe.id, probe.path, formatValue(value))
				}
				return
			}
			if !ok {
				t.Fatalf("%s: path %v is missing from the configuration", probe.id, probe.path)
			}
			if got := formatValue(value); got != probe.want {
				t.Fatalf("%s: %v = %q, want %q", probe.id, probe.path, got, probe.want)
			}
		})
	}
}

func TestTemplateSectionsAndTags(t *testing.T) {
	tests := []struct {
		id            string
		wantOutbounds []string
		wantInbounds  []string
		wantRoute     string
		wantDNS       string
	}{
		{id: "empty", wantOutbounds: []string{"direct"}, wantInbounds: nil, wantRoute: "", wantDNS: ""},
		{id: "tun-basic", wantOutbounds: []string{"direct"}, wantInbounds: []string{"mixed-in", "tun-in"}, wantRoute: "direct", wantDNS: "cloudflare"},
		{id: "tun-vless-reality", wantOutbounds: []string{"direct", "proxy"}, wantInbounds: []string{"mixed-in", "tun-in"}, wantRoute: "proxy", wantDNS: "remote"},
		{id: "socks-local", wantOutbounds: []string{"direct", "proxy"}, wantInbounds: []string{"mixed-in"}, wantRoute: "proxy", wantDNS: ""},
		{id: "selector", wantOutbounds: []string{"auto", "direct", "select", "server-a", "server-b"}, wantInbounds: []string{"mixed-in"}, wantRoute: "select", wantDNS: "remote"},
	}

	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			config := []byte(templateByID(t, tc.id).Config)
			if got := domainconfig.Tags(config, "outbounds"); !reflect.DeepEqual(got, tc.wantOutbounds) {
				t.Errorf("outbound tags = %v, want %v", got, tc.wantOutbounds)
			}
			if got := domainconfig.Tags(config, "inbounds"); !reflect.DeepEqual(got, tc.wantInbounds) {
				t.Errorf("inbound tags = %v, want %v", got, tc.wantInbounds)
			}

			root, err := domainconfig.Parse(config)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got := sectionFinal(t, root, "route"); got != tc.wantRoute {
				t.Errorf("route.final = %q, want %q", got, tc.wantRoute)
			}
			if got := sectionFinal(t, root, "dns"); got != tc.wantDNS {
				t.Errorf("dns.final = %q, want %q", got, tc.wantDNS)
			}
			if tc.wantRoute == "" {
				if _, ok := root["route"]; ok {
					t.Errorf("route section present although the template is %q", tc.id)
				}
			}
		})
	}
}

func sectionFinal(t *testing.T, root map[string]any, section string) string {
	t.Helper()
	raw, ok := root[section]
	if !ok {
		return ""
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("%s is not an object", section)
	}
	final, _ := obj["final"].(string)
	return final
}

// TestTemplatesWithPlaceholdersStayJsonValid documents the design decision that
// user-supplied values are shipped as non-empty placeholder strings: the
// template must be parseable before the user edits it.
func TestTemplatesWithPlaceholdersStayJsonValid(t *testing.T) {
	tests := []struct {
		id      string
		needs   []string
		without bool
	}{
		{id: "empty", without: true},
		{id: "tun-basic", without: true},
		{id: "tun-vless-reality", needs: []string{"REPLACE_WITH_SERVER", "REPLACE_WITH_UUID", "REPLACE_WITH_SNI"}},
		{id: "socks-local", needs: []string{"REPLACE_WITH_SERVER"}},
		{id: "selector", needs: []string{"REPLACE_WITH_SERVER_A", "REPLACE_WITH_SERVER_B", "REPLACE_WITH_UUID", "REPLACE_WITH_SNI"}},
	}

	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			tpl := templateByID(t, tc.id)
			if tc.without {
				if strings.Contains(tpl.Config, "REPLACE_WITH") {
					t.Errorf("%s must not contain placeholders", tc.id)
				}
				return
			}
			for _, token := range tc.needs {
				if !strings.Contains(tpl.Config, token) {
					t.Errorf("%s is missing the placeholder %s", tc.id, token)
				}
			}
			// A placeholder must never leave an empty JSON value behind. Empty
			// strings are reserved for values that are meaningful when empty
			// (for example tls.reality.short_id).
			for _, empty := range []string{`"server": ""`, `"uuid": ""`, `"password": ""`, `"public_key": ""`, `"server_name": ""`} {
				if strings.Contains(tpl.Config, empty) {
					t.Errorf("%s ships the empty required value %s", tc.id, empty)
				}
			}
			if result := domainconfig.Validate([]byte(tpl.Config)); !result.OK {
				t.Errorf("validation of the untouched template failed: %v", result.Errors)
			}
		})
	}
}

func TestGetUnknownTemplateReturnsATypedNotFound(t *testing.T) {
	for _, id := range []string{"", "nope", "EMPTY", "empty "} {
		t.Run("id="+id, func(t *testing.T) {
			tpl, err := Get(id)
			if err == nil {
				t.Fatalf("Get(%q) = %+v, want an error", id, tpl)
			}
			if code := apperr.CodeOf(err); code != apperr.CodeNotFound {
				t.Fatalf("Get(%q) code = %q, want %q", id, code, apperr.CodeNotFound)
			}
			if message := err.Error(); !strings.Contains(message, "unknown template") || !strings.Contains(message, id) {
				t.Fatalf("Get(%q) message = %q, want it to name the template", id, message)
			}
			if tpl.ID != "" || tpl.Config != "" {
				t.Fatalf("Get(%q) returned %+v, want the zero template", id, tpl)
			}
		})
	}
}

func TestMustGetPanicsOnAnUnknownID(t *testing.T) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("MustGet did not panic for an unknown id")
		}
		message, ok := recovered.(string)
		if !ok {
			t.Fatalf("panic value is %T, want a string", recovered)
		}
		if !strings.HasPrefix(message, "templates:") || !strings.Contains(message, "does-not-exist") {
			t.Fatalf("panic value = %q, want the wrapped template error", message)
		}
	}()
	_ = MustGet("does-not-exist")
}

func TestMustGetReturnsTheCatalogEntry(t *testing.T) {
	for _, id := range wantIDs {
		want, err := Get(id)
		if err != nil {
			t.Fatalf("Get(%q): %v", id, err)
		}
		if got := MustGet(id); got != want {
			t.Fatalf("MustGet(%q) = %+v, want %+v", id, got, want)
		}
	}
}

func TestAllReturnsCopiesThatCannotCorruptTheCatalog(t *testing.T) {
	first := All()
	first[0].ID = "mutated"
	first[0].Config = "{}"
	first[0].RequiresPrivilege = true

	second := All()
	if !reflect.DeepEqual(second, All()) {
		t.Error("two consecutive All() calls differ")
	}
	if second[0].ID != "empty" || strings.Contains(second[0].Config, "{}") || second[0].RequiresPrivilege {
		t.Fatalf("All() leaked the mutation: %+v", second[0])
	}
	if tpl := templateByID(t, "empty"); tpl.ID != "empty" || tpl.RequiresPrivilege {
		t.Fatalf("Get(\"empty\") leaked the mutation: %+v", tpl)
	}
	if got := IDs()[0]; got != "empty" {
		t.Fatalf("IDs()[0] = %q after a mutation", got)
	}
}

func TestTemplateJSONContract(t *testing.T) {
	raw, err := json.Marshal(MustGet("tun-basic"))
	if err != nil {
		t.Fatalf("marshal template: %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal template: %v", err)
	}
	for _, key := range []string{"id", "name", "description", "config", "requiresPrivilege"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("template JSON is missing %q", key)
		}
	}

	rawIDs, err := json.Marshal(IDs())
	if err != nil {
		t.Fatalf("marshal ids: %v", err)
	}
	var ids []string
	if err := json.Unmarshal(rawIDs, &ids); err != nil {
		t.Fatalf("unmarshal ids: %v", err)
	}
	if !reflect.DeepEqual(ids, wantIDs) {
		t.Fatalf("IDs() round-trip = %v, want %v", ids, wantIDs)
	}
}

// managedBinaryForTest returns an installed sing-box for the validator-backed
// tests. SINGBOXUI_TEST_SINGBOX wins, then the managed install directory, then
// PATH; an empty result means the test is skipped rather than failed.
func managedBinaryForTest(t *testing.T) string {
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

// TestTemplatesPassTheManagedValidator runs every template through the real
// validator, which is the only check that catches a field a sing-box release
// removed: the app refuses to start a configuration the binary rejects
// (spec §37, §38), so a starter that fails here ships a dead end. Skipped when
// no binary is installed.
func TestTemplatesPassTheManagedValidator(t *testing.T) {
	binary := managedBinaryForTest(t)
	if binary == "" {
		t.Skip("no sing-box binary found; set SINGBOXUI_TEST_SINGBOX to validate the templates")
	}
	t.Logf("validating templates with %s", binary)

	for _, tpl := range All() {
		t.Run(tpl.ID, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(tpl.Config), 0o600); err != nil {
				t.Fatalf("write the template: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			out, err := exec.CommandContext(ctx, binary, "check", "-c", path).CombinedOutput()
			if err != nil {
				t.Fatalf("sing-box check rejected the %s template: %v\n%s", tpl.ID, err, strings.TrimSpace(string(out)))
			}
		})
	}
}

// TestTemplatesAvoidFormsTheCurrentSingBoxRemoved keeps the starters clear of
// the settings sing-box dropped over 1.12-1.14. The validator test above proves
// the current binary accepts them; this one explains *why* a field may not come
// back, so a future edit does not reintroduce an old spelling.
func TestTemplatesAvoidFormsTheCurrentSingBoxRemoved(t *testing.T) {
	// Fields removed from the inbound level (1.11 deprecated, 1.13 removed).
	removedInboundFields := []string{"sniff", "sniff_override_destination", "domain_strategy"}

	for _, tpl := range All() {
		t.Run(tpl.ID, func(t *testing.T) {
			root, err := domainconfig.Parse([]byte(tpl.Config))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}

			dns, _ := root["dns"].(map[string]any)
			servers, _ := dns["servers"].([]any)
			for i, raw := range servers {
				server, ok := raw.(map[string]any)
				if !ok {
					t.Fatalf("dns.servers[%d] is not an object", i)
				}
				if _, ok := server["address"]; ok {
					t.Errorf("dns.servers[%d] uses the legacy \"address\" form (deprecated in sing-box 1.12, removed in 1.14); use type/server/tls", i)
				}
				if kind, _ := server["type"].(string); strings.TrimSpace(kind) == "" {
					t.Errorf("dns.servers[%d] has no \"type\"", i)
				}
				// A DNS server already dials directly, so naming the implicit
				// direct outbound is rejected at start-up ("detour to an empty
				// direct outbound makes no sense") — and `check` does not catch
				// it, which is exactly the "profile does not start" report.
				if detour, _ := server["detour"].(string); detour == "direct" {
					t.Errorf("dns.servers[%d] detours through \"direct\", which sing-box 1.14 refuses at start-up; drop the detour or name a real outbound", i)
				}
			}

			// A remote DNS server means sing-box has to resolve a domain
			// somewhere; 1.14 refuses to start when nothing declares that
			// resolver for a dial.
			if len(servers) > 0 {
				route, _ := root["route"].(map[string]any)
				if _, ok := route["default_domain_resolver"]; !ok {
					t.Errorf("route.default_domain_resolver is missing although %d DNS servers are declared; sing-box 1.14 refuses to start without it", len(servers))
				}
			}

			inbounds, _ := root["inbounds"].([]any)
			for i, raw := range inbounds {
				inbound, ok := raw.(map[string]any)
				if !ok {
					t.Fatalf("inbounds[%d] is not an object", i)
				}
				for _, field := range removedInboundFields {
					if _, ok := inbound[field]; ok {
						t.Errorf("inbounds[%d] uses %q, which was removed in sing-box 1.13; use a route rule with \"action\": \"sniff\" instead", i, field)
					}
				}
				// A pinned TUN name only works on the platform it was written
				// for: macOS accepts utun* and nothing else, and sing-box dies
				// with "bad tun name" before it ever asks for privileges. Left
				// unset, sing-box picks a name the platform accepts.
				if kind, _ := inbound["type"].(string); kind == "tun" {
					if name, ok := inbound["interface_name"]; ok {
						t.Errorf("inbounds[%d] pins interface_name %v; leave it unset so sing-box chooses a platform-valid device name", i, name)
					}
				}
			}
		})
	}
}

// TestTemplateSecretsAreValidShapedPlaceholders documents why the secrets in the
// templates are valid base64 rather than readable words: sing-box decodes them
// before it starts, so "REPLACE_WITH_PUBLIC_KEY" makes the template unusable and
// "invalid public_key" is all the user would ever see. The values are still
// obvious placeholders (zeros), and the template description says what to fill in.
func TestTemplateSecretsAreValidShapedPlaceholders(t *testing.T) {
	t.Run("reality public key", func(t *testing.T) {
		root, err := domainconfig.Parse([]byte(templateByID(t, "tun-vless-reality").Config))
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		raw, ok := lookup(t, root, []any{"outbounds", 0, "tls", "reality", "public_key"})
		if !ok {
			t.Fatal("tls.reality.public_key is missing")
		}
		key, _ := raw.(string)
		decoded, err := base64.RawURLEncoding.DecodeString(key)
		if err != nil {
			t.Fatalf("public_key %q is not raw-url base64: %v", key, err)
		}
		if len(decoded) != 32 {
			t.Errorf("public_key decodes to %d bytes, want the 32 bytes of an X25519 key", len(decoded))
		}
	})

	t.Run("shadowsocks password", func(t *testing.T) {
		root, err := domainconfig.Parse([]byte(templateByID(t, "socks-local").Config))
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		method, _ := lookup(t, root, []any{"outbounds", 0, "method"})
		raw, ok := lookup(t, root, []any{"outbounds", 0, "password"})
		if !ok {
			t.Fatal("password is missing")
		}
		password, _ := raw.(string)
		decoded, err := base64.StdEncoding.DecodeString(password)
		if err != nil {
			t.Fatalf("password %q is not standard base64: %v", password, err)
		}
		// 2022-blake3-aes-128-gcm keys are 16 bytes; sing-box rejects any other
		// length with "decode key".
		if len(decoded) != 16 {
			t.Errorf("password for %v decodes to %d bytes, want 16", method, len(decoded))
		}
	})
}

// startingTemplates is the shared harness for the two tests below: it writes a
// template to a temporary file with every inbound port reassigned to an
// ephemeral one, starts the real binary and reports the log together with
// whether sing-box announced a start.
//
// The child writes to a file rather than a shared buffer: the test polls it while
// sing-box runs, and file reads keep that free of data races under -race.
func startingTemplate(t *testing.T, binary, configJSON string) (log string, started bool) {
	t.Helper()
	root, err := domainconfig.Parse([]byte(configJSON))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if inbounds, ok := root["inbounds"].([]any); ok {
		for i, raw := range inbounds {
			inbound, ok := raw.(map[string]any)
			if !ok {
				t.Fatalf("inbounds[%d] is not an object", i)
			}
			if _, ok := inbound["listen_port"]; ok {
				inbound["listen_port"] = 0
			}
		}
	}
	raw, err := json.Marshal(root)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, raw, 0o600); err != nil {
		t.Fatalf("write the config: %v", err)
	}
	logPath := filepath.Join(dir, "sing-box.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create the log: %v", err)
	}
	defer logFile.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "run", "-c", configPath)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sing-box: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	deadline := time.After(15 * time.Second)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return readLog(t, logPath), false
		case <-deadline:
			t.Fatalf("sing-box reported neither a start nor a failure within 15s:\n%s", readLog(t, logPath))
		case <-ticker.C:
			if strings.Contains(readLog(t, logPath), "sing-box started") {
				_ = cmd.Process.Kill()
				<-done
				return readLog(t, logPath), true
			}
		}
	}
}

// fatalLine returns the first FATAL line of a sing-box log, without the escape
// sequences sing-box adds when it writes to a terminal.
func fatalLine(log string) string {
	clean := regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(log, "")
	for _, line := range strings.Split(clean, "\n") {
		if strings.Contains(line, "FATAL") {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

// TestTemplatesWithoutTunStartUnderTheManagedBinary is the only test that proves
// a starter really comes up. `sing-box check` only parses the file; sing-box runs
// further checks when it starts, and a configuration rejected there is what the
// user reports as "sing-box does not start".
func TestTemplatesWithoutTunStartUnderTheManagedBinary(t *testing.T) {
	binary := managedBinaryForTest(t)
	if binary == "" {
		t.Skip("no sing-box binary found; set SINGBOXUI_TEST_SINGBOX to start the templates")
	}
	t.Logf("starting templates with %s", binary)

	for _, tpl := range All() {
		t.Run(tpl.ID, func(t *testing.T) {
			root, err := domainconfig.Parse([]byte(tpl.Config))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if hasTun(root) {
				t.Skip("starting a TUN needs root; TestTemplatesWithTunOnlyFailWithoutPrivileges covers it")
			}
			log, started := startingTemplate(t, binary, tpl.Config)
			if !started {
				t.Fatalf("the %s template did not start:\n%s", tpl.ID, log)
			}
			// The DNS and route services start before the inbounds, so a server
			// that cannot dial has already complained by now.
			if fatal := fatalLine(log); fatal != "" {
				t.Errorf("sing-box started but logged %s", fatal)
			}
		})
	}
}

// TestTemplatesWithTunOnlyFailWithoutPrivileges starts the TUN templates too,
// knowing they cannot succeed here: the suite never asks for root.
//
// What matters is *why* they fail. Anything about the configuration — a name the
// platform refuses ("bad tun name: singboxui0" on macOS), a field this sing-box
// release removed — is a template the user can never start, which is exactly the
// "sing-box does not start" report. A missing privilege, on the other hand, is
// the app's own business: it elevates through the privileged helper (spec §27).
func TestTemplatesWithTunOnlyFailWithoutPrivileges(t *testing.T) {
	binary := managedBinaryForTest(t)
	if binary == "" {
		t.Skip("no sing-box binary found; set SINGBOXUI_TEST_SINGBOX to start the templates")
	}

	configProblems := []string{
		"bad tun name",
		"decode config",
		"detour to an empty",
		"unknown field",
		"legacy",
	}
	for _, tpl := range All() {
		t.Run(tpl.ID, func(t *testing.T) {
			root, err := domainconfig.Parse([]byte(tpl.Config))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if !hasTun(root) {
				t.Skip("no TUN inbound; TestTemplatesWithoutTunStartUnderTheManagedBinary covers it")
			}
			log, started := startingTemplate(t, binary, tpl.Config)
			if started {
				return // running as root: the interface came up
			}
			fatal := fatalLine(log)
			if fatal == "" {
				t.Fatalf("sing-box neither started nor said why:\n%s", log)
			}
			for _, problem := range configProblems {
				if strings.Contains(fatal, problem) {
					t.Errorf("the %s template dies on its own configuration, not on missing privileges: %s", tpl.ID, fatal)
				}
			}
		})
	}
}

// hasTun reports whether a parsed configuration declares a TUN inbound, which is
// what decides whether starting it needs privileges.
func hasTun(root map[string]any) bool {
	inbounds, _ := root["inbounds"].([]any)
	for _, raw := range inbounds {
		inbound, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if kind, _ := inbound["type"].(string); kind == "tun" {
			return true
		}
	}
	return false
}

// readLog returns the log written so far, or a placeholder when the file cannot
// be read; the assertion that follows always explains the failure itself.
func readLog(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		return "(the log could not be read: " + err.Error() + ")"
	}
	return string(raw)
}
