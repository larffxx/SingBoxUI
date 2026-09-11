package com.larffxx.singboxui.service;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.boot.context.event.ApplicationReadyEvent;
import org.springframework.context.event.EventListener;
import org.springframework.stereotype.Component;

/** Автоподключение VPN при старте приложения (если включено в настройках). */
@Component
public class StartupTasks {

    private static final Logger log = LoggerFactory.getLogger(StartupTasks.class);

    private final SettingsService settings;
    private final SingBoxProcessService proc;

    public StartupTasks(SettingsService settings, SingBoxProcessService proc) {
        this.settings = settings;
        this.proc = proc;
    }

    @EventListener(ApplicationReadyEvent.class)
    public void autoConnect() {
        if (!settings.autoConnect()) {
            return;
        }
        try {
            Thread.sleep(2000);
            proc.start();
            log.info("Auto-connected sing-box on startup");
        } catch (Exception e) {
            log.warn("Auto-connect failed: {}", e.getMessage());
        }
    }
}
