package com.larffxx.singboxui.service;

import com.larffxx.singboxui.config.SingBoxProperties;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.boot.context.event.ApplicationReadyEvent;
import org.springframework.context.event.EventListener;
import org.springframework.stereotype.Service;
import tools.jackson.databind.JsonNode;
import tools.jackson.databind.ObjectMapper;

import java.io.IOException;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.StandardCopyOption;
import java.nio.file.attribute.PosixFilePermissions;
import java.time.Duration;
import java.util.LinkedHashMap;
import java.util.Locale;
import java.util.Map;
import java.util.concurrent.TimeUnit;
import java.util.stream.Stream;

/**
 * Находит sing-box без ручной настройки PATH:
 * явный путь → ./bin/ → PATH → автоскачивание с GitHub под текущую ОС.
 */
@Service
public class SingBoxProvisioningService {

    private static final Logger log = LoggerFactory.getLogger(SingBoxProvisioningService.class);

    private final SingBoxProperties props;
    private final ObjectMapper mapper;
    private final HttpClient http = HttpClient.newBuilder()
            .followRedirects(HttpClient.Redirect.NORMAL)
            .connectTimeout(Duration.ofSeconds(15))
            .build();

    private volatile String resolved;
    private volatile String installNote = "";

    public SingBoxProvisioningService(SingBoxProperties props, ObjectMapper mapper) {
        this.props = props;
        this.mapper = mapper;
    }

    /** Путь до бинарника или null, если нигде нет. */
    public synchronized String resolveBinary() {
        if (resolved != null) {
            return resolved;
        }
        String explicit = props.getBinary();
        if (explicit != null && !explicit.isBlank()) {
            resolved = explicit;
            return resolved;
        }
        Path local = localBinaryPath();
        if (Files.isRegularFile(local) && Files.isExecutable(local)) {
            resolved = local.toAbsolutePath().toString();
            return resolved;
        }
        if (probeVersion("sing-box") != null) {
            resolved = "sing-box";
            return resolved;
        }
        return null;
    }

    public Map<String, Object> info() {
        String bin = resolveBinary();
        Map<String, Object> m = new LinkedHashMap<>();
        m.put("found", bin != null);
        m.put("binary", bin == null ? "" : bin);
        m.put("version", bin == null ? "" : orEmpty(probeVersion(bin)));
        m.put("source", sourceOf(bin));
        m.put("os", osName());
        m.put("arch", archName());
        m.put("note", installNote);
        return m;
    }

    /** Скачать последний релиз с GitHub под текущую ОС в ./bin/. */
    public synchronized Map<String, Object> install() {
        String os = osName();
        String arch = archName();
        if (os.isEmpty() || arch.isEmpty()) {
            throw new IllegalArgumentException("unsupported platform: " + System.getProperty("os.name"));
        }
        try {
            String[] rel = latestRelease(os, arch);
            String tag = rel[0];
            String url = rel[1];
            installNote = "downloading " + tag + " (" + os + "/" + arch + ")…";
            log.info("Downloading sing-box {} for {}/{}", tag, os, arch);
            Path tmpFile = Files.createTempFile("singbox", url.endsWith(".zip") ? ".zip" : ".tar.gz");
            Path tmpDir = Files.createTempDirectory("singbox");
            try {
                HttpRequest req = HttpRequest.newBuilder(URI.create(url))
                        .timeout(Duration.ofMinutes(5)).GET().build();
                HttpResponse<Path> resp = http.send(req, HttpResponse.BodyHandlers.ofFile(tmpFile));
                if (resp.statusCode() != 200) {
                    throw new IllegalStateException("download failed: HTTP " + resp.statusCode());
                }
                run("tar", "-xf", tmpFile.toString(), "-C", tmpDir.toString());
                Path found = findBinary(tmpDir, isWindows());
                if (found == null) {
                    throw new IllegalStateException("binary not found in archive");
                }
                Path dest = localBinaryPath();
                Files.createDirectories(dest.toAbsolutePath().getParent());
                Files.copy(found, dest, StandardCopyOption.REPLACE_EXISTING);
                try {
                    Files.setPosixFilePermissions(dest,
                            PosixFilePermissions.fromString("rwxr-xr-x"));
                } catch (UnsupportedOperationException ignored) {
                    dest.toFile().setExecutable(true);
                }
                String ver = probeVersion(dest.toAbsolutePath().toString());
                if (ver == null) {
                    throw new IllegalStateException("downloaded binary does not run");
                }
                resolved = dest.toAbsolutePath().toString();
                installNote = "";
                log.info("sing-box installed: {} ({})", resolved, ver);
                Map<String, Object> m = new LinkedHashMap<>();
                m.put("ok", true);
                m.put("binary", resolved);
                m.put("version", ver);
                return m;
            } finally {
                deleteQuiet(tmpFile);
                deleteQuiet(tmpDir);
            }
        } catch (IOException e) {
            throw new IllegalStateException("install failed: " + e.getMessage());
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            throw new IllegalStateException("install interrupted");
        }
    }

