package share

import (
	"encoding/json"
	"strings"
)

// Build renders a sing-box outbound back into a share link. It dispatches on
// the outbound's `type` and returns a SHARE_LINK_INVALID typed error for an
// unsupported type or a missing endpoint/credential.
//
// Fields the outbound carries that have no share-link representation are
// ignored rather than rejected: Build is a lossy export, never a validator.
func Build(outbound map[string]any) (string, error) {
	if outbound == nil {
		return "", invalidErr(opBuild, "there is no outbound to export")
	}
	typ := strings.ToLower(strings.TrimSpace(asString(outbound["type"])))
	if typ == "" {
		return "", invalidErr(opBuild, "the outbound has no type", "missing type")
	}
	switch typ {
	case KindVless:
		return buildVless(outbound)
	case KindVmess:
		return buildVmess(outbound)
	case KindTrojan:
		return buildTrojan(outbound)
	case KindShadowsocks, "ss":
		return buildShadowsocks(outbound)
	case KindHysteria2, "hy2":
		return buildHysteria2(outbound)
	case KindTuic:
		return buildTuic(outbound)
	}
	return "", invalidErr(opBuild, "share links are not supported for this outbound type", "unsupported type: "+typ)
}

// ---------------------------------------------------------------------------
// vless / trojan (URI-based)
// ---------------------------------------------------------------------------

func buildVless(o map[string]any) (string, error) {
	server, port, err := endpoint(o, "vless")
	if err != nil {
		return "", err
	}
	uuid := strings.TrimSpace(asString(o["uuid"]))
	if uuid == "" {
		return "", invalidErr(opBuild, "vless outbound has no UUID", detailMissingUUID)
	}
	q := newQuery()
	q.set("encryption", "none")
	q.set("type", networkOfOutbound(o))
	if flow := strings.TrimSpace(asString(o["flow"])); flow != "" {
		q.set("flow", flow)
	}
	if pe := strings.TrimSpace(asString(o["packet_encoding"])); pe != "" {
		q.set("packetEncoding", pe)
	}
	exportTLS(q, o)
	exportTransportOptions(q, o)
	return "vless://" + queryEscape(uuid) + "@" + hostPort(server, port) +
		"?" + q.Encode() + "#" + queryEscape(tagOf(o, KindVless)), nil
}

func buildTrojan(o map[string]any) (string, error) {
	server, port, err := endpoint(o, "trojan")
	if err != nil {
		return "", err
	}
	password := asString(o["password"])
	if strings.TrimSpace(password) == "" {
		return "", invalidErr(opBuild, "trojan outbound has no password", detailMissingPassword)
	}
	q := newQuery()
	q.set("type", networkOfOutbound(o))
	exportTLS(q, o)
	exportTransportOptions(q, o)
	return "trojan://" + queryEscape(password) + "@" + hostPort(server, port) +
		"?" + q.Encode() + "#" + queryEscape(tagOf(o, KindTrojan)), nil
}

// ---------------------------------------------------------------------------
// shadowsocks
// ---------------------------------------------------------------------------

func buildShadowsocks(o map[string]any) (string, error) {
	server, port, err := endpoint(o, KindShadowsocks)
	if err != nil {
		return "", err
	}
	method := strings.TrimSpace(asString(o["method"]))
	if method == "" {
		return "", invalidErr(opBuild, "shadowsocks outbound has no method", detailMissingMethod)
	}
	password := asString(o["password"])
	if password == "" {
		return "", invalidErr(opBuild, "shadowsocks outbound has no password", detailMissingPassword)
	}
	link := "ss://" + encodeBase64(method+":"+password) + "@" + hostPort(server, port)
	q := newQuery()
	if plugin := strings.TrimSpace(asString(o["plugin"])); plugin != "" {
		if opts := strings.TrimSpace(asString(o["plugin_opts"])); opts != "" {
			plugin = plugin + ";" + opts
		}
		q.set("plugin", plugin)
	}
	if enc := q.Encode(); enc != "" {
		link += "?" + enc
	}
	return link + "#" + queryEscape(tagOf(o, KindShadowsocks)), nil
}

// ---------------------------------------------------------------------------
// hysteria2 / tuic
// ---------------------------------------------------------------------------

