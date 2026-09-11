package com.larffxx.singboxui.service;

import tools.jackson.databind.ObjectMapper;
import tools.jackson.databind.node.ArrayNode;
import tools.jackson.databind.node.ObjectNode;
import org.springframework.stereotype.Service;

import java.net.URI;
import java.net.URLDecoder;
import java.net.URLEncoder;
import java.nio.charset.StandardCharsets;
import java.util.Base64;
import java.util.LinkedHashMap;
import java.util.Map;

/**
 * Parses common proxy share-links into sing-box outbound JSON and back.
 * Supported: vless, vmess, trojan, ss/shadowsocks, hysteria2/hy2, tuic.
 */
@Service
public class ShareLinkService {

    private final ObjectMapper mapper;

    public ShareLinkService(ObjectMapper mapper) {
        this.mapper = mapper;
    }

    public ObjectNode parse(String link) {
        String s = link.trim();
        int schemeEnd = s.indexOf("://");
        if (schemeEnd < 0) {
            throw new IllegalArgumentException("Not a share link (missing ://)");
        }
        String scheme = s.substring(0, schemeEnd).toLowerCase();
        return switch (scheme) {
            case "vless" -> parseVless(s);
            case "vmess" -> parseVmess(s);
            case "trojan" -> parseTrojan(s);
            case "ss", "shadowsocks" -> parseSs(s);
            case "hy2", "hysteria2" -> parseHysteria2(s);
            case "tuic" -> parseTuic(s);
            default -> throw new IllegalArgumentException("Unsupported scheme: " + scheme);
        };
    }

    public String build(ObjectNode outbound) {
        String type = outbound.path("type").asText("");
        return switch (type) {
            case "vless" -> buildVless(outbound);
            case "vmess" -> buildVmess(outbound);
            case "trojan" -> buildTrojan(outbound);
            case "shadowsocks" -> buildSs(outbound);
            case "hysteria2" -> buildHysteria2(outbound);
            case "tuic" -> buildTuic(outbound);
            default -> throw new IllegalArgumentException("Share-link export not supported for type: " + type);
        };
    }

    // ---- vless ----

    private ObjectNode parseVless(String s) {
        URI uri = URI.create(s);
        Map<String, String> q = query(uri.getRawQuery());
        ObjectNode out = mapper.createObjectNode();
        out.put("type", "vless");
        out.put("tag", decodeFragment(uri.getRawFragment(), "vless"));
        out.put("server", uri.getHost());
        out.put("server_port", port(uri, 443));
        out.put("uuid", decode(uri.getUserInfo()));
        out.put("network", q.getOrDefault("type", "tcp"));
        if (q.containsKey("flow") && !q.get("flow").isBlank()) {
            out.put("flow", q.get("flow"));
        }
        out.put("packet_encoding", q.getOrDefault("packetEncoding", "xudp"));
        applyTls(out, q);
        applyTransport(out, q);
        return out;
    }

    private String buildVless(ObjectNode o) {
        Map<String, String> q = new LinkedHashMap<>();
        q.put("encryption", "none");
        q.put("type", o.path("network").asText("tcp"));
        if (o.hasNonNull("flow")) {
            q.put("flow", o.get("flow").asText());
        }
        exportTls(o, q);
        exportTransport(o, q);
        return "vless://" + o.path("uuid").asText() + "@" + o.path("server").asText()
                + ":" + o.path("server_port").asInt(443) + "?" + encodeQuery(q)
                + "#" + enc(o.path("tag").asText("vless"));
    }

    // ---- vmess ----

    private ObjectNode parseVmess(String s) {
        try {
            String payload = s.substring("vmess://".length());
            int hash = payload.indexOf('#');
            String tag = hash >= 0 ? decode(payload.substring(hash + 1)) : "vmess";
            payload = hash >= 0 ? payload.substring(0, hash) : payload;
            int qm = payload.indexOf('?');
            if (qm >= 0) {
                payload = payload.substring(0, qm);
            }
            ObjectNode v = (ObjectNode) mapper.readTree(Base64.getDecoder().decode(pad(payload)));
            ObjectNode out = mapper.createObjectNode();
            out.put("type", "vmess");
            out.put("tag", tag.isBlank() ? v.path("ps").asText("vmess") : tag);
            out.put("server", v.path("add").asText());
            out.put("server_port", v.path("port").asInt(443));
            out.put("uuid", v.path("id").asText());
            out.put("alter_id", v.path("aid").asInt(0));
            out.put("security", v.path("scy").asText("auto"));
            out.put("network", v.path("net").asText("tcp"));
            String tls = v.path("tls").asText("");
            if (tls.equals("tls") || tls.equals("reality")) {
                ObjectNode t = mapper.createObjectNode();
                t.put("enabled", true);
                t.put("server_name", v.path("sni").asText(v.path("add").asText()));
                if (!v.path("fp").asText("").isBlank()) {
                    t.set("utls", mapper.createObjectNode().put("enabled", true).put("fingerprint", v.path("fp").asText()));
                }
                out.set("tls", t);
            }
            String net = v.path("net").asText("tcp");
            if (net.equals("ws")) {
                ObjectNode tr = mapper.createObjectNode();
                tr.put("type", "ws");
                tr.put("path", v.path("path").asText("/"));
                tr.put("host", v.path("host").asText(""));
                out.set("transport", tr);
            } else if (net.equals("grpc")) {
                ObjectNode tr = mapper.createObjectNode();
                tr.put("type", "grpc");
                tr.put("service_name", v.path("path").asText(""));
                out.set("transport", tr);
            }
            return out;
        } catch (Exception e) {
            throw new IllegalArgumentException("Invalid vmess link: " + e.getMessage());
        }
    }

