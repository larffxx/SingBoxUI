package share

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// vless
// ---------------------------------------------------------------------------

func parseVless(raw string) (Parsed, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return Parsed{}, invalidErr(opParse, "vless link is not a valid URL", detailMalformedURL)
	}
	q := u.Query()
	host, err := requireHost(u.Hostname(), "vless")
	if err != nil {
		return Parsed{}, err
	}
	port, err := requirePort(u.Port(), "vless")
	if err != nil {
		return Parsed{}, err
	}
	uuid := ""
	if u.User != nil {
		uuid = strings.TrimSpace(u.User.Username())
	}
	if uuid == "" {
		return Parsed{}, invalidErr(opParse, "vless link has no UUID", detailMissingUUID)
	}
	out := map[string]any{
		"type":            KindVless,
		"server":          host,
		"server_port":     port,
		"uuid":            uuid,
		"network":         networkOf(q),
		"packet_encoding": valueOr(strings.TrimSpace(q.Get("packetEncoding")), "xudp"),
	}
	if flow := strings.TrimSpace(q.Get("flow")); flow != "" {
		out["flow"] = flow
	}
	if tls := applyTLS(q, false); tls != nil {
		out["tls"] = tls
	}
	if tr := applyTransport(q); tr != nil {
		out["transport"] = tr
	}
	return Parsed{
		Kind:     KindVless,
		Tag:      u.Fragment,
		Outbound: out,
		Warnings: unknownParams("vless", q, vlessKnown),
	}, nil
}

// ---------------------------------------------------------------------------
// trojan
// ---------------------------------------------------------------------------

func parseTrojan(raw string) (Parsed, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return Parsed{}, invalidErr(opParse, "trojan link is not a valid URL", detailMalformedURL)
	}
	q := u.Query()
	host, err := requireHost(u.Hostname(), "trojan")
	if err != nil {
		return Parsed{}, err
	}
	port, err := requirePort(u.Port(), "trojan")
	if err != nil {
		return Parsed{}, err
	}
	password := userSecret(u.User)
	if password == "" {
		return Parsed{}, invalidErr(opParse, "trojan link has no password", detailMissingPassword)
	}
	out := map[string]any{
		"type":        KindTrojan,
		"server":      host,
		"server_port": port,
		"password":    password,
		"network":     networkOf(q),
	}
	// Trojan is a TLS protocol: TLS is on unless the link explicitly disables it.
	if tls := applyTLS(q, true); tls != nil {
		out["tls"] = tls
	}
	if tr := applyTransport(q); tr != nil {
		out["transport"] = tr
	}
	return Parsed{
		Kind:     KindTrojan,
		Tag:      u.Fragment,
		Outbound: out,
		Warnings: unknownParams("trojan", q, trojanKnown),
	}, nil
}

// ---------------------------------------------------------------------------
// shadowsocks
// ---------------------------------------------------------------------------

func parseShadowsocks(raw string) (Parsed, error) {
	rest := raw[strings.Index(raw, "://")+3:]
	tag := ""
	if i := strings.Index(rest, "#"); i >= 0 {
		tag = decodeFragment(rest[i+1:])
		rest = rest[:i]
	}
	query := ""
	if i := strings.Index(rest, "?"); i >= 0 {
		query = rest[i+1:]
		rest = rest[:i]
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return Parsed{}, invalidErr(opParse, "ss link has no server", detailMissingHost)
	}

	var cred, hostport string
	if at := strings.LastIndex(rest, "@"); at >= 0 {
		// SIP002: ss://base64(method:password)@host:port (userinfo may also be
		// plaintext method:password, percent-encoded).
		cred, hostport = rest[:at], rest[at+1:]
	} else {
		// Legacy: the whole body is base64(method:password@host:port).
		decoded, ok := decodeBase64(rest)
		if !ok {
			return Parsed{}, invalidErr(opParse, "ss link is malformed", "invalid credentials", detailMissingHost)
		}
		body := string(decoded)
		at := strings.LastIndex(body, "@")
		if at < 0 {
			return Parsed{}, invalidErr(opParse, "ss link is malformed", detailMissingHost)
		}
		cred, hostport = body[:at], body[at+1:]
	}
	method, password, err := decodeSSCredentials(cred)
	if err != nil {
		return Parsed{}, err
	}
	u, err := url.Parse("ss://" + hostport)
	if err != nil {
		return Parsed{}, invalidErr(opParse, "ss link has an invalid server", detailMalformedURL)
	}
	host, err := requireHost(u.Hostname(), "ss")
	if err != nil {
		return Parsed{}, err
	}
	port, err := requirePort(u.Port(), "ss")
	if err != nil {
		return Parsed{}, err
	}
	out := map[string]any{
		"type":        KindShadowsocks,
		"server":      host,
		"server_port": port,
		"method":      method,
		"password":    password,
		"network":     "tcp",
	}
	params, _ := url.ParseQuery(query)
	if plugin := strings.TrimSpace(params.Get("plugin")); plugin != "" {
		name, opts := splitPlugin(plugin)
		out["plugin"] = name
		if opts != "" {
			out["plugin_opts"] = opts
		}
	}
	return Parsed{
		Kind:     KindShadowsocks,
		Tag:      tag,
		Outbound: out,
		Warnings: unknownParams("ss", params, ssKnown),
	}, nil
}

