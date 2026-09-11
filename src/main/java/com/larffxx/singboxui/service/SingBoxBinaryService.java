package com.larffxx.singboxui.service;

import com.larffxx.singboxui.config.SingBoxProperties;
import org.springframework.stereotype.Service;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.TimeUnit;

/** Wraps the sing-box binary when present: version / check / format. */
@Service
public class SingBoxBinaryService {

    private final SingBoxProperties props;
    private final SingBoxProvisioningService provisioning;

    public SingBoxBinaryService(SingBoxProperties props, SingBoxProvisioningService provisioning) {
        this.props = props;
        this.provisioning = provisioning;
    }

    private String binOrThrow() {
        String bin = provisioning.resolveBinary();
        if (bin == null) {
            throw new IllegalStateException("sing-box не найден — скачай его кнопкой во вкладке «Запуск»");
        }
        return bin;
    }

    public Map<String, Object> status(List<String> structuralErrors) {
        String bin = provisioning.resolveBinary();
        String version = bin == null ? null : version();
        Map<String, Object> m = new LinkedHashMap<>();
        m.put("binary", bin == null ? "(не найден)" : bin);
        m.put("found", version != null);
        m.put("version", version == null ? "" : version);
        m.put("configPath", Path.of(props.getConfigPath()).toAbsolutePath().toString());
        m.put("valid", structuralErrors.isEmpty());
        m.put("errors", structuralErrors);
        return m;
    }

    public String version() {
        String bin = provisioning.resolveBinary();
        if (bin == null) {
            return null;
        }
        try {
            Process p = new ProcessBuilder(bin, "version").redirectErrorStream(true).start();
            String out = new String(p.getInputStream().readAllBytes(), StandardCharsets.UTF_8);
            boolean done = p.waitFor(10, TimeUnit.SECONDS);
            if (!done) {
                p.destroyForcibly();
                return null;
            }
            String first = out.lines().findFirst().orElse("").trim();
            return first.isEmpty() ? null : first;
        } catch (IOException e) {
            return null;
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            return null;
        }
    }

    public Map<String, Object> check() {
        Path path = Path.of(props.getConfigPath());
        if (!Files.exists(path)) {
            return Map.of("ok", false, "output", "config file not found: " + path.toAbsolutePath());
        }
        String bin;
        try {
            bin = binOrThrow();
        } catch (IllegalStateException e) {
            return Map.of("ok", false, "output", e.getMessage());
        }
        try {
            Process p = new ProcessBuilder(bin, "check", "-c", path.toAbsolutePath().toString())
                    .redirectErrorStream(true).start();
            String out = new String(p.getInputStream().readAllBytes(), StandardCharsets.UTF_8);
            boolean done = p.waitFor(15, TimeUnit.SECONDS);
            if (!done) {
                p.destroyForcibly();
                return Map.of("ok", false, "output", "timed out");
            }
            return Map.of("ok", p.exitValue() == 0, "output", out.trim());
        } catch (IOException e) {
            return Map.of("ok", false, "output", "cannot run " + bin + ": " + e.getMessage());
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            return Map.of("ok", false, "output", "interrupted");
        }
    }

    public Map<String, Object> format(String rawJson) {
        String bin;
        try {
            bin = binOrThrow();
        } catch (IllegalStateException e) {
            return Map.of("ok", false, "output", e.getMessage());
        }
        try {
            Path tmp = Files.createTempFile("singboxui", ".json");
            Files.writeString(tmp, rawJson);
            try {
                Process p = new ProcessBuilder(bin, "format", "-c", tmp.toString())
                        .redirectErrorStream(true).start();
                String out = new String(p.getInputStream().readAllBytes(), StandardCharsets.UTF_8);
                boolean done = p.waitFor(15, TimeUnit.SECONDS);
                if (!done) {
                    p.destroyForcibly();
                    return Map.of("ok", false, "output", "timed out");
                }
                String formatted = Files.exists(tmp) ? Files.readString(tmp) : out;
                return Map.of("ok", p.exitValue() == 0, "output", out.trim(), "formatted", formatted);
            } finally {
                Files.deleteIfExists(tmp);
            }
        } catch (IOException e) {
            return Map.of("ok", false, "output", "cannot run " + bin + ": " + e.getMessage());
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            return Map.of("ok", false, "output", "interrupted");
        }
    }
}