    private String buildVmess(ObjectNode o) {
        ObjectNode v = mapper.createObjectNode();
        v.put("v", "2");
        v.put("ps", o.path("tag").asText("vmess"));
        v.put("add", o.path("server").asText());
        v.put("port", o.path("server_port").asInt(443));
        v.put("id", o.path("uuid").asText());
        v.put("aid", o.path("alter_id").asInt(0));
        v.put("scy", o.path("security").asText("auto"));
        v.put("net", o.path("network").asText("tcp"));
        v.put("type", "none");
        boolean tls = o.path("tls").path("enabled").asBoolean(false);
        v.put("tls", tls ? "tls" : "");
        v.put("sni", o.path("tls").path("server_name").asText(""));
        v.put("fp", o.path("tls").path("utls").path("fingerprint").asText(""));
        String b64 = Base64.getEncoder().encodeToString(v.toString().getBytes(StandardCharsets.UTF_8));
        return "vmess://" + b64 + "#" + enc(o.path("tag").asText("vmess"));
    }

    // ---- trojan ----

    private ObjectNode parseTrojan(String s) {
        URI uri = URI.create(s);
        Map<String, String> q = query(uri.getRawQuery());
        ObjectNode out = mapper.createObjectNode();
        out.put("type", "trojan");
        out.put("tag", decodeFragment(uri.getRawFragment(), "trojan"));
        out.put("server", uri.getHost());
        out.put("server_port", port(uri, 443));
        out.put("password", decode(uri.getUserInfo()));
        out.put("network", q.getOrDefault("type", "tcp"));
        applyTls(out, q);
        applyTransport(out, q);
        return out;
    }

    private String buildTrojan(ObjectNode o) {
        Map<String, String> q = new LinkedHashMap<>();
        q.put("type", o.path("network").asText("tcp"));
        exportTls(o, q);
        exportTransport(o, q);
        return "trojan://" + o.path("password").asText() + "@" + o.path("server").asText()
                + ":" + o.path("server_port").asInt(443) + "?" + encodeQuery(q)
                + "#" + enc(o.path("tag").asText("trojan"));
    }

    // ---- shadowsocks ----

    private ObjectNode parseSs(String s) {
        try {
            URI uri = URI.create(s);
            String userInfo = uri.getUserInfo();
            String method;
            String password;
            if (userInfo != null && userInfo.contains(":")) {
                method = decode(userInfo.substring(0, userInfo.indexOf(':')));
                password = decode(userInfo.substring(userInfo.indexOf(':') + 1));
            } else {
                String payload = s.substring(s.indexOf("://") + 3);
                int hash = payload.indexOf('#');
                if (hash >= 0) {
                    payload = payload.substring(0, hash);
                }
                int at = payload.lastIndexOf('@');
                String decoded = new String(Base64.getDecoder().decode(pad(at >= 0 ? payload.substring(0, at) : payload)), StandardCharsets.UTF_8);
                int colon = decoded.indexOf(':');
                method = decoded.substring(0, colon);
                password = decoded.substring(colon + 1);
                URI rest = URI.create("ss://" + payload.substring(at + 1));
                ObjectNode out = mapper.createObjectNode();
                out.put("type", "shadowsocks");
                out.put("tag", decodeFragment(s.contains("#") ? s.substring(s.indexOf('#') + 1) : "ss", "ss"));
                out.put("server", rest.getHost());
                out.put("server_port", port(rest, 8388));
                out.put("method", method);
                out.put("password", password);
                out.put("network", "tcp");
                return out;
            }
            ObjectNode out = mapper.createObjectNode();
            out.put("type", "shadowsocks");
            out.put("tag", decodeFragment(uri.getRawFragment(), "ss"));
            out.put("server", uri.getHost());
            out.put("server_port", port(uri, 8388));
            out.put("method", method);
            out.put("password", password);
            out.put("network", "tcp");
            return out;
        } catch (Exception e) {
            throw new IllegalArgumentException("Invalid ss link: " + e.getMessage());
        }
    }

