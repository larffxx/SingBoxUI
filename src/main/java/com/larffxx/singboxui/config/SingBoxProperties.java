package com.larffxx.singboxui.config;

import org.springframework.boot.context.properties.ConfigurationProperties;

@ConfigurationProperties(prefix = "singbox")
public class SingBoxProperties {
    /** Явный путь/имя бинарника. Пусто = авто: ./bin → PATH → скачать. */
    private String binary = "";
    private boolean autoInstall = true;
    private String configPath = "./config.json";
    /** Clash API sing-box для мониторинга трафика. */
    private String clashUrl = "http://127.0.0.1:9090";

    public String getConfigPath() { return configPath; }
    public void setConfigPath(String configPath) { this.configPath = configPath; }
    public String getBinary() { return binary; }
    public void setBinary(String binary) { this.binary = binary; }
    public boolean isAutoInstall() { return autoInstall; }
    public void setAutoInstall(boolean autoInstall) { this.autoInstall = autoInstall; }
    public String getClashUrl() { return clashUrl; }
    public void setClashUrl(String clashUrl) { this.clashUrl = clashUrl; }
}
