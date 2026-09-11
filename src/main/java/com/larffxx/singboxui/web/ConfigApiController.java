package com.larffxx.singboxui.web;

import tools.jackson.databind.ObjectMapper;
import tools.jackson.databind.node.ArrayNode;
import tools.jackson.databind.node.ObjectNode;
import com.larffxx.singboxui.service.AutostartService;
import com.larffxx.singboxui.service.SettingsService;
import com.larffxx.singboxui.service.ShareLinkService;
import com.larffxx.singboxui.service.SingBoxBinaryService;
import com.larffxx.singboxui.service.SingBoxConfigService;
import com.larffxx.singboxui.service.SingBoxProcessService;
import com.larffxx.singboxui.service.SingBoxProvisioningService;
import com.larffxx.singboxui.service.TrafficService;
import org.springframework.http.ContentDisposition;
import org.springframework.http.HttpHeaders;
import org.springframework.http.MediaType;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.DeleteMapping;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

import java.nio.charset.StandardCharsets;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

@RestController
@RequestMapping("/api")
public class ConfigApiController {

    private final SingBoxConfigService configs;
    private final SingBoxBinaryService binary;
    private final SingBoxProcessService proc;
    private final SingBoxProvisioningService provisioning;
    private final TrafficService traffic;
    private final SettingsService settings;
    private final AutostartService autostart;
    private final ShareLinkService share;
    private final ObjectMapper mapper;

    public ConfigApiController(SingBoxConfigService configs, SingBoxBinaryService binary,
                               SingBoxProcessService proc, SingBoxProvisioningService provisioning,
                               TrafficService traffic, SettingsService settings, AutostartService autostart,
                               ShareLinkService share, ObjectMapper mapper) {
        this.configs = configs;
        this.binary = binary;
        this.proc = proc;
        this.provisioning = provisioning;
        this.traffic = traffic;
        this.settings = settings;
        this.autostart = autostart;
        this.share = share;
        this.mapper = mapper;
    }

    @GetMapping("/config")
    public ObjectNode config() {
        return configs.getConfig();
    }

    @PostMapping("/config")
    public Object config(@RequestBody ObjectNode next) {
        try {
            return configs.replaceConfig(next);
        } catch (IllegalArgumentException e) {
            return Map.of("error", e.getMessage());
        }
    }

    @PostMapping("/config/template/{name}")
    public ObjectNode template(@PathVariable String name) {
        return configs.applyTemplate(name);
    }

    @GetMapping("/export")
    public ResponseEntity<byte[]> exportConfig() {
        String json;
        try {
            json = mapper.writerWithDefaultPrettyPrinter().writeValueAsString(configs.getConfig());
        } catch (Exception e) {
            throw new IllegalStateException(e);
        }
        return ResponseEntity.ok()
                .header(HttpHeaders.CONTENT_DISPOSITION,
                        ContentDisposition.attachment().filename("config.json").build().toString())
                .contentType(MediaType.APPLICATION_JSON)
                .body(json.getBytes(StandardCharsets.UTF_8));
    }

    @PostMapping("/import")
    public Object importConfig(@RequestBody ObjectNode next) {
        try {
            return configs.replaceConfig(next);
        } catch (IllegalArgumentException e) {
            return Map.of("error", e.getMessage());
        }
    }

    @PostMapping("/inbounds")
    public ObjectNode upsertInbound(@RequestBody ObjectNode entry) {
        requireType(entry);
        return configs.upsertByTag("inbounds", entry);
    }

    @DeleteMapping("/inbounds/{tag}")
    public Map<String, Object> deleteInbound(@PathVariable String tag) {
        return Map.of("deleted", configs.deleteByTag("inbounds", tag));
    }

    @PostMapping("/outbounds")
    public ObjectNode upsertOutbound(@RequestBody ObjectNode entry) {
        requireType(entry);
        return configs.upsertByTag("outbounds", entry);
    }

    @DeleteMapping("/outbounds/{tag}")
    public Map<String, Object> deleteOutbound(@PathVariable String tag) {
        return Map.of("deleted", configs.deleteByTag("outbounds", tag));
    }

    @PostMapping("/dns/servers")
    public ObjectNode upsertDnsServer(@RequestBody ObjectNode entry) {
        ObjectNode dns = configs.getSectionObject("dns");
        var servers = dns.has("servers") && dns.get("servers").isArray()
                ? (tools.jackson.databind.node.ArrayNode) dns.get("servers")
                : mapper.createArrayNode();
        String tag = entry.hasNonNull("tag") ? entry.get("tag").asText() : "dns";
        boolean replaced = false;
        for (int i = 0; i < servers.size(); i++) {
            if (servers.get(i).has("tag") && tag.equals(servers.get(i).get("tag").asText())) {
                servers.set(i, entry);
                replaced = true;
                break;
            }
        }
        if (!replaced) {
            servers.add(entry);
        }
        dns.set("servers", servers);
        configs.setDns(dns);
        return entry;
    }

