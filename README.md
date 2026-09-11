# SingBoxUI

Веб-интерфейс для настройки [sing-box](https://github.com/sagernet/sing-box) ( universal proxy platform ):
редактирование `config.json` через формы (inbounds / outbounds / route / DNS),
правка сырого JSON, импорт share-ссылок (`vless://`, `vmess://`, `trojan://`, `ss://`, `hy2://`, `tuic://`),
импорт/экспорт конфига файлом, проверка через `sing-box check`.

Работает на **Windows** и **macOS** — это обычный Spring Boot uber-jar, нужен только Java 17+.

## Быстрый старт

### macOS

```sh
# 1. Java 17+ (если нет)
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
brew install --cask temurin

# 2. Сборка (или возьми готовый singbox-ui.jar из Releases)
./mvnw package

# 3. Запуск
./run.sh
# открой http://localhost:8000
```

### Windows

```bat
:: 1. Поставь Java 17+ (Temurin): https://adoptium.net/
::    при установке отметь "Add to PATH"

:: 2. Сборка (или возьми готовый singbox-ui.jar)
mvnw.cmd package

:: 3. Запуск (двойной клик или из cmd)
run.bat
:: открой http://localhost:8000
```

> **Права администратора.** TUN-режим требует рута: `run.sh` сам перезапустится
> через `sudo` (пароль спросит один раз), `run.bat` покажет UAC-запрос.
> Если tun в конфиге нет — всё работает из-под обычного пользователя.

Другой порт: `PORT=8888 ./run.sh` или `set PORT=8888 && run.bat`,
либо `java -jar singbox-ui.jar --server.port=8888`.

## sing-box binary (ставить руками не нужно)

Ничего в PATH вписывать не надо: при первом запуске SingBoxUI сам скачает
подходящий sing-box с GitHub в папку `./bin/` рядом с программой и будет им
пользоваться (видно во вкладке «Запуск»: версия + откуда взят). Там же кнопка
«Скачать/Обновить sing-box». Если бинарник уже есть в PATH — подхватится он.

Вручную — только если хочется свой конкретный:

- macOS: `brew install sing-box`
- Windows: скачай `sing-box-windows-amd64.zip` со страницы
  [релизов](https://github.com/SagerNet/sing-box/releases), распакуй и добавь папку в PATH
  (или укажи путь явно — см. ниже).

Если бинарник называется иначе / лежит не в PATH:

```sh
java -jar singbox-ui.jar --singbox.binary="C:\tools\sing-box\sing-box.exe"
java -jar singbox-ui.jar --singbox.binary=/opt/sing-box/sing-box
```

## Иконка в трее (macOS)

При запуске на Маке в меню-баре появляется круглая иконка SingBoxUI
(зелёная точка — sing-box запущен, серая — остановлен). Меню: открыть интерфейс
в браузере, запустить/остановить sing-box, выход. Работает при запуске через
`run.sh` или `java -jar` в обычной GUI-сессии; на headless-сервере тихо отключается.

## Где лежит конфиг
По умолчанию `config.json` создаётся в рабочей папке рядом с jar (стартовый шаблон:
TUN + SOCKS inbounds, direct/block/dns outbounds). Свой путь:

```sh
java -jar singbox-ui.jar --singbox.config-path="C:\vpn\config.json"
```

Запуск самого sing-box с этим конфигом — из вкладки **Запуск** в интерфейсе
(старт/стоп/рестарт + живой лог) или вручную:

```sh
sing-box run -c config.json        # macOS / Linux
sing-box.exe run -c config.json    # Windows
```

## Возможности

- Outbounds: vless/vmess/trojan/shadowsocks/wireguard/hysteria(2)/tuic/anytls/socks/http/ssh/tor/selector/urltest/direct/block/dns (+TLS/Reality/uTLS/transport)
- Inbounds: tun/mixed/socks/http/tproxy/redirect/…
- Сплит-туннелирование: домены / IP / гео-наборы / программы / порты → через VPN или напрямую
- Route-правила, DNS-серверы, Raw JSON с валидацией
- Просмотр JSON любого элемента (кнопка `{ }`), мониторинг трафика снизу:
  общий / через VPN / напрямую (берётся из Clash API sing-box)
- Шаблоны: «TUN + VLESS Reality», «SOCKS + selector», пустой
- REST API: `/api/config`, `/api/inbounds`, `/api/outbounds`, `/api/route/rules`,
  `/api/split/rules`, `/api/proc/start|stop|restart|status|logs`,
  `/api/share/import`, `/api/share/export/{tag}`, `/api/validate`, `/api/check`, `/api/export`