func buildHysteria2(o map[string]any) (string, error) {
	server, port, err := endpoint(o, KindHysteria2)
	if err != nil {
		return "", err
	}
	password := asString(o["password"])
	if strings.TrimSpace(password) == "" {
		return "", invalidErr(opBuild, "hysteria2 outbound has no password", detailMissingPassword)
	}
	q := newQuery()
	exportSimpleTLS(q, o)
	if obfs := asMap(o["obfs"]); len(obfs) > 0 {
		if typ := strings.TrimSpace(asString(obfs["type"])); typ != "" {
			q.set("obfs", typ)
			if pw := asString(obfs["password"]); pw != "" {
				q.set("obfs-password", pw)
			}
		}
	}
	link := "hysteria2://" + queryEscape(password) + "@" + hostPort(server, port)
	if enc := q.Encode(); enc != "" {
		link += "?" + enc
	}
	return link + "#" + queryEscape(tagOf(o, KindHysteria2)), nil
}

func buildTuic(o map[string]any) (string, error) {
	server, port, err := endpoint(o, KindTuic)
	if err != nil {
		return "", err
	}
	uuid := strings.TrimSpace(asString(o["uuid"]))
	if uuid == "" {
		return "", invalidErr(opBuild, "tuic outbound has no UUID", detailMissingUUID)
	}
	password := asString(o["password"])
	if password == "" {
		return "", invalidErr(opBuild, "tuic outbound has no password", detailMissingPassword)
	}
	q := newQuery()
	exportSimpleTLS(q, o)
	if mode := strings.TrimSpace(asString(o["udp_relay_mode"])); mode != "" {
		q.set("udp_relay_mode", mode)
	}
	if cc := strings.TrimSpace(asString(o["congestion_control"])); cc != "" {
		q.set("congestion_control", cc)
	}
	link := "tuic://" + queryEscape(uuid) + ":" + queryEscape(password) + "@" + hostPort(server, port)
	if enc := q.Encode(); enc != "" {
		link += "?" + enc
	}
	return link + "#" + queryEscape(tagOf(o, KindTuic)), nil
}

// ---------------------------------------------------------------------------
// vmess
// ---------------------------------------------------------------------------

func buildVmess(o map[string]any) (string, error) {
	server, port, err := endpoint(o, KindVmess)
	if err != nil {
		return "", err
	}
	uuid := strings.TrimSpace(asString(o["uuid"]))
	if uuid == "" {
		return "", invalidErr(opBuild, "vmess outbound has no UUID", detailMissingUUID)
	}

	tr := asMap(o["transport"])
	transportType := strings.ToLower(asString(tr["type"]))
	network := strings.ToLower(valueOr(asString(o["network"]), "tcp"))
	switch transportType {
	case "ws", "grpc", "httpupgrade":
		network = transportType
	}
	headerType := "none"
	if transportType == "http" || network == "http" {
		network = "tcp"
		headerType = "http"
	}

	v := map[string]any{
		"v":    "2",
		"ps":   tagOf(o, KindVmess),
		"add":  server,
		"port": port,
		"id":   uuid,
		"aid":  intDefault(o["alter_id"], 0),
		"scy":  valueOr(strings.TrimSpace(asString(o["security"])), "auto"),
		"net":  network,
		"type": headerType,
	}
	tls := asMap(o["tls"])
	reality := asMap(tls["reality"])
	realityEnabled := asBool(reality["enabled"])
	switch {
	case realityEnabled:
		v["tls"] = "reality"
	case asBool(tls["enabled"]):
		v["tls"] = "tls"
	default:
		v["tls"] = ""
	}
	if sni := asString(tls["server_name"]); sni != "" {
		v["sni"] = sni
	}
	if fp := asString(asMap(tls["utls"])["fingerprint"]); fp != "" {
		v["fp"] = fp
	}
	if alpn := asStrings(tls["alpn"]); len(alpn) > 0 {
		v["alpn"] = strings.Join(alpn, ",")
	}
	if realityEnabled {
		v["pbk"] = asString(reality["public_key"])
		v["sid"] = asString(reality["short_id"])
	}
	if asBool(tls["insecure"]) {
		v["allowInsecure"] = true
	}
	switch transportType {
	case "ws", "http", "httpupgrade":
		v["path"] = valueOr(asString(tr["path"]), "/")
		if host := transportHost(tr); host != "" {
			v["host"] = host
		}
	case "grpc":
		v["path"] = asString(tr["service_name"])
	}

	data, err := json.Marshal(v)
	if err != nil {
		return "", invalidErr(opBuild, "vmess outbound could not be encoded", "encoding failed")
	}
	return "vmess://" + encodeBase64(string(data)) + "#" + queryEscape(tagOf(o, KindVmess)), nil
}

