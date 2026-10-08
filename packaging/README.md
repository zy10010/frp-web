# frp-manager

A self-contained web management panel for [frp](https://github.com/fatedier/frp).
It lets you configure every frp setting from a browser, supervise the `frps`
(server) and `frpc` (client) processes, and update frp in place — with optional
GitHub mirror acceleration.

## Features

- **Full visual configuration** — every `frps.toml` / `frpc.toml` setting is
  exposed as a form, generated directly from frp's own configuration types, so
  it always stays in sync with the frp version you run. This includes the web
  panel port/password, auth token, transports, TLS, logging, load balancing,
  health checks, and every proxy type (`tcp`, `udp`, `http`, `https`, `tcpmux`,
  `stcp`, `xtcp`, `sudp`) and visitor type.
- **Server + client in one place** — the same panel shows the server (frps)
  settings and the client (frpc) settings, with start/stop/restart and live log
  tail for each.
- **One-click update** — checks GitHub for the latest release, auto-detects the
  running OS/architecture, downloads and installs the matching binary (with a
  `.bak` backup), then restarts the affected processes.
- **GitHub mirror** — optionally set a mirror prefix (e.g. `https://ghproxy.com/`)
  to accelerate downloads; leave it empty to download directly.
- **One-click install** — `install.sh` (systemd: Debian/Ubuntu/CentOS) and
  `install-alpine.sh` (OpenRC: Alpine) with a boot auto-start choice.
- **Adjust later** — everything (including the web port and password) can be
  changed from the web UI after installation.

## Build

Requires Go 1.25+ (the frp module toolchain will be fetched automatically).

```bash
# manager only, for the current platform
make -C "$(pwd)" frp-manager        # not wired into frp's Makefile
# or directly:
go build -o frp-manager ./cmd/frp-manager
```

Build a distributable release tarball:

```bash
cd packaging
./build-release.sh                    # current platform
./build-release.sh --os linux --arch arm64
./build-release.sh --bundle-frp       # also bundle frps/frpc for offline install
```

The tarball is written to `release/frp-manager-<version>-<os>-<arch>.tar.gz` and
contains `frp-manager`, `install.sh` and `install-alpine.sh` (and `frps`/`frpc`
when `--bundle-frp` is used).

### Build in GitHub Actions (no server needed)

You don't need your own build server. Go cross-compiles, so GitHub Actions can
build the release for every platform. A ready-to-use workflow is included at
`.github/workflows/release.yml`: push a `v*` tag (or run it manually) and it
builds `frp-manager` + `frps`/`frpc` for all Linux architectures, macOS and
Windows, then publishes the tarballs as a GitHub Release.

## Install

### Debian / Ubuntu / any systemd Linux

```bash
tar xzf frp-manager-1.0.0-linux-amd64.tar.gz
cd frp-manager-1.0.0-linux-amd64   # or wherever you extracted it
sudo bash install.sh \
  --port 7500 \
  --password 'change-me' \
  --mirror 'https://ghproxy.com/' \
  --autostart yes
```

### Alpine Linux (OpenRC)

```bash
sudo sh install-alpine.sh --port 7500 --password 'change-me' --autostart yes
```

`install.sh` downloads the official frp release (frps/frpc) from GitHub unless
`--skip-download` is given and binaries are bundled next to the script. It then
writes minimal `frps.toml`/`frpc.toml`, registers the boot service, and starts
the panel.

Install options (both scripts):

| Option | Default | Meaning |
| --- | --- | --- |
| `--dir PATH` | `/opt/frp` | Install directory |
| `--port PORT` | `7500` | Web panel port |
| `--addr ADDR` | `0.0.0.0` | Web panel bind address |
| `--user NAME` | `admin` | Web panel username |
| `--password PASS` | random | Web panel password |
| `--mirror URL` | empty | GitHub mirror prefix |
| `--bind-port PORT` | `7000` | frps bind port |
| `--token TOKEN` | random | frp auth token |
| `--server-addr ADDR` | `127.0.0.1` | frpc server address |
| `--frp-version VER` | `v0.71.0` | frp release to install |
| `--autostart yes/no` | `yes` | Install & enable the boot service |
| `--frps-autostart yes/no` | `yes` | Start frps when the manager boots |
| `--frpc-autostart yes/no` | `no` | Start frpc when the manager boots |
| `--manager-url URL` | — | Download frp-manager instead of using bundled |
| `--skip-download` | — | Use bundled frps/frpc (offline install) |
| `-y` | — | Non-interactive (accept defaults) |

## Usage

Open `http://<server-ip>:<port>` and sign in. The default password is printed at
the end of the install (or set with `--password`).

- **Dashboard** — status, versions, platform, start/stop/restart.
- **Server (frps)** / **Client (frpc)** — edit every setting and each proxy,
  then **Save & Apply** (restarts the process) or **Save only**.
- **Updates** — check for a new release and apply it (downloads the matching
  binary for your OS/arch, backs up the old one, restarts processes).
- **Settings** — change the web panel address/port/username/password, binary and
  config paths, the GitHub mirror, and auto-start flags.

## Configuration files

| File | Purpose |
| --- | --- |
| `frp-manager.toml` | Manager settings (web panel, paths, mirror, autostart) |
| `frps.toml` | frps server configuration |
| `frpc.toml` | frpc client configuration (proxies + visitors) |

## Notes & limitations

- The manager supervises frps/frpc as child processes and restarts them when you
  save a configuration.
- Proxy/visitor **plugins** (`http_proxy`, `socks5`, `static_file`,
  `unix_domain_socket`, `tls2raw`, `virtual_net`, …) are fully visualized as
  forms too — select a plugin type and its fields appear.
- The web panel is password-protected with an HTTP session cookie. Restrict
  network access to the panel port (e.g. firewall) for production use.
- The mirror setting is a URL prefix (`https://mirror.example/`); the manager
  requests `{mirror}{github-url}` and falls back to a direct request if the
  mirror fails.
