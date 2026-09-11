package com.larffxx.singboxui.service;

import tools.jackson.databind.JsonNode;
import tools.jackson.databind.ObjectMapper;
import tools.jackson.databind.node.ArrayNode;
import tools.jackson.databind.node.ObjectNode;
import com.larffxx.singboxui.config.SingBoxProperties;
import org.springframework.stereotype.Service;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.UUID;

/**
 * File-backed sing-box config store. The whole config is a Jackson tree,
 * sections are edited structurally so unknown fields survive round-trips.
 */
@Service
public class SingBoxConfigService {

    private final ObjectMapper mapper;
    private final SingBoxProperties props;
    private ObjectNode config;

    public SingBoxConfigService(ObjectMapper mapper, SingBoxProperties props) {
        this.mapper = mapper;
        this.props = props;
        this.config = loadOrDefault();
    }

    public synchronized ObjectNode getConfig() {
        return config.deepCopy();
    }

    public synchronized ObjectNode replaceConfig(ObjectNode next) {
        List<String> errors = validate(next);
        if (!errors.isEmpty()) {
            throw new IllegalArgumentException(String.join("; ", errors));
        }
        ensureDefaults(next);
        this.config = next.deepCopy();
        save();
        return getConfig();
    }

    public synchronized ArrayNode getSectionArray(String section) {
        JsonNode node = config.get(section);
        if (node instanceof ArrayNode array) {
            return array;
        }
        ArrayNode created = mapper.createArrayNode();
        config.set(section, created);
        return created;
    }

    public synchronized ObjectNode getSectionObject(String section) {
        JsonNode node = config.get(section);
        if (node instanceof ObjectNode obj) {
            return obj.deepCopy();
        }
        return mapper.createObjectNode();
    }

    public synchronized void setSectionObject(String section, ObjectNode value) {
        config.set(section, value);
        save();
    }

    /** Add or replace an entry in an array section by its "tag". */
    public synchronized ObjectNode upsertByTag(String section, ObjectNode entry) {
        ArrayNode array = getSectionArray(section);
        String tag = entry.hasNonNull("tag") ? entry.get("tag").asText() : null;
        if (tag == null || tag.isBlank()) {
            tag = entry.get("type").asText("") + "-" + UUID.randomUUID().toString().substring(0, 6);
            entry.put("tag", tag);
        }
        boolean replaced = false;
        for (int i = 0; i < array.size(); i++) {
            JsonNode el = array.get(i);
            if (el.has("tag") && tag.equals(el.get("tag").asText())) {
                array.set(i, entry);
                replaced = true;
                break;
            }
        }
        if (!replaced) {
            array.add(entry);
        }
        save();
        return entry;
    }

    public synchronized boolean deleteByTag(String section, String tag) {
        ArrayNode array = getSectionArray(section);
        for (int i = 0; i < array.size(); i++) {
            JsonNode el = array.get(i);
            if (el.has("tag") && tag.equals(el.get("tag").asText())) {
                array.remove(i);
                save();
                return true;
            }
        }
        return false;
    }

    public synchronized ObjectNode addRouteRule(ObjectNode rule) {
        ObjectNode route = getRoute();
        ArrayNode rules = route.has("rules") && route.get("rules").isArray()
                ? (ArrayNode) route.get("rules") : mapper.createArrayNode();
        rules.add(rule);
        route.set("rules", rules);
        config.set("route", route);
        save();
        return rule;
    }

    public synchronized boolean deleteRouteRule(int index) {
        ObjectNode route = getRoute();
        if (!route.has("rules") || !route.get("rules").isArray()) {
            return false;
        }
        ArrayNode rules = (ArrayNode) route.get("rules");
        if (index < 0 || index >= rules.size()) {
            return false;
        }
        rules.remove(index);
        config.set("route", route);
        save();
        return true;
    }

    public synchronized void setRoute(ObjectNode route) {
        config.set("route", route);
        save();
    }

    public synchronized void setDns(ObjectNode dns) {
        config.set("dns", dns);
        save();
    }

    public synchronized void setLog(ObjectNode log) {
        config.set("log", log);
        save();
    }

    public synchronized ObjectNode applyTemplate(String name) {
        try {
            String resource = switch (name) {
                case "tun-vless" -> "/templates/tun-vless.json";
                case "socks-selector" -> "/templates/socks-selector.json";
                default -> "/templates/empty.json";
            };
            var in = getClass().getResourceAsStream(resource);
            if (in == null) {
                throw new IllegalArgumentException("Unknown template: " + name);
            }
            ObjectNode next = (ObjectNode) mapper.readTree(in);
            this.config = next;
            save();
            return getConfig();
        } catch (RuntimeException e) {
            throw new IllegalStateException("Cannot load template", e);
        }
    }