// ---------------------------------------------------------------------------
// shared export helpers
// ---------------------------------------------------------------------------

func endpoint(o map[string]any, kind string) (string, int, error) {
	server := strings.TrimSpace(asString(o["server"]))
	if server == "" {
		return "", 0, invalidErr(opBuild, kind+" outbound has no server", detailMissingHost)
	}
	if o["server_port"] == nil {
		return "", 0, invalidErr(opBuild, kind+" outbound has no server_port", detailMissingPort)
	}
	port, ok := toInt(o["server_port"])
	if !ok || port < 1 || port > 65535 {
		return "", 0, invalidErr(opBuild, kind+" outbound has an invalid server_port", detailInvalidPort)
	}
	return server, port, nil
}

func tagOf(o map[string]any, kind string) string {
	return sanitizeTag(asString(o["tag"]), defaultTag(kind))
}

// networkOfOutbound prefers the transport type over the `network` field,
// because sing-box v1.11+ expresses ws/grpc/httpupgrade/http in `transport`.
func networkOfOutbound(o map[string]any) string {
	switch t := strings.ToLower(asString(asMap(o["transport"])["type"])); t {
	case "ws", "grpc", "httpupgrade", "http":
		return t
	}
	return strings.ToLower(valueOr(asString(o["network"]), "tcp"))
}

// exportTLS writes the vless/trojan security block (off by default).
func exportTLS(q *orderedQuery, o map[string]any) {
	tls := asMap(o["tls"])
	reality := asMap(tls["reality"])
	realityEnabled := asBool(reality["enabled"])
	switch {
	case realityEnabled:
		q.set("security", "reality")
	case asBool(tls["enabled"]):
		q.set("security", "tls")
	default:
		q.set("security", "none")
	}
	exportSimpleTLS(q, o)
}

// exportSimpleTLS writes only the parameters shared by every TLS protocol.
func exportSimpleTLS(q *orderedQuery, o map[string]any) {
	tls := asMap(o["tls"])
	if sni := strings.TrimSpace(asString(tls["server_name"])); sni != "" {
		q.set("sni", sni)
	}
	if asBool(tls["insecure"]) {
		q.set("insecure", "1")
	}
	if alpn := asStrings(tls["alpn"]); len(alpn) > 0 {
		q.set("alpn", strings.Join(alpn, ","))
	}
	if fp := strings.TrimSpace(asString(asMap(tls["utls"])["fingerprint"])); fp != "" {
		q.set("fp", fp)
	}
	reality := asMap(tls["reality"])
	if asBool(reality["enabled"]) {
		if pbk := strings.TrimSpace(asString(reality["public_key"])); pbk != "" {
			q.set("pbk", pbk)
		}
		if sid := strings.TrimSpace(asString(reality["short_id"])); sid != "" {
			q.set("sid", sid)
		}
	}
}

// exportTransportOptions writes the transport-specific parameters; the network
// itself is already in `type`.
func exportTransportOptions(q *orderedQuery, o map[string]any) {
	tr := asMap(o["transport"])
	if len(tr) == 0 {
		return
	}
	switch strings.ToLower(asString(tr["type"])) {
	case "ws", "httpupgrade", "http":
		q.set("path", valueOr(asString(tr["path"]), "/"))
		if host := transportHost(tr); host != "" {
			q.set("host", host)
		}
	case "grpc":
		if service := asString(tr["service_name"]); service != "" {
			q.set("serviceName", service)
		}
	}
}

// transportHost reads the Host a transport carries. A websocket transport keeps
// it in headers["Host"], because sing-box 1.14 removed the flat `host` field and
// refuses a configuration that still has one; the flat field is read as well so
// an outbound written by an older build exports the link it came from.
func transportHost(tr map[string]any) string {
	if host := strings.TrimSpace(asString(tr["host"])); host != "" {
		return host
	}
	for key, value := range asMap(tr["headers"]) {
		if strings.EqualFold(strings.TrimSpace(key), "Host") {
			return strings.TrimSpace(asString(value))
		}
	}
	return ""
}
