# NaiveProxy

Управление naive-серверами рядом с Xray. Naive не является протоколом Xray,
запускается как отдельный процесс.

## Установка

Положи бинарь `naive` в `$PATH` или укажи `NAIVE_BIN=/path/to/naive` в окружении.
Релизы: https://github.com/klzgrad/naiveproxy/releases

## Поля сервера

`remark, enable, listen, port, domain, certFile, keyFile, authUser, authPass,
padding, logLevel, extraArgs`

Минимум для запуска: port, domain, certFile, keyFile, authUser, authPass.

## API

Под `/panel/api/naive`, авторизация как у остального panel API.

```
GET    /list
GET    /get/:id
POST   /add
POST   /update/:id
POST   /delete/:id
GET    /status/:id
POST   /start/:id
POST   /stop/:id
POST   /restart/:id
```

## Где что лежит

- конфиг: `<bin>/naive/naive-<id>.json`
- лог:    `<bin>/naive/naive-<id>.log`

Серверы с `enable=true` стартуют при запуске панели.

## Клиентский URL

```
naive+https://USER:PASS@DOMAIN:PORT
```
