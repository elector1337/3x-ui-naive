# NaiveProxy в 3x-ui

3x-ui управляет naive-серверами как отдельными процессами рядом с Xray.
Naive-сервер — это Caddy с плагином `forward_proxy` (форк klzgrad).
Панель умеет сама собрать нужный бинарь через `xcaddy`, генерирует Caddyfile,
запускает процессы, переживает рестарты, делает health-check и
предотвращает конфликты портов с Xray-inbound'ами.

## Установка Caddy

Нужен бинарь Caddy **с плагином forward_proxy** — стандартный `caddy` из
дистрибутива не подойдёт. Есть пять вариантов в порядке предпочтения:

1. **Docker-образ** `ghcr.io/elector1337/3x-ui-naive` уже содержит готовый
   Caddy с плагином в `/app/bin/caddy` — ничего дополнительно ставить
   не нужно. Просто `docker pull ghcr.io/elector1337/3x-ui-naive:latest`.
2. **Через install.sh** (для свежей установки панели). Скрипт спросит
   «Install Caddy with forward_proxy now?» и сам выполнит сборку через
   xcaddy. Можно автоматизировать: `NAIVE_INSTALL_CADDY=yes bash <(curl ...)`.
3. **Через панель**. Откройте `/panel/naive` — если caddy не найден, появится
   жёлтый баннер «Caddy не установлен» и кнопка **Install**. Панель запустит
   `xcaddy build` с нужным плагином и положит бинарь в `<bin>/caddy`.
   Требует Go 1.22+ на хосте; сборка занимает 1–3 минуты, лог идёт live
   по SSE.
4. **Готовый бинарь из релизов klzgrad/naiveproxy** (положить в `$PATH`).
5. **Самостоятельная сборка** через `xcaddy build --with github.com/caddyserver/forwardproxy@caddy2=github.com/klzgrad/forwardproxy@naive`.

Поиск бинаря (в этом порядке): `CADDY_BIN` → `<bin>/caddy` → `caddy` в PATH.

Чтобы слушать порт ≤1024 без root:

```
sudo setcap cap_net_bind_service=+ep <путь_к_caddy>
```

## TLS

Два режима, переключаются переключателем «Auto TLS» в форме:

