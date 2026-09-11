package com.larffxx.singboxui;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;

@SpringBootApplication
public class SingBoxUiApplication {

    public static void main(String[] args) {
        // headless=false: иначе Spring гасит AWT и иконка трея не встанет
        SpringApplication app = new SpringApplication(SingBoxUiApplication.class);
        app.setHeadless(false);
        app.run(args);
    }

}
