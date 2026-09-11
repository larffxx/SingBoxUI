package com.larffxx.singboxui.service;

import com.larffxx.singboxui.config.SingBoxProperties;
import org.springframework.stereotype.Service;
import tools.jackson.databind.ObjectMapper;
import tools.jackson.databind.node.ObjectNode;

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Map;

/** Простые настройки UI в ui-settings.json рядом с рабочей папкой. */
@Service
public class SettingsService {

    private final ObjectMapper mapper;
    private final Path file;

    public SettingsService(ObjectMapper mapper, SingBoxProperties props) {
        this.mapper = mapper;
        Path cfg = Path.of(props.getConfigPath()).toAbsolutePath();
        Path dir = cfg.getParent() != null ? cfg.getParent() : Path.of(".");
        this.file = dir.resolve("ui-settings.json");
    }

    public synchronized Map<String, Object> get() {
        return Map.of("autoConnect", read().path("autoConnect").asBoolean(false));
    }

    public synchronized Map<String, Object> set(boolean autoConnect) {
        ObjectNode o = read();
        o.put("autoConnect", autoConnect);
        try {
            Files.writeString(file, mapper.writerWithDefaultPrettyPrinter().writeValueAsString(o));
        } catch (Exception e) {
            throw new IllegalStateException("cannot write " + file + ": " + e.getMessage());
        }
        return get();
    }

    public boolean autoConnect() {
        return read().path("autoConnect").asBoolean(false);
    }

    private ObjectNode read() {
        try {
            if (Files.exists(file)) {
                return (ObjectNode) mapper.readTree(Files.readString(file));
            }
        } catch (Exception ignored) {
        }
        return mapper.createObjectNode();
    }
}
