package com.larffxx.singboxui.service;

import com.larffxx.singboxui.config.SingBoxProperties;
import org.springframework.stereotype.Service;

import java.io.BufferedReader;
import java.io.IOException;
import java.io.InputStreamReader;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.Deque;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.TimeUnit;

/** Starts/stops the sing-box binary with the current config, keeps a log tail. */
@Service
public class SingBoxProcessService {

    private static final int MAX_LINES = 1000;

    private final SingBoxProperties props;
    private final SingBoxConfigService configs;
    private final SingBoxProvisioningService provisioning;

    private Process process;
    private long startedAt;
    private int lastExit = -1;
    private final Deque<String> logBuf = new ArrayDeque<>();

    public SingBoxProcessService(SingBoxProperties props, SingBoxConfigService configs,
                                 SingBoxProvisioningService provisioning) {
        this.props = props;
        this.configs = configs;
        this.provisioning = provisioning;
    }

    public synchronized Map<String, Object> status() {
        boolean running = process != null && process.isAlive();
        Map<String, Object> m = new LinkedHashMap<>();
        m.put("running", running);
        m.put("pid", running ? process.pid() : -1);
        m.put("uptimeSec", running ? (System.currentTimeMillis() - startedAt) / 1000 : 0);
        m.put("lastExit", lastExit);
        String bin = provisioning.resolveBinary();
        m.put("binary", bin == null ? "(не найден)" : bin);
        m.put("configPath", Path.of(props.getConfigPath()).toAbsolutePath().toString());
        return m;
    }

    public synchronized Map<String, Object> start() {
        if (process != null && process.isAlive()) {
            throw new IllegalArgumentException("already running (pid " + process.pid() + ")");
        }
        List<String> errors = configs.validateCurrent();
        if (!errors.isEmpty()) {
            throw new IllegalArgumentException("config invalid: " + String.join("; ", errors));
        }
        Path cfg = Path.of(props.getConfigPath()).toAbsolutePath();
        if (!Files.exists(cfg)) {
            throw new IllegalArgumentException("config file not found: " + cfg);
        }
        String bin = provisioning.resolveBinary();
        if (bin == null) {
            throw new IllegalArgumentException("sing-box не найден — скачай его кнопкой во вкладке «Запуск»");
        }
        try {
            ProcessBuilder pb = new ProcessBuilder(bin, "run", "-c", cfg.toString())
                    .redirectErrorStream(true);
            if (cfg.getParent() != null) {
                pb.directory(cfg.getParent().toFile());
            }
            appendLog("$ " + String.join(" ", pb.command()));
            process = pb.start();
            startedAt = System.currentTimeMillis();
            lastExit = -1;
            drain(process);
            Map<String, Object> m = new LinkedHashMap<>();
            m.put("running", true);
            m.put("pid", process.pid());
            return m;
        } catch (IOException e) {
            process = null;
            appendLog("start failed: " + e.getMessage());
            throw new IllegalArgumentException("cannot start " + bin + ": " + e.getMessage());
        }
    }

    public synchronized Map<String, Object> stop() {
        if (process == null || !process.isAlive()) {
            process = null;
            return Map.of("running", false);
        }
        long pid = process.pid();
        process.destroy();
        try {
            if (!process.waitFor(5, TimeUnit.SECONDS)) {
                process.destroyForcibly();
                process.waitFor(5, TimeUnit.SECONDS);
            }
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            process.destroyForcibly();
        }
        try {
            lastExit = process.exitValue();
        } catch (IllegalThreadStateException e) {
            lastExit = -1;
        }
        appendLog("stopped pid " + pid + " (exit " + lastExit + ")");
        process = null;
        Map<String, Object> m = new LinkedHashMap<>();
        m.put("running", false);
        m.put("exit", lastExit);
        return m;
    }

    public synchronized Map<String, Object> restart() {
        stop();
        return start();
    }

    public List<String> logs(int lines) {
        synchronized (logBuf) {
            List<String> all = new ArrayList<>(logBuf);
            if (lines <= 0 || lines >= all.size()) {
                return all;
            }
            return all.subList(all.size() - lines, all.size());
        }
    }

    private void drain(Process p) {
        Thread t = new Thread(() -> {
            try (BufferedReader r = new BufferedReader(
                    new InputStreamReader(p.getInputStream(), StandardCharsets.UTF_8))) {
                String line;
                while ((line = r.readLine()) != null) {
                    appendLog(line);
                }
            } catch (IOException ignored) {
            }
            synchronized (SingBoxProcessService.this) {
                if (SingBoxProcessService.this.process == p) {
                    try {
                        lastExit = p.exitValue();
                    } catch (IllegalThreadStateException e) {
                        lastExit = -1;
                    }
                    appendLog("process exited (" + lastExit + ")");
                    SingBoxProcessService.this.process = null;
                }
            }
        }, "singbox-log-drain");
        t.setDaemon(true);
        t.start();
    }

    private void appendLog(String line) {
        // sing-box красит вывод даже в pipe — режем ANSI, иначе в UI мусор
        String clean = line.replaceAll("\u001B\\[[;\\d]*m", "");
        synchronized (logBuf) {
            logBuf.addLast(clean);
            while (logBuf.size() > MAX_LINES) {
                logBuf.removeFirst();
            }
        }
    }
}