// decodeSSCredentials accepts either plaintext method:password (percent-encoded
// as per SIP002) or base64(method:password).
func decodeSSCredentials(cred string) (string, string, error) {
	if plain, err := url.QueryUnescape(cred); err == nil && strings.Contains(plain, ":") {
		i := strings.Index(plain, ":")
		method := strings.TrimSpace(plain[:i])
		password := plain[i+1:]
		return validatedCredentials(method, password)
	}
	decoded, ok := decodeBase64(cred)
	if !ok {
		return "", "", invalidErr(opParse, "ss link has invalid credentials", "invalid credentials")
	}
	plain := string(decoded)
	i := strings.Index(plain, ":")
	if i < 0 {
		return "", "", invalidErr(opParse, "ss link has invalid credentials", "invalid credentials")
	}
	return validatedCredentials(strings.TrimSpace(plain[:i]), plain[i+1:])
}

func validatedCredentials(method, password string) (string, string, error) {
	if method == "" {
		return "", "", invalidErr(opParse, "ss link has no method", detailMissingMethod)
	}
	if password == "" {
		return "", "", invalidErr(opParse, "ss link has no password", detailMissingPassword)
	}
	return method, password, nil
}

// splitPlugin splits a SIP002 plugin string ("v2ray-plugin;mode=websocket")
// into the plugin name and its options.
func splitPlugin(plugin string) (string, string) {
	if i := strings.Index(plugin, ";"); i >= 0 {
		return strings.TrimSpace(plugin[:i]), strings.TrimSpace(plugin[i+1:])
	}
	return plugin, ""
}

// ---------------------------------------------------------------------------
// hysteria2
// ---------------------------------------------------------------------------

func parseHysteria2(raw string) (Parsed, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return Parsed{}, invalidErr(opParse, "hysteria2 link is not a valid URL", detailMalformedURL)
	}
	q := u.Query()
	host, err := requireHost(u.Hostname(), "hysteria2")
	if err != nil {
		return Parsed{}, err
	}
	port, err := requirePort(u.Port(), "hysteria2")
	if err != nil {
		return Parsed{}, err
	}
	password := userSecret(u.User)
	if password == "" {
		return Parsed{}, invalidErr(opParse, "hysteria2 link has no password", detailMissingPassword)
	}
	tls := map[string]any{"enabled": true}
	if sni := strings.TrimSpace(q.Get("sni")); sni != "" {
		tls["server_name"] = sni
	}
	if alpn := splitList(q.Get("alpn")); len(alpn) > 0 {
		tls["alpn"] = alpn
	}
	if isTrue(q.Get("insecure")) || isTrue(q.Get("allow_insecure")) {
		tls["insecure"] = true
	}
	out := map[string]any{
		"type":        KindHysteria2,
		"server":      host,
		"server_port": port,
		"password":    password,
		"tls":         tls,
	}
	if obfs := strings.TrimSpace(q.Get("obfs")); obfs != "" {
		block := map[string]any{"type": obfs}
		if pw := q.Get("obfs-password"); pw != "" {
			block["password"] = pw
		}
		out["obfs"] = block
	}
	return Parsed{
		Kind:     KindHysteria2,
		Tag:      u.Fragment,
		Outbound: out,
		Warnings: unknownParams("hysteria2", q, hysteria2Known),
	}, nil
}

// ---------------------------------------------------------------------------
// tuic
// ---------------------------------------------------------------------------

