# UNNAMED PTERO

**A Windows-local Minecraft server control panel.** Create and manage Minecraft servers on your own PC through a polished, Pterodactyl-inspired web dashboard.

> UNNAMED PTERO is an independent Windows-first application. It is not a Windows port of Pterodactyl Panel or Wings.

## Highlights

- **Local dashboard:** Go serves an embedded HTML/CSS/JavaScript interface at `http://127.0.0.1:8787`.
- **Minecraft server types:** Vanilla, Paper, Purpur and Fabric, with provider-specific version catalogues.
- **Storage selection:** choose a Windows drive or folder for each server. The app detects drive capacity and creates a dedicated server directory.
- **Console:** start, stop, restart, send commands and view live process output. Manually stopping a server clears the panel console view after Java exits; Minecraft's own log files remain untouched.
- **File manager:** browse folders; create folders; upload, download, edit supported text files, rename and delete files.
- **Backups:** create, download, list and restore ZIP backups while the server is stopped.
- **Schedules:** configure repeating `save-all`, restart or custom console-command tasks while the app is running.
- **Startup:** configure Java heap memory and additional JVM arguments.
- **Network:** view the game port and local IPv4 addresses for LAN play.
- **Settings and activity:** edit server metadata and inspect local operational history.
- **Managed Java:** download/manage Java runtimes for server versions that require them.
- **Single executable:** dashboard assets are embedded; no Node.js, Docker, PHP, MariaDB or Redis is needed to run the app.

## Run on Windows

### Requirements

- Windows 10 or Windows 11, 64-bit
- Internet connection for downloading Minecraft server software and Java runtimes

### Start the application

1. Download or clone this repository.
2. Open the project folder.
3. Double-click **`start-unnamed-ptero.bat`**.
4. Your browser opens `http://127.0.0.1:8787`.
5. Keep the application console window open while using the dashboard.

You can also run `dist/UnnamedPtero.exe` directly. Closing the application asks managed Minecraft servers to stop gracefully.

## Build from source

### Requirements

- Go 1.23 or newer
- PowerShell

Run in PowerShell from the repository root:

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\build.ps1
```

The build script runs Go tests and `go vet`, then creates the Windows x64 executable at `dist/UnnamedPtero.exe`.

To run from source:

```powershell
cd backend
go run ./cmd/unnamedptero
```

Then open `http://127.0.0.1:8787`.

## Architecture

```text
Browser dashboard (embedded HTML / CSS / JavaScript)
                     │ HTTP / JSON
                     ▼
             Go local HTTP API
              (127.0.0.1 only)
                     │
        ┌────────────┼─────────────┐
        ▼            ▼             ▼
  Server manager  File manager  System services
  Java lifecycle  Safe paths    Drives / Java
  Console / tasks Upload/edit   Folder picker
        └────────────┼─────────────┘
                     ▼
             Local JSON metadata
                     │
                     ▼
          Native Windows Java processes
```

## Data locations

For compatibility with earlier preview builds, the application keeps its existing data root:

- Metadata: `%LOCALAPPDATA%\LocalPtero\servers.json`
- Managed Java runtimes: `%LOCALAPPDATA%\LocalPtero\runtimes\`
- Server files: the per-server directory created in the folder selected during server creation
- Backups: `<server-folder>\backups\`

The branding and executable are named **UNNAMED PTERO**, but the legacy `LocalPtero` data directory is intentionally retained so existing installations do not lose track of their servers.

## Supported sections

| Section | Local implementation |
|---|---|
| Console | Process controls, live output and commands |
| Files | Directory browser, upload/download, rename, delete and text editing |
| Backups | ZIP backup and restore |
| Schedules | Repeating save, restart and command tasks while app is open |
| Network | Game port and local address information |
| Startup | Java memory and additional JVM arguments |
| Settings | Server name and game port |
| Activity | Local server and file operation history |
| Databases | Not provisioned by this single-user edition |
| Users | Separate panel user accounts are not part of this single-user edition |

## Security and limitations

- **Local-only control panel:** the API binds to `127.0.0.1`. Do not expose it to the public internet.
- **Game servers run as your Windows user:** server JARs, mods and plugins inherit that account's permissions. The application does not provide Docker-equivalent isolation. Only run software you trust.
- **LAN connectivity:** Minecraft may accept LAN connections depending on `server.properties` and Windows Firewall. This does not expose the control panel itself.
- **Schedules:** tasks run only while UNNAMED PTERO remains open.
- **Backups/restores:** stop the server before creating or restoring backups. Keep independent copies of important worlds.
- **Databases and multi-user accounts:** this local single-user edition does not provision SQL servers or separate panel users.
- **Resource telemetry:** configured memory and process state are shown; detailed measured per-process CPU/RAM graphs are not yet included.
- **Future work:** Forge/NeoForge, more game families, stronger process isolation, Windows service integration and signed automatic updates.

## Project structure

```text
.
├── backend/
│   ├── cmd/unnamedptero/       # App entry point and embedded dashboard
│   ├── internal/api/           # Local HTTP API
│   ├── internal/server/        # Minecraft installation and process lifecycle
│   ├── internal/panel/         # Server file and backup operations
│   ├── internal/store/         # Local metadata persistence
│   └── internal/system/        # Windows drive, folder and Java helpers
├── dist/                       # Windows executable
├── build.ps1                   # Build and test script
└── start-unnamed-ptero.bat     # Branded Windows launcher
```

## License

This repository contains the UNNAMED PTERO application code and branding assets. Check third-party Minecraft software, Java runtime and provider terms before redistribution. Pterodactyl is a separate project: <https://github.com/pterodactyl/panel>.