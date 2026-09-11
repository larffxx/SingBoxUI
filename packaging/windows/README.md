# Windows installer

Wails v2 generates the installer from an NSIS project it renders into
`build/windows/installer/project.nsi`, so the installer definition is not
duplicated here — it is produced by the build and owned by the build
workstream.

## What to configure in `wails.json` / `build/windows/`

| Concern             | Where                                   | Value for this app          |
| ------------------- | --------------------------------------- | --------------------------- |
| Webview2 install    | `build/windows/installer/project.nsi`   | `-webview2 embed` / downloader |
| Installer language  | `project.nsi`                           | English                     |
| Per-user vs machine | `project.nsi` (`MANUP`)                 | per-user (no UAC at install)|
| Install directory   | `project.nsi`                           | `$LOCALAPPDATA\SingBoxUI`   |

**Do not** add the `webview2runtimecheck` build tag: the runtime is handled by
the installer, not by build tags, and the CLI defaults are sufficient.

## Signing

Signing happens after the build (`scripts/package-windows.ps1`) so that both the
application `.exe` and the NSIS `*-setup.exe` are Authenticode-signed:

```
signtool sign /fd SHA256 /f <pfx> /p <password> /tr http://timestamp.digicert.com /td SHA256 <artifact>
```

The `.pfx` is decoded at runtime from the base64 `WINDOWS_CERTIFICATE` secret
into `%TEMP%` and deleted in a `finally` block; it is never written into the
repository, the installer inputs, or the build output.

## Unsigned builds

When `WINDOWS_CERTIFICATE` is unset the script emits an unsigned build and
prints a warning. Installing it triggers SmartScreen's "unknown publisher"
prompt, which is expected for local/dev artifacts.
