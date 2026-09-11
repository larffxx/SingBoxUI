package config

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestParseAcceptsValidObjectsAndPreservesNumberLiterals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		raw   string
		check func(t *testing.T, root map[string]any)
	}{
		{
			name: "empty object",
			raw:  `{}`,
			check: func(t *testing.T, root map[string]any) {
				if len(root) != 0 {
					t.Errorf("root = %#v, want an empty map", root)
				}
			},
		},
		{
			name: "nested sections survive",
			raw:  `{"inbounds":[{"type":"tun","tag":"tun-in"}],"route":{"rules":[]},"log":{"level":"info"}}`,
			check: func(t *testing.T, root map[string]any) {
				inbounds, ok := root["inbounds"].([]any)
				if !ok || len(inbounds) != 1 {
					t.Fatalf("inbounds = %#v, want one entry", root["inbounds"])
				}
				route, ok := root["route"].(map[string]any)
				if !ok {
					t.Fatalf("route = %#v, want an object", root["route"])
				}
				if _, ok := route["rules"].([]any); !ok {
					t.Errorf("route.rules = %#v, want an array", route["rules"])
				}
			},
		},
		{
			name: "large integer keeps every digit",
			raw:  `{"n":9007199254740993}`,
			check: func(t *testing.T, root map[string]any) {
				num, ok := root["n"].(json.Number)
				if !ok {
					t.Fatalf("n is %T, want json.Number so the literal is not lost", root["n"])
				}
				if num.String() != "9007199254740993" {
					t.Errorf("n = %q, want the literal 9007199254740993", num.String())
				}
			},
		},
		{
			name: "exponent literal is not normalised",
			raw:  `{"n":1e3}`,
			check: func(t *testing.T, root map[string]any) {
				num, ok := root["n"].(json.Number)
				if !ok {
					t.Fatalf("n is %T, want json.Number", root["n"])
				}
				if num.String() != "1e3" {
					t.Errorf("n = %q, want the literal 1e3", num.String())
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root, err := Parse([]byte(tc.raw))
			if err != nil {
				t.Fatalf("Parse() = %v, want nil", err)
			}
			tc.check(t, root)
		})
	}
}

func TestParseRejectsMalformedInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
	}{
		{name: "empty input", raw: ``},
		{name: "truncated object", raw: `{"a":1`},
		{name: "trailing second value", raw: `{"a":1}{"b":2}`},
		{name: "top-level array", raw: `[1,2]`},
		{name: "top-level scalar", raw: `123`},
		{name: "garbage", raw: `not json`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root, err := Parse([]byte(tc.raw))
			if err == nil {
				t.Fatalf("Parse(%q) = %#v, want an error", tc.raw, root)
			}
			if root != nil {
				t.Errorf("Parse(%q) returned a map alongside the error: %#v", tc.raw, root)
			}
			if !strings.Contains(err.Error(), "invalid JSON") {
				t.Errorf("error %q should be recognisable as an invalid-JSON failure", err)
			}
		})
	}
}

// TestParseAcceptsBareNull pins the current contract: a top-level null decodes
// into a nil map without erroring. It is documented here so a change to reject
// it explicitly is visible rather than silent.
func TestParseAcceptsBareNull(t *testing.T) {
	t.Parallel()

	root, err := Parse([]byte("null"))
	if err != nil {
		t.Fatalf("Parse(null) = %v, want nil", err)
	}
	if root != nil {
		t.Errorf("Parse(null) = %#v, want a nil map", root)
	}
}

func TestPrettyIndentsSortsAndAppendsNewline(t *testing.T) {
	t.Parallel()

	got, err := Pretty([]byte(`{"b":2,"a":1}`))
	if err != nil {
		t.Fatalf("Pretty() = %v, want nil", err)
	}
	want := "{\n  \"a\": 1,\n  \"b\": 2\n}\n"
	if string(got) != want {
		t.Errorf("Pretty() = %q, want %q", got, want)
	}

	// Number literals must not be rewritten into floats.
	got, err = Pretty([]byte(`{"n":9007199254740993}`))
	if err != nil {
		t.Fatalf("Pretty() = %v, want nil", err)
	}
	if !strings.Contains(string(got), "9007199254740993") {
		t.Errorf("Pretty() = %q, want the integer literal preserved", got)
	}

	if _, err := Pretty([]byte(`{`)); err == nil {
		t.Error("Pretty() = nil, want an error for invalid JSON")
	}
}

func TestMarshalRendersCanonicalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   map[string]any
		want string
	}{
		{name: "nil map", in: nil, want: "null\n"},
		{name: "empty map", in: map[string]any{}, want: "{}\n"},
		{
			name: "sorted and indented",
			in:   map[string]any{"b": json.Number("2"), "a": json.Number("1")},
			want: "{\n  \"a\": 1,\n  \"b\": 2\n}\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := Marshal(tc.in)
			if err != nil {
				t.Fatalf("Marshal() = %v, want nil", err)
			}
			if string(got) != tc.want {
				t.Errorf("Marshal() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		raw        string
		wantOK     bool
		wantErrors []string
		wantWarns  []string
	}{
		{
			name:   "empty config is structurally valid",
			raw:    `{}`,
			wantOK: true,
		},
		{
			name:   "sections are optional",
			raw:    `{"log":{"level":"info"}}`,
			wantOK: true,
		},
		{
			name:   "valid inbound and outbound",
			raw:    `{"inbounds":[{"type":"mixed","tag":"in"}],"outbounds":[{"type":"direct","tag":"out"}]}`,
			wantOK: true,
		},
		{
			name:       "section must be an array",
			raw:        `{"inbounds":{}}`,
			wantOK:     false,
			wantErrors: []string{"inbounds must be an array"},
		},
		{
			name:       "entry must be an object",
			raw:        `{"inbounds":["x"]}`,
			wantOK:     false,
			wantErrors: []string{"inbounds[0] must be an object"},
		},
		{
			name:       "missing tag",
			raw:        `{"inbounds":[{"type":"mixed"}]}`,
			wantOK:     false,
			wantErrors: []string{"inbounds[0]: missing tag"},
		},
		{
			name:       "duplicate tag reports both indexes",
			raw:        `{"inbounds":[{"type":"mixed","tag":"a"},{"type":"mixed","tag":"a"}]}`,
			wantOK:     false,
			wantErrors: []string{`inbounds: duplicate tag "a" (indexes 0 and 1)`},
		},
		{
			name:       "missing type",
			raw:        `{"inbounds":[{"tag":"a"}]}`,
			wantOK:     false,
			wantErrors: []string{`inbounds "a": missing type`},
		},
		{
			name:       "empty outbounds rejected",
			raw:        `{"outbounds":[]}`,
			wantOK:     false,
			wantErrors: []string{"outbounds must be a non-empty array"},
		},
		{
			name:   "empty inbounds allowed",
			raw:    `{"inbounds":[]}`,
			wantOK: true,
		},
		{
			name:   "remote outbound needs a server and a port",
			raw:    `{"outbounds":[{"type":"trojan","tag":"t","password":"p"}]}`,
			wantOK: false,
			wantErrors: []string{
				`outbound "t" (trojan): missing server`,
				`outbound "t" (trojan): missing server_port`,
			},
		},
		{
			name:       "trojan needs a password",
			raw:        `{"outbounds":[{"type":"trojan","tag":"t","server":"h","server_port":443}]}`,
			wantOK:     false,
			wantErrors: []string{`outbound "t" (trojan): missing password`},
		},
		{
			name:       "selector needs an outbounds list",
			raw:        `{"outbounds":[{"type":"selector","tag":"s"}]}`,
			wantOK:     false,
			wantErrors: []string{`outbound "s" (selector): missing outbounds list`},
		},
		{
			name:       "urltest needs an outbounds list",
			raw:        `{"outbounds":[{"type":"urltest","tag":"u"}]}`,
			wantOK:     false,
			wantErrors: []string{`outbound "u" (urltest): missing outbounds list`},
		},
		{
			name:       "vless needs a uuid",
			raw:        `{"outbounds":[{"type":"vless","tag":"v","server":"h","server_port":443}]}`,
			wantOK:     false,
			wantErrors: []string{`outbound "v" (vless): missing uuid`},
		},
		{
			name:   "shadowsocks needs method and password",
			raw:    `{"outbounds":[{"type":"shadowsocks","tag":"s","server":"h","server_port":8388}]}`,
			wantOK: false,
			wantErrors: []string{
				`outbound "s" (shadowsocks): missing method`,
				`outbound "s" (shadowsocks): missing password`,
			},
		},
		{
			name:       "wireguard needs a private key",
			raw:        `{"outbounds":[{"type":"wireguard","tag":"w","server":"h","server_port":443}]}`,
			wantOK:     false,
			wantErrors: []string{`outbound "w" (wireguard): missing private_key`},
		},
		{
			name:       "tuic needs a uuid or password",
			raw:        `{"outbounds":[{"type":"tuic","tag":"t","server":"h","server_port":443}]}`,
			wantOK:     false,
			wantErrors: []string{`outbound "t" (tuic): missing uuid/password`},
		},
		{
			name:   "direct outbound is complete",
			raw:    `{"outbounds":[{"type":"direct","tag":"d"}]}`,
			wantOK: true,
		},
		{
			name:       "route must be an object",
			raw:        `{"route":[]}`,
			wantOK:     false,
			wantErrors: []string{"route must be an object"},
		},
		{
			name:       "route.rules must be an array",
			raw:        `{"route":{"rules":{}}}`,
			wantOK:     false,
			wantErrors: []string{"route.rules must be an array"},
		},
		{
			name:       "route rule must be an object",
			raw:        `{"route":{"rules":[1]}}`,
			wantOK:     false,
			wantErrors: []string{"route.rules[0] must be an object"},
		},
		{
			name:       "dns must be an object",
			raw:        `{"dns":[]}`,
			wantOK:     false,
			wantErrors: []string{"dns must be an object"},
		},
		{
			name:       "dns.servers must be an array",
			raw:        `{"dns":{"servers":{}}}`,
			wantOK:     false,
			wantErrors: []string{"dns.servers must be an array"},
		},
		{
			name:       "dns server must be an object",
			raw:        `{"dns":{"servers":[1]}}`,
			wantOK:     false,
			wantErrors: []string{"dns.servers[0] must be an object"},
		},
		{
			name:      "dns server without a tag warns but passes",
			raw:       `{"dns":{"servers":[{"address":"1.1.1.1"}]}}`,
			wantOK:    true,
			wantWarns: []string{"dns.servers[0]: missing tag"},
		},
		{
			name:      "tun inbound warns about privileges",
			raw:       `{"inbounds":[{"type":"tun","tag":"tun0"}]}`,
			wantOK:    true,
			wantWarns: []string{"configuration contains a tun inbound: starting it requires administrator privileges"},
		},
		{
			name:      "tun endpoint warns about privileges",
			raw:       `{"endpoints":[{"type":"tun","tag":"tun0"}]}`,
			wantOK:    true,
			wantWarns: []string{"configuration contains a tun inbound: starting it requires administrator privileges"},
		},
		{
			name:       "malformed JSON fails",
			raw:        `{`,
			wantOK:     false,
			wantErrors: []string{"invalid JSON"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := Validate([]byte(tc.raw))
			if res.OK != tc.wantOK {
				t.Errorf("OK = %v, want %v (errors %v)", res.OK, tc.wantOK, res.Errors)
			}
			for _, want := range tc.wantErrors {
				if !containsSubstring(res.Errors, want) {
					t.Errorf("Errors = %v, want an entry containing %q", res.Errors, want)
				}
			}
			for _, want := range tc.wantWarns {
				if !containsSubstring(res.Warnings, want) {
					t.Errorf("Warnings = %v, want an entry containing %q", res.Warnings, want)
				}
			}
			if tc.wantOK && len(tc.wantErrors) == 0 && len(res.Errors) != 0 {
				t.Errorf("Errors = %v, want none for a valid config", res.Errors)
			}
		})
	}
}

// TestValidateInitialisesResultSlices pins that both slices are non-nil, so the
// JSON contract to the frontend is always an array and never null.
func TestValidateInitialisesResultSlices(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{`{}`, `{`} {
		res := Validate([]byte(raw))
		if res.Errors == nil {
			t.Errorf("Validate(%q).Errors = nil, want an empty slice", raw)
		}
		if res.Warnings == nil {
			t.Errorf("Validate(%q).Warnings = nil, want an empty slice", raw)
		}
	}
}

func TestHasTUN(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "tun inbound", raw: `{"inbounds":[{"type":"tun","tag":"t"}]}`, want: true},
		{name: "tun endpoint", raw: `{"endpoints":[{"type":"tun"}]}`, want: true},
		{name: "mixed inbound only", raw: `{"inbounds":[{"type":"mixed"}]}`, want: false},
		{name: "no sections", raw: `{}`, want: false},
		{name: "invalid JSON", raw: `{`, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := HasTUN([]byte(tc.raw)); got != tc.want {
				t.Errorf("HasTUN(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestTags(t *testing.T) {
	t.Parallel()

	t.Run("sorted and filtered", func(t *testing.T) {
		t.Parallel()
		raw := `{"outbounds":[{"tag":"b"},{"tag":"a"},{"tag":"c"}]}`
		got := Tags([]byte(raw), "outbounds")
		want := []string{"a", "b", "c"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Tags() = %v, want %v", got, want)
		}
	})

	t.Run("skips empty, non-string and non-object entries", func(t *testing.T) {
		t.Parallel()
		raw := `{"inbounds":[{"tag":""},{"tag":1},"x",{"tag":"ok"},{"type":"tun"}]}`
		got := Tags([]byte(raw), "inbounds")
		want := []string{"ok"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Tags() = %v, want %v", got, want)
		}
	})

	t.Run("nil for missing or malformed sections", func(t *testing.T) {
		t.Parallel()
		for _, tc := range []struct {
			name    string
			raw     string
			section string
		}{
			{name: "missing section", raw: `{}`, section: "inbounds"},
			{name: "non-array section", raw: `{"inbounds":{}}`, section: "inbounds"},
			{name: "invalid JSON", raw: `{`, section: "inbounds"},
		} {
			if got := Tags([]byte(tc.raw), tc.section); got != nil {
				t.Errorf("Tags(%q, %q) = %v, want nil", tc.raw, tc.section, got)
			}
		}
	})

	t.Run("empty array yields no tags", func(t *testing.T) {
		t.Parallel()
		got := Tags([]byte(`{"inbounds":[]}`), "inbounds")
		if len(got) != 0 {
			t.Errorf("Tags() = %v, want empty", got)
		}
	})
}

func containsSubstring(haystack []string, needle string) bool {
	for _, s := range haystack {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}