    private String buildSs(ObjectNode o) {
        String user = Base64.getEncoder().encodeToString(
                (o.path("method").asText() + ":" + o.path("password").asText()).getBytes(StandardCharsets.UTF_8));
        return "ss://" + user + "@" + o.path("server").asText() + ":" + o.path("server_port").asInt(8388)
                + "#" + enc(o.path("tag").asText("ss"));
    }

    // ---- hysteria2 ----

    private ObjectNode parseHysteria2(String s) {
        URI uri = URI.create(s);
        Map<String, String> q = query(uri.getRawQuery());
        ObjectNode out = mapper.createObjectNode();
        out.put("type", "hysteria2");
        out.put("tag", decodeFragment(uri.getRawFragment(), "hy2"));
        out.put("server", uri.getHost());
        out.put("server_port", port(uri, 443));
        out.put("password", decode(uri.getUserInfo()));
        ObjectNode tls = mapper.createObjectNode();
        tls.put("enabled", true);
        if (q.containsKey("sni")) {
            tls.put("server_name", q.get("sni"));
        }
        if (q.getOrDefault("insecure", "0").equals("1")) {
            tls.put("insecure", true);
        }
        out.set("tls", tls);
        if (q.containsKey("obfs") && !q.get("obfs").isBlank()) {
            ObjectNode obfs = mapper.createObjectNode();
            obfs.put("type", q.get("obfs"));
            obfs.put("password", q.getOrDefault("obfs-password", ""));
            out.set("obfs", obfs);
        }
        return out;
    }

    private String buildHysteria2(ObjectNode o) {
        Map<String, String> q = new LinkedHashMap<>();
        if (o.path("tls").hasNonNull("server_name")) {
            q.put("sni", o.path("tls").path("server_name").asText());
        }
        if (o.path("tls").path("insecure").asBoolean(false)) {
            q.put("insecure", "1");
        }
        return "hysteria2://" + o.path("password").asText() + "@" + o.path("server").asText()
                + ":" + o.path("server_port").asInt(443) + (q.isEmpty() ? "" : "?" + encodeQuery(q))
                + "#" + enc(o.path("tag").asText("hy2"));
    }

    // ---- tuic ----

    private ObjectNode parseTuic(String s) {
        URI uri = URI.create(s);
        Map<String, String> q = query(uri.getRawQuery());
        ObjectNode out = mapper.createObjectNode();
        out.put("type", "tuic");
        out.put("tag", decodeFragment(uri.getRawFragment(), "tuic"));
        out.put("server", uri.getHost());
        out.put("server_port", port(uri, 443));
        String ui = decode(uri.getUserInfo());
        int colon = ui.indexOf(':');
        out.put("uuid", colon >= 0 ? ui.substring(0, colon) : ui);
        out.put("password", colon >= 0 ? ui.substring(colon + 1) : "");
        ObjectNode tls = mapper.createObjectNode();
        tls.put("enabled", true);
        if (q.containsKey("sni")) {
            tls.put("server_name", q.get("sni"));
        }
        if (q.getOrDefault("insecure", "0").equals("1") || q.getOrDefault("allow_insecure", "0").equals("1")) {
            tls.put("insecure", true);
        }
        out.set("tls", tls);
        return out;
    }

    private String buildTuic(ObjectNode o) {
        Map<String, String> q = new LinkedHashMap<>();
        if (o.path("tls").hasNonNull("server_name")) {
            q.put("sni", o.path("tls").path("server_name").asText());
        }
        return "tuic://" + o.path("uuid").asText() + ":" + o.path("password").asText()
                + "@" + o.path("server").asText() + ":" + o.path("server_port").asInt(443)
                + (q.isEmpty() ? "" : "?" + encodeQuery(q)) + "#" + enc(o.path("tag").asText("tuic"));
    }

    // ---- shared tls/transport ----

