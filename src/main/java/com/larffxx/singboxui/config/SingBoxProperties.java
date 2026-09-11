package com.larffxx.singboxui.config;

import org.springframework.boot.context.properties.ConfigurationProperties;

import java.nio.file.Files;
import java.nio.file.Path;

@ConfigurationProperties(prefix = "singbox")
public class SingBoxProperties {
    /** Явный путь/имя бинарника. Пусто = авто: ./bin → PATH → скачать. */
    private String binary = "";
    private boolean autoInstall = true;
    private String configPath = "./config.json";
    /** Clash API sing-box для мониторинга трафика. */
    private String clashUrl = "http://127.0.0.1:9090";

    public String getConfigPath() { return resolve(configPath); }
    public void setConfigPath(String configPath) { this.configPath = configPath; }
    public String getBinary() { return binary; }
    public void setBinary(String binary) { this.binary = binary; }
    public boolean isAutoInstall() { return autoInstall; }
    public void setAutoInstall(boolean autoInstall) { this.autoInstall = autoInstall; }
    public String getClashUrl() { return clashUrl; }
    public void setClashUrl(String clashUrl) { this.clashUrl = clashUrl; }

    /**
     * Папка данных: текущая, если доступна для записи, иначе ~/.singboxui.
     * Нужно для запуска из .app (там CWD=/).
     */
    public Path dataDir() {
        Path cwd = Path.of("").toAbsolutePath();
        if (Files.isWritable(cwd)) {
            return cwd;
        }
        Path home = Path.of(System.getProperty("user.home"), ".singboxui");
        try {
            Files.createDirectories(home);
        } catch (Exception ignored) {
        }
        return home;
    }

    public Path resolveBin(String name) {
        return dataDir().resolve("bin").resolve(name);
    }

    private String resolve(String p) {
        Path q = Path.of(p);
        if (q.isAbsolute()) {
            return p;
        }
        return dataDir().resolve(q).toString();
    }
}
