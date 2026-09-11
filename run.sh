#!/bin/sh
# SingBoxUI launcher (macOS / Linux). Needs Java 17+.
cd "$(dirname "$0")" || exit 1

if ! command -v java >/dev/null 2>&1; then
  echo "Java not found. Install Temurin/OpenJDK 17+: https://adoptium.net/"
  exit 1
fi

JAR=""
for f in singbox-ui.jar target/singbox-ui.jar singbox-ui-*.jar target/singbox-ui-*.jar; do
  if [ -f "$f" ]; then JAR="$f"; break; fi
done
if [ -z "$JAR" ]; then
  echo "singbox-ui.jar not found. Build it first: ./mvnw package"
  exit 1
fi

PORT="${PORT:-8000}"

# TUN + auto_route требуют рута: если в конфиге есть tun и мы не root — перезапуск через sudo.
if [ -z "$SINGBOXUI_ELEVATED" ] && [ -f ./config.json ] \
    && grep -q '"type" *: *"tun"' ./config.json 2>/dev/null \
    && [ "$(id -u)" -ne 0 ]; then
  if command -v sudo >/dev/null 2>&1; then
    echo "TUN needs root — re-running with sudo..."
    exec sudo -E SINGBOXUI_ELEVATED=1 "$0" "$@"
  else
    echo "WARNING: no sudo found; TUN will fail without root. Continuing anyway."
  fi
fi

echo "Starting SingBoxUI ($JAR) on http://localhost:$PORT ..."
# apple.awt.UIElement: без иконки в Dock, только меню-бар (macOS)
exec java -Dapple.awt.UIElement=true -jar "$JAR" --server.port="$PORT" "$@"
