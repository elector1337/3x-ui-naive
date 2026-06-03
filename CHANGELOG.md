# Changelog

All notable differences from upstream [MHSanaei/3x-ui](https://github.com/MHSanaei/3x-ui)
are documented here.

Format loosely follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versions are tagged against the upstream commit the fork is based on.

---

## [Unreleased] — feature/naive-proxy

### Added

#### Multiple users per naive server

- New `naive_users` table (gorm auto-migrated) holding extra `basic_auth`
  credentials beyond the server's primary `authUser`/`authPass`. Passwords
  are encrypted at rest with the same AES-256-GCM scheme.
- The Caddyfile generator emits one `basic_auth` line per enabled credential
  (primary + each enabled extra user). Verified valid against a real
  `caddy adapt`, and both users authenticate at runtime while a wrong one
  gets 407.
- Add/edit form gains an **Additional users** section (username / password /
  enable, add & remove). Update replaces the user set.
- Each enabled credential gets its own client URL and its own QR panel in the
  QR modal. Validation enforces the same charset/length rules per user and
  rejects duplicate usernames.

#### Naive traffic limits, expiry & auto-stop

- New `total` (quota, bytes), `expiryTime` (absolute ms), `trafficReset`
  (never|day|week|month) and `lastTrafficResetTime` columns on
  `naive_servers` (gorm auto-migrated).
- The naive traffic job (already running every 30 s) now enforces quota and
  expiry: a running server that reaches `total` bytes or passes `expiryTime`
  is stopped. `enable` is left untouched, so it resumes automatically once
  the counter is reset or the expiry extended.
- Periodic counter resets via cron (`@daily` / `@weekly` / `@monthly`),
  mirroring the inbound reset cadence; an enabled, non-expired server that was
  stopped on quota is restarted right after its reset.
- New endpoint reuse: quota/expiry are edited in the add/edit form (traffic
  limit in GB, expiry date-time picker, auto-reset schedule).
- UI shows **Depleted** / **Expired** badges in the status column.

#### QR code / share-link generator

- QR button on each naive server (desktop table + mobile card) opens a modal
  rendering the client URL as a QR code, reusing the inbounds `QrPanel`
  component (copy-text and copy/download-PNG included). No new dependencies.

#### Naive traffic stats

- Per-server upload / download byte counters, shown in a new **Traffic**
  column on `/panel/naive` (`↑ up / ↓ down`, human-readable sizes) and
  reset via a per-row button.
- Counts come from the **kernel** (nftables): Caddy's `forward_proxy` does
  not report tunnel bytes (verified — a CONNECT request logs `size:0`,
  `bytes_read:0`), so the panel installs one input + one output counter per
  naive port in an `inet 3xui_naive` table and reads them with
  `nft -j reset counters` (atomic read-and-zero).
- New `up` / `down` columns on `naive_servers` (gorm auto-migrated),
  cumulative across restarts until reset.
- `NaiveTrafficJob` samples every 30 s into the DB. Counter table is rebuilt
  on every add/update/delete and on boot; torn down when no servers remain.
- New endpoint `POST /panel/api/naive/reset-traffic/:id`.
- **Linux + root only.** On any other OS, without root, or without the `nft`
  binary, the whole feature is a graceful no-op and traffic stays at 0 —
  removing the "traffic stats not collected" known limitation there.

---

## [0.2.0] — 2026-06-01

Based on upstream commit [`f9ae0347`](https://github.com/MHSanaei/3x-ui/commit/f9ae0347).

### Added

#### NaiveProxy server management (the main feature of this fork)

- New `NaiveServer` model and `naive_servers` table (auto-migrated).
- Panel page `/panel/naive` with list, summary card, responsive mobile
  card view and matching dark / ultra-dark theme.
- Add / edit modal with two modes:
  - **Simple** — `domain`, `port`, `listen`, `authUser`, `authPass`,
    `padding`, `logLevel`, `extraArgs`; TLS toggle between Let's Encrypt
    (ACME) and manual cert/key files.
  - **Advanced** — raw Caddyfile editor with `caddy adapt` validation
    button and «Regenerate from form» helper.
- One-click **Install Caddy** button that builds Caddy with the
  `klzgrad/forwardproxy` plugin via `xcaddy`. Live build log streamed
  over SSE.
- REST API under `/panel/api/naive`:
  - `GET /list`, `GET /get/:id`
  - `POST /add`, `POST /update/:id`, `POST /delete/:id`
  - `GET /status/:id` — `{ running, listening, pid, since, logPath }`
  - `POST /start/:id`, `POST /stop/:id`, `POST /restart/:id`
  - `GET /caddy-status` — `{ installed, path, source, version, goPresent }`
  - `POST /install-caddy` — SSE log of xcaddy build
  - `POST /preview` — render the Caddyfile that the panel would write
  - `POST /validate` — run `caddy adapt` against arbitrary Caddyfile text
- Lifecycle:
  - Each server runs in its own child Caddy process.
  - `admin off` in the generated config so multiple instances don't
    fight over `:2019`.
  - Per-server `XDG_DATA_HOME=<bin>/naive/data-<id>/` so ACME storage
    and locks don't collide.
  - Real TCP probe in status (`listening`) — not just «PID alive».
  - `enable=true` servers are auto-restarted on panel boot.
- Cross-port collision check between naive servers and Xray inbounds
  (both directions). Reports the offending source by name. UDP-only
  inbounds (hysteria2, wireguard) correctly do not conflict with naive.
- Sidebar entry and i18n (en-US, ru-RU) for the new page.
- Client URL generator on the list page: copies `naive+https://USER:PASS@DOMAIN:PORT`.
- Tests:
  - `naive_test.go` — Caddyfile rendering (manual TLS, ACME, raw mode,
    padding off, legacy `WARNING` log level normalization, specific
    bind interface).
  - `naive_cross_port_test.go` — cross-port collisions in both
    directions with realistic seed data.

#### Cleanup: dependency injection + reaper hardening

- Removed the package-level `naiveSvc` singleton. `NewNaiveService()`
  is now the only constructor; the web layer owns one instance and
  passes it into `NewAPIController` → `NewNaiveController` and into the
  boot-time `Restore()` call.
- Reaper goroutine in `Start()` now has a `defer recover()` so a
  panic inside `cmd.Wait()` / log close / map cleanup can't take the
  whole panel down (it logs and exits the goroutine instead).
- `api_docs_test.go` taught about the new `/panel/api/naive/*` and
  `/panel/naive` routes — `endpoints.js` documents all 13 of them.

#### Docker image with bundled Caddy

- New `caddy-builder` stage in the Dockerfile uses xcaddy with
  `CGO_ENABLED=0` to cross-compile Caddy + `klzgrad/forwardproxy`
  for the target architecture (amd64 / arm64 / arm/v7 / arm/v6 / 386).
- Resulting binary is copied to `/app/bin/caddy` in the final image —
  the panel's `findCaddy()` discovers it automatically on first start.
- `docker pull ghcr.io/elector1337/3x-ui-naive:latest` is now truly
  out-of-the-box: NaiveProxy works without an extra Install Caddy step.
- Image grows by ~50 MB (Caddy is statically linked, pure Go, no CGO).

#### Auth field validation

- `validateNaive()` now rejects forbidden characters in `AuthUser` and
  `AuthPass`: whitespace (Caddyfile token separator), control chars,
  and Caddyfile metacharacters `"`, `\`, `#`.
- `AuthUser` additionally rejects `:` (reserved as user/password
  separator in the `naive+https://USER:PASS@HOST` client URL).
- Length capped at 64 chars for user and 128 for pass.
- Error messages name the offending character (`auth pass contains
  forbidden character space`).
- 14 new sub-tests under `TestValidateNaive_AuthCharsetRejection`.

#### Naive logs in the UI

- New endpoint `GET /panel/api/naive/log/:id?tail=200` — returns last
  N lines from the per-server log file. Default 200, max 1000. Reads
  only the last 256 KiB of the file from disk.
- UI: «Show log» button (file-text icon) added to the action column
  in both desktop table and mobile card view.
- Modal with monospaced log view, refresh button, dark-theme styling.
- New i18n keys: `pages.naive.viewLog`, `logTitle`, `logEmpty`,
  `logHint`, `logError`.

#### Reliability & observability

- `NaiveService.Restore()` now starts all `enable=true` servers in parallel
  goroutines with a `sync.WaitGroup`; on a 3-server bench restore time drops
  from sequential to ~5 ms. A `recover()` per goroutine keeps a runaway
  panic from killing the panel.
- New `responding` field in `NaiveStatus`: HTTPS HEAD probe (1s timeout,
  `InsecureSkipVerify`) against `127.0.0.1:port` after the TCP probe.
  Lets the UI distinguish «socket open but caddy hung» from «socket open
  and TLS responds».
- UI now shows three running-state colours: green «Running»
  (running + listening + responding), yellow «Unresponsive»
  (running + listening, but no TLS reply), blue «Starting…»
  (running, port not yet open).

#### Integration tests

- `testdata/mock_caddy/` — tiny Go binary that mimics caddy's CLI surface
  (`version`, `adapt`, `run --config X.caddyfile --adapter caddyfile`)
  and serves a self-signed TLS endpoint on the port from the Caddyfile.
- `naive_lifecycle_test.go`:
  - `TestNaiveLifecycle_Integration` — full add → start → wait for
    Listening + Responding → stop → wait for reap → restart. Catches
    regressions in the proc map, reaper goroutine and probe logic.
  - `TestNaiveRestore_ParallelStartsAllEnabledRows` — three servers
    added, `Restore()` called once, all become listening.

#### AuthPass encryption at rest

- `util/crypto/crypto.go` — AES-256-GCM helpers `EncryptString` /
  `DecryptString` with idempotent `enc:v1:` prefix.
- 32-byte AES key auto-generated on first start, stored at
  `<db_folder>/encryption.key` (mode `0600`, base64-encoded).
- gorm hooks `BeforeSave` / `AfterFind` on `NaiveServer` transparently
  encrypt / decrypt `AuthPass`.
- Service-layer round-trip helper restores plaintext after `Create` /
  `Save` so API responses keep returning the original value.
- Legacy plaintext rows are auto-tolerated and re-encrypted on next save.
- Tests cover roundtrip, idempotence, legacy passthrough, file
  permissions, key-rotation failure.

#### install.sh integration

- New helpers (`install.sh`):
  - `caddy_with_forwardproxy_works` — detect whether an existing
    `caddy` binary knows the `forward_proxy` directive (probes
    `caddy adapt` against a stub Caddyfile).
  - `ensure_go_for_xcaddy` — verify Go ≥ 1.22 is on PATH; otherwise
    print a clear hint to install from go.dev/dl.
  - `setup_caddy_capabilities` — `setcap cap_net_bind_service=+ep`
    so the binary can listen on ports ≤ 1024 without root.
  - `install_caddy_naive` — idempotent xcaddy build into
    `${xui_folder}/bin/caddy`.
  - `prompt_install_caddy` — interactive y/N prompt at the end of
    install. Supports non-interactive override via
    `NAIVE_INSTALL_CADDY=yes|no`.
- Hooked into `install_x-ui()` so a fresh `bash install.sh` install
  can build Caddy in one step.

#### Documentation

- `README.md` — new top-of-page section «NaiveProxy support (this fork)»
  summarising features and linking to the full doc.
- `docs/NAIVE.md` — complete reference: prerequisites, two TLS modes,
  field table, simple vs. advanced configuration modes, REST API
  table, status semantics, cross-port checks, on-disk layout and known
  limitations.

### Changed

- `web/service/port_conflict.go` — `checkPortConflict` now also queries
  the naive table so an Xray inbound add/edit can flag collisions with
  managed naive servers.

### Files added

```
CHANGELOG.md
database/model/naive.go
docs/NAIVE.md
frontend/naive.html
frontend/src/entries/naive.js
frontend/src/pages/naive/NaiveFormModal.vue
frontend/src/pages/naive/NaivePage.vue
frontend/src/pages/naive/useNaive.js
web/controller/naive.go
web/service/naive.go
web/service/naive_cross_port.go
web/service/naive_cross_port_test.go
web/service/naive_installer.go
web/service/naive_lifecycle_test.go
web/service/naive_test.go
web/service/testdata/mock_caddy/main.go
util/crypto/crypto_test.go
```

### Files modified

```
database/db.go                              # register NaiveServer migration
util/crypto/crypto.go                       # AES-256-GCM helpers + keyfile
frontend/src/components/AppSidebar.vue      # add Naive entry to sidebar
frontend/vite.config.js                     # register naive entry + redirect
install.sh                                  # Caddy install integration
web/controller/api.go                       # mount /panel/api/naive
web/controller/xui.go                       # serve /panel/naive page
web/service/port_conflict.go                # cross-check against naive table
web/translation/en-US.json                  # menu.naive + pages.naive.*
web/translation/ru-RU.json                  # same for Russian
web/web.go                                  # call NaiveService.Restore() at boot
```

### Tracking upstream

The fork is rebased / merged against [MHSanaei/3x-ui](https://github.com/MHSanaei/3x-ui)
periodically. Upstream changes that don't touch any of the files above
should apply cleanly. If conflicts hit `port_conflict.go`, look at how
the new `naivePortConflictsInbound` hook is wired in — that's the only
non-additive change in the fork.