    private void applyTls(ObjectNode out, Map<String, String> q) {
        String sec = q.getOrDefault("security", "");
        if (sec.equals("tls") || sec.equals("reality") || q.containsKey("sni")) {
            ObjectNode tls = mapper.createObjectNode();
            tls.put("enabled", true);
            tls.put("server_name", q.getOrDefault("sni", q.getOrDefault("host", "")));
            if (q.containsKey("fp") && !q.get("fp").isBlank()) {
                tls.set("utls", mapper.createObjectNode().put("enabled", true).put("fingerprint", q.get("fp")));
            }
            if (q.containsKey("alpn") && !q.get("alpn").isBlank()) {
                ArrayNode alpn = mapper.createArrayNode();
                for (String a : q.get("alpn").split(",")) {
                    alpn.add(a.trim());
                }
                tls.set("alpn", alpn);
            }
            if (sec.equals("reality")) {
                ObjectNode reality = mapper.createObjectNode();
                reality.put("enabled", true);
                reality.put("public_key", q.getOrDefault("pbk", ""));
                reality.put("short_id", q.getOrDefault("sid", ""));
                tls.set("reality", reality);
            }
            if (q.getOrDefault("insecure", "0").equals("1") || q.getOrDefault("allowInsecure", "0").equals("1")) {
                tls.put("insecure", true);
            }
            out.set("tls", tls);
        }
    }

    private void exportTls(ObjectNode o, Map<String, String> q) {
        boolean tls = o.path("tls").path("enabled").asBoolean(false);
        boolean reality = o.path("tls").path("reality").path("enabled").asBoolean(false);
        q.put("security", reality ? "reality" : (tls ? "tls" : "none"));
        if (o.path("tls").hasNonNull("server_name")) {
            q.put("sni", o.path("tls").path("server_name").asText());
        }
        if (o.path("tls").path("utls").hasNonNull("fingerprint")) {
            q.put("fp", o.path("tls").path("utls").path("fingerprint").asText());
        }
        if (reality) {
            q.put("pbk", o.path("tls").path("reality").path("public_key").asText(""));
            q.put("sid", o.path("tls").path("reality").path("short_id").asText(""));
        }
    }

    private void applyTransport(ObjectNode out, Map<String, String> q) {
        String net = out.path("network").asText("tcp");
        if (net.equals("ws")) {
            ObjectNode tr = mapper.createObjectNode();
            tr.put("type", "ws");
            tr.put("path", q.getOrDefault("path", "/"));
            tr.put("host", q.getOrDefault("host", ""));
            out.set("transport", tr);
        } else if (net.equals("grpc")) {
            ObjectNode tr = mapper.createObjectNode();
            tr.put("type", "grpc");
            tr.put("service_name", q.getOrDefault("serviceName", q.getOrDefault("path", "")));
            out.set("transport", tr);
        } else if (net.equals("httpupgrade")) {
            ObjectNode tr = mapper.createObjectNode();
            tr.put("type", "httpupgrade");
            tr.put("path", q.getOrDefault("path", "/"));
            tr.put("host", q.getOrDefault("host", ""));
            out.set("transport", tr);
        }
    }

    private void exportTransport(ObjectNode o, Map<String, String> q) {
        String t = o.path("transport").path("type").asText("");
        if (t.equals("ws") || t.equals("httpupgrade")) {
            q.put("path", o.path("transport").path("path").asText("/"));
            if (o.path("transport").hasNonNull("host")) {
                q.put("host", o.path("transport").path("host").asText());
            }
        } else if (t.equals("grpc")) {
            q.put("serviceName", o.path("transport").path("service_name").asText(""));
        }
    }

    // ---- helpers ----

    private static Map<String, String> query(String raw) {
        Map<String, String> m = new LinkedHashMap<>();
        if (raw == null || raw.isBlank()) {
            return m;
        }
        for (String kv : raw.split("&")) {
            int eq = kv.indexOf('=');
            if (eq < 0) {
                m.put(decode(kv), "");
            } else {
                m.put(decode(kv.substring(0, eq)), decode(kv.substring(eq + 1)));
            }
        }
        return m;
    }

    private static String encodeQuery(Map<String, String> q) {
        StringBuilder sb = new StringBuilder();
        for (var e : q.entrySet()) {
            if (!sb.isEmpty()) {
                sb.append('&');
            }
            sb.append(enc(e.getKey())).append('=').append(enc(e.getValue()));
        }
        return sb.toString();
    }

    private static int port(URI uri, int def) {
        return uri.getPort() < 0 ? def : uri.getPort();
    }

    private static String decode(String s) {
        if (s == null) {
            return "";
        }
        try {
            return URLDecoder.decode(s, StandardCharsets.UTF_8);
        } catch (Exception e) {
            return s;
        }
    }

    private static String decodeFragment(String frag, String def) {
        if (frag == null || frag.isBlank()) {
            return def;
        }
        return decode(frag);
    }

    private static String enc(String s) {
        return URLEncoder.encode(s == null ? "" : s, StandardCharsets.UTF_8).replace("+", "%20");
    }

    private static String pad(String b64) {
        String p = b64.replace('-', '+').replace('_', '/').replaceAll("\\s", "");
        while (p.length() % 4 != 0) {
            p += "=";
        }
        return p;
    }
}