func parseTuic(raw string) (Parsed, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return Parsed{}, invalidErr(opParse, "tuic link is not a valid URL", detailMalformedURL)
	}
	q := u.Query()
	host, err := requireHost(u.Hostname(), "tuic")
	if err != nil {
		return Parsed{}, err
	}
	port, err := requirePort(u.Port(), "tuic")
	if err != nil {
		return Parsed{}, err
	}
	uuid := ""
	password := ""
	if u.User != nil {
		uuid = strings.TrimSpace(u.User.Username())
		password, _ = u.User.Password()
	}
	if uuid == "" {
		return Parsed{}, invalidErr(opParse, "tuic link has no UUID", detailMissingUUID)
	}
	if password == "" {
		return Parsed{}, invalidErr(opParse, "tuic link has no password", detailMissingPassword)
	}
	tls := map[string]any{"enabled": true}
	if sni := strings.TrimSpace(q.Get("sni")); sni != "" {
		tls["server_name"] = sni
	}
	if alpn := splitList(q.Get("alpn")); len(alpn) > 0 {
		tls["alpn"] = alpn
	}
	if isTrue(q.Get("insecure")) || isTrue(q.Get("allow_insecure")) {
		tls["insecure"] = true
	}
	out := map[string]any{
		"type":        KindTuic,
		"server":      host,
		"server_port": port,
		"uuid":        uuid,
		"password":    password,
		"tls":         tls,
	}
	if cc := strings.TrimSpace(q.Get("congestion_control")); cc != "" {
		out["congestion_control"] = cc
	}
	if mode := strings.TrimSpace(q.Get("udp_relay_mode")); mode != "" {
		out["udp_relay_mode"] = mode
	}
	return Parsed{
		Kind:     KindTuic,
		Tag:      u.Fragment,
		Outbound: out,
		Warnings: unknownParams("tuic", q, tuicKnown),
	}, nil
}

// ---------------------------------------------------------------------------
// vmess
// ---------------------------------------------------------------------------

func parseVmess(raw string) (Parsed, error) {
	rest := raw[strings.Index(raw, "://")+3:]
	tag := ""
	if i := strings.Index(rest, "#"); i >= 0 {
		tag = decodeFragment(rest[i+1:])
		rest = rest[:i]
	}
	if i := strings.Index(rest, "?"); i >= 0 {
		// Some generators append a plain ?remarks= query after the payload.
		rest = rest[:i]
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return Parsed{}, invalidErr(opParse, "vmess link has no payload", "missing payload")
	}
	payload, ok := decodeBase64(rest)
	if !ok {
		return Parsed{}, invalidErr(opParse, "vmess payload is not valid base64", "invalid base64")
	}
	var v map[string]any
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return Parsed{}, invalidErr(opParse, "vmess payload is not valid JSON", "invalid JSON")
	}
	if strings.TrimSpace(tag) == "" {
		// vmess has no URL fragment in practice; the remarks live in "ps".
		tag = strings.TrimSpace(asString(v["ps"]))
	}

	server := strings.TrimSpace(asString(v["add"]))
	if server == "" {
		return Parsed{}, invalidErr(opParse, "vmess link has no host", detailMissingHost)
	}
	if v["port"] == nil {
		return Parsed{}, invalidErr(opParse, "vmess link has no port", detailMissingPort)
	}
	port, ok := toInt(v["port"])
	if !ok || port < 1 || port > 65535 {
		return Parsed{}, invalidErr(opParse, "vmess link has an invalid port", detailInvalidPort)
	}
	uuid := strings.TrimSpace(asString(v["id"]))
	if uuid == "" {
		return Parsed{}, invalidErr(opParse, "vmess link has no UUID", detailMissingUUID)
	}

	network := valueOr(strings.TrimSpace(asString(v["net"])), "tcp")
	out := map[string]any{
		"type":        KindVmess,
		"server":      server,
		"server_port": port,
		"uuid":        uuid,
		"alter_id":    intDefault(v["aid"], 0),
		"security":    valueOr(strings.TrimSpace(asString(v["scy"])), valueOr(strings.TrimSpace(asString(v["security"])), "auto")),
		"network":     network,
	}

	tlsMode := strings.ToLower(strings.TrimSpace(asString(v["tls"])))
	if tlsMode == "tls" || tlsMode == "reality" {
		tls := map[string]any{"enabled": true}
		sni := strings.TrimSpace(asString(v["sni"]))
		if sni == "" {
			sni = server
		}
		tls["server_name"] = sni
		if fp := strings.TrimSpace(asString(v["fp"])); fp != "" {
			tls["utls"] = map[string]any{"enabled": true, "fingerprint": fp}
		}
		if alpn := splitList(asString(v["alpn"])); len(alpn) > 0 {
			tls["alpn"] = alpn
		}
		if tlsMode == "reality" {
			tls["reality"] = map[string]any{
				"enabled":    true,
				"public_key": asString(v["pbk"]),
				"short_id":   asString(v["sid"]),
			}
		}
		if isTrue(asString(v["allowInsecure"])) || isTrue(asString(v["allow_insecure"])) || asBool(v["insecure"]) {
			tls["insecure"] = true
		}
		out["tls"] = tls
	}
	if tr := vmessTransport(v, network); tr != nil {
		out["transport"] = tr
	}
	return Parsed{
		Kind:     KindVmess,
		Tag:      tag,
		Outbound: out,
		Warnings: unknownFields("vmess", v, vmessKnown),
	}, nil
}

