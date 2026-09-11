package com.larffxx.singboxui.service;

import com.larffxx.singboxui.config.SingBoxProperties;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Service;
import tools.jackson.databind.JsonNode;
import tools.jackson.databind.ObjectMapper;

import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.time.Duration;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.Set;
import java.util.TreeMap;

/**
 * Мониторинг трафика через Clash API sing-box (experimental.clash_api).
 * Опрашивает /connections, считает общий трафик и разбивку VPN/direct.
 */
@Service
public class TrafficService {

    private static final Logger log = LoggerFactory.getLogger(TrafficService.class);
    private static final Set<String> LOCAL_OUTBOUNDS = Set.of("direct", "block", "dns", "dns-out");

    private final SingBoxProperties props;
    private final ObjectMapper mapper;
    private final HttpClient http = HttpClient.newBuilder()
            .connectTimeout(Duration.ofSeconds(2))
            .build();

    private volatile Map<String, Object> snapshot = baseSnapshot(false);
    private volatile long lastFetch;
    private Thread poller;

    public TrafficService(SingBoxProperties props, ObjectMapper mapper) {
        this.props = props;
        this.mapper = mapper;
    }

    public synchronized Map<String, Object> current() {
        if (poller == null) {
            poller = new Thread(this::loop, "traffic-poller");
            poller.setDaemon(true);
            poller.start();
        }
        Map<String, Object> m = new LinkedHashMap<>(snapshot);
        m.put("available", System.currentTimeMillis() - lastFetch < 6000
                && Boolean.TRUE.equals(snapshot.get("reachable")));
        return m;
    }

    private void loop() {
        while (!Thread.currentThread().isInterrupted()) {
            try {
                fetch();
                Thread.sleep(2000);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
            } catch (Exception e) {
                snapshot = baseSnapshot(false);
                sleepQuiet(2000);
            }
        }
    }

    private void fetch() {
        try {
            HttpRequest req = HttpRequest.newBuilder(
                            URI.create(props.getClashUrl() + "/connections"))
                    .timeout(Duration.ofSeconds(3)).GET().build();
            HttpResponse<String> resp = http.send(req, HttpResponse.BodyHandlers.ofString());
            if (resp.statusCode() != 200) {
                snapshot = baseSnapshot(false);
                return;
            }
            JsonNode conns = mapper.readTree(resp.body());
            long up = 0, down = 0, vpnUp = 0, vpnDown = 0, dirUp = 0, dirDown = 0;
            Map<String, long[]> byOut = new TreeMap<>();
            int n = 0;
            for (JsonNode c : conns) {
                n++;
                long u = c.path("upload").asLong();
                long d = c.path("download").asLong();
                up += u;
                down += d;
                String ob = outboundOf(c);
                byOut.computeIfAbsent(ob, k -> new long[2])[0] += u;
                byOut.computeIfAbsent(ob, k -> new long[2])[1] += d;
                if (LOCAL_OUTBOUNDS.contains(ob)) {
                    dirUp += u;
                    dirDown += d;
                } else {
                    vpnUp += u;
                    vpnDown += d;
                }
            }
            Map<String, Object> m = baseSnapshot(true);
            m.put("conns", n);
            m.put("up", up);
            m.put("down", down);
            m.put("vpn", Map.of("up", vpnUp, "down", vpnDown));
            m.put("direct", Map.of("up", dirUp, "down", dirDown));
            Map<String, Object> detail = new LinkedHashMap<>();
            byOut.forEach((k, v) -> detail.put(k, Map.of("up", v[0], "down", v[1])));
            m.put("byOutbound", detail);
            snapshot = m;
            lastFetch = System.currentTimeMillis();
        } catch (Exception e) {
            snapshot = baseSnapshot(false);
        }
    }

    private String outboundOf(JsonNode c) {
        JsonNode chains = c.path("chains");
        if (chains.isArray() && !chains.isEmpty()) {
            return chains.get(chains.size() - 1).asText("?");
        }
        return c.path("rulePayload").asText("?");
    }

    private Map<String, Object> baseSnapshot(boolean reachable) {
        Map<String, Object> m = new LinkedHashMap<>();
        m.put("reachable", reachable);
        m.put("available", false);
        m.put("conns", 0);
        m.put("up", 0L);
        m.put("down", 0L);
        m.put("vpn", Map.of("up", 0L, "down", 0L));
        m.put("direct", Map.of("up", 0L, "down", 0L));
        m.put("byOutbound", Map.of());
        return m;
    }

    private void sleepQuiet(long ms) {
        try {
            Thread.sleep(ms);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
        }
    }
}