    @DeleteMapping("/dns/servers/{tag}")
    public Map<String, Object> deleteDnsServer(@PathVariable String tag) {
        ObjectNode dns = configs.getSectionObject("dns");
        if (dns.has("servers") && dns.get("servers").isArray()) {
            var servers = (tools.jackson.databind.node.ArrayNode) dns.get("servers");
            for (int i = 0; i < servers.size(); i++) {
                if (servers.get(i).has("tag") && tag.equals(servers.get(i).get("tag").asText())) {
                    servers.remove(i);
                    configs.setDns(dns);
                    return Map.of("deleted", true);
                }
            }
        }
        return Map.of("deleted", false);
    }

    @PostMapping({"/dns", "/route", "/log", "/experimental", "/ntp"})
    public Map<String, Object> unsupported() {
        return Map.of("error", "use PUT /api/section/{name}");
    }

    @PostMapping("/section/{name}")
    public Object section(@PathVariable String name, @RequestBody ObjectNode value) {
        if (!List.of("dns", "route", "log", "experimental", "ntp").contains(name)) {
            return Map.of("error", "unknown section: " + name);
        }
        configs.setSectionObject(name, value);
        return configs.getSectionObject(name);
    }

    @PostMapping("/route/rules")
    public ObjectNode addRouteRule(@RequestBody ObjectNode rule) {
        return configs.addRouteRule(rule);
    }

    @DeleteMapping("/route/rules/{index}")
    public Map<String, Object> deleteRouteRule(@PathVariable int index) {
        return Map.of("deleted", configs.deleteRouteRule(index));
    }

    /**
     * Split tunneling: friendly rule builder over route.rules.
     * Body: {kind: domain|ip|geo|process|port|protocol, values: "a, b" | [...], outbound: "tag"}.
     * Geo rule_sets (geoip-X, geosite-X) are auto-registered as remote rule-sets.
     */
    @PostMapping("/split/rules")
    public Object addSplitRule(@RequestBody Map<String, Object> body) {
        String kind = String.valueOf(body.getOrDefault("kind", "")).trim();
        String outbound = String.valueOf(body.getOrDefault("outbound", "")).trim();
        if (outbound.isEmpty()) {
            throw new IllegalArgumentException("outbound required");
        }
        List<String> values = parseValues(body.get("values"));
        if (values.isEmpty()) {
            throw new IllegalArgumentException("values required");
        }
        ObjectNode rule = mapper.createObjectNode();
        switch (kind) {
            case "domain" -> rule.set("domain_suffix", strings(values));
            case "ip" -> rule.set("ip_cidr", strings(values));
            case "geo" -> {
                rule.set("rule_set", strings(values));
                ensureRuleSets(values);
            }
            case "process" -> rule.set("process_name", strings(values));
            case "port" -> {
                ArrayNode ports = mapper.createArrayNode();
                for (String v : values) {
                    try {
                        ports.add(Integer.parseInt(v));
                    } catch (NumberFormatException e) {
                        throw new IllegalArgumentException("bad port: " + v);
                    }
                }
                rule.set("port", ports);
            }
            case "protocol" -> rule.set("protocol", strings(values));
            default -> throw new IllegalArgumentException("unknown kind: " + kind);
        }
        rule.put("action", "route");
        rule.put("outbound", outbound);
        return configs.addRouteRule(rule);
    }

    @GetMapping("/validate")
    public Map<String, Object> validate() {
        List<String> errors = configs.validateCurrent();
        return Map.of("valid", errors.isEmpty(), "errors", errors);
    }

    @GetMapping("/status")
    public Map<String, Object> status() {
        return binary.status(configs.validateCurrent());
    }

    @PostMapping("/check")
    public Map<String, Object> check() {
        return binary.check();
    }

    @GetMapping("/proc/status")
    public Map<String, Object> procStatus() {
        return proc.status();
    }

    @PostMapping("/proc/start")
    public Map<String, Object> procStart() {
        return proc.start();
    }

    @PostMapping("/proc/stop")
    public Map<String, Object> procStop() {
        return proc.stop();
    }

    @PostMapping("/proc/restart")
    public Map<String, Object> procRestart() {
        return proc.restart();
    }