func vmessTransport(v map[string]any, network string) map[string]any {
	path := asString(v["path"])
	host := asString(v["host"])
	headerType := strings.ToLower(strings.TrimSpace(asString(v["type"])))
	switch strings.ToLower(network) {
	case "ws":
		tr := map[string]any{"type": "ws", "path": valueOr(path, "/")}
		if host != "" {
			tr["host"] = host
		}
		return tr
	case "httpupgrade":
		tr := map[string]any{"type": "httpupgrade", "path": valueOr(path, "/")}
		if host != "" {
			tr["host"] = host
		}
		return tr
	case "grpc":
		return map[string]any{"type": "grpc", "service_name": path}
	case "http":
		tr := map[string]any{"type": "http", "path": valueOr(path, "/")}
		if host != "" {
			tr["host"] = host
		}
		return tr
	case "tcp":
		if headerType == "http" {
			tr := map[string]any{"type": "http", "path": valueOr(path, "/")}
			if host != "" {
				tr["host"] = host
			}
			return tr
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// shared TLS / transport mapping (URI-based protocols)
// ---------------------------------------------------------------------------

// applyTLS maps the security/sni/fp/alpn/pbk/sid/insecure query parameters onto
// a sing-box tls object. defaultEnabled reflects whether the protocol is
// TLS by default (trojan, hysteria2, tuic) rather than opt-in (vless).
func applyTLS(q url.Values, defaultEnabled bool) map[string]any {
	sec := strings.ToLower(strings.TrimSpace(q.Get("security")))
	sni := strings.TrimSpace(q.Get("sni"))
	if sni == "" {
		sni = strings.TrimSpace(q.Get("host"))
	}
	if sec == "none" {
		return nil
	}
	reality := sec == "reality"
	hint := sec == "tls" || reality || sni != "" ||
		q.Get("fp") != "" || q.Get("alpn") != "" || q.Get("pbk") != ""
	if !defaultEnabled && !hint {
		return nil
	}
	tls := map[string]any{"enabled": true}
	if sni != "" {
		tls["server_name"] = sni
	}
	if fp := strings.TrimSpace(q.Get("fp")); fp != "" {
		tls["utls"] = map[string]any{"enabled": true, "fingerprint": fp}
	}
	if alpn := splitList(q.Get("alpn")); len(alpn) > 0 {
		tls["alpn"] = alpn
	}
	if reality {
		tls["reality"] = map[string]any{
			"enabled":    true,
			"public_key": q.Get("pbk"),
			"short_id":   q.Get("sid"),
		}
	}
	if isTrue(q.Get("insecure")) || isTrue(q.Get("allowInsecure")) || isTrue(q.Get("allow_insecure")) {
		tls["insecure"] = true
	}
	return tls
}

// applyTransport maps the `type` (network) and its options onto a sing-box
// transport object.
func applyTransport(q url.Values) map[string]any {
	switch networkOf(q) {
	case "ws":
		tr := map[string]any{"type": "ws", "path": valueOr(q.Get("path"), "/")}
		if host := q.Get("host"); host != "" {
			tr["host"] = host
		}
		return tr
	case "httpupgrade":
		tr := map[string]any{"type": "httpupgrade", "path": valueOr(q.Get("path"), "/")}
		if host := q.Get("host"); host != "" {
			tr["host"] = host
		}
		return tr
	case "grpc":
		return map[string]any{"type": "grpc", "service_name": valueOr(q.Get("serviceName"), q.Get("path"))}
	case "http":
		tr := map[string]any{"type": "http", "path": valueOr(q.Get("path"), "/")}
		if host := q.Get("host"); host != "" {
			tr["host"] = host
		}
		return tr
	case "tcp":
		if strings.EqualFold(q.Get("headerType"), "http") {
			tr := map[string]any{"type": "http", "path": valueOr(q.Get("path"), "/")}
			if host := q.Get("host"); host != "" {
				tr["host"] = host
			}
			return tr
		}
	}
	return nil
}

func networkOf(q url.Values) string {
	n := strings.ToLower(strings.TrimSpace(q.Get("type")))
	if n == "" {
		n = strings.ToLower(strings.TrimSpace(q.Get("network")))
	}
	if n == "" {
		n = "tcp"
	}
	return n
}

// ---------------------------------------------------------------------------
// validation + warnings
// ---------------------------------------------------------------------------

func requireHost(host, kind string) (string, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", invalidErr(opParse, kind+" link has no host", detailMissingHost)
	}
	return host, nil
}

func requirePort(raw, kind string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, invalidErr(opParse, kind+" link has no port", detailMissingPort)
	}
	if n, ok := toInt(raw); ok && n >= 1 && n <= 65535 {
		return n, nil
	}
	return 0, invalidErr(opParse, kind+" link has an invalid port", detailInvalidPort)
}

// userSecret extracts the credential from URL userinfo. A password component is
// appended when present (hysteria2 allows "user:pass"); the value is never
// split further so no part of it is lost.
func userSecret(u *url.Userinfo) string {
	if u == nil {
		return ""
	}
	secret := u.Username()
	if pw, ok := u.Password(); ok {
		secret = secret + ":" + pw
	}
	return strings.TrimSpace(secret)
}

func unknownParams(kind string, q url.Values, known map[string]bool) []string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []string
	for _, k := range keys {
		if known[strings.ToLower(k)] {
			continue
		}
		out = append(out, fmt.Sprintf("%s parameter %q is not mapped to a sing-box field", kind, k))
	}
	return out
}

