# NaiveProxy support

3x-ui can manage [NaiveProxy](https://github.com/klzgrad/naiveproxy) servers
alongside Xray. NaiveProxy is **not** an Xray protocol — the panel runs the
standalone `naive` binary as a child process and stores its configuration in
the database.

## Prerequisites

* Build or download the `naive` binary from <https://github.com/klzgrad/naiveproxy/releases>.
* Place it in `$PATH` (the panel finds it via `exec.LookPath("naive")`),
  or set `NAIVE_BIN=/absolute/path/to/naive` in the panel's environment.
* Have a valid TLS certificate (e.g. from Let's Encrypt) for the public domain.

## Data model

Table `naive_servers`:

| field      | meaning                                            |
|------------|----------------------------------------------------|
| `id`       | autoincrement                                      |
| `remark`   | free-text label                                    |
| `enable`   | auto-start on panel boot                           |
| `listen`   | bind address, default `0.0.0.0`                    |
| `port`     | TCP port                                           |
| `domain`   | server name presented in TLS / required by client  |
| `certFile` | path to TLS certificate                            |
| `keyFile`  | path to TLS private key                            |
| `authUser` | basic auth user                                    |
| `authPass` | basic auth password                                |
| `padding`  | enable HTTP/2 padding (recommended)                |
| `logLevel` | `DEBUG` / `INFO` / `WARNING` / `ERROR`             |
| `extraArgs`| optional extra CLI args appended verbatim          |

## REST API

Mounted under `/panel/api/naive`. Requires panel auth (session cookie or
`Authorization: Bearer <api_token>`) and CSRF, same as the rest of the
panel API.

| method | path             | description           |
|--------|------------------|-----------------------|
| GET    | `/list`          | list all servers      |
| GET    | `/get/:id`       | get one server        |
| POST   | `/add`           | create + optional start |
| POST   | `/update/:id`    | update + restart if running |
| POST   | `/delete/:id`    | stop + delete         |
| GET    | `/status/:id`    | `{running, pid, since, logPath}` |
| POST   | `/start/:id`     | start process         |
| POST   | `/stop/:id`      | SIGTERM (SIGKILL on fail) |
| POST   | `/restart/:id`   | stop, 200 ms wait, start |

## Lifecycle

* Configuration is rendered to `<bin>/naive/naive-<id>.json` before launch.
* Stdout/stderr are redirected to `<bin>/naive/naive-<id>.log` (append, 0600).
* The panel reaps exited processes via a per-process goroutine, so a crashed
  server is reflected in `/status/:id` on the next call.
* Servers with `enable=true` are auto-started on panel boot
  (see `web/service/naive.go` → `Restore`).

## Client URL

Generate manually from the configured fields:

```
naive+https://USER:PASS@DOMAIN:PORT
```

(URL-encode special characters in USER/PASS.)

## Limitations

* No UI page — REST only. A Vue page in `frontend/src/views` can be added
  later; the API surface is stable.
* Health is shallow: presence of a PID, not protocol-level liveness.
  Add an outbound probe if you need true health checking.
* Multiple naive servers per panel are supported (separate config + log per id).
