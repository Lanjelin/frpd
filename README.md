# frpd

A browser dashboard for [frp](https://github.com/fatedier/frp). frpd lists your frpc instances, lets you create, edit, start, stop and delete their proxies, and can show traffic and connection data from frps. It is a single static binary (or container) with no database, aimed at small deployments.

## Features

- Manage several frpc instances from one page through frpc's Store API.
- A guided editor for tcp, udp, http, https, tcpmux, stcp, sudp and xtcp proxies (including plugins). Fields the form can't represent are flagged and can be edited as raw Store API JSON.
- Live proxy state from frpc, plus per-proxy traffic and connection counts when frps is connected.
- Optional view of frps clients and proxies that aren't linked to a configured frpc instance.
- Sortable, collapsible tables, selectable columns, themes and languages. These preferences are stored in your browser.
- Optional HTTP Basic login.

## Requirements

- frpc v0.68.0 or newer with its web server and `[store]` enabled. frpd manages the proxies in frpc's Store; proxies defined in frpc's own config file are not listed.
- An frps management API, only if you want traffic metrics or the unmatched-client view.
- Docker, a [release binary](https://github.com/Lanjelin/frpd/releases), or Go 1.26+ to build from source.

## Quick start

Enable the web server and Store in each frpc you want to manage:

```toml
webServer.addr = "127.0.0.1"
webServer.port = 7400
webServer.user = "admin"
webServer.password = "replace-with-a-secret"

[store]
path = "/var/lib/frpc/store.json"
```

frpd must be able to reach this address. Keep the frpc web server off untrusted networks.

Create `frpd.toml` (or start from [`frpd.example.toml`](frpd.example.toml)):

```toml
listen = "127.0.0.1:8080"

[[frpc]]
name = "edge-a"
url = "http://127.0.0.1:7400"
username = "admin"
password = "replace-with-a-secret"
```

Run it and open <http://127.0.0.1:8080>:

```sh
./frpd -c ./frpd.toml                # release or ./build.sh binary
go run ./cmd/frpd -c ./frpd.toml     # from a source checkout
```

For containers, see [Docker](#docker).

## Configuration

frpd reads one TOML file: the `-c` flag, else `$FRPD_CONFIG`, else `./frpd.toml`. Unknown keys are rejected at startup. Top-level keys must come before any `[[frpc]]` or `[[frps]]` table.

**Top level**

| Key | Default | Description |
| --- | --- | --- |
| `listen` | `:8080` | Address to listen on. |
| `username`, `password` | unset | Dashboard Basic login. Set both or neither. |
| `allowedOrigins` | none | Extra trusted browser origins; see [Cross-site protection](#cross-site-request-protection). |

**`[[frpc]]`**: one per frpc instance.

| Key | Description |
| --- | --- |
| `name` | Unique display label. |
| `url` | frpc web server URL. Use `https` if frpc has `webServer.tls`. |
| `username`, `password` | frpc web server credentials. |
| `clientID` | The frpc's `clientID`. Required when `frps` is set. |
| `frps` | Name of an `[[frps]]` entry to link this instance to. |
| `order` | Optional integer. Instances with an order come first, ascending; the rest are sorted by name. |

**`[[frps]]`**: optional, one per frps.

| Key | Description |
| --- | --- |
| `name` | Unique label, referenced by `frps` in `[[frpc]]`. |
| `url` | frps management API (its `webServer`) URL. |
| `username`, `password` | frps management API credentials. |

### Connecting frps

Enable the frps management API and add it to `frpd.toml`, then link each instance with `frps` and `clientID`:

```toml
[[frps]]
name = "public-edge"
url = "https://frps.example.internal:7500"
username = "admin"
password = "replace-with-a-secret"

[[frpc]]
name = "Arch laptop"
clientID = "arch"
url = "http://frpc-arch:7400"
frps = "public-edge"
```

`clientID` must match the top-level `clientID` in that instance's `frpc.toml`, not its `user` or hostname. It must also identify exactly one client on that frps. If several users share it, frpd leaves those clients unmatched instead of guessing.

In **Settings**:

- **Show unmatched FRPS clients** (off by default) lists frps clients and proxies that aren't linked to a configured instance.
- **Hide offline FRPS clients and proxies** (on by default) hides offline unmatched clients and offline proxies. Online clients stay visible even if all their proxies are offline.
- **Clear offline** removes offline proxy statistics on that frps, including those of configured instances. It does not delete stored proxy definitions.

## Security

- **Keep it private.** The dashboard can create, change and delete proxies, including plugin proxies. Keep frpd and the frpc/frps management APIs on a trusted network or behind an authenticated reverse proxy. frpd logs a warning at startup if it listens on a non-loopback address (the default `:8080` counts) without a login.
- **Basic login.** Set `username` and `password` to protect every route, including static files. Basic auth is not encrypted, so use HTTPS, and a long random password: there is no rate limiting or lockout. If a reverse proxy already handles login (for example Authelia), leave these unset. If you use both, the proxy must forward the `Authorization` header unchanged, or the login prompt repeats.
- **Config file.** It contains passwords. Use `chmod 600 frpd.toml` and mount it read-only in containers.

### Cross-site request protection

State-changing requests (create, edit, toggle, delete, Clear offline) are rejected with `403 cross-origin request blocked` when the browser says they came from another site. frpd also sends anti-framing headers and a strict Content Security Policy. This needs no configuration in normal setups:

- Opening frpd directly by IP or name (`http://192.168.1.5:8080`, `http://nas.lan:8080`), or through a published Docker port, works as is.
- A reverse proxy that terminates HTTPS and keeps the original `Host` header works as is.
- `curl` and scripts, which send no browser headers, are not affected.

You only need `allowedOrigins` if every change fails with that 403. This happens when frpd is served over plain HTTP behind a proxy that rewrites `Host` (for example to `frpd:8080`). Prefer making the proxy forward the original `Host` (`proxy_set_header Host $host;` in nginx; Caddy and Traefik do this by default). Otherwise list the address you type into the browser:

```toml
allowedOrigins = ["http://frp.lan", "https://frp.example.com"]
```

Each entry is `scheme://host[:port]` with `http` or `https`. Wildcards, paths and credentials are rejected at startup, and case and default ports (`:80`, `:443`) are ignored. The scheme and port must match what the browser shows: `http://frp.lan` does not cover `https://frp.lan`. A listed origin is trusted even when the browser marks the request cross-site, so list only addresses you control.

## Docker

Multi-arch images (amd64, arm64) are published to `ghcr.io/lanjelin/frpd` for each release. Use `latest` for the newest stable release, or pin a version such as `1.2.3` or `1.2`.

Set `listen = ":8080"` in `frpd.toml` so the dashboard is reachable from outside the container, then run:

```sh
docker run --rm --user "$(id -u):$(id -g)" \
  -p 127.0.0.1:8080:8080 \
  -v "$PWD/frpd.toml:/config/frpd.toml:ro" \
  ghcr.io/lanjelin/frpd:latest
```

- `--user` lets the container read a config file that is only readable by you.
- The frpc and frps URLs must be reachable **from the container**. `127.0.0.1` inside it is not the host.
- To publish beyond localhost, enable the Basic login and put TLS in front.

To build the image yourself: `docker build -t frpd .` (add `--build-arg VERSION=v1.2.3` to set the reported version; the default is `dev`).

## Binaries and building

Each [release](https://github.com/Lanjelin/frpd/releases) has archives for linux (amd64, arm64), macOS (amd64, arm64) and Windows (amd64), each with `frpd.example.toml`, this README and the license, plus a `checksums.txt` (`sha256sum -c checksums.txt`).

To build from a checkout:

```sh
./build.sh                          # current platform
./build.sh linux/amd64 linux/arm64  # specific targets
./build.sh --all                    # all release targets
```

Binaries are written to `dist/<os>_<arch>/frpd` (`frpd.exe` on Windows). `frpd -version` prints the version, which is also in the startup log. `build.sh` derives it from `git describe`; override it with `VERSION=v1.2.3 ./build.sh`.

## License

MIT. See [LICENSE](LICENSE).
