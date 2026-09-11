package com.larffxx.singboxui.tray;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.awt.Desktop;
import java.io.IOException;
import java.net.HttpURLConnection;
import java.net.URI;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.Locale;
import java.util.Optional;
import java.util.regex.Pattern;

/**
 * Стартовые удобства для запуска двойным кликом (dmg/.app, exe):
 * 1. Уже запущено? Открыть браузер и тихо выйти (никаких войн за порт).
 * 2. Нужен root для TUN, а мы из бандла? Перезапустить себя с повышением
 *    (macOS — системный диалог пароля, Windows — UAC) и выйти.
 * Возвращает true, если main() должен завершиться.
 */
public final class PrivilegeEscalation {

    private static final Logger log = LoggerFactory.getLogger(PrivilegeEscalation.class);

    private PrivilegeEscalation() {
    }

    public static boolean handleStartup(String[] args) {
        // Перезапущенная с повышением — сразу грузимся (порт наш по праву).
        if (System.getenv("SINGBOXUI_ELEVATED") != null) {
            return false;
        }
        int port = parsePort(args, 8000);
        if (isOursUp(port)) {
            openBrowser(port);
            return true;
        }
        if (isRoot() || !configHasTun(args) || !isPackagedApp()) {
            return false;
        }
        if (!tryRelaunchElevated(args)) {
            return false;
        }
        // Даём повышенной копии время на ввод пароля и старт; если взяла порт — выходим.
        for (int i = 0; i < 12; i++) {
            sleepQuiet(2500);
            if (isOursUp(port)) {
                return true;
            }
        }
        // Пароль отменили/не ввели — грузимся без привилегий (TUN не встанет, остальное работает).
        log.info("Continuing without elevation");
        return false;
    }

    // ---- already running ----

    private static boolean isOursUp(int port) {
        try {
            HttpURLConnection c = (HttpURLConnection) URI
                    .create("http://localhost:" + port + "/api/status").toURL().openConnection();
            c.setConnectTimeout(1500);
            c.setReadTimeout(1500);
            if (c.getResponseCode() != 200) {
                return false;
            }
            String body = new String(c.getInputStream().readAllBytes(), StandardCharsets.UTF_8);
            return body.contains("configPath");
        } catch (IOException e) {
            return false;
        }
    }

    private static void openBrowser(int port) {
        try {
            if (Desktop.isDesktopSupported()) {
                Desktop.getDesktop().browse(URI.create("http://localhost:" + port + "/"));
            }
        } catch (Exception e) {
            log.debug("cannot open browser: {}", e.getMessage());
        }
    }

    // ---- elevation ----

    private static boolean isRoot() {
        return "root".equals(System.getProperty("user.name"));
    }

    private static boolean isPackagedApp() {
        String cmd = ProcessHandle.current().info().command().orElse("");
        String os = System.getProperty("os.name", "").toLowerCase(Locale.ROOT);
        if (os.contains("mac")) {
            return cmd.contains(".app/Contents/MacOS/");
        }
        if (os.contains("win")) {
            String low = cmd.toLowerCase(Locale.ROOT);
            return low.endsWith("singboxui.exe");
        }
        return false;
    }

    /** Есть ли tun в конфиге, который будет использован (дефолт содержит tun). */
    private static boolean configHasTun(String[] args) {
        Path cfg = null;
        for (String a : args) {
            if (a.startsWith("--singbox.config-path=")) {
                cfg = Path.of(a.substring("--singbox.config-path=".length()));
            }
        }
        if (cfg == null) {
            Path cwd = Path.of("").toAbsolutePath();
            cfg = Files.isWritable(cwd)
                    ? cwd.resolve("config.json")
                    : Path.of(System.getProperty("user.home"), ".singboxui", "config.json");
        }
        if (!Files.exists(cfg)) {
            return true; // свежего конфига нет — создастся дефолт с tun
        }
        try {
            String text = Files.readString(cfg);
            return Pattern.compile("\"type\"\\s*:\\s*\"tun\"").matcher(text).find();
        } catch (IOException e) {
            return false;
        }
    }

    private static boolean tryRelaunchElevated(String[] args) {
        Optional<String> cmd = ProcessHandle.current().info().command();
        if (cmd.isEmpty()) {
            return false;
        }
        List<String> appArgs = new ArrayList<>();
        ProcessHandle.current().info().arguments().ifPresent(a -> List.of(a).forEach(appArgs::add));
        String os = System.getProperty("os.name", "").toLowerCase(Locale.ROOT);
        try {
            if (os.contains("mac")) {
                String script = "SINGBOXUI_ELEVATED=1 exec " + q(cmd.get()) + argsStr(appArgs);
                new ProcessBuilder("osascript", "-e",
                        "do shell script " + appleQ(script) + " with administrator privileges")
                        .start();
                log.info("Relaunching elevated via system password dialog");
                return true;
            }
            if (os.contains("win")) {
                String argList = String.join(" ", appArgs.stream().map(PrivilegeEscalation::winQ).toList());
                String ps = "Start-Process " + winQ(cmd.get()) + " -ArgumentList " + winQ(argList)
                        + " -Verb RunAs";
                new ProcessBuilder("powershell", "-NoProfile", "-Command", ps).start();
                log.info("Relaunching elevated via UAC");
                return true;
            }
        } catch (IOException e) {
            log.debug("elevated relaunch failed: {}", e.getMessage());
        }
        return false;
    }

    private static String argsStr(List<String> args) {
        StringBuilder sb = new StringBuilder();
        for (String a : args) {
            sb.append(' ').append(q(a));
        }
        return sb.toString();
    }

    private static String q(String s) {
        return "'" + s.replace("'", "'\\''") + "'";
    }

    private static String appleQ(String s) {
        return "\"" + s.replace("\\", "\\\\").replace("\"", "\\\"") + "\"";
    }

    private static String winQ(String s) {
        return "'" + s.replace("'", "''") + "'";
    }

    private static int parsePort(String[] args, int def) {
        for (String a : args) {
            if (a.startsWith("--server.port=")) {
                try {
                    return Integer.parseInt(a.substring("--server.port=".length()));
                } catch (NumberFormatException ignored) {
                }
            }
        }
        return def;
    }

    private static void sleepQuiet(long ms) {
        try {
            Thread.sleep(ms);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
        }
    }
}
