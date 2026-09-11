package com.larffxx.singboxui.service;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Service;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Locale;
import java.util.Map;
import java.util.concurrent.TimeUnit;

/**
 * Автозапуск приложения при входе в систему.
 * macOS: LaunchAgent ~/Library/LaunchAgents/com.larffxx.singboxui.plist.
 * Windows: ключ HKCU\...\Run (javaw, без окна консоли).
 */
@Service
public class AutostartService {

    private static final Logger log = LoggerFactory.getLogger(AutostartService.class);
    private static final String LABEL = "com.larffxx.singboxui";

    public String platform() {
        String os = System.getProperty("os.name", "").toLowerCase(Locale.ROOT);
        if (os.contains("mac")) {
            return "mac";
        }
        if (os.contains("win")) {
            return "win";
        }
        return "other";
    }

    public boolean supported() {
        return !platform().equals("other");
    }

    public Map<String, Object> status() {
        return Map.of("supported", supported(), "platform", platform(), "enabled", supported() && isEnabled());
    }

    public Map<String, Object> setEnabled(boolean enabled) {
        if (!supported()) {
            throw new IllegalArgumentException("autostart not supported on this OS");
        }
        try {
            if (platform().equals("mac")) {
                setMac(enabled);
            } else {
                setWin(enabled);
            }
        } catch (IOException | InterruptedException e) {
            if (e instanceof InterruptedException) {
                Thread.currentThread().interrupt();
            }
            throw new IllegalStateException("autostart failed: " + e.getMessage());
        }
        return status();
    }

    // ---- macOS ----

    private Path plistPath() {
        return Path.of(System.getProperty("user.home"), "Library", "LaunchAgents", LABEL + ".plist");
    }

    private boolean isEnabledMac() {
        return Files.exists(plistPath());
    }

    private void setMac(boolean enabled) throws IOException, InterruptedException {
        Path plist = plistPath();
        if (enabled) {
            Files.createDirectories(plist.getParent());
            Files.writeString(plist, plistXml(javaCmd(), jarPath()));
            run("launchctl", "load", plist.toString());
        } else {
            if (Files.exists(plist)) {
                runQuiet("launchctl", "unload", plist.toString());
                Files.deleteIfExists(plist);
            }
        }
    }

    private String plistXml(String javaBin, String jar) {
        return """
                <?xml version="1.0" encoding="UTF-8"?>
                <!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
                <plist version="1.0">
                <dict>
                    <key>Label</key><string>%s</string>
                    <key>ProgramArguments</key>
                    <array><string>%s</string><string>-jar</string><string>%s</string></array>
                    <key>RunAtLoad</key><true/>
                    <key>StandardOutPath</key><string>/tmp/singboxui.log</string>
                    <key>StandardErrorPath</key><string>/tmp/singboxui.log</string>
                </dict>
                </plist>
                """.formatted(LABEL, javaBin, jar);
    }

    // ---- Windows ----

    private boolean isEnabledWin() {
        try {
            Process p = new ProcessBuilder("reg", "query",
                    "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Run", "/v", "SingBoxUI")
                    .redirectErrorStream(true).start();
            String out = new String(p.getInputStream().readAllBytes(), StandardCharsets.UTF_8);
            p.waitFor(10, TimeUnit.SECONDS);
            return p.exitValue() == 0 && out.contains("SingBoxUI");
        } catch (IOException | InterruptedException e) {
            if (e instanceof InterruptedException) {
                Thread.currentThread().interrupt();
            }
            return false;
        }
    }

    private void setWin(boolean enabled) throws IOException, InterruptedException {
        if (enabled) {
            String javaw = javaBin().replace("java.exe", "javaw.exe");
            run("reg", "add", "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Run",
                    "/v", "SingBoxUI", "/t", "REG_SZ", "/d",
                    "\"" + javaw + "\" -jar \"" + jarPath() + "\"", "/f");
        } else {
            runQuiet("reg", "delete", "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Run",
                    "/v", "SingBoxUI", "/f");
        }
    }

    // ---- shared ----

    private boolean isEnabled() {
        return platform().equals("mac") ? isEnabledMac() : isEnabledWin();
    }

    private String javaBin() {
        return Path.of(System.getProperty("java.home"), "bin",
                platform().equals("win") ? "java.exe" : "java").toString();
    }

    private String javaCmd() {
        return javaBin();
    }

    /** Путь до запущенного jar (или classes при dev-запуске). */
    private String jarPath() {
        try {
            Path p = Path.of(AutostartService.class.getProtectionDomain()
                    .getCodeSource().getLocation().toURI()).toAbsolutePath();
            return p.toString();
        } catch (Exception e) {
            throw new IllegalStateException("cannot locate app jar: " + e.getMessage());
        }
    }

    private void run(String... cmd) throws IOException, InterruptedException {
        Process p = new ProcessBuilder(cmd).redirectErrorStream(true).start();
        String out = new String(p.getInputStream().readAllBytes(), StandardCharsets.UTF_8);
        if (!p.waitFor(15, TimeUnit.SECONDS) || p.exitValue() != 0) {
            throw new IllegalStateException(String.join(" ", cmd) + ": " + out.trim());
        }
    }

    private void runQuiet(String... cmd) {
        try {
            run(cmd);
        } catch (Exception e) {
            log.debug("autostart cleanup: {}", e.getMessage());
        }
    }
}
