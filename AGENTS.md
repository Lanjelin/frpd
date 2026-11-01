# Repository Guidelines

## Project Overview
`frpd` (module `github.com/Lanjelin/frpd`, binary `frpd`; MIT licensed, see `LICENSE`) is a stateless, server-rendered Go dashboard for managing [frp](https://github.com/fatedier/frp) proxies. It talks to:
- each **frpc** web server's Store API (read and write proxy definitions; needs frpc >= v0.68.0 with `[store] path`);
- optional **frps** management APIs (read-only metrics and discovery, plus one "prune offline" action).

No database, no cache: every page load calls upstream live. No frontend build step.

## Architecture & Data Flow
1. `cmd/frpd/main.go`: parse `-c`, `config.Load`, build one shared `&http.Client{Timeout: 10s}`, `web.New(cfg, httpClient)`, `net.Listen`, `http.Serve`. Also runs `go verifyConnections` (log-only probes, never blocks startup).
2. `internal/config`: TOML to validated `Config` (by value). Strict: `DisallowUnknownFields`.
3. `internal/web.New` builds `store.Client` / `frps.Client` maps (constructor injection of `*http.Client`; nil falls back to `http.DefaultClient`). Maps are read-only after `New`, so no mutexes.
4. Request: mux, then optional Basic-auth wrapper (covers static files too), then handler, then upstream clients, then view struct, then `render` (executes the template into a buffer first, so a failure sends a single 500).
5. Mutations use POST, redirect (303), GET (PRG). Feedback travels in `?message=` / `?error=` (`redirectMessage`, `redirectError`). Create and update failures re-render the form with `Error`.

Key patterns:
- `store.Proxy` is `map[string]any`, so unknown frpc fields pass through untouched. Don't replace it with a closed struct.
- Toggle = `GetProxy`, set `proxy[type]["enabled"]`, `UpdateProxy`.
- `parseProxy` expects form field `proxy` as JSON with `name`, `type`, and a `proxy[type]` object.
- FRPS linkage: `[[frpc]].clientID` must equal the frpc's `clientID`. Matching key is `User\x00ClientID`, and only if the ID is unique on that FRPS (ambiguous means unmatched). Metrics match by proxy name, falling back to `clientID.name`.
- Page-level upstream failures are shown in the view (`Error`, `StatusError`, `FRPSError`). They don't fail the page.
- Instances load concurrently (`loadInstances`, results written to an indexed slice to keep order) under one shared 8s `context.WithTimeout`. Within an instance, upstream calls are sequential. `serverInfoCache` (`internal/web/security.go`) memoizes frps `ServerInfo` per request.
- `guardCrossOrigin` (`internal/web/security.go`) wraps everything (including the auth wrapper). It sets `X-Frame-Options`, the strict `contentSecurityPolicy` and `nosniff`. For non-GET/HEAD/OPTIONS: an `Origin` in the `allowedOrigins` config list always passes; else `Sec-Fetch-Site` same-origin/none passes; else (header absent) `Origin` host must equal `Host`; else 403. Requests with neither header pass. `allowedOrigins` entries are normalized by `config.NormalizeOrigin` (scheme+host[:port], no wildcards, default ports dropped) at `Load` and again in `web.New`. New mutating routes get the guard automatically; keep them POST.
- Routes use Go 1.22 `ServeMux` method+wildcard patterns (`POST /instances/{instance}/proxies/{name}/toggle`). All routes are in `Server.Handler()`.

## Key Directories
- `cmd/frpd/`: entry point only.
- `internal/config/`: config structs, defaults, validation, `DefaultPath()`.
- `internal/store/`: frpc Store API client (`client.go`).
- `internal/frps/`: frps management API client (`client.go`).
- `internal/web/`: `server.go` (routes, handlers, view models), `discovery.go` (unmatched FRPS clients, prune).
- `internal/web/templates/*.html`: `html/template` pages (dashboard, instance, proxy, settings, frps-discovery).
- `internal/web/static/`: vanilla JS/CSS/icons, embedded in the binary.
- `.github/workflows/build.yml`: the only CI workflow.

## Development Commands
```sh
go run ./cmd/frpd -c ./frpd.toml     # run (copy frpd.example.toml first); FRPD_CONFIG also sets the path
go build ./cmd/frpd
go test ./...
go test ./internal/web -run TestName -v   # single test
go vet ./... && gofmt -l .                # CI fails on any gofmt output or vet finding
node --test internal/web/jstest/proxy-editor-model.test.js   # JS model test (Node 22 in CI)
./build.sh                                # host build to dist/<os>_<arch>/frpd
./build.sh linux/amd64 linux/arm64        # cross-compile; --all = 5 release targets (not combinable with explicit targets)
OUTPUT_DIR=out ./build.sh darwin/arm64
docker build -t frpd .
```

## Code Conventions & Common Patterns
- Standard `gofmt`. Single-word lowercase packages. Constructors are `New(...)`. Package docs are `// Package x ...` comments.
- Handlers are lowerCamelCase methods on `*Server`. View structs are unexported and end in `View` or `Page`.
- Errors: wrap with `fmt.Errorf("context: %w", err)`. No custom or sentinel error types. Upstream errors are prefixed "Store API ..." / "FRPS API ...". Ignored write errors are written `_, _ = w.Write(...)`. Startup failures use `log.Fatal`.
- Upstream clients: path segments go through `url.PathEscape`. Basic auth is set only if credentials exist. Responses are capped at 1 MiB. Upstream error bodies are reduced to a bounded first line (frps drops HTML, store falls back to the HTTP status via `errorDetail`) because they end up in redirect URLs. Keep new client errors bounded the same way.
- Concurrency: request-level fan-out in `loadInstances`, `frps.Client.AllProxies` (8 types, results kept in `proxyTypes` order, earliest-type error wins) and FRPS discovery (servers, plus `Clients` alongside `AllProxies`); always write results into indexed slots, never append from goroutines (`sync.WaitGroup`; loop vars are per-iteration in Go 1.22+). `main.verifyConnections` also uses goroutines. `main` serves via `http.Server` (`ReadHeaderTimeout`, SIGINT/SIGTERM graceful `Shutdown`, 15s). Don't set a short `WriteTimeout`; pages can take 8s.
- Mutating handlers (create/update/toggle/delete) hold a per-instance mutex via `defer s.lockInstance(name)()` (`internal/web/locks.go`) because toggle is read-modify-write against frpc. Reads stay lock-free. New mutating handlers must take it too.
- State: immutable after construction. Don't add mutable shared state without locking.
- Dependencies: the stdlib plus `github.com/pelletier/go-toml/v2`. Avoid adding more. Builds are `CGO_ENABLED=0`, so no cgo.
- Embedding: `//go:embed templates/*.html static/*` in `server.go`. Templates must be direct children of `templates/` and assets direct children of `static/` (no subdirectories). Any asset change needs a rebuild. JS tests live in `internal/web/jstest/` so they are not embedded or served.
- Static assets: never hand-write `/static/...?v=N` in templates. Use `{{asset "app.js"}}` (`internal/web/assets.go`), which emits `/static/app.js?v=<12-hex content hash>` and errors on unknown names. `staticHandler` sends an ETag for every file and `immutable` one-year caching only when `?v=` matches the current hash; everything else is `no-cache`. `site.webmanifest` icons are referenced from the manifest itself and stay unversioned.
- UI must stay CSP-clean: `contentSecurityPolicy` (`security.go`) allows only same-origin script/style/img/connect/form-action and no `unsafe-inline`. No inline `<script>`, `style=`, `<style>` or `onclick`/`onsubmit` handlers in templates or JS-built HTML. Put behavior in `static/*.js` and drive it with `data-*` attributes (e.g. `data-confirm="..."` is handled by a delegated `submit` listener in `app.js`; `theme.js` is the synchronous head script that sets theme/lang). `TestEmbeddedUIIsCSPCompatible` enforces this. A blocked inline handler fails silently in browsers, so a lost `confirm()` means unconfirmed deletes.
- Instance ordering: instances with an integer `order` come first (ascending), then ties and unordered ones by name.
- UI preferences (theme, columns, sort, collapse state) live in browser storage, not on the server.

## Important Files
- `cmd/frpd/main.go`: entry, `-c` flag.
- `internal/config/config.go`: TOML schema. Top level: `listen` (default `:8080`), optional `username`/`password` (both or neither). `[[frps]]`: `name,url,username,password`. `[[frpc]]`: `name,url,username,password,clientID,frps,order`.
- Config validation rules:
  - `frpc.frps` requires `clientID` and must name a defined `[[frps]]`.
  - Names must be unique.
  - Trailing `/` is stripped from URLs.
  - The legacy `[[instances]]` table is rejected.
- `internal/web/server.go`, `internal/web/discovery.go`: handlers and routes.
- `frpd.example.toml`: config template. Never commit a real `frpd.toml` or `.env` (gitignored).
- `build.sh`, `Dockerfile`, `.github/workflows/build.yml`.

## Runtime/Tooling Preferences
- Go 1.26 (`go.mod` is `go 1.26.0`, the language minimum). Three places must move together: `go.mod`, the `Dockerfile` (`golang:1.26-alpine`) and both `setup-go` steps in `.github/workflows/build.yml` (`go-version: 1.26.x` with `check-latest: true`, so CI always gets the newest patch release). Run `govulncheck ./...` when bumping.
- Node is needed only for the one JS test (Node 22 in CI). There is no npm, bundler or package.json.
- Build flags: `CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=$VERSION"`. The version lives in `cmd/frpd/version.go` (`var version = "dev"`, overridden by `-X`; falls back to `debug.ReadBuildInfo` module version, else `dev`). `build.sh` derives it from `VERSION` or `git describe --tags --always --dirty`. Docker takes `--build-arg VERSION=...` (`.dockerignore` excludes `.git`, so no `git describe` there); CI passes the tag. `frpd -version` prints it.
- Docker runtime: distroless nonroot, config mounted at `/config/frpd.toml`, port 8080.
- Startup warns (`cmd/frpd/exposure.go`) when listening on a non-loopback address without `username`/`password`. The default `listen` stays `:8080`.
- CI (`.github/workflows/build.yml`): `test` (gofmt, vet, go test, JS test), then a 5-target `build` matrix on pushes to `main`, PRs and tags matching `v[0-9]+.[0-9]+.[0-9]+*`. Tags additionally run `release` (packages `frpd_<tag>_<os>_<arch>.tar.gz`/`.zip` with README, `LICENSE` and `frpd.example.toml`, plus `checksums.txt`, then `gh release create --generate-notes --verify-tag`, adding `--prerelease` when the tag contains `-`) and `docker` (multi-arch push to `ghcr.io/lanjelin/frpd` with `docker/metadata-action` semver tags, OCI labels and index annotations, `VERSION` build-arg = tag). Repo is `github.com/Lanjelin/frpd`. If you add release assets or change artifact layout, re-test the inline packaging script (extract it from the YAML and run it on fake `artifacts/frpd-<os>-<arch>/<os>_<arch>/frpd` trees).

## Testing & QA
- Go stdlib `testing` only (no testify or mock libraries). Tests are white-box, in the same package, next to the code.
- Fake upstreams use `httptest.NewServer` (frpc `/api/store/proxies`, `/api/status`; frps `/api/serverinfo`, `/api/clients`, `/api/proxy/<type>`). Pass `srv.Client()` to `web.New(cfg, client)` and drive `s.Handler().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(...))`.
- Transport failures use `roundTripFunc` and `unreachableRequest` from `internal/web/connection_error_test.go`.
- Handler tests use `fakeFRPC` (in-memory Store API with delay, write counting, `rejectWrites`, in-flight tracking), `dashboardFor`, `submit` (form POST) and `fetch` (GET) from `internal/web/handlers_test.go`; `redirectQuery` asserts a 303 to `/` and returns its `message`/`error` params.
- Fixtures are inline (JSON/TOML strings, `t.TempDir()`, `t.Setenv`). There is no `testdata/`.
- Names are long and descriptive: `TestDiscoveryDoesNotGuessWhichUserOwnsSharedClientID`. Prefer table-driven tests with `t.Run`; the model is `TestDiscoveryDisplaysAvailableFRPSDomains`.
- Assert with `strings.Contains` on the HTML body and status codes. Use `t.Fatalf("x = %d %q", code, body)`, and `t.Errorf` (not `Fatal`) inside handler goroutines.
- Known gaps:
  - no tests for `main.go`'s serve/shutdown path (only `exposedWithoutAuth` and version resolution are tested);
  - `app.js` and `proxy-editor.js` have no automated tests. They were only verified by hand in headless Chromium (no CSP violations on any page, delete confirm works), and `node` is not installed on the dev machine, so the JS model test runs in CI only.
- No coverage threshold is enforced. Run `go test ./...` before finishing, plus the node test when touching `static/proxy-editor-model.js`.
