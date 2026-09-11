@echo off
rem SingBoxUI launcher (Windows). Needs Java 17+.
cd /d %~dp0

where java >nul 2>nul
if errorlevel 1 (
  echo Java not found. Install Temurin/OpenJDK 17+: https://adoptium.net/
  pause
  exit /b 1
)

set JAR=
if exist singbox-ui.jar set JAR=singbox-ui.jar
if not defined JAR if exist target\singbox-ui.jar set JAR=target\singbox-ui.jar
for %%f in (singbox-ui-*.jar) do set JAR=%%f
for %%f in (target\singbox-ui-*.jar) do set JAR=%%f

if not defined JAR (
  echo singbox-ui.jar not found. Build it first: mvnw.cmd package
  pause
  exit /b 1
)

if not defined PORT set PORT=8080

rem TUN needs Administrator: if config uses tun and we're not elevated - relaunch via UAC.
if not defined SINGBOXUI_ELEVATED (
  if exist config.json (
    powershell -NoProfile -Command "if ((Get-Content config.json -Raw) -match '\"type\"\s*:\s*\"tun\"') { exit 0 } else { exit 1 }" >nul 2>&1
    if not errorlevel 1 (
      NET SESSION >nul 2>&1
      if errorlevel 1 (
        echo TUN needs Administrator - relaunching elevated...
        if defined PORT (set "FWD=--server.port=%PORT% %*") else (set "FWD=%*")
        powershell -NoProfile -Command "Start-Process -FilePath '%~f0' -ArgumentList '%FWD%' -Verb RunAs -WorkingDirectory '%CD%'"
        exit /b
      )
    )
  )
)

echo Starting SingBoxUI (%JAR%) on http://localhost:%PORT% ...
java -jar "%JAR%" --server.port=%PORT% %*
pause