    /** Structural validation: JSON shape + required fields per known type. */
    public List<String> validate(JsonNode root) {
        List<String> errors = new ArrayList<>();
        if (!(root instanceof ObjectNode)) {
            errors.add("Root must be a JSON object");
            return errors;
        }
        checkTags(root, "inbounds", errors);
        checkTags(root, "outbounds", errors);
        checkTags(root, "endpoints", errors);
        if (root.has("inbounds")) {
            for (JsonNode in : root.get("inbounds")) {
                require(in, "type", errors, "inbound");
                require(in, "tag", errors, "inbound");
            }
        }
        if (root.has("outbounds")) {
            if (!root.get("outbounds").isArray() || root.get("outbounds").isEmpty()) {
                errors.add("outbounds must be a non-empty array");
            } else {
                for (JsonNode out : root.get("outbounds")) {
                    validateOutbound(out, errors);
                }
            }
        }
        return errors;
    }

    public List<String> validateCurrent() {
        return validate(config);
    }

    // ---- internals ----

    private ObjectNode getRoute() {
        JsonNode node = config.get("route");
        if (node instanceof ObjectNode obj) {
            return obj.deepCopy();
        }
        ObjectNode route = mapper.createObjectNode();
        route.put("final", "direct");
        route.set("rules", mapper.createArrayNode());
        return route;
    }

    private void validateOutbound(JsonNode out, List<String> errors) {
        String tag = out.has("tag") ? out.get("tag").asText() : "?";
        if (!out.hasNonNull("type")) {
            errors.add("outbound '" + tag + "': missing type");
            return;
        }
        String type = out.get("type").asText();
        Set<String> remoteTypes = Set.of("shadowsocks", "vmess", "vless", "trojan", "socks",
                "http", "wireguard", "hysteria", "hysteria2", "tuic", "anytls", "snell",
                "naive", "shadowtls", "tor", "ssh");
        if (remoteTypes.contains(type)) {
            if (!out.hasNonNull("server")) {
                errors.add("outbound '" + tag + "' (" + type + "): missing server");
            }
            if (!out.has("server_port")) {
                errors.add("outbound '" + tag + "' (" + type + "): missing server_port");
            }
        }
        if (Set.of("selector", "urltest").contains(type) && !out.has("outbounds")) {
            errors.add("outbound '" + tag + "' (" + type + "): missing outbounds list");
        }
        if (Set.of("vless", "vmess", "trojan", "naive").contains(type) && !out.hasNonNull("uuid")
                && !out.hasNonNull("password")) {
            // vless uses uuid, trojan/naive use password; accept either field present
            if (type.equals("vless") && !out.hasNonNull("uuid")) {
                errors.add("outbound '" + tag + "' (vless): missing uuid");
            }
            if ((type.equals("trojan") || type.equals("naive")) && !out.hasNonNull("password")) {
                errors.add("outbound '" + tag + "' (" + type + "): missing password");
            }
        }
        if (type.equals("shadowsocks") && (!out.hasNonNull("method") || !out.hasNonNull("password"))) {
            errors.add("outbound '" + tag + "' (shadowsocks): missing method/password");
        }
        if (type.equals("wireguard") && (!out.hasNonNull("private_key") || !out.hasNonNull("peer_public_key"))) {
            errors.add("outbound '" + tag + "' (wireguard): missing private_key/peer_public_key");
        }
    }

    private void checkTags(JsonNode root, String section, List<String> errors) {
        if (!root.has(section)) {
            return;
        }
        JsonNode arr = root.get(section);
        if (!arr.isArray()) {
            errors.add(section + " must be an array");
            return;
        }
        Set<String> seen = new HashSet<>();
        for (JsonNode el : arr) {
            if (!el.hasNonNull("tag")) {
                errors.add(section + ": entry without tag");
                continue;
            }
            String tag = el.get("tag").asText();
            if (!seen.add(tag)) {
                errors.add(section + ": duplicate tag '" + tag + "'");
            }
        }
    }

    private void require(JsonNode node, String field, List<String> errors, String what) {
        if (!node.hasNonNull(field)) {
            errors.add(what + ": missing " + field);
        }
    }

    private void ensureDefaults(ObjectNode root) {
        if (!root.has("$schema")) {
            root.put("$schema", "https://sing-box.sagernet.org/schema.json");
        }
        // Имя TUN зависит от ОС: на macOS пустое = sing-box сам возьмёт свободный utunN.
        // Заполняем дефолт только там, где он валиден (Linux/Windows: tun0).
        String tunDef = tunDefault();
        if (!tunDef.isEmpty() && root.has("inbounds") && root.get("inbounds").isArray()) {
            for (JsonNode in : root.get("inbounds")) {
                if (in instanceof ObjectNode obj && "tun".equals(obj.path("type").asText())
                        && !obj.hasNonNull("interface_name")) {
                    obj.put("interface_name", tunDef);
                }
            }
        }
    }

