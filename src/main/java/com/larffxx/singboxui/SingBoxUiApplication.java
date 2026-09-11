package com.larffxx.singboxui;

import com.larffxx.singboxui.tray.PrivilegeEscalation;
import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;

@SpringBootApplication
public class SingBoxUiApplication {

    public static void main(String[] args) {
        // Двойной клик: уже запущено -> открыть браузер; нужен root для TUN -> перезапуск с повышением.
        if (PrivilegeEscalation.handleStartup(args)) {
            return;
        }
        // headless=false: иначе Spring гасит AWT и иконка трея не встанет
        SpringApplication app = new SpringApplication(SingBoxUiApplication.class);
        app.setHeadless(false);
        app.run(args);
    }

}