func unknownFields(kind string, m map[string]any, known map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []string
	for _, k := range keys {
		if known[strings.ToLower(k)] {
			continue
		}
		out = append(out, fmt.Sprintf("%s field %q is not mapped to a sing-box field", kind, k))
	}
	return out
}

// Known-parameter sets keep the warning channel meaningful: only genuinely
// unmapped keys are reported.
var vlessKnown = knownSet(
	"type", "network", "security", "encryption", "flow", "sni", "fp", "alpn",
	"pbk", "sid", "spx", "path", "host", "servicename", "mode", "packetencoding",
	"headertype", "insecure", "allowinsecure", "allow_insecure", "ech", "udp",
	"vcn", "pqv", "mport", "ports", "up", "down", "mtu", "remark",
)

var trojanKnown = knownSet(
	"type", "network", "security", "sni", "fp", "alpn", "pbk", "sid", "spx",
	"path", "host", "servicename", "mode", "headertype", "insecure",
	"allowinsecure", "allow_insecure", "udp", "mport", "ports", "remark",
)

var ssKnown = knownSet(
	"plugin", "plugin_opts", "plugin-opts", "outline",
)

var hysteria2Known = knownSet(
	"sni", "insecure", "allow_insecure", "alpn", "obfs", "obfs-password",
	"obfs_password", "pinSHA256", "pinsha256", "mport", "ports", "up", "down",
	"mtu", "fastopen", "remark",
)

var tuicKnown = knownSet(
	"sni", "insecure", "allow_insecure", "alpn", "congestion_control",
	"udp_relay_mode", "udp", "udp_over_stream", "zero_rtt_handshake",
	"heartbeat", "disable_sni", "remark",
)

var vmessKnown = knownSet(
	"v", "ps", "add", "port", "id", "aid", "scy", "security", "net", "type",
	"host", "path", "tls", "sni", "alpn", "fp", "pbk", "sid", "allowinsecure",
	"allow_insecure", "insecure", "remarks", "remark", "headerType",
)

func knownSet(keys ...string) map[string]bool {
	set := make(map[string]bool, len(keys))
	for _, k := range keys {
		set[strings.ToLower(k)] = true
	}
	return set
}
