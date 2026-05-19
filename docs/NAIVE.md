# NaiveProxy

Управление naive-серверами рядом с Xray. Naive-сервер — это Caddy с плагином
`forward_proxy` (форк klzgrad), запускается как отдельный процесс.

## Установка

Нужен бинарь Caddy с плагином `forward_proxy`. Готовая сборка — в релизах
klzgrad/naiveproxy: https://github.com/klzgrad/naiveproxy/releases (там же
лежит исходник для своей сборки через `xcaddy`).

Положи бинарь в `$PATH` под именем `caddy` или укажи `CADDY_BIN=/path/to/caddy`.

Чтобы слушать порт ≤1024 без root: `sudo setcap cap_net_bind_service=+ep <caddy>`.

## TLS

Два варианта в форме:

- **Auto TLS (Let's Encrypt)** — указать только email. Домен должен резолвиться
  на сервер, порт 80 должен быть открыт для HTTP-01 challenge.
- **Свои cert/key** — указать пути к файлам.

## Поля сервера

`remark, enable, listen, port, domain, useAcme, acmeEmail, certFile, keyFile,
authUser, authPass, padding, logLevel, extraArgs`

## API

Под `/panel/api/naive`, авторизация как у остального panel API.

```
GET    /list
GET    /get/:id
POST   /add
POST   /update/:id
POST   /delete/:id
GET    /status/:id          -> { running, listening, pid, since, logPath }
POST   /start/:id
POST   /stop/:id
POST   /restart/:id
```

`listening: true` означает, что процесс открыл порт и принимает TCP-соединения
(не только что PID жив).

## Где что лежит

- Caddyfile: `<bin>/naive/naive-<id>.caddyfile`
- лог:       `<bin>/naive/naive-<id>.log`
- состояние Caddy (включая ACME-сертификаты): `<bin>/naive/data-<id>/`

Каждому серверу — свой `data-<id>` через `XDG_DATA_HOME`, чтобы инстансы не
конфликтовали. Серверы с `enable=true` стартуют при запуске панели.

## Клиентский URL

```
naive+https://USER:PASS@DOMAIN:PORT
```