    @EventListener(ApplicationReadyEvent.class)
    public void autoInstall() {
        if (!props.isAutoInstall() || resolveBinary() != null) {
            return;
        }
        Thread t = new Thread(() -> {
            try {
                install();
            } catch (Exception e) {
                installNote = e.getMessage();
                log.warn("sing-box auto-install failed: {}", e.getMessage());
            }
        }, "singbox-install");
        t.setDaemon(true);
        t.start();
    }

    // ---- internals ----

    private String[] latestRelease(String os, String arch) throws IOException, InterruptedException {
        HttpRequest req = HttpRequest.newBuilder(
                        URI.create("https://api.github.com/repos/SagerNet/sing-box/releases/latest"))
                .timeout(Duration.ofSeconds(20))
                .header("Accept", "application/vnd.github+json")
                .header("User-Agent", "SingBoxUI")
                .GET().build();
        HttpResponse<String> resp = http.send(req, HttpResponse.BodyHandlers.ofString());
        if (resp.statusCode() != 200) {
            throw new IllegalStateException("GitHub API: HTTP " + resp.statusCode());
        }
        JsonNode root = mapper.readTree(resp.body());
        String tag = root.path("tag_name").asText("");
        boolean win = os.equals("windows");
        // assets look like sing-box-1.14.0-darwin-arm64.tar.gz (no 'v' prefix)
        String ver = tag.startsWith("v") ? tag.substring(1) : tag;
        String want = "sing-box-" + ver + "-" + os + "-" + arch + (win ? ".zip" : ".tar.gz");
        for (JsonNode a : root.path("assets")) {
            if (want.equals(a.path("name").asText())) {
                return new String[]{tag, a.path("browser_download_url").asText()};
            }
        }
        throw new IllegalStateException("no asset " + want + " in " + tag);
    }

    private Path findBinary(Path dir, boolean win) throws IOException {
        String name = win ? "sing-box.exe" : "sing-box";
        try (Stream<Path> s = Files.walk(dir, 3)) {
            return s.filter(p -> p.getFileName().toString().equals(name)
                            && Files.isRegularFile(p))
                    .findFirst().orElse(null);
        }
    }

    private void run(String... cmd) throws IOException, InterruptedException {
        Process p = new ProcessBuilder(cmd).redirectErrorStream(true).start();
        String out = new String(p.getInputStream().readAllBytes(), StandardCharsets.UTF_8);
        if (!p.waitFor(60, TimeUnit.SECONDS) || p.exitValue() != 0) {
            throw new IllegalStateException("extract failed: " + out.trim());
        }
    }

    private void deleteQuiet(Path p) {
        try {
            if (Files.isDirectory(p)) {
                try (Stream<Path> s = Files.walk(p)) {
                    for (Path q : s.sorted((a, b) -> b.compareTo(a)).toList()) {
                        Files.deleteIfExists(q);
                    }
                }
            } else {
                Files.deleteIfExists(p);
            }
        } catch (IOException ignored) {
        }
    }

    /** Первая строка `cmd version` или null. */
    private String probeVersion(String cmd) {
        try {
            Process p = new ProcessBuilder(cmd, "version").redirectErrorStream(true).start();
            String out = new String(p.getInputStream().readAllBytes(), StandardCharsets.UTF_8);
            if (!p.waitFor(10, TimeUnit.SECONDS)) {
                p.destroyForcibly();
                return null;
            }
            String first = out.lines().findFirst().orElse("").trim();
            return first.isEmpty() ? null : first;
        } catch (IOException | InterruptedException e) {
            if (e instanceof InterruptedException) {
                Thread.currentThread().interrupt();
            }
            return null;
        }
    }

    private Path localBinaryPath() {
        return Path.of(isWindows() ? "bin/sing-box.exe" : "bin/sing-box");
    }

    private String sourceOf(String bin) {
        if (bin == null) {
            return "missing";
        }
        if (props.getBinary() != null && !props.getBinary().isBlank()) {
            return "settings";
        }
        if (bin.contains("bin" + java.io.File.separator + "sing-box")
                || bin.contains("bin/sing-box")) {
            return "managed";
        }
        return "PATH";
    }

    private boolean isWindows() {
        return System.getProperty("os.name", "").toLowerCase(Locale.ROOT).contains("win");
    }

    private String osName() {
        String os = System.getProperty("os.name", "").toLowerCase(Locale.ROOT);
        if (os.contains("mac")) {
            return "darwin";
        }
        if (os.contains("win")) {
            return "windows";
        }
        if (os.contains("linux")) {
            return "linux";
        }
        return "";
    }

    private String archName() {
        String arch = System.getProperty("os.arch", "").toLowerCase(Locale.ROOT);
        if (arch.equals("aarch64") || arch.equals("arm64")) {
            return "arm64";
        }
        if (arch.equals("x86_64") || arch.equals("amd64")) {
            return "amd64";
        }
        return "";
    }

    private String orEmpty(String s) {
        return s == null ? "" : s;
    }
}
