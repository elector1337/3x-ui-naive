# Changelog

All notable differences from upstream [MHSanaei/3x-ui](https://github.com/MHSanaei/3x-ui)
are documented here.

Format loosely follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versions are tagged against the upstream commit the fork is based on.

---

## [Unreleased] — feature/naive-proxy

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
web/service/naive_test.go
```

### Files modified

```
database/db.go                              # register NaiveServer migration
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