    @GetMapping("/proc/logs")
    public Map<String, Object> procLogs(@RequestParam(defaultValue = "200") int lines) {
        return Map.of("logs", proc.logs(lines));
    }

    @GetMapping("/traffic")
    public Map<String, Object> traffic() {
        return traffic.current();
    }

    @GetMapping("/settings")
    public Map<String, Object> getSettings() {
        return settings.get();
    }

    @PostMapping("/settings")
    public Object saveSettings(@RequestBody Map<String, Object> body) {
        try {
            Object v = body.get("autoConnect");
            boolean autoConnect = v instanceof Boolean b ? b : Boolean.parseBoolean(String.valueOf(v));
            return settings.set(autoConnect);
        } catch (IllegalStateException e) {
            return Map.of("error", e.getMessage());
        }
    }

    @GetMapping("/autostart")
    public Map<String, Object> autostartStatus() {
        return autostart.status();
    }

    @PostMapping("/autostart")
    public Object setAutostart(@RequestBody Map<String, Object> body) {
        try {
            Object v = body.get("enabled");
            boolean enabled = v instanceof Boolean b ? b : Boolean.parseBoolean(String.valueOf(v));
            return autostart.setEnabled(enabled);
        } catch (IllegalArgumentException | IllegalStateException e) {
            return Map.of("error", e.getMessage());
        }
    }

    @GetMapping("/singbox")
    public Map<String, Object> singboxInfo() {
        return provisioning.info();
    }

    @PostMapping("/singbox/install")
    public Object singboxInstall() {
        try {
            return provisioning.install();
        } catch (IllegalArgumentException | IllegalStateException e) {
            return Map.of("error", e.getMessage());
        }
    }

    @PostMapping("/share/import")
    public Object importShare(@RequestBody Map<String, String> body) {
        try {
            ObjectNode outbound = share.parse(body.getOrDefault("link", ""));
            return configs.upsertByTag("outbounds", outbound);
        } catch (IllegalArgumentException e) {
            return Map.of("error", e.getMessage());
        }
    }

    @GetMapping("/share/export/{tag}")
    public Map<String, Object> exportShare(@PathVariable String tag) {
        for (var out : configs.getSectionArray("outbounds")) {
            if (out.has("tag") && tag.equals(out.get("tag").asText()) && out instanceof ObjectNode obj) {
                try {
                    return Map.of("link", share.build(obj));
                } catch (IllegalArgumentException e) {
                    return Map.of("error", e.getMessage());
                }
            }
        }
        return Map.of("error", "outbound not found: " + tag);
    }

    @GetMapping("/meta")
    public Map<String, Object> meta() {
        return configs.knownTypes();
    }

    private void requireType(ObjectNode entry) {
        if (!entry.hasNonNull("type")) {
            throw new IllegalArgumentException("missing type");
        }
    }

    private List<String> parseValues(Object raw) {
        List<String> out = new java.util.ArrayList<>();
        if (raw instanceof List<?> list) {
            for (Object o : list) {
                splitValues(String.valueOf(o), out);
            }
        } else if (raw != null) {
            splitValues(String.valueOf(raw), out);
        }
        return out.stream().map(String::trim).filter(s -> !s.isEmpty()).distinct().toList();
    }

    private void splitValues(String s, List<String> out) {
        for (String part : s.split("[,\\n]+")) {
            String t = part.trim();
            if (!t.isEmpty()) {
                out.add(t);
            }
        }
    }

    private ArrayNode strings(List<String> values) {
        ArrayNode arr = mapper.createArrayNode();
        values.forEach(arr::add);
        return arr;
    }

    private void ensureRuleSets(List<String> tags) {
        ObjectNode route = configs.getSectionObject("route");
        ArrayNode arr = route.has("rule_set") && route.get("rule_set").isArray()
                ? (ArrayNode) route.get("rule_set") : mapper.createArrayNode();
        Set<String> have = new HashSet<>();
        arr.forEach(n -> {
            if (n.has("tag")) {
                have.add(n.get("tag").asText());
            }
        });
        for (String t : tags) {
            String base;
            if (t.startsWith("geosite-")) {
                base = "https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/";
            } else if (t.startsWith("geoip-")) {
                base = "https://raw.githubusercontent.com/SagerNet/sing-geoip/rule-set/";
            } else {
                continue;
            }
            if (!have.add(t)) {
                continue;
            }
            ObjectNode rs = mapper.createObjectNode();
            rs.put("type", "remote");
            rs.put("tag", t);
            rs.put("format", "binary");
            rs.put("url", base + t + ".srs");
            rs.put("download_detour", "direct");
            arr.add(rs);
        }
        route.set("rule_set", arr);
        configs.setRoute(route);
    }
}
