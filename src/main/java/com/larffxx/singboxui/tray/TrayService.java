package com.larffxx.singboxui.tray;

import com.larffxx.singboxui.service.SingBoxProcessService;
import jakarta.annotation.PreDestroy;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.boot.SpringApplication;
import org.springframework.boot.context.event.ApplicationReadyEvent;
import org.springframework.context.ApplicationContext;
import org.springframework.context.event.EventListener;
import org.springframework.stereotype.Service;

import java.awt.Desktop;
import java.awt.Graphics2D;
import java.awt.GraphicsEnvironment;
import java.awt.MenuItem;
import java.awt.PopupMenu;
import java.awt.RenderingHints;
import java.awt.SystemTray;
import java.awt.TrayIcon;
import java.awt.image.BufferedImage;
import java.net.URI;

/**
 * Иконка в меню-баре macOS (и трее других ОС): открыть интерфейс,
 * старт/стоп sing-box, выход. На headless-машинах тихо отключается.
 */
@Service
public class TrayService {

    private static final Logger log = LoggerFactory.getLogger(TrayService.class);

    private final SingBoxProcessService proc;
    private final ApplicationContext ctx;
    private final int port;

    private TrayIcon icon;
    private MenuItem startItem;
    private MenuItem stopItem;
    private Thread watcher;

    public TrayService(SingBoxProcessService proc, ApplicationContext ctx,
                       @Value("${server.port:8080}") int port) {
        this.proc = proc;
        this.ctx = ctx;
        this.port = port;
    }

    @EventListener(ApplicationReadyEvent.class)
    public void init() {
        if (GraphicsEnvironment.isHeadless() || !SystemTray.isSupported()) {
            log.info("Tray not available (headless or unsupported), skipping");
            return;
        }
        try {
            PopupMenu menu = new PopupMenu();

            MenuItem open = new MenuItem("Открыть интерфейс");
            open.addActionListener(e -> browse());
            menu.add(open);
            menu.addSeparator();

            startItem = new MenuItem("Запустить sing-box");
            startItem.addActionListener(e -> {
                try {
                    proc.start();
                    notify("sing-box запущен", TrayIcon.MessageType.INFO);
                } catch (Exception ex) {
                    notify("Не запустился: " + ex.getMessage(), TrayIcon.MessageType.ERROR);
                }
                refresh();
            });
            menu.add(startItem);

            stopItem = new MenuItem("Остановить sing-box");
            stopItem.addActionListener(e -> {
                proc.stop();
                notify("sing-box остановлен", TrayIcon.MessageType.INFO);
                refresh();
            });
            menu.add(stopItem);
            menu.addSeparator();

            MenuItem quit = new MenuItem("Выход");
            quit.addActionListener(e -> {
                SpringApplication.exit(ctx);
                System.exit(0);
            });
            menu.add(quit);

            icon = new TrayIcon(drawIcon(false), "SingBoxUI", menu);
            icon.setImageAutoSize(true);
            SystemTray.getSystemTray().add(icon);

            watcher = new Thread(() -> {
                while (!Thread.currentThread().isInterrupted()) {
                    try {
                        Thread.sleep(3000);
                        refresh();
                    } catch (InterruptedException ie) {
                        Thread.currentThread().interrupt();
                    }
                }
            }, "tray-watcher");
            watcher.setDaemon(true);
            watcher.start();

            refresh();
            log.info("Tray icon installed");
        } catch (Exception e) {
            log.warn("Cannot install tray icon: {}", e.getMessage());
        }
    }

    private void refresh() {
        if (icon == null) {
            return;
        }
        boolean running;
        try {
            running = Boolean.TRUE.equals(proc.status().get("running"));
        } catch (Exception e) {
            running = false;
        }
        icon.setImage(drawIcon(running));
        icon.setToolTip(running ? "SingBoxUI — sing-box запущен" : "SingBoxUI — остановлен");
        startItem.setEnabled(!running);
        stopItem.setEnabled(running);
    }

    private void browse() {
        try {
            Desktop.getDesktop().browse(URI.create("http://localhost:" + port + "/"));
        } catch (Exception e) {
            notify("Не открыть браузер: " + e.getMessage(), TrayIcon.MessageType.ERROR);
        }
    }

    private void notify(String text, TrayIcon.MessageType type) {
        if (icon != null) {
            icon.displayMessage("SingBoxUI", text, type);
        }
    }

    /** Круглая иконка: тёмный фон, точка состояния (зелёная = VPN запущен). */
    private BufferedImage drawIcon(boolean running) {
        int s = 64;
        BufferedImage img = new BufferedImage(s, s, BufferedImage.TYPE_INT_ARGB);
        Graphics2D g = img.createGraphics();
        try {
            g.setRenderingHint(RenderingHints.KEY_ANTIALIASING, RenderingHints.VALUE_ANTIALIAS_ON);
            g.setColor(new java.awt.Color(0x22, 0x22, 0x22, 0xFF));
            g.fillOval(4, 4, s - 8, s - 8);
            g.setColor(java.awt.Color.WHITE);
            g.drawOval(10, 10, s - 20, s - 20);
            g.setColor(running ? new java.awt.Color(0x3F, 0xB9, 0x50) : new java.awt.Color(0x8B, 0x94, 0x9E));
            int r = 14;
            g.fillOval(s / 2 - r / 2, s / 2 - r / 2, r, r);
        } finally {
            g.dispose();
        }
        return img;
    }

    @PreDestroy
    public void remove() {
        if (watcher != null) {
            watcher.interrupt();
        }
        if (icon != null) {
            try {
                SystemTray.getSystemTray().remove(icon);
            } catch (Exception ignored) {
            }
        }
    }
}
