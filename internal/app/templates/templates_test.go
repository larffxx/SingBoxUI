package templates

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	domainconfig "github.com/larffxx/singboxui/internal/domain/config"
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
	}{
		{name: "empty log level", id: "empty", path: []any{"log", "level"}, want: "info"},
		{name: "empty timestamp", id: "empty", path: []any{"log", "timestamp"}, want: "true"},
		{name: "empty single direct outbound", id: "empty", path: []any{"outbounds", 0, "type"}, want: "direct"},
		{name: "empty direct tag", id: "empty", path: []any{"outbounds", 0, "tag"}, want: "direct"},

		{name: "tun inbound type", id: "tun-basic", path: []any{"inbounds", 0, "type"}, want: "tun"},
		{name: "tun tag", id: "tun-basic", path: []any{"inbounds", 0, "tag"}, want: "tun-in"},
		{name: "tun interface name", id: "tun-basic", path: []any{"inbounds", 0, "interface_name"}, want: "singboxui0"},
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
		{name: "tun-basic dns server detour", id: "tun-basic", path: []any{"dns", "servers", 0, "detour"}, want: "direct"},
		{name: "tun-basic dns address", id: "tun-basic", path: []any{"dns", "servers", 0, "address"}, want: "https://1.1.1.1/dns-query"},
		{name: "tun-basic rule 3 keeps private traffic direct", id: "tun-basic", path: []any{"route", "rules", 2, "outbound"}, want: "direct"},
		{name: "tun-basic rule 3 matches private ip", id: "tun-basic", path: []any{"route", "rules", 2, "ip_is_private"}, want: "true"},
		{name: "tun-basic dns rule hijacks", id: "tun-basic", path: []any{"route", "rules", 1, "action"}, want: "hijack-dns"},

		{name: "reality inbound is tun", id: "tun-vless-reality", path: []any{"inbounds", 0, "type"}, want: "tun"},
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
	}

	for _, probe := range probes {
		t.Run(probe.name, func(t *testing.T) {
			root, err := domainconfig.Parse([]byte(templateByID(t, probe.id).Config))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			value, ok := lookup(t, root, probe.path)
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
		{id: "tun-vless-reality", needs: []string{"REPLACE_WITH_SERVER", "REPLACE_WITH_UUID", "REPLACE_WITH_SNI", "REPLACE_WITH_PUBLIC_KEY"}},
		{id: "socks-local", needs: []string{"REPLACE_WITH_SERVER", "REPLACE_WITH_PASSWORD"}},
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