- **Auto TLS (Let's Encrypt)** — указать только email. Домен должен
  резолвиться на сервер, порт 80 должен быть открыт (HTTP-01 challenge).
  Сертификаты Caddy сохраняет в `<bin>/naive/data-<id>/` (изолировано
  на сервер, чтобы инстансы не дрались за хранилище).
- **Свои cert/key** — указать пути к файлам.

## Поля сервера

| Поле           | Описание                                                        |
|----------------|------------------------------------------------------------------|
| `remark`       | свободный текст-метка                                            |
| `enable`       | автозапуск при старте панели                                     |
| `listen`       | bind-адрес; пусто / `0.0.0.0` / `::` = все интерфейсы            |
| `port`         | TCP-порт                                                         |
| `domain`       | имя в TLS-сертификате (требуется клиентом)                       |
| `useAcme`      | флаг: использовать Let's Encrypt вместо своих cert/key           |
| `acmeEmail`    | email для ACME (при `useAcme=true`)                              |
| `certFile`     | путь к PEM-сертификату (при `useAcme=false`)                     |
| `keyFile`      | путь к PEM-ключу                                                 |
| `authUser`     | basic auth пользователь                                          |
| `authPass`     | basic auth пароль                                                |
| `padding`      | включить HTTP/2 padding (рекомендуется)                          |
| `logLevel`     | `DEBUG` / `INFO` / `WARN` / `ERROR`                              |
| `extraArgs`    | доп. CLI-аргументы caddy, через пробел                           |
| `useRawConfig` | флаг: показывать редактор Caddyfile вместо простой формы         |
| `rawConfig`    | полный текст Caddyfile (при `useRawConfig=true`)                 |

## Режимы конфигурации

### Simple (по умолчанию)

Заполняем поля формы — панель генерирует Caddyfile вида:

```
{
    admin off
    log {
        level WARN
    }
}

:443, naive.example.com {
    tls /etc/ssl/cert.pem /etc/ssl/key.pem
    route {
        forward_proxy {
            basic_auth alice s3cret
            hide_ip
            hide_via
            probe_resistance
            padding
        }
    }
}
```

Особенности:

- `admin off` — чтобы несколько naive-инстансов не дрались за `:2019`
- `0.0.0.0` / `::` нормализуются в bare `:port` (Caddy воспринимает явные
  `0.0.0.0:port` как host-matcher, а не как bind-адрес); конкретный IP
  выносится в директиву `bind`
- кастомный `Listen` пишется как `bind <addr>` внутри блока

### Advanced (raw Caddyfile)

Переключатель «Advanced» в форме открывает textarea с полным Caddyfile.
Простые поля при этом игнорируются. Кнопки:

- **Regenerate from form** — заполняет редактор тем Caddyfile, который
  выдал бы simple-режим (удобно начинать кастомизацию)
- **Validate** — гоняет `caddy adapt` против текста, показывает ошибку с
  номером строки или зелёное «valid»

Этот режим нужен для:

- кастомного фронтинга (`file_server` на корне)
- нескольких доменов / SAN
- кастомного `probe_resistance <secret>`
- опций forward_proxy сверх базовых
- кастомного ACME-CA (стейджинг, ZeroSSL)
- HTTP/3 и т.п.

## API

Под `/panel/api/naive`, авторизация и CSRF как у остального panel API.

| Метод | Путь                  | Что делает                                                                 |
|-------|-----------------------|----------------------------------------------------------------------------|
| GET   | `/list`               | список всех серверов                                                       |
| GET   | `/get/:id`            | одна запись                                                                |
| POST  | `/add`                | создать; делает кросс-проверку портов; если `enable=true` — стартует       |
| POST  | `/update/:id`         | обновить; рестартует, если процесс был запущен                             |
| POST  | `/delete/:id`         | остановить и удалить                                                       |
| GET   | `/status/:id`         | `{ running, listening, pid, since, logPath }`                              |
| POST  | `/start/:id`          | запустить процесс                                                          |
| POST  | `/stop/:id`           | SIGTERM (SIGKILL fallback)                                                 |
| POST  | `/restart/:id`        | stop, 200ms, start                                                         |
| GET   | `/caddy-status`       | `{ installed, path, source, version, goPresent }` — для UI-баннера         |
| POST  | `/install-caddy`      | SSE: live-лог `xcaddy build`, в конце `event: done` или `event: error`    |
| POST  | `/preview`            | принимает поля формы, возвращает текст Caddyfile, который панель сгенерит  |
| POST  | `/validate`           | принимает `{text}`, прогоняет `caddy adapt`, возвращает ok / ошибку парсера|

### Статусы

- `running: true` — процесс жив (kill -0 успешен)
- `listening: true` — поверх `running`: TCP-probe `127.0.0.1:port` ответил
  (значит naive реально открыл сокет, не висит в init)

В UI:

- зелёный «Running» — running ∧ listening
- синий «Starting…» — running ∧ ¬listening (только запустили или зависли)
- серый «Stopped» — процесс отсутствует

## Кросс-проверка портов

Чтобы naive не «съел» порт, который уже занят Xray-inbound'ом (или наоборот),
панель делает проверку в обе стороны:

- при **add/update naive**: проверяется, не занят ли `port` уже Xray
  TCP-inbound'ом на том же интерфейсе (или другим naive-сервером);
  ошибка с именем конфликтующего источника
- при **add/update Xray-inbound**: проверяется обратное

Особенности:

- ACME-режим и raw-режим naive пропускают эту проверку (raw — потому что
  пользователь сам пишет конфиг; ACME — потому что наоборот, проверка
  тщательная, см. код)
- UDP-инбаунды (hysteria2, wireguard) не конфликтуют с naive (она всегда TCP)
- учитывается перекрытие интерфейсов: `0.0.0.0:443` конфликтует с
  `127.0.0.1:443`

## Где что лежит на диске

```
<bin>/caddy                          # бинарь, поставленный через панель
<bin>/naive/                         # директория модуля
<bin>/naive/naive-<id>.caddyfile    # конфиг
<bin>/naive/naive-<id>.log          # stdout+stderr (append, 0600)
<bin>/naive/data-<id>/               # XDG_DATA_HOME для этого инстанса
                                     # (ACME-сертификаты, кэш)
```

Серверы с `enable=true` стартуют при загрузке панели (`Restore()`).

## Клиентский URL

В UI есть кнопка-копирка в каждой строке таблицы:

```
naive+https://USER:PASS@DOMAIN:PORT
```

USER/PASS URL-encoded.

## Шифрование пароля

`authPass` шифруется AES-256-GCM перед записью в `x-ui.db`. Ключ
шифрования автоматически генерируется при первом запуске и сохраняется
в `<db_folder>/encryption.key` (по умолчанию `/etc/x-ui/encryption.key`)
с правами `0600`.

Поведение:

- Через API и в UI `authPass` всегда виден в plaintext (это нужно для
  заполнения формы при редактировании).
- В БД хранится строка вида `enc:v1:<base64>`.
- Если в строке нет префикса `enc:v1:`, она считается legacy-plaintext
  и читается как есть — это позволяет обновить панель без миграции.
- При следующем сохранении такая строка перешифруется автоматически.
- Если потерять `encryption.key` — расшифровать существующие пароли
  будет невозможно; **бэкапьте файл вместе с БД**.

## Известные ограничения

- xcaddy-инсталлер требует **Go 1.22+** на хосте; на голом сервере без Go
  баннер покажет ссылку на go.dev/dl
- TCP-probe в статусе проверяет только локальный сокет, не HTTP-ответ
- traffic stats считаются из ядра (nftables-счётчики на порт) — **только
  Linux + root**; на других платформах / без root / без `nft` колонка
  трафика остаётся в нуле (Caddy сам байты туннелей не отдаёт)
- мульти-инстансы изолированы по `data-dir`, но **общий port:2019 для admin
  отключён** (`admin off` в конфиге) — если нужен caddy admin, придётся
  включить и развести вручную через `extraArgs`