    /** Дефолтное имя TUN: macOS — пусто (авто utunN), иначе tun0. */
    private String tunDefault() {
        String os = System.getProperty("os.name", "").toLowerCase(java.util.Locale.ROOT);
        return os.contains("mac") ? "" : "tun0";
    }

    private ObjectNode loadOrDefault() {
        Path path = Path.of(props.getConfigPath());
        if (Files.exists(path)) {
            try {
                JsonNode node = mapper.readTree(Files.readString(path));
                if (node instanceof ObjectNode obj) {
                    return obj;
                }
            } catch (IOException ignored) {
            }
        }
        ObjectNode def = mapper.createObjectNode();
        def.put("$schema", "https://sing-box.sagernet.org/schema.json");
        def.set("log", mapper.createObjectNode().put("level", "info").put("timestamp", true));
        def.set("dns", defaultDns());
        def.set("inbounds", mapper.createArrayNode().add(defaultTun()).add(defaultMixed()));
        ArrayNode outbounds = mapper.createArrayNode();
        outbounds.add(mapper.createObjectNode().put("type", "direct").put("tag", "direct"));
        outbounds.add(mapper.createObjectNode().put("type", "block").put("tag", "block"));
        outbounds.add(mapper.createObjectNode().put("type", "dns").put("tag", "dns-out"));
        def.set("outbounds", outbounds);
        def.set("route", defaultRoute());
        saveTo(def);
        return def;
    }

    private ObjectNode defaultDns() {
        ObjectNode dns = mapper.createObjectNode();
        ArrayNode servers = mapper.createArrayNode();
        servers.add(mapper.createObjectNode().put("tag", "google").put("type", "tls").put("server", "8.8.8.8"));
        servers.add(mapper.createObjectNode().put("tag", "local").put("type", "udp").put("server", "223.5.5.5"));
        dns.set("servers", servers);
        dns.put("strategy", "ipv4_only");
        return dns;
    }

    private ObjectNode defaultTun() {
        ObjectNode tun = mapper.createObjectNode();
        tun.put("type", "tun");
        tun.put("tag", "tun-in");
        if (!tunDefault().isEmpty()) {
            tun.put("interface_name", tunDefault());
        }
        tun.set("address", mapper.createArrayNode().add("172.19.0.1/30"));
        tun.put("auto_route", true);
        tun.put("strict_route", true);
        tun.put("stack", "system");
        return tun;
    }

    private ObjectNode defaultMixed() {
        ObjectNode mixed = mapper.createObjectNode();
        mixed.put("type", "mixed");
        mixed.put("tag", "mixed-in");
        mixed.put("listen", "127.0.0.1");
        mixed.put("listen_port", 2080);
        return mixed;
    }

    private ObjectNode defaultRoute() {
        ObjectNode route = mapper.createObjectNode();
        ArrayNode rules = mapper.createArrayNode();
        rules.add(mapper.createObjectNode().put("action", "sniff"));
        rules.add(mapper.createObjectNode().put("protocol", "dns").put("action", "hijack-dns"));
        ObjectNode priv = mapper.createObjectNode();
        priv.put("ip_is_private", true);
        priv.put("outbound", "direct");
        priv.put("action", "route");
        rules.add(priv);
        route.set("rules", rules);
        route.put("final", "direct");
        route.put("auto_detect_interface", true);
        return route;
    }

    private synchronized void save() {
        saveTo(config);
    }

    private void saveTo(ObjectNode root) {
        try {
            Path path = Path.of(props.getConfigPath());
            if (path.getParent() != null) {
                Files.createDirectories(path.toAbsolutePath().getParent());
            }
            Files.writeString(path, mapper.writerWithDefaultPrettyPrinter().writeValueAsString(root));
        } catch (IOException e) {
            throw new IllegalStateException("Cannot write " + props.getConfigPath(), e);
        }
    }

    public Map<String, Object> knownTypes() {
        return Map.of(
                "tunDefault", tunDefault(),
                "inbounds", List.of("tun", "mixed", "socks", "http", "direct", "shadowsocks",
                        "vmess", "vless", "trojan", "naive", "hysteria2", "tuic", "anytls", "redirect", "tproxy"),
                "outbounds", List.of("direct", "block", "dns", "selector", "urltest", "socks", "http",
                        "shadowsocks", "vmess", "vless", "trojan", "wireguard", "hysteria", "hysteria2",
                        "tuic", "anytls", "snell", "naive", "shadowtls", "tor", "ssh"),
                "dnsTypes", List.of("udp", "tcp", "tls", "https", "quic", "h3", "dhcp", "fakeip", "hosts", "predefined", "platform"),
                "ruleActions", List.of("route", "route-options", "reject", "hijack-dns", "sniff", "resolve", "reject-drop")
        );
    }
}
