# ========================================================
# Stage: Frontend (Vite)
# ========================================================
FROM --platform=$BUILDPLATFORM node:22-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
COPY web/translation /src/web/translation
RUN npm run build

# ========================================================
# Stage: Caddy with forward_proxy (klzgrad fork)
# ========================================================
# This stage is what makes the image "naive-ready" — without it the panel
# can build Caddy at runtime via /panel/api/naive/install-caddy, but baking
# it in saves the first-start cost.
#
# Built with CGO_ENABLED=0 so xcaddy can cross-compile cleanly under
# BuildKit's emulation matrix (amd64/arm64/arm/v7/arm/v6/386).
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS caddy-builder
ARG TARGETARCH
ARG TARGETVARIANT
RUN apk --no-cache add git
RUN go install github.com/caddyserver/xcaddy/cmd/xcaddy@latest
WORKDIR /caddy-build
RUN set -eu; \
    GOOS=linux; \
    case "$TARGETARCH" in \
      amd64) GOARCH=amd64 ;; \
      arm64) GOARCH=arm64 ;; \
      arm)   GOARCH=arm; GOARM="${TARGETVARIANT#v}"; export GOARM ;; \
      386)   GOARCH=386 ;; \
      *)     GOARCH="$TARGETARCH" ;; \
    esac; \
    export GOOS GOARCH; \
    CGO_ENABLED=0 xcaddy build \
      --with github.com/caddyserver/forwardproxy@caddy2=github.com/klzgrad/forwardproxy@naive \
      --output /caddy

# ========================================================
# Stage: Builder
# ========================================================
FROM golang:1.26-alpine AS builder
WORKDIR /app
ARG TARGETARCH

RUN apk --no-cache --update add \
  build-base \
  gcc \
  curl \
  unzip

COPY . .
COPY --from=frontend /src/web/dist ./web/dist

ENV CGO_ENABLED=1
ENV CGO_CFLAGS="-D_LARGEFILE64_SOURCE"
RUN go build -ldflags "-w -s" -o build/x-ui main.go
RUN ./DockerInit.sh "$TARGETARCH"

# ========================================================
# Stage: Final Image of 3x-ui
# ========================================================
FROM alpine
ENV TZ=Asia/Tehran
WORKDIR /app

RUN apk add --no-cache --update \
  ca-certificates \
  tzdata \
  fail2ban \
  bash \
  curl \
  openssl

COPY --from=builder /app/build/ /app/
COPY --from=builder /app/DockerEntrypoint.sh /app/
COPY --from=builder /app/x-ui.sh /usr/bin/x-ui
COPY --from=builder /app/web/translation /app/web/translation

# pre-built Caddy with forward_proxy — panel's findCaddy() picks up <bin>/caddy
COPY --from=caddy-builder /caddy /app/bin/caddy


# Configure fail2ban
RUN rm -f /etc/fail2ban/jail.d/alpine-ssh.conf \
  && cp /etc/fail2ban/jail.conf /etc/fail2ban/jail.local \
  && sed -i "s/^\[ssh\]$/&\nenabled = false/" /etc/fail2ban/jail.local \
  && sed -i "s/^\[sshd\]$/&\nenabled = false/" /etc/fail2ban/jail.local \
  && sed -i "s/#allowipv6 = auto/allowipv6 = auto/g" /etc/fail2ban/fail2ban.conf

RUN chmod +x \
  /app/DockerEntrypoint.sh \
  /app/x-ui \
  /app/bin/caddy \
  /usr/bin/x-ui

ENV XUI_ENABLE_FAIL2BAN="true"
EXPOSE 2053
VOLUME [ "/etc/x-ui" ]
CMD [ "./x-ui" ]
ENTRYPOINT [ "/app/DockerEntrypoint.sh" ]
