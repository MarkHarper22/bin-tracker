# Bin Tracker

A self-contained storage-bin inventory app for Mac and Windows. It tracks where each bin is, what's inside, and who changed what. It scans QR codes and barcodes (handheld scanners, phone cameras, webcams) and prints labels on sheet labels or thermal label printers.

It's one executable with no installer and no internet or cloud. Data lives in a SQLite file next to the app.

End-user instructions are in [READ ME.txt](READ ME.txt), which ships inside each download.

## Features

- **Bins**: code, name, location (free text), notes, and itemized contents with quantities. Adding an item that already exists increases its quantity.
- **Search** across codes, names, locations, notes, and item names, with matching items highlighted.
- **Scanning**
  - Handheld USB/Bluetooth scanners work on every screen (fast keystroke bursts ending in Enter).
  - Phone and webcam scanning in the browser uses the native `BarcodeDetector` when available. Otherwise frames go to the server and are decoded in Go.
  - "Take a photo" fallback works on plain HTTP.
  - Unknown codes offer to create a bin, so pre-printed labels can be set up by scanning.
  - "Move bins" mode sets a location, then each scan moves a bin there.
- **Labels**: QR code or Code 128 barcode. Presets for Avery 5160/5163/5164 sheets, Dymo, Brother DK, and 2×1 / 3×2 / 4×6 thermal rolls. Also custom sizes, copies, skipping used labels, rotation, print offset, and pre-printing new codes.
- **History log** of every change, with a per-device name (no logins).
- **Automatic backups** (`VACUUM INTO`) on a schedule, only when data changed, with retention and one-click restore. A safety backup is made before every restore.
- **Multiple devices at once** on the local network: HTTP on port 8420 for the host computer, HTTPS on 8421 (self-signed certificate) so phones can use the camera.

## Build

Requires Go 1.27+. This machine has it at `~/sdk/go/bin`; `build.sh` finds it automatically.

```bash
./build.sh
```

Output in `dist/`:

- `BinTracker-<version>-Mac.zip`: universal binary (Intel + Apple Silicon)
- `BinTracker-<version>-Windows.zip`: `BinTracker.exe` (x64)

Everything is pure Go (`CGO_ENABLED=0`), so both platforms cross-compile from one Mac.

## Docker

The same app also runs as a server container. The web app is on port 8420, HTTPS for phone cameras is on 8421, and data lives in the `/data` volume.

### Published image

GitHub Actions ([.github/workflows/docker.yml](.github/workflows/docker.yml)) tests the code, then builds and publishes `ghcr.io/markharper22/bin-tracker` for x86 and ARM:

- every push to `main` publishes `:latest` and `:sha-<commit>`
- a tag like `v1.0.1` publishes `:1.0.1` and `:1.0`

The image is private, so log in on the Docker host once. Use your GitHub username and a personal access token (classic) with the `read:packages` scope:

```bash
docker login ghcr.io
```

Then, from a folder containing `docker-compose.yml`:

```bash
docker compose up -d
```

To update later: `docker compose pull && docker compose up -d`.

### Build it yourself

```bash
docker compose up -d --build
```

Then open `http://<server-address>:8420` from any computer on the network. **Settings → Connect a phone** shows the QR code for phones.

Without Compose:

```bash
docker build -t bintracker .
docker run -d --name bintracker --restart unless-stopped -p 8420:8420 -p 8421:8421 -v bintracker-data:/data bintracker
```

Multi-architecture image, e.g. for a Raspberry Pi or ARM NAS:

```bash
docker buildx build --platform linux/amd64,linux/arm64 -t <registry>/bintracker:1.0.0 --push .
```

The image is built from `golang:1.27-alpine` and runs on `gcr.io/distroless/static-debian13:nonroot`: no shell, non-root user, about 15 MB.

| Variable | Image default | Purpose |
| --- | --- | --- |
| `BINTRACKER_DATA` | `/data` | Database, backups, HTTPS certificate |
| `BINTRACKER_PORT` | `8420` | HTTP port; HTTPS uses this + 1 |
| `BINTRACKER_SERVER` | `true` | Headless: no browser, no Stop button, no keypress prompts |
| `BINTRACKER_PHONE_URL` | automatic | Link phones should open, e.g. `https://192.168.1.50:8421` or `https://bins.example.com` |
| `TZ` | `UTC` | Time zone for backup file names |

- **Phone link.** A container can't see the host's network address, so Settings builds the phone link from the address you opened the web app with. Open it by the server's IP or hostname (not `localhost`), and publish the HTTPS port as the same number on both sides. Otherwise, set `BINTRACKER_PHONE_URL`.
- **Reverse proxy with a real certificate** (Caddy, Traefik, nginx). Proxy to port 8420 only. Phones then get camera access through the proxy's HTTPS without a certificate warning. Allow request bodies up to 15 MB, since photo scans are uploaded. Serve the app at the root of a host name; sub-paths like `/bins` aren't supported.
- **No logins.** Anyone who can reach the port can change the inventory. Keep it on your local network, or add authentication in the reverse proxy before exposing it further.
- **Bind mounts.** The container runs as UID 65532. If you mount a host folder instead of a named volume, give that user access (`sudo chown -R 65532:65532 ./data`), or set `user: "<uid>:<gid>"` to match the folder's owner.
- **Health and backups.** `HEALTHCHECK` runs `/bintracker -healthcheck`. `docker stop` shuts the app down cleanly. Automatic backups go to `/data/backups`; include the volume in your server's own backups too.

## Develop

```bash
go run . -data ./dev-data        # opens http://localhost:8420
go test ./...
```

Flags (each also reads the environment variable in brackets): `-port N` [`BINTRACKER_PORT`] (HTTPS uses N+1), `-data DIR` [`BINTRACKER_DATA`], `-server` [`BINTRACKER_SERVER`], `-phone-url URL` [`BINTRACKER_PHONE_URL`], `-no-browser`, `-healthcheck`.

The web UI in `web/` is plain HTML/CSS/ES modules embedded with `go:embed`, so there's no JS build step. Restart `go run` after editing web files.

## Layout

| File | Purpose |
| --- | --- |
| `main.go` | Startup: flags, data folder, listeners, console banner, opens browser |
| `config.go` | Environment variables and phone URL parsing |
| `Dockerfile`, `docker-compose.yml` | Server container image and example deployment |
| `db.go` | SQLite schema and all bin/item/history/settings queries |
| `api.go` | JSON API routes and cross-site write protection |
| `backup.go` | Scheduled backups, retention, restore |
| `codes.go` | QR/Code 128 SVG rendering and image decoding |
| `tls.go` | Self-signed certificate and LAN address detection |
| `web/app.js` | Hash router |
| `web/lib.js` | API client, templating, dialogs, scanner handling |
| `web/views/*.js` | Bins, Scan, Labels, History, and Settings screens |

## Data

`BinTracker-Data/` next to the executable (falls back to the user config folder if that isn't writable):

- `bintracker.db`: SQLite database (WAL mode)
- `backups/`: `bintracker-YYYYMMDD-HHMMSS-<auto|manual|before-restore>.db`
- `https-cert.pem`, `https-key.pem`: regenerated when the LAN address changes or expiry is near
- `bintracker.log`
